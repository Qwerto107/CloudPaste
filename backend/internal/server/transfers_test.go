package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type transferTestPeer struct {
	conn *websocket.Conn
	t    *testing.T
}

func (p transferTestPeer) send(value any) {
	p.t.Helper()
	raw, _ := json.Marshal(value)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.conn.Write(ctx, websocket.MessageText, raw); err != nil {
		p.t.Fatal(err)
	}
}
func (p transferTestPeer) next(kind string) map[string]any {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, raw, err := p.conn.Read(ctx)
		if err != nil {
			p.t.Fatalf("waiting %s: %v", kind, err)
		}
		var value map[string]any
		if err = json.Unmarshal(raw, &value); err != nil {
			p.t.Fatal(err)
		}
		if value["type"] == kind {
			return value
		}
		if value["type"] == "error" {
			p.t.Fatalf("waiting %s: %v", kind, value)
		}
	}
}

func transferPair(t *testing.T) (*fixture, *httptest.Server, transferTestPeer, transferTestPeer) {
	t.Helper()
	f := newFixture(t)
	s := httptest.NewServer(f.handler)
	t.Cleanup(s.Close)
	dial := func(token string) transferTestPeer {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/api/transfer/socket", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{s.URL}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		p := transferTestPeer{conn, t}
		p.send(map[string]any{"type": "auth", "token": token})
		p.next("ready")
		return p
	}
	created := dataMap(t, f.request(t, "POST", "/api/transfer/create", f.admin, map[string]any{"name": "设备 A", "relay_only": true}, nil, 201))
	a := dial(created["token"].(string))
	joined := dataMap(t, f.request(t, "POST", "/api/transfer/join", "", map[string]any{"code": created["code"], "name": "设备 B"}, nil, 200))
	b := dial(joined["token"].(string))
	a.next("joining")
	// 配对前不能发消息，第三人也不能占据已经预留的位置。
	b.send(map[string]any{"type": "text", "text": "before-confirm"})
	b.next("error")
	f.request(t, "POST", "/api/transfer/join", "", map[string]any{"code": created["code"], "name": "设备 C"}, nil, 404)
	a.send(map[string]any{"type": "confirm"})
	a.next("paired")
	b.next("paired")
	f.request(t, "POST", "/api/transfer/join", "", map[string]any{"code": created["code"], "name": "设备 C"}, nil, 404)
	return f, s, a, b
}

func offerRelay(t *testing.T, sender, receiver transferTestPeer, size int64) (string, string) {
	t.Helper()
	sender.send(map[string]any{"type": "offer", "client_id": randomID(), "name": "测试文件.bin", "size": size})
	file := sender.next("file")
	other := receiver.next("file")
	// 文件通知使用服务端时间，两个设备收到相同的值。
	if file["sent_at"] != other["sent_at"] {
		t.Fatal("file timestamp mismatch")
	}
	if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(file["sent_at"])); err != nil {
		t.Fatalf("invalid file timestamp: %v", err)
	}
	id := file["id"].(string)
	receiver.send(map[string]any{"type": "accept", "id": id, "mode": "relay"})
	download := receiver.next("download")
	receiver.next("state")
	sender.next("state")
	return id, download["url"].(string)
}

func TestTransferClientFailureReleasesRelay(t *testing.T) {
	f, s, a, b := transferPair(t)
	id, download := offerRelay(t, a, b, 10)
	a.send(map[string]any{"type": "fail", "id": id})
	if a.next("state")["state"] != "failed" || b.next("state")["state"] != "failed" {
		t.Fatal("client failure was not reported to both peers")
	}
	f.app.transfers.mu.Lock()
	remaining := f.app.transfers.relays
	f.app.transfers.mu.Unlock()
	if remaining != 0 {
		t.Fatal("failed transfer retained relay slot")
	}
	response, err := http.Get(s.URL + download)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("old ticket still usable: %d", response.StatusCode)
	}
	// 旧任务的取消或迟到失败不得影响重发产生的新任务。
	newID, _ := offerRelay(t, a, b, 10)
	if newID == id {
		t.Fatal("retry reused the previous task")
	}
	a.send(map[string]any{"type": "fail", "id": id})
	a.send(map[string]any{"type": "cancel", "id": newID})
	if a.next("state")["state"] != "cancelled" {
		t.Fatal("new transfer was changed by an old failure")
	}
}

func TestTransferChatImageLimits(t *testing.T) {
	_, _, a, b := transferPair(t)
	for _, invalid := range []map[string]any{
		{"image_type": "image/svg+xml", "size": 100},
		{"image_type": "image/png", "size": (4 << 20) + 1},
		{"image_type": "image/jpeg", "size": 0},
	} {
		invalid["type"], invalid["name"], invalid["client_id"] = "offer", "image.png", randomID()
		a.send(invalid)
		a.next("error")
	}
	a.send(map[string]any{"type": "offer", "name": "image.png", "client_id": randomID(), "image_type": "image/png", "size": 100})
	if a.next("file")["image_type"] != "image/png" || b.next("file")["image_type"] != "image/png" {
		t.Fatal("image metadata was not delivered")
	}
}

// 模拟取消回调尚未结束时处理器已经发送响应，验证不会把回调留给下一个复用请求。
type transferDeadlineRecorder struct {
	*httptest.ResponseRecorder
	interrupted, release chan struct{}
	restored             bool
}

func (w *transferDeadlineRecorder) SetReadDeadline(deadline time.Time) error {
	if deadline.IsZero() {
		w.restored = true
	} else if !deadline.After(time.Now()) {
		close(w.interrupted)
		<-w.release
	}
	return nil
}

type transferInterruptedBody struct {
	cancel      context.CancelFunc
	interrupted <-chan struct{}
}

func (b transferInterruptedBody) Read([]byte) (int, error) {
	b.cancel()
	<-b.interrupted
	return 0, io.ErrUnexpectedEOF
}
func (transferInterruptedBody) Close() error { return nil }

func TestTransferUploadWaitsForCancellationCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &transferDeadlineRecorder{ResponseRecorder: httptest.NewRecorder(), interrupted: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(w.release) }) }
	defer release()
	r := httptest.NewRequest("PUT", "/upload", nil)
	r.Body = transferInterruptedBody{cancel: cancel, interrupted: w.interrupted}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { done <- streamTransferUpload(w, r, ctx, writer, 1, func() {}) }()
	select {
	case <-w.interrupted:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation callback did not start")
	}
	select {
	case <-done:
		t.Fatal("handler returned while cancellation still used its connection")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("interrupted body was accepted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not finish cleanup")
	}
	if !w.restored {
		t.Fatal("read deadline was not restored")
	}
}

// 正常读完请求体后，结束传输不能再触发连接读超时，空文件也要遵守此规则。
func TestTransferUploadStopsReadInterruptBeforeCompletion(t *testing.T) {
	for _, payload := range [][]byte{nil, {42}} {
		ctx, cancel := context.WithCancel(context.Background())
		w := &transferDeadlineRecorder{ResponseRecorder: httptest.NewRecorder(), interrupted: make(chan struct{}), release: make(chan struct{})}
		close(w.release)
		r := httptest.NewRequest("PUT", "/upload", bytes.NewReader(payload))
		reader, writer := io.Pipe()
		drained := make(chan struct{})
		go func() { _, _ = io.Copy(io.Discard, reader); close(drained) }()
		err := streamTransferUpload(w, r, ctx, writer, int64(len(payload)), cancel)
		reader.Close()
		writer.Close()
		<-drained
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-w.interrupted:
			t.Fatal("completed upload interrupted a reusable connection")
		default:
		}
		if !w.restored {
			t.Fatal("completed upload retained its read deadline")
		}
	}
}

func transferPut(t *testing.T, base, id, token string, body io.Reader, length int64) *http.Response {
	t.Helper()
	r, err := http.NewRequest("PUT", base+"/api/transfer/upload/"+id, body)
	if err != nil {
		t.Fatal(err)
	}
	r.ContentLength = length
	r.Header.Set("Authorization", "Transfer "+token)
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestTransferPairAndBidirectionalRelay(t *testing.T) {
	f, s, a, b := transferPair(t)
	for _, pair := range [][2]transferTestPeer{{a, b}, {b, a}} {
		sender, receiver := pair[0], pair[1]
		sender.send(map[string]any{"type": "text", "text": "<script>仅作为文字</script>", "sent_at": "2000-01-01T00:00:00Z"})
		message, other := sender.next("text"), receiver.next("text")
		if message["text"] != other["text"] {
			t.Fatal("text mismatch")
		}
		// 不信任发送方附带的时间，避免聊天时间被任意伪造。
		if message["sent_at"] != other["sent_at"] || message["sent_at"] == "2000-01-01T00:00:00Z" {
			t.Fatal("invalid shared text timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(message["sent_at"])); err != nil {
			t.Fatalf("invalid text timestamp: %v", err)
		}
		payload := bytes.Repeat([]byte("cloudpaste-transfer\x00"), 65536)
		id, download := offerRelay(t, sender, receiver, int64(len(payload)))
		response, err := http.Get(s.URL + download)
		if err != nil {
			t.Fatal(err)
		}
		if response.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(response.Header.Get("Content-Disposition"), "attachment;") {
			t.Fatal("unsafe download headers")
		}
		start := sender.next("start")
		// 一次票据不能重放；下载票据也不能用于上传。
		replay, err := http.Get(s.URL + download)
		if err != nil {
			t.Fatal(err)
		}
		replay.Body.Close()
		if replay.StatusCode != 404 {
			t.Fatalf("replay: %d", replay.StatusCode)
		}
		bad := transferPut(t, s.URL, id, strings.Split(download, "ticket=")[1], bytes.NewReader(payload), int64(len(payload)))
		bad.Body.Close()
		if bad.StatusCode != 404 {
			t.Fatal("download token used for upload")
		}
		done := make(chan []byte, 1)
		go func() { data, _ := io.ReadAll(response.Body); response.Body.Close(); done <- data }()
		up := transferPut(t, s.URL, id, start["token"].(string), bytes.NewReader(payload), int64(len(payload)))
		up.Body.Close()
		if up.StatusCode != 200 {
			t.Fatalf("upload status %d", up.StatusCode)
		}
		if !bytes.Equal(<-done, payload) {
			t.Fatal("relay changed file bytes")
		}
		for {
			state := receiver.next("state")
			if state["state"] == "complete" {
				break
			}
		}
	}
	// WS 不持有普通 API 并发槽。
	if len(f.app.requests) != 0 {
		t.Fatal("transfer consumed ordinary API slots")
	}
	f.request(t, "GET", "/api/health", "", nil, nil, 200)
}

func TestTransferOriginAndCreationPermissions(t *testing.T) {
	f := newFixture(t)
	f.request(t, "POST", "/api/transfer/create", "", map[string]any{"name": "guest"}, nil, 401)
	key, _ := f.key(t, "text-only", "/", TextShare)
	f.request(t, "POST", "/api/transfer/create", key, map[string]any{"name": "guest"}, nil, 403)
	f.request(t, "POST", "/api/transfer/create", f.admin, map[string]any{"name": "admin"}, map[string]string{"Origin": "https://evil.example"}, 403)
	s := httptest.NewServer(f.handler)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/api/transfer/socket", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}}})
	if err == nil || response == nil || response.StatusCode != 403 {
		t.Fatal("foreign origin accepted")
	}
}

func TestTransferCancellationUnblocksBothStreams(t *testing.T) {
	f, s, a, b := transferPair(t)
	id, download := offerRelay(t, a, b, 1<<30)
	response, err := http.Get(s.URL + download)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	start := a.next("start")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	req, _ := http.NewRequestWithContext(ctx, "PUT", s.URL+"/api/transfer/upload/"+id, reader)
	req.ContentLength = 1 << 30
	req.Header.Set("Authorization", "Transfer "+start["token"].(string))
	done := make(chan struct{})
	go func() {
		resp, _ := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		close(done)
	}()
	b.send(map[string]any{"type": "cancel", "id": id})
	if b.next("state")["state"] != "cancelled" {
		t.Fatal("cancel not delivered")
	}
	// 发送端未发送任何字节，也必须能被取消；不要等到完整请求体结束。
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("upload remained blocked after cancel")
	}
	f.app.transfers.mu.Lock()
	slots := f.app.transfers.relays
	f.app.transfers.mu.Unlock()
	if slots != 0 {
		t.Fatal("relay slot leaked")
	}
}

func TestTransferRejectsSpoofedSizeAndCrossFileToken(t *testing.T) {
	_, s, a, b := transferPair(t)
	id, download := offerRelay(t, a, b, 64)
	response, err := http.Get(s.URL + download)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	start := a.next("start")
	bad := transferPut(t, s.URL, randomID(), start["token"].(string), strings.NewReader("x"), 1)
	bad.Body.Close()
	if bad.StatusCode != 404 {
		t.Fatal("cross-file token accepted")
	}
	// chunked 请求声明不足的实际字节数，必须截断下载并报告失败。
	bad = transferPut(t, s.URL, id, start["token"].(string), io.NopCloser(strings.NewReader("short")), -1)
	bad.Body.Close()
	if bad.StatusCode != 400 {
		t.Fatalf("short upload status %d", bad.StatusCode)
	}
	if _, err = io.ReadAll(response.Body); err == nil {
		t.Fatal("truncated download appeared complete")
	}
}

func TestTransferLimitsStayBounded(t *testing.T) {
	f := newFixture(t)
	h := f.app.transfers
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := 0; i < 1024; i++ {
		h.allowLocked(fmt.Sprintf("test:%d", i), 1)
	}
	if h.allowLocked("overflow", 1) || len(h.limits) > 1024 {
		t.Fatal("unbounded rate-limit map")
	}
}

func TestTransferAtomicJoin(t *testing.T) {
	f := newFixture(t)
	created := dataMap(t, f.request(t, "POST", "/api/transfer/create", f.admin, map[string]any{"name": "owner"}, nil, 201))
	// 将创建者标记为在线，集中验证同一个真实配对码的原子占位。
	f.app.transfers.mu.Lock()
	for _, room := range f.app.transfers.rooms {
		room.members[0].connected = true
	}
	f.app.transfers.mu.Unlock()
	var accepted atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			raw, _ := json.Marshal(map[string]any{"code": created["code"], "name": "guest"})
			req := httptest.NewRequest("POST", "/api/transfer/join", bytes.NewReader(raw))
			req.RemoteAddr = "192.0.2.1:80"
			out := httptest.NewRecorder()
			f.handler.ServeHTTP(out, req)
			if out.Code == 200 {
				accepted.Add(1)
			} else if out.Code != 404 {
				t.Errorf("join status %d", out.Code)
			}
		}()
	}
	group.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("concurrent join accepted %d guests", accepted.Load())
	}
}

func TestTransferReceivesBeforeUploadFinishes(t *testing.T) {
	_, s, a, b := transferPair(t)
	id, download := offerRelay(t, a, b, 128<<10)
	response, err := http.Get(s.URL + download)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	start := a.next("start")
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "PUT", s.URL+"/api/transfer/upload/"+id, input)
	req.Header.Set("Authorization", "Transfer "+start["token"].(string))
	req.ContentLength = 128 << 10
	upDone := make(chan error, 1)
	go func() {
		up, uploadErr := http.DefaultClient.Do(req)
		if up != nil {
			up.Body.Close()
			if up.StatusCode != 200 {
				uploadErr = fmt.Errorf("upload status %d", up.StatusCode)
			}
		}
		upDone <- uploadErr
	}()
	first := bytes.Repeat([]byte{71}, 64<<10)
	if _, err = writer.Write(first); err != nil {
		t.Fatal(err)
	}
	// 剩余一半尚未生成，接收端必须已读到首块，证明并未整文件暂存。
	// 客户端 HTTP 写缓冲可能暂留少量尾部字节，不要求首批字节全部到达。
	readDone := make(chan error, 1)
	go func() {
		got := make([]byte, 16<<10)
		_, readErr := io.ReadFull(response.Body, got)
		if readErr == nil && !bytes.Equal(got, first[:len(got)]) {
			readErr = fmt.Errorf("first block changed")
		}
		readDone <- readErr
	}()
	select {
	case err = <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("receiver waited for the entire upload")
	}
	if _, err = writer.Write(first); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	last, err := io.ReadAll(response.Body)
	if err != nil || !bytes.Equal(last, bytes.Repeat([]byte{71}, 112<<10)) {
		t.Fatalf("remaining download: %v", err)
	}
	if err = <-upDone; err != nil {
		t.Fatal(err)
	}
}

func TestTransferSettingsValidatePersistAndSnapshot(t *testing.T) {
	f := newFixture(t)
	h := f.app.transfers
	created := dataMap(t, f.request(t, "POST", "/api/transfer/create", f.admin, map[string]any{"name": "existing"}, nil, 201))
	oldLimit := int64(created["max_file_size"].(float64))
	key, _ := f.key(t, "transfer-user", "/", TextShare|FileShare)
	f.request(t, "PUT", "/api/admin/settings/group/3", key, map[string]any{"transfer_rate_mb": 0}, nil, 403)
	for _, body := range []map[string]any{
		{"transfer_rate_mb": -1}, {"transfer_rate_mb": "NaN"}, {"transfer_rate_mb": 10241},
		{"transfer_max_relays": 0}, {"transfer_max_relays": 17}, {"transfer_max_file_mb": 10241},
		{"transfer_rate_mb": 0, "transfer_max_relays": 99},
	} {
		f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, body, nil, 400)
	}
	f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, map[string]any{"transfer_rate_mb": 0, "transfer_max_relays": 3, "transfer_max_file_mb": 64}, nil, 200)
	h.mu.Lock()
	if h.bytesPerSecond != 0 || h.maxRelays != 3 || h.maxFile != 64<<20 {
		t.Error("runtime settings not applied")
	}
	for _, room := range h.rooms {
		if room.maxFile != oldLimit {
			t.Error("existing room size limit changed")
		}
	}
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if h.pace(ctx, 32<<10) != context.Canceled {
		t.Fatal("unlimited transfer ignored cancellation")
	}
	created = dataMap(t, f.request(t, "POST", "/api/transfer/create", f.admin, map[string]any{"name": "new"}, nil, 201))
	if int64(created["max_file_size"].(float64)) != 64<<20 {
		t.Fatal("new room did not use saved size limit")
	}
	// 真正关闭并重建 App，验证不限速的 0 值不会被环境默认值覆盖。
	if err := f.app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(f.app.cfg, f.app.logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	values := restarted.transferSettingValues()
	if values["transfer_rate_mb"] != "0" || values["transfer_max_relays"] != "3" || values["transfer_max_file_mb"] != "64" {
		t.Fatalf("settings not restored: %v", values)
	}
}
