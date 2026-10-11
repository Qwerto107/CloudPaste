package server

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type shortDownloadDeadline struct{ http.ResponseWriter }

func (w shortDownloadDeadline) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w shortDownloadDeadline) SetWriteDeadline(deadline time.Time) error {
	// 使用真实 HTTP 连接，只缩短生产期限，使停读测试快速结束。
	if !deadline.IsZero() {
		deadline = time.Now().Add(250 * time.Millisecond)
	}
	return http.NewResponseController(w.ResponseWriter).SetWriteDeadline(deadline)
}

type chunkedDownloadDriver struct{ Driver }

func (d chunkedDownloadDriver) Open(ctx context.Context, key string, r *http.Request) (Stream, error) {
	stream, err := d.Driver.Open(ctx, key, r)
	if err == nil {
		// 模拟没有 Content-Length 的远程存储流，不暴露本地文件的 Seek 方法。
		stream.Body = struct{ io.ReadCloser }{stream.Body}
	}
	return stream, err
}

func TestHTTPStalledDownloadsReleaseCapacity(t *testing.T) {
	for _, kind := range []string{"local", "chunked"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			file, err := os.Create(filepath.Join(root, "large.bin"))
			if err != nil {
				t.Fatal(err)
			}
			size := int64(32 << 20)
			err = file.Truncate(size)
			_ = file.Close()
			if err != nil {
				t.Fatal(err)
			}
			driver, err := newLocalDriver(Storage{RootPath: root})
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if kind == "chunked" {
				driver = chunkedDownloadDriver{driver}
			}
			app := &App{downloads: make(chan struct{}, 8), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			info := FileInfo{Name: "large.bin", MimeType: "application/octet-stream", Size: &size}
			done := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				done <- app.deliver(shortDownloadDeadline{w}, r, driver, "large.bin", info, false)
			}))
			defer server.Close()
			defer server.CloseClientConnections()
			address := strings.TrimPrefix(server.URL, "http://")
			connection, err := net.Dial("tcp", address)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if err = connection.(*net.TCPConn).SetReadBuffer(1024); err != nil {
				t.Fatal(err)
			}
			_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err = io.WriteString(connection, "GET /file HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(connection), nil)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 || (kind == "chunked" && len(response.TransferEncoding) == 0) {
				t.Fatalf("响应类型不正确: status=%d encoding=%v", response.StatusCode, response.TransferEncoding)
			}
			// 收到响应头后停止读正文；覆盖 net/http 缓冲和 chunked 收尾的真实路径。
			select {
			case err = <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("真实 HTTP 停读连接未退出")
			}
			if len(app.downloads) != 0 || len(app.downloadsByIP) != 0 {
				t.Fatal("HTTP 停读超时后仍占用下载额度")
			}
		})
	}
}
