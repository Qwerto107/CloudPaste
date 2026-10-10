package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// 存储地址只接受管理员配置，不提供任意 URL 代理；重定向不携带存储凭证。
func storageHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, IdleConnTimeout: 60 * time.Second, MaxIdleConns: 16, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 16, ForceAttemptHTTP2: true}, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
}

type s3Driver struct {
	client     *s3.Client
	httpClient *http.Client
	bucket     string
}

func newS3Driver(s Storage) (Driver, error) {
	client := storageHTTPClient()
	region := s.Region
	if region == "" {
		region = "us-east-1"
	}
	options := s3.Options{Region: region, Credentials: credentials.NewStaticCredentialsProvider(s.AccessKey, s.SecretKey, ""), UsePathStyle: s.PathStyle == 1, HTTPClient: client}
	if s.Endpoint != "" {
		options.BaseEndpoint = aws.String(s.Endpoint)
	}
	return &s3Driver{client: s3.New(options), httpClient: client, bucket: s.Bucket}, nil
}
func (d *s3Driver) Close() error { d.httpClient.CloseIdleConnections(); return nil }
func s3Error(err error) error {
	var response *smithyhttp.ResponseError
	if errors.As(err, &response) {
		switch response.HTTPStatusCode() {
		case 404:
			return notFound()
		case 403:
			return forbidden()
		case 416:
			return &apiError{416, "RANGE_NOT_SATISFIABLE", "请求范围超出文件大小"}
		}
	}
	var api smithy.APIError
	if errors.As(err, &api) && (api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "NotFound") {
		return notFound()
	}
	return err
}
func (d *s3Driver) List(ctx context.Context, key, cursor string, limit int) (Listing, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	prefix := strings.Trim(key, "/")
	if prefix != "" {
		prefix += "/"
	}
	input := &s3.ListObjectsV2Input{Bucket: aws.String(d.bucket), Prefix: aws.String(prefix), Delimiter: aws.String("/"), MaxKeys: aws.Int32(int32(limit))}
	if cursor != "" {
		input.ContinuationToken = aws.String(cursor)
	}
	out, err := d.client.ListObjectsV2(ctx, input)
	if err != nil {
		return Listing{}, s3Error(err)
	}
	result := Listing{Items: []FileInfo{}, HasMore: aws.ToBool(out.IsTruncated), NextCursor: aws.ToString(out.NextContinuationToken)}
	for _, folder := range out.CommonPrefixes {
		name := strings.TrimSuffix(strings.TrimPrefix(aws.ToString(folder.Prefix), prefix), "/")
		if name == "" || strings.Contains(name, "/") {
			continue
		}
		info := FileInfo{Name: name, Directory: true}
		classify(&info)
		result.Items = append(result.Items, info)
	}
	for _, object := range out.Contents {
		name := strings.TrimPrefix(aws.ToString(object.Key), prefix)
		if name == "" || strings.Contains(name, "/") {
			continue
		}
		info := FileInfo{Name: name, Size: object.Size, Modified: object.LastModified}
		classify(&info)
		result.Items = append(result.Items, info)
	}
	return result, nil
}
func (d *s3Driver) Stat(ctx context.Context, key string) (FileInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if key == "" {
		info := FileInfo{Name: "", Directory: true}
		classify(&info)
		return info, nil
	}
	out, err := d.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(d.bucket), Key: aws.String(key)})
	if err != nil {
		if response := s3Error(err); response != nil {
			var api *apiError
			if errors.As(response, &api) && api.Status == 404 {
				listing, listErr := d.List(ctx, key, "", 1)
				if listErr == nil && len(listing.Items) > 0 {
					info := FileInfo{Name: key[strings.LastIndex(key, "/")+1:], Directory: true}
					classify(&info)
					return info, nil
				}
			}
		}
		return FileInfo{}, s3Error(err)
	}
	info := FileInfo{Name: key[strings.LastIndex(key, "/")+1:], Size: out.ContentLength, Modified: out.LastModified, MimeType: aws.ToString(out.ContentType)}
	classify(&info)
	return info, nil
}
func (d *s3Driver) Open(ctx context.Context, key string, r *http.Request) (Stream, error) {
	input := &s3.GetObjectInput{Bucket: aws.String(d.bucket), Key: aws.String(key)}
	if value := r.Header.Get("Range"); value != "" {
		if strings.Contains(value, ",") {
			return Stream{}, badRequest("远程存储仅支持单段 Range")
		}
		input.Range = aws.String(value)
		if condition := r.Header.Get("If-Range"); condition != "" {
			// 条件不匹配时返回完整内容，避免客户端把不同版本的分段拼接在一起。
			head, err := d.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(d.bucket), Key: aws.String(key)})
			if err != nil {
				return Stream{}, s3Error(err)
			}
			matches := condition == aws.ToString(head.ETag) && !strings.HasPrefix(condition, "W/")
			if date, err := http.ParseTime(condition); err == nil && head.LastModified != nil {
				matches = date.Unix() == head.LastModified.Unix()
			}
			if !matches {
				input.Range = nil
			}
		}
	}
	if value := r.Header.Get("If-None-Match"); value != "" {
		input.IfNoneMatch = aws.String(value)
	}
	if value := r.Header.Get("If-Match"); value != "" {
		input.IfMatch = aws.String(value)
	}
	out, err := d.client.GetObject(ctx, input)
	if err != nil {
		var response *smithyhttp.ResponseError
		if errors.As(err, &response) && response.HTTPStatusCode() == 304 {
			return Stream{Status: 304, Header: make(http.Header)}, nil
		}
		return Stream{}, s3Error(err)
	}
	header := make(http.Header)
	header.Set("Accept-Ranges", "bytes")
	if out.ContentLength != nil {
		header.Set("Content-Length", strconv.FormatInt(*out.ContentLength, 10))
	}
	if out.ETag != nil {
		header.Set("ETag", *out.ETag)
	}
	if out.LastModified != nil {
		header.Set("Last-Modified", out.LastModified.UTC().Format(http.TimeFormat))
	}
	status := 200
	if out.ContentRange != nil {
		header.Set("Content-Range", *out.ContentRange)
		status = 206
	}
	return Stream{Body: out.Body, Status: status, Header: header}, nil
}
