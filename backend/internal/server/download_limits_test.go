package server

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pausedDownloadResponse struct {
	*httptest.ResponseRecorder
	entered chan<- struct{}
	resume  <-chan struct{}
}

func (w *pausedDownloadResponse) Write(data []byte) (int, error) {
	w.entered <- struct{}{}
	<-w.resume
	return w.ResponseRecorder.Write(data)
}

func downloadTestApp(t *testing.T) (*App, Driver, FileInfo) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "example.txt", "download fixture")
	driver, err := newLocalDriver(Storage{RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = driver.Close() })
	size := int64(len("download fixture"))
	app := &App{downloads: make(chan struct{}, 8), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return app, driver, FileInfo{Name: "example.txt", MimeType: "text/plain", Size: &size}
}

func TestSlowDownloadsLeaveCapacityForOtherClients(t *testing.T) {
	app, driver, info := downloadTestApp(t)
	entered, resume := make(chan struct{}, 2), make(chan struct{})
	done := make(chan error, 2)
	// 两个真实写入停顿占住同一来源额度，拒绝第三个但仍服务其他来源。
	for range 2 {
		request := httptest.NewRequest("GET", "/file", nil)
		request.RemoteAddr = "192.0.2.1:1234"
		writer := &pausedDownloadResponse{httptest.NewRecorder(), entered, resume}
		go func() { done <- app.deliver(writer, request, driver, "example.txt", info, false) }()
	}
	t.Cleanup(func() {
		close(resume)
		for range 2 {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(2 * time.Second):
				t.Error("下载未能在恢复读取后退出")
			}
		}
		if len(app.downloads) != 0 || len(app.downloadsByIP) != 0 {
			t.Error("下载完成后未释放额度")
		}
	})
	for range 2 {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("下载未进入写入")
		}
	}
	request := httptest.NewRequest("GET", "/file", nil)
	request.RemoteAddr = "192.0.2.1:5678"
	request.Header.Set("X-Forwarded-For", "198.51.100.2")
	err := app.deliver(httptest.NewRecorder(), request, driver, "example.txt", info, false)
	if api, ok := err.(*apiError); !ok || api.Status != 429 {
		t.Fatalf("相同来源应被限流且不能伪造来源绕过: %v", err)
	}
	request.RemoteAddr = "192.0.2.2:1234"
	request.Header.Set("Range", "bytes=0-7")
	writer := httptest.NewRecorder()
	if err = app.deliver(writer, request, driver, "example.txt", info, false); err != nil {
		t.Fatal(err)
	}
	if writer.Code != 206 || writer.Body.String() != "download" {
		t.Fatalf("其他来源的分段下载被影响: %d %q", writer.Code, writer.Body.String())
	}
}

type pipeDownloadResponse struct {
	net.Conn
	header http.Header
	err    error
}

func (w *pipeDownloadResponse) Header() http.Header { return w.header }
func (w *pipeDownloadResponse) WriteHeader(int)     {}
func (w *pipeDownloadResponse) Write(data []byte) (int, error) {
	n, err := w.Conn.Write(data)
	w.err = err
	return n, err
}
func (w *pipeDownloadResponse) FlushError() error { return w.err }
func (w *pipeDownloadResponse) SetWriteDeadline(deadline time.Time) error {
	// 缩短测试连接的真实写入期限，避免让安全回归测试等待生产环境的三十秒。
	if !deadline.IsZero() {
		deadline = time.Now().Add(100 * time.Millisecond)
	}
	return w.Conn.SetWriteDeadline(deadline)
}

func TestStalledDownloadTimesOutAndReleasesCapacity(t *testing.T) {
	app, driver, info := downloadTestApp(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	writer := &pipeDownloadResponse{Conn: server, header: make(http.Header)}
	done := make(chan error, 1)
	go func() {
		done <- app.deliver(writer, httptest.NewRequest("GET", "/file", nil), driver, "example.txt", info, false)
	}()
	// 对端始终不读，实际网络写入必须超时，且 defer 必须归还所有下载计数。
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("停读连接未被写入期限终止")
	}
	if timeout, ok := writer.err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("期望真实连接写入超时，得到 %v", writer.err)
	}
	if len(app.downloads) != 0 || len(app.downloadsByIP) != 0 {
		t.Fatal("超时后仍占用下载额度")
	}
	if err := app.deliver(httptest.NewRecorder(), httptest.NewRequest("GET", "/file", nil), driver, "example.txt", info, false); err != nil {
		t.Fatalf("后续正常下载受影响: %v", err)
	}
}

func TestDownloadDeadlineRefreshesAndProtectsFinalFraming(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	writer := &pipeDownloadResponse{Conn: server, header: make(http.Header)}
	response, err := newDownloadResponse(writer, downloadWriteTimeout)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		buffer := make([]byte, 32<<10)
		for range 5 {
			time.Sleep(40 * time.Millisecond)
			if _, err := io.ReadFull(client, buffer); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	// 总传输时间超过单次期限，但每块持续取得进展，不能被总时限误伤。
	if _, err = response.Write([]byte(strings.Repeat("x", 5*32<<10))); err != nil {
		t.Fatal(err)
	}
	response.finish()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	// HTTP 服务在 handler 返回后还会发送分块结束帧，成功 flush 也不能提前清零期限。
	go func() {
		_, err := writer.Write([]byte("0\r\n\r\n"))
		done <- err
	}()
	select {
	case err = <-done:
		if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			t.Fatalf("最后的收尾写入没有保留期限: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("成功 flush 后的收尾写入无限等待")
	}
}
