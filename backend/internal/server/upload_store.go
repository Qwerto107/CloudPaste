package server

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const uploadTempPrefix = ".cloudpaste-upload-"

// 保留临时文件命名空间，大小写不敏感文件系统也不能绕过访问限制。
func isUploadTemporary(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), uploadTempPrefix)
}

type uploadWriter interface {
	PutNew(context.Context, string, *os.File, int64, string) error
}

func storageCapabilities(s Storage) []string {
	if s.Type != "LOCAL" && s.Type != "S3" {
		return []string{}
	}
	capabilities := []string{"reader", "proxy"}
	if s.ReadOnly != 1 {
		capabilities = append(capabilities, "writer")
	}
	return capabilities
}

func fileExists() error {
	return &apiError{409, "FILE_EXISTS", "同名文件已存在，请修改文件名"}
}

func copyUpload(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	return io.CopyBuffer(destination, &contextReader{ctx: ctx, reader: source}, make([]byte, 32<<10))
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

// 本地上传直接写入目标目录中的隐藏文件，发布时只建立硬链接，不再复制完整文件。
type localUpload struct {
	root      *os.Root
	file      *os.File
	temporary string
	name      string
}

func (d *localDriver) beginUpload(key, id string) (*localUpload, error) {
	// 固定经过安全检查的父目录，整个上传期间不按绝对路径重新打开临时文件。
	root, name, err := d.openParent(key, true)
	if err != nil {
		return nil, localError(err)
	}
	temporary := uploadTempPrefix + id
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		_ = root.Close()
		return nil, localError(err)
	}
	return &localUpload{root: root, file: file, temporary: temporary, name: name}, nil
}

func (u *localUpload) close() error {
	_ = u.file.Close()
	defer u.root.Close()
	if err := u.root.Remove(u.temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (u *localUpload) publish(ctx context.Context, current *localDriver, key string, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// 接收期间目录被移动、替换或改成链接时，不向旧目录发布文件。
	root, name, err := current.openParent(key, false)
	if err != nil {
		return err
	}
	defer root.Close()
	before, err := u.root.Stat(".")
	if err != nil {
		return err
	}
	after, err := root.Stat(".")
	if err != nil || name != u.name || !os.SameFile(before, after) {
		return forbidden()
	}
	opened, err := u.file.Stat()
	if err != nil {
		return err
	}
	staged, err := u.root.Lstat(u.temporary)
	if err != nil || !staged.Mode().IsRegular() || !os.SameFile(opened, staged) {
		return forbidden()
	}
	if opened.Size() != size {
		return badRequest("实际文件大小与声明不一致")
	}
	if err = u.file.Sync(); err != nil {
		return err
	}
	if err = u.file.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// 硬链接发布不会覆盖已有文件或跟随目的符号链接，且不会暴露半成品。
	if err = u.root.Link(u.temporary, name); errors.Is(err, os.ErrExist) {
		return fileExists()
	}
	return localErrorOrNil(err)
}
func localErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return localError(err)
}

func uploadS3Error(err error) error {
	var response *smithyhttp.ResponseError
	if errors.As(err, &response) && (response.HTTPStatusCode() == 412 || response.HTTPStatusCode() == 409) {
		return fileExists()
	}
	return s3Error(err)
}

func (d *s3Driver) PutNew(ctx context.Context, key string, file *os.File, size int64, contentType string) error {
	if size <= 64<<20 {
		_, err := d.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(d.bucket), Key: aws.String(key), Body: file, ContentLength: aws.Int64(size), ContentType: aws.String(contentType), IfNoneMatch: aws.String("*")})
		return uploadS3Error(err)
	}
	// 大文件按固定区间读取并逐片发送，不在内存中组装文件或使用并发缓冲池。
	created, err := d.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(d.bucket), Key: aws.String(key), ContentType: aws.String(contentType)})
	if err != nil {
		return s3Error(err)
	}
	completed := false
	defer func() {
		if !completed {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_, _ = d.client.AbortMultipartUpload(cleanup, &s3.AbortMultipartUploadInput{Bucket: aws.String(d.bucket), Key: aws.String(key), UploadId: created.UploadId})
		}
	}()
	parts := []types.CompletedPart{}
	const partSize int64 = 8 << 20
	for offset := int64(0); offset < size; offset += partSize {
		number := int32(len(parts) + 1)
		length := min(partSize, size-offset)
		part, err := d.client.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(d.bucket), Key: aws.String(key), UploadId: created.UploadId, PartNumber: aws.Int32(number), Body: io.NewSectionReader(file, offset, length), ContentLength: aws.Int64(length)})
		if err != nil {
			return s3Error(err)
		}
		parts = append(parts, types.CompletedPart{PartNumber: aws.Int32(number), ETag: part.ETag})
	}
	_, err = d.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String(d.bucket), Key: aws.String(key), UploadId: created.UploadId, MultipartUpload: &types.CompletedMultipartUpload{Parts: parts}, IfNoneMatch: aws.String("*")})
	if err != nil {
		return uploadS3Error(err)
	}
	completed = true
	return nil
}
