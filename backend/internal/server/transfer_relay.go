package server

import (
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// 实时中继没有文件缓存；io.Pipe 把接收端背压一直传递到上传端。
type transferFile struct {
	id, clientID, name, state, mode   string
	imageType                         string
	size, bytes                       int64
	sender                            int
	room                              *transferRoom
	created, progress, notified       time.Time
	uploadToken, downloadToken        string
	uploading, downloading, relaySlot bool
	ctx                               context.Context
	cancel                            context.CancelFunc
	reader                            *io.PipeReader
	writer                            *io.PipeWriter
	tool                              *transferTool
	connection                        *transferConnection
}

func transferEndpointLabel(endpoint transferEndpoint) string {
	if endpoint.Port < 1 || endpoint.Port > 65535 {
		return "不可用"
	}
	address, err := netip.ParseAddr(endpoint.Address)
	if err != nil {
		return "浏览器未公开（端口 " + strconv.Itoa(endpoint.Port) + "）"
	}
	return net.JoinHostPort(address.Unmap().String(), strconv.Itoa(endpoint.Port))
}

// 文件与图片只记录必要的传输元数据，开始和结束事件使用同一套字段。
func transferAuditDetails(f *transferFile) map[string]any {
	details := map[string]any{
		"name": f.name, "state": f.state, "mode": f.mode,
		"bytes": f.bytes, "size": f.size, "room": f.room.id,
		"started_at": f.created.UTC().Format(time.RFC3339Nano),
	}
	// 两侧 IP 来自服务端连接信息；直连端口只采用经过格式校验的浏览器统计。
	if sender := f.room.members[f.sender]; sender != nil {
		details["sender_ip"] = sender.audit.IP
	}
	if receiver := f.room.members[1-f.sender]; receiver != nil {
		details["receiver_ip"] = receiver.audit.IP
	}
	if f.mode == "p2p" && f.connection != nil {
		details["p2p_local"] = transferEndpointLabel(f.connection.Local)
		details["p2p_remote"] = transferEndpointLabel(f.connection.Remote)
		details["p2p_reporter"] = "接收端浏览器"
	}
	if f.imageType != "" {
		details["image_type"] = f.imageType
	}
	return details
}

func (h *transferHub) finishLocked(f *transferFile, state string) {
	if f.state != "offered" && f.state != "active" {
		return
	}
	f.state = state
	result := "failure"
	if state == "complete" {
		result = "success"
	}
	if state == "cancelled" || state == "rejected" {
		result = "cancelled"
	}
	details := transferAuditDetails(f)
	action := "文件传输结束"
	if f.imageType != "" {
		action = "图片传输结束"
	}
	h.auditTransfer(f.room.members[f.sender], action, f.id, result, f.created, details)
	f.uploadToken = ""
	f.downloadToken = ""
	if f.cancel != nil {
		f.cancel()
	}
	if f.relaySlot {
		f.relaySlot = false
		h.relays--
	}
	h.broadcastLocked(f.room, map[string]any{"type": "state", "id": f.id, "state": state, "mode": f.mode, "bytes": f.bytes})
}

func (a *App) claimTransfer(r *http.Request, download bool) (*transferFile, error) {
	if a.cfg.TransferDisabled || !a.transferOrigin(r, false) {
		return nil, transferUnavailable()
	}
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Transfer ")
	if download {
		secret = r.URL.Query().Get("ticket")
	}
	if len(secret) != 43 {
		return nil, transferUnavailable()
	}
	h := a.transfers
	ip := a.clientIP(r)
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.allowLocked("relay:"+ip, a.cfg.RateLimits.TransferRelayPerIP) {
		return nil, transferBusy()
	}
	for _, room := range h.rooms {
		f := room.files[r.PathValue("id")]
		if f == nil || f.mode != "relay" || f.state != "active" || room.ctx.Err() != nil || f.ctx.Err() != nil {
			continue
		}
		if download {
			if f.downloading || f.downloadToken != secretHash(secret) {
				return nil, transferUnavailable()
			}
			// 第一版不支持重试或 Range，票据只允许一个实际 GET 消费者。
			if r.Method != "GET" || r.Header.Get("Range") != "" {
				return nil, badRequest("实时传输不支持分段下载，请重新发送")
			}
			f.downloading = true
			f.downloadToken = ""
		} else {
			if f.uploading || !f.downloading || f.uploadToken != secretHash(secret) {
				return nil, transferUnavailable()
			}
			if r.ContentLength >= 0 && r.ContentLength != f.size {
				return nil, badRequest("文件长度不一致")
			}
			f.uploading = true
			f.uploadToken = ""
		}
		return f, nil
	}
	return nil, transferUnavailable()
}

func (a *App) transferUpload(w http.ResponseWriter, r *http.Request) error {
	f, err := a.claimTransfer(r, false)
	if err != nil {
		return err
	}
	h := a.transfers
	ok := false
	defer func() {
		if !ok {
			h.mu.Lock()
			h.finishLocked(f, "failed")
			h.mu.Unlock()
		}
	}()
	if err = streamTransferUpload(w, r, f.ctx, f.writer, f.size, func() {
		ok = true
		a.write(w, 200, nil, "文件已发送至中继")
	}); err != nil {
		return err
	}
	return nil
}

// 普通中继和按需分段共用固定缓冲与长度校验，不在服务器聚合文件。
func streamTransferUpload(w http.ResponseWriter, r *http.Request, ctx context.Context, writer *io.PipeWriter, size int64, completed func()) error {
	r.Body = http.MaxBytesReader(w, r.Body, size+1)
	control := http.NewResponseController(w)
	// 只调整这个流式请求的读期限，保留其他 API 的全局超时。
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(interrupted)
		_ = control.SetReadDeadline(time.Now())
		_ = r.Body.Close()
	})
	stopInterrupt := func() {
		if stop == nil {
			return
		}
		// stop 不会等待已经开始的回调；退出前等待并清除期限，避免误伤复用连接的下个请求。
		if !stop() {
			<-interrupted
		}
		stop = nil
		_ = control.SetReadDeadline(time.Time{})
	}
	defer stopInterrupt()
	buffer := make([]byte, 32<<10)
	remaining := size
	var err error
	for remaining > 0 {
		if err = control.SetReadDeadline(time.Now().Add(transferIdle)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		n := int(min(remaining, int64(len(buffer))))
		if _, err = io.ReadFull(r.Body, buffer[:n]); err != nil {
			return badRequest("文件传输提前中断")
		}
		remaining -= int64(n)
		// 最后一块在检查结束标记之后才交给下载端，避免多发字节被误报为成功。
		if remaining == 0 {
			var extra [1]byte
			if _, err = io.ReadFull(r.Body, extra[:]); err != io.EOF {
				return badRequest("文件长度不一致")
			}
			// 请求体已读完，取消管道不能再打断连接的后台读取或下一个 API 请求。
			stopInterrupt()
		}
		if _, err = writer.Write(buffer[:n]); err != nil {
			return badRequest("接收已中断")
		}
	}
	if size == 0 {
		_ = control.SetReadDeadline(time.Now().Add(transferIdle))
		var extra [1]byte
		if _, err = io.ReadFull(r.Body, extra[:]); err != io.EOF {
			return badRequest("文件长度不一致")
		}
		stopInterrupt()
	}
	_ = writer.Close()
	// 完成管道可能取消传输上下文，此时请求体的中断回调已经安全停止。
	completed()
	return nil
}

// 全局按字节安排发送时刻，每路最多等待一块，不创建无界限速队列。
func (h *transferHub) pace(ctx context.Context, n int) error {
	h.mu.Lock()
	if h.bytesPerSecond == 0 {
		h.mu.Unlock()
		return ctx.Err()
	}
	now := time.Now()
	if h.nextByte.Before(now) {
		h.nextByte = now
	}
	ready := h.nextByte
	h.nextByte = h.nextByte.Add(time.Duration(int64(n) * int64(time.Second) / h.bytesPerSecond))
	h.mu.Unlock()
	timer := time.NewTimer(time.Until(ready))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (a *App) transferDownload(w http.ResponseWriter, r *http.Request) error {
	f, err := a.claimTransfer(r, true)
	if err != nil {
		return err
	}
	h := a.transfers
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()
	stopPipe := context.AfterFunc(ctx, func() { f.reader.CloseWithError(context.Canceled) })
	defer stopPipe()
	success := false
	defer func() {
		h.mu.Lock()
		if success {
			h.finishLocked(f, "complete")
		} else {
			h.finishLocked(f, "failed")
		}
		h.mu.Unlock()
	}()
	response, err := newDownloadResponse(w, 30*time.Second)
	if err != nil {
		return err
	}
	defer response.finish()
	// 未知文件一律作为附件下载，名称只能影响展示，不能成为磁盘路径或响应头。
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.name}))
	w.Header().Set("Content-Length", strconv.FormatInt(f.size, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(200)
	if err = http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	h.mu.Lock()
	secret := transferSecret()
	f.uploadToken = secretHash(secret)
	h.sendLocked(f.room.members[f.sender], map[string]any{"type": "start", "id": f.id, "mode": "relay", "token": secret})
	h.mu.Unlock()
	buffer := make([]byte, 32<<10)
	var total int64
	for {
		n, readErr := f.reader.Read(buffer)
		if n > 0 {
			if err = h.pace(ctx, n); err != nil {
				panic(http.ErrAbortHandler)
			}
			written, writeErr := response.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil || written != n {
				panic(http.ErrAbortHandler)
			}
			// 及时推出 HTTP 自身的尾部缓冲，上传暂停时接收端也能读完当前块。
			if err = http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
				panic(http.ErrAbortHandler)
			}
			h.mu.Lock()
			f.bytes = total
			f.progress = time.Now()
			if time.Since(f.notified) >= time.Second {
				h.broadcastLocked(f.room, map[string]any{"type": "progress", "id": f.id, "bytes": total})
				f.notified = time.Now()
			}
			h.mu.Unlock()
		}
		if readErr != nil {
			if readErr != io.EOF || total != f.size {
				panic(http.ErrAbortHandler)
			}
			break
		}
	}
	// 这里只能确认服务器已转发，不能证明原生下载器最终写盘成功。
	success = true
	return nil
}
