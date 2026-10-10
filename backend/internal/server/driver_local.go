package server

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

type localDriver struct{ rootPath, scope string }

// 将读取根进一步限制到挂载根或 API Key 的 basic_path，防止根内链接跨越授权目录。
func (d *localDriver) openRoot() (*os.Root, error) {
	return d.openLocalRoot(false)
}

func newLocalDriver(s Storage) (Driver, error) {
	root, err := os.OpenRoot(s.RootPath)
	if err != nil {
		return nil, err
	}
	_ = root.Close()
	return &localDriver{rootPath: s.RootPath}, nil
}
func (d *localDriver) Close() error { return nil }
func localError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return notFound()
	}
	if errors.Is(err, fs.ErrPermission) || strings.Contains(err.Error(), "escapes from") {
		return forbidden()
	}
	return err
}
func localKey(key string) string {
	if key == "" {
		return "."
	}
	return key
}

func fileInfo(name string, stat fs.FileInfo) FileInfo {
	size, modified := stat.Size(), stat.ModTime()
	info := FileInfo{Name: name, Directory: stat.IsDir(), Size: &size, Modified: &modified}
	classify(&info)
	return info
}

func (d *localDriver) Stat(ctx context.Context, key string) (FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return FileInfo{}, err
	}
	// 目录保护按真实路径生效，根内相对链接也必须拒绝。
	root, name, err := d.openParent(key, false)
	if err != nil {
		return FileInfo{}, localError(err)
	}
	defer root.Close()
	stat, err := root.Lstat(name)
	if err != nil {
		return FileInfo{}, localError(err)
	}
	if stat.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return FileInfo{}, forbidden()
	}
	if !stat.IsDir() && !stat.Mode().IsRegular() {
		return FileInfo{}, badRequest("只支持普通文件和目录")
	}
	return fileInfo(path.Base(key), stat), nil
}

func (d *localDriver) List(ctx context.Context, key, cursor string, limit int) (Listing, error) {
	result := Listing{Items: []FileInfo{}}
	root, err := d.openRoot()
	if err != nil {
		return result, localError(err)
	}
	root, err = localDirectory(root, localKey(key), false)
	if err != nil {
		return result, err
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return result, localError(err)
	}
	defer f.Close()
	skip := 0
	if cursor != "" {
		skip, err = strconv.Atoi(cursor)
		if err != nil || skip < 0 || skip > 1000000 {
			return result, badRequest("目录游标无效")
		}
	}
	for remaining := skip; remaining > 0; {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		entries, readErr := f.ReadDir(min(remaining, 200))
		remaining -= len(entries)
		if errors.Is(readErr, io.EOF) {
			return result, nil
		}
		if readErr != nil {
			return result, localError(readErr)
		}
	}
	entries, readErr := f.ReadDir(limit + 1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return result, localError(readErr)
	}
	result.HasMore = len(entries) > limit
	if result.HasMore {
		entries = entries[:limit]
		result.NextCursor = strconv.Itoa(skip + limit)
	}
	for _, entry := range entries {
		if isUploadTemporary(entry.Name()) {
			continue
		}
		if err = ctx.Err(); err != nil {
			return result, err
		}
		stat, err := root.Lstat(entry.Name())
		if err != nil || stat.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || (!stat.IsDir() && !stat.Mode().IsRegular()) {
			continue
		}
		result.Items = append(result.Items, fileInfo(entry.Name(), stat))
	}
	return result, nil
}

func (d *localDriver) Open(ctx context.Context, key string, r *http.Request) (Stream, error) {
	root, name, err := d.openParent(key, false)
	if err != nil {
		return Stream{}, localError(err)
	}
	defer root.Close()
	// 在打开前排除管道和设备，避免读取普通文件以外的对象时阻塞。
	before, err := root.Lstat(name)
	if err != nil {
		return Stream{}, localError(err)
	}
	if before.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return Stream{}, forbidden()
	}
	if !before.Mode().IsRegular() {
		return Stream{}, badRequest("只能读取普通文件")
	}
	// 提前固定 Windows 的文件身份，不能在打开后才按路径获取旧对象的身份。
	if !os.SameFile(before, before) {
		return Stream{}, forbidden()
	}
	f, err := root.Open(name)
	if err != nil {
		return Stream{}, localError(err)
	}
	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return Stream{}, err
	}
	if !stat.Mode().IsRegular() {
		_ = f.Close()
		return Stream{}, badRequest("只能读取普通文件")
	}
	// 文件在检查后被换成链接或其他对象时，不读取也不返回任何内容。
	after, err := root.Lstat(name)
	if err != nil || after.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !os.SameFile(before, stat) || !os.SameFile(stat, after) {
		_ = f.Close()
		return Stream{}, forbidden()
	}
	return Stream{Body: f, Status: 200, Header: make(http.Header)}, nil
}
