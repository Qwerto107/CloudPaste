package server

import (
	"errors"
	"io"
	"net/http"
	"time"
)

const downloadWriteTimeout = 30 * time.Second

// 同一来源最多占两个下载槽，较小的全局额度也为其他来源留出位置。
// 只记录正在下载的来源，完成后删除，计数表不会随历史访客增长。
func (a *App) beginDownload(r *http.Request) (func(), error) {
	ip := a.clientIP(r)
	a.downloadMu.Lock()
	defer a.downloadMu.Unlock()
	limit := max(1, min(2, cap(a.downloads)/2))
	if a.downloadsByIP[ip] >= limit {
		return nil, &apiError{429, "DOWNLOAD_RATE_LIMITED", "当前来源同时下载过多，请稍后重试"}
	}
	select {
	case a.downloads <- struct{}{}:
	default:
		return nil, &apiError{503, "BUSY", "下载并发已满，请稍后重试"}
	}
	if a.downloadsByIP == nil {
		a.downloadsByIP = make(map[string]int)
	}
	a.downloadsByIP[ip]++
	return func() {
		a.downloadMu.Lock()
		defer a.downloadMu.Unlock()
		a.downloadsByIP[ip]--
		if a.downloadsByIP[ip] == 0 {
			delete(a.downloadsByIP, ip)
		}
		<-a.downloads
	}, nil
}

// 不暴露 ReaderFrom，确保本地文件的快速复制也经过分块写入期限检查。
type downloadResponse struct {
	http.ResponseWriter
	controller *http.ResponseController
	timeout    time.Duration
}

func newDownloadResponse(w http.ResponseWriter, timeout time.Duration) (*downloadResponse, error) {
	response := &downloadResponse{ResponseWriter: w, controller: http.NewResponseController(w), timeout: timeout}
	return response, response.refreshDeadline()
}

func (w *downloadResponse) refreshDeadline() error {
	if w.controller == nil {
		return nil
	}
	err := w.controller.SetWriteDeadline(time.Now().Add(w.timeout))
	if errors.Is(err, http.ErrNotSupported) {
		// 内存测试响应不支持连接期限；真实 net/http 连接支持此控制器。
		w.controller = nil
		return nil
	}
	return err
}

func (w *downloadResponse) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		if err := w.refreshDeadline(); err != nil {
			return written, err
		}
		// 期限随每个有界数据块刷新，持续传输的大文件不受固定总时限影响。
		chunk := data[:min(len(data), 32<<10)]
		n, err := w.ResponseWriter.Write(chunk)
		written += n
		if err != nil {
			return written, err
		}
		if n != len(chunk) {
			return written, io.ErrShortWrite
		}
		data = data[n:]
	}
	return written, nil
}

func (w *downloadResponse) finish() {
	if w.controller == nil {
		return
	}
	// 在期限内发送剩余缓冲；保留期限保护 HTTP 服务随后写入的 chunked 结束帧。
	// net/http 在整个响应结束后自行清除期限，避免提前清零留下无界收尾写入。
	_ = w.controller.Flush()
}
