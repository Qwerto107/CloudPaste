package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 下载工具按需索取原文件区间；这里只保存有界的请求状态和区间，不保存文件内容。
type transferTool struct {
	expires   time.Time
	requests  map[string]*transferRangeRequest
	attempts  int
	round     int
	sent      int64
	completed []transferByteRange
}

type transferByteRange struct{ start, end int64 } // end 为不包含的结束位置。

type transferRangeRequest struct {
	id, token string
	transferByteRange
	forwarded int64
	round     int
	uploading bool
	ctx       context.Context
	cancel    context.CancelFunc
	reader    *io.PipeReader
	writer    *io.PipeWriter
}

// 仅支持单个区间；下载工具的并发分段通过多个独立请求实现，不返回 multipart 响应。
func transferRequestedRange(header string, size int64) (transferByteRange, bool, error) {
	full := transferByteRange{0, size}
	if header == "" {
		return full, false, nil
	}
	if !strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") || size == 0 {
		return full, false, errors.New("invalid range")
	}
	parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(header, "bytes=")), "-")
	if len(parts) != 2 {
		return full, false, errors.New("invalid range")
	}
	if parts[0] == "" {
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 {
			return full, false, errors.New("invalid suffix")
		}
		return transferByteRange{size - min(n, size), size}, true, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return full, false, errors.New("invalid start")
	}
	end := size
	if parts[1] != "" {
		last, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || last < start {
			return full, false, errors.New("invalid end")
		}
		// 先截断再加一，避免极大的结束位置溢出。
		end = min(last, size-1) + 1
	}
	return transferByteRange{start, end}, true, nil
}

// 区间合并后用于展示已转发的唯一字节数；重试同一分段不会让进度超过文件大小。
func mergeTransferRange(ranges []transferByteRange, added transferByteRange) []transferByteRange {
	if added.start == added.end {
		return ranges
	}
	out := make([]transferByteRange, 0, len(ranges)+1)
	for _, current := range ranges {
		if current.end < added.start {
			out = append(out, current)
		} else if added.end < current.start {
			out = append(out, added)
			added = current
		} else {
			added.start = min(added.start, current.start)
			added.end = max(added.end, current.end)
		}
	}
	return append(out, added)
}

// 已完成后再次下载启动新一轮；只清理显示进度，不重置累计流量与请求额度。
func (h *transferHub) startToolRoundLocked(f *transferFile) {
	tool := f.tool
	if tool.round > 0 {
		if f.bytes != f.size {
			return
		}
		for _, request := range tool.requests {
			if request.round == tool.round && request.forwarded != request.end-request.start {
				return
			}
		}
	}
	tool.round++
	tool.completed = nil
	f.bytes = 0
	h.toolProgressLocked(f, true)
}

func (h *transferHub) toolProgressLocked(f *transferFile, force bool) {
	ranges := append([]transferByteRange(nil), f.tool.completed...)
	for _, request := range f.tool.requests {
		// 旧一轮已写完但尚未退出的 HTTP 请求，不参与新一轮区间统计。
		if request.round != f.tool.round {
			continue
		}
		ranges = mergeTransferRange(ranges, transferByteRange{request.start, request.start + request.forwarded})
	}
	var total int64
	for _, span := range ranges {
		total += span.end - span.start
	}
	f.bytes = total
	f.progress = time.Now()
	if f.state == "active" && (force || time.Since(f.notified) >= time.Second) {
		h.broadcastLocked(f.room, map[string]any{"type": "progress", "id": f.id, "bytes": total, "round": f.tool.round})
		f.notified = time.Now()
	}
}

func (a *App) toolFileLocked(r *http.Request) (*transferFile, error) {
	if a.cfg.TransferDisabled || !a.transferOrigin(r, false) {
		return nil, transferUnavailable()
	}
	secret := r.URL.Query().Get("ticket")
	if len(secret) != 43 {
		return nil, transferUnavailable()
	}
	for _, room := range a.transfers.rooms {
		f := room.files[r.PathValue("id")]
		if f != nil && f.mode == "tool" && f.state == "active" && f.ctx.Err() == nil && room.ctx.Err() == nil && time.Now().Before(f.tool.expires) && f.downloadToken == secretHash(secret) {
			return f, nil
		}
	}
	return nil, transferUnavailable()
}

func (a *App) transferToolDownload(w http.ResponseWriter, r *http.Request) error {
	h := a.transfers
	h.mu.Lock()
	f, err := a.toolFileLocked(r)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	if !h.allowLocked("tool:"+a.clientIP(r), a.cfg.RateLimits.TransferToolPerIP) {
		h.mu.Unlock()
		return transferBusy()
	}
	etag := `"` + f.id + `"`
	modified := f.created.UTC().Truncate(time.Second).Format(http.TimeFormat)
	header := r.Header.Get("Range")
	// HEAD 只探测元数据，绝不触发发送端上传；If-Range 不匹配时返回整个文件。
	if r.Method == "HEAD" || (r.Header.Get("If-Range") != "" && r.Header.Get("If-Range") != etag && r.Header.Get("If-Range") != modified) {
		header = ""
	}
	span, partial, rangeErr := transferRequestedRange(header, f.size)
	if rangeErr != nil {
		h.mu.Unlock()
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", f.size))
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}
	var request *transferRangeRequest
	if r.Method != "HEAD" {
		// 并发占用现有中继额度；限制总请求数和实际重传流量，探测 GET 中断不消耗整文件额度。
		if len(f.tool.requests) >= 4 || h.relays >= h.maxRelays || f.tool.attempts >= 256 || f.tool.sent >= max(1<<20, f.size*4) {
			h.mu.Unlock()
			w.Header().Set("Retry-After", "2")
			return transferBusy()
		}
		h.startToolRoundLocked(f)
		ctx, cancel := context.WithCancel(f.ctx)
		reader, writer := io.Pipe()
		secret := transferSecret()
		request = &transferRangeRequest{id: randomID(), token: secretHash(secret), transferByteRange: span, round: f.tool.round, ctx: ctx, cancel: cancel, reader: reader, writer: writer}
		context.AfterFunc(ctx, func() { reader.CloseWithError(context.Canceled); writer.CloseWithError(context.Canceled) })
		f.tool.requests[request.id] = request
		f.tool.attempts++
		h.relays++
		defer func() {
			request.cancel()
			h.mu.Lock()
			delete(f.tool.requests, request.id)
			h.relays--
			if request.round == f.tool.round {
				f.tool.completed = mergeTransferRange(f.tool.completed, transferByteRange{request.start, request.start + request.forwarded})
			}
			if f.state == "active" {
				h.toolProgressLocked(f, true)
			}
			h.sendLocked(f.room.members[f.sender], map[string]any{"type": "range-stop", "id": f.id, "request_id": request.id})
			h.mu.Unlock()
		}()
		h.sendLocked(f.room.members[f.sender], map[string]any{"type": "range-start", "id": f.id, "request_id": request.id, "token": secret, "start": span.start, "end": span.end})
	}
	f.progress = time.Now()
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.name}))
	w.Header().Set("Content-Length", strconv.FormatInt(span.end-span.start, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", modified)
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", span.start, span.end-1, f.size))
	}
	if r.Method == "HEAD" {
		w.WriteHeader(200)
		return nil
	}
	stop := context.AfterFunc(r.Context(), request.cancel)
	defer stop()
	idle := time.AfterFunc(transferIdle, request.cancel)
	defer idle.Stop()
	response, err := newDownloadResponse(w, 30*time.Second)
	if err != nil {
		return err
	}
	defer response.finish()
	code := http.StatusOK
	if partial {
		code = http.StatusPartialContent
	}
	w.WriteHeader(code)
	controller := http.NewResponseController(w)
	if err = controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		panic(http.ErrAbortHandler)
	}
	buffer := make([]byte, 32<<10)
	var total int64
	for {
		n, readErr := request.reader.Read(buffer)
		if n > 0 {
			h.mu.Lock()
			allowed := f.tool.sent+int64(n) <= max(1<<20, f.size*4)
			if allowed {
				f.tool.sent += int64(n)
			}
			h.mu.Unlock()
			if !allowed || h.pace(request.ctx, n) != nil {
				panic(http.ErrAbortHandler)
			}
			written, writeErr := response.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil || written != n {
				panic(http.ErrAbortHandler)
			}
			if err = controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
				panic(http.ErrAbortHandler)
			}
			idle.Reset(transferIdle)
			h.mu.Lock()
			request.forwarded = total
			h.toolProgressLocked(f, false)
			h.mu.Unlock()
		}
		if readErr != nil {
			if readErr != io.EOF || total != span.end-span.start {
				panic(http.ErrAbortHandler)
			}
			return nil
		}
	}
}

func (a *App) transferToolUpload(w http.ResponseWriter, r *http.Request) error {
	if a.cfg.TransferDisabled || !a.transferOrigin(r, false) {
		return transferUnavailable()
	}
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Transfer ")
	if len(secret) != 43 {
		return transferUnavailable()
	}
	h := a.transfers
	h.mu.Lock()
	var request *transferRangeRequest
	for _, room := range h.rooms {
		if f := room.files[r.PathValue("id")]; f != nil && f.state == "active" && f.tool != nil {
			request = f.tool.requests[r.PathValue("request")]
			break
		}
	}
	if request == nil || request.ctx.Err() != nil || request.uploading || request.token != secretHash(secret) {
		h.mu.Unlock()
		return transferUnavailable()
	}
	size := request.end - request.start
	if r.ContentLength >= 0 && r.ContentLength != size {
		h.mu.Unlock()
		return badRequest("分段长度不一致")
	}
	request.uploading = true
	request.token = ""
	h.mu.Unlock()
	// 每段独立鉴权并继承请求取消，断开的下载不会留下上传或管道等待。
	err := streamTransferUpload(w, r, request.ctx, request.writer, size, func() { a.write(w, 200, nil, "分段已发送至中继") })
	if err != nil {
		request.writer.CloseWithError(err)
		return err
	}
	return nil
}
