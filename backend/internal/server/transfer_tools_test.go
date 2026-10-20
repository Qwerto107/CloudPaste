package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func offerTool(t *testing.T, sender, receiver transferTestPeer, size int64) (string, string) {
	t.Helper()
	sender.send(map[string]any{"type": "offer", "client_id": randomID(), "name": "分段测试.bin", "size": size})
	id := sender.next("file")["id"].(string)
	receiver.next("file")
	receiver.send(map[string]any{"type": "accept", "id": id, "mode": "tool"})
	link := receiver.next("download-tool")
	if connections, ok := link["max_connections"].(float64); !ok || connections < 1 || connections > 4 {
		t.Fatal("invalid tool connection limit", link)
	}
	sender.next("state")
	receiver.next("state")
	return id, link["url"].(string)
}

func TestTransferToolRangesAndProbes(t *testing.T) {
	f, server, sender, receiver := transferPair(t)
	f.app.applyTransferSettings(map[string]string{"transfer_max_relays": "0"})
	f.app.audit.enabled.Store(true)
	payload := bytes.Repeat([]byte("abcdefgh"), 16384)
	id, link := offerTool(t, sender, receiver, int64(len(payload)))
	waitAudit(t, f, "module='transfer' AND action='文件传输开始'", 1)
	url := server.URL + link
	// 完成接收不能绕过传输进度检查，提前点击不应撤销仍有效的链接。
	receiver.send(map[string]any{"type": "finish-download", "id": id})
	receiver.next("error")
	// 探测不消费票据，不创建上传，也不占用中继槽。
	var etag string
	for i := 0; i < 2; i++ {
		response, err := http.Head(url)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		etag = response.Header.Get("ETag")
		if response.StatusCode != 200 || response.ContentLength != int64(len(payload)) || response.Header.Get("Accept-Ranges") != "bytes" || etag == "" {
			t.Fatal("HEAD metadata mismatch", response.StatusCode, response.Header)
		}
	}
	h := f.app.transfers
	h.mu.Lock()
	if h.relays != 0 {
		t.Fatal("HEAD claimed a relay slot")
	}
	h.mu.Unlock()
	for _, value := range []string{"bytes=999999-", "bytes=5-1", "bytes=0-1,3-4", "bytes=-0", "bytes=9223372036854775808-"} {
		request, _ := http.NewRequest("GET", url, nil)
		request.Header.Set("Range", value)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 416 || response.Header.Get("Content-Range") != fmt.Sprintf("bytes */%d", len(payload)) {
			t.Fatalf("invalid range accepted: %s: %d", value, response.StatusCode)
		}
	}
	// 两路真实 HTTP 下载和上传同时运行，验证区间、独立凭证及不会重复统计进度。
	type rangeSegment struct {
		response    *http.Response
		start       map[string]any
		first, last int
	}
	var segments []rangeSegment
	for _, span := range [][2]int{{0, 65535}, {65536, len(payload) - 1}} {
		request, _ := http.NewRequest("GET", url, nil)
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", span[0], span[1]))
		request.Header.Set("If-Range", etag)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		start := sender.next("range-start")
		if response.StatusCode != 206 || int(start["start"].(float64)) != span[0] || int(start["end"].(float64)) != span[1]+1 || response.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", span[0], span[1], len(payload)) {
			t.Fatal("range metadata mismatch", response.StatusCode, start)
		}
		segments = append(segments, rangeSegment{response, start, span[0], span[1]})
	}
	for _, segment := range segments {
		// 另一段的凭证不能用于本段上传。
		wrong := segments[0].start["token"].(string)
		if wrong == segment.start["token"] {
			wrong = segments[1].start["token"].(string)
		}
		path := id + "/" + segment.start["request_id"].(string)
		denied := transferPut(t, server.URL, path, wrong, strings.NewReader(""), 0)
		denied.Body.Close()
		if denied.StatusCode != 404 {
			t.Fatal("cross-range upload token accepted")
		}
	}
	uploadResults := make(chan error, len(segments))
	for _, segment := range segments {
		go func(s rangeSegment) {
			body := payload[s.first : s.last+1]
			request, _ := http.NewRequest("PUT", server.URL+"/api/transfer/upload/"+id+"/"+s.start["request_id"].(string), bytes.NewReader(body))
			request.Header.Set("Authorization", "Transfer "+s.start["token"].(string))
			response, err := http.DefaultClient.Do(request)
			if err == nil {
				response.Body.Close()
				if response.StatusCode != 200 {
					err = fmt.Errorf("upload status %d", response.StatusCode)
				}
			}
			uploadResults <- err
		}(segment)
	}
	for _, segment := range segments {
		body, err := io.ReadAll(segment.response.Body)
		segment.response.Body.Close()
		if err != nil || !bytes.Equal(body, payload[segment.first:segment.last+1]) {
			t.Fatal("range bytes mismatch", err)
		}
	}
	for range segments {
		if err := <-uploadResults; err != nil {
			t.Fatal(err)
		}
	}
	waitAudit(t, f, "module='transfer' AND action='下载完成'", 1)
	// 所有数据转发后仍允许重试，直至接收方确认结束；服务器不擅自宣称落盘成功。
	response, err := http.Head(url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("link revoked after segmented download")
	}
	response, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	// 完整下载后再次请求，双方都收到新轮次的归零进度，随后再接收整份数据。
	for _, peer := range []transferTestPeer{sender, receiver} {
		for {
			progress := peer.next("progress")
			if progress["round"] == float64(2) {
				if progress["bytes"] != float64(0) {
					t.Fatal("repeat progress did not reset", progress)
				}
				break
			}
		}
	}
	start := sender.next("range-start")
	done := make(chan error, 1)
	go func() {
		request, _ := http.NewRequest("PUT", server.URL+"/api/transfer/upload/"+id+"/"+start["request_id"].(string), bytes.NewReader(payload))
		request.Header.Set("Authorization", "Transfer "+start["token"].(string))
		upload, err := http.DefaultClient.Do(request)
		if err == nil {
			upload.Body.Close()
			if upload.StatusCode != 200 {
				err = fmt.Errorf("repeat upload: %d", upload.StatusCode)
			}
		}
		done <- err
	}()
	repeated, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || !bytes.Equal(repeated, payload) {
		t.Fatal("repeat bytes mismatch", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	var progressBytes, sentBytes int64
	var attempts int
	for _, room := range h.rooms {
		if file := room.files[id]; file != nil {
			progressBytes, sentBytes, attempts = file.bytes, file.tool.sent, file.tool.attempts
		}
	}
	h.mu.Unlock()
	if progressBytes != int64(len(payload)) || sentBytes != int64(2*len(payload)) || attempts != 3 {
		t.Fatal("repeat progress or cumulative limits mismatch", progressBytes, sentBytes, attempts)
	}
	waitAudit(t, f, "module='transfer' AND action='下载完成'", 2)
	var rounds int
	if err := f.app.audit.db.QueryRow("SELECT count(DISTINCT json_extract(details,'$.round')) FROM operation_logs WHERE action='下载完成' AND target=?", id).Scan(&rounds); err != nil || rounds != 2 {
		t.Fatalf("每次完整下载应分别记录: rounds=%d, error=%v", rounds, err)
	}
	var downloadIPs string
	if err := f.app.audit.db.QueryRow("SELECT json_extract(details,'$.download_ips') FROM operation_logs WHERE action='下载完成' AND target=? ORDER BY id DESC LIMIT 1", id).Scan(&downloadIPs); err != nil || !strings.Contains(downloadIPs, "127.0.0.1") {
		t.Fatalf("下载日志缺少实际请求 IP: %q, error=%v", downloadIPs, err)
	}
	receiver.send(map[string]any{"type": "finish-download", "id": id})
	if receiver.next("state")["state"] != "complete" {
		t.Fatal("manual completion failed")
	}
	response, err = http.Head(url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("completed ticket reusable")
	}
}

func TestTransferToolRoundIgnoresPreviousRequests(t *testing.T) {
	h := &transferHub{}
	f := &transferFile{size: 100, bytes: 100, state: "active", room: &transferRoom{}, tool: &transferTool{
		round: 1, loggedRound: 1, sent: 150, attempts: 3, completed: []transferByteRange{{0, 100}},
		requests: map[string]*transferRangeRequest{"old": {round: 1, transferByteRange: transferByteRange{0, 100}, forwarded: 100}},
	}}
	h.startToolRoundLocked(f)
	if f.tool.round != 2 || f.bytes != 0 || f.tool.sent != 150 || f.tool.attempts != 3 {
		t.Fatal("round reset altered limits or retained old bytes")
	}
	f.tool.requests["new"] = &transferRangeRequest{round: 2, transferByteRange: transferByteRange{0, 100}, forwarded: 25}
	h.toolProgressLocked(f, true)
	if f.bytes != 25 {
		t.Fatal("old request restored completed progress", f.bytes)
	}
	h.startToolRoundLocked(f)
	if f.tool.round != 2 {
		t.Fatal("partial retry started a new round")
	}
}

func TestTransferToolAbortResumeAndLimits(t *testing.T) {
	f, server, sender, receiver := transferPair(t)
	id, link := offerTool(t, sender, receiver, 100)
	url := server.URL + link
	// 模拟下载工具 GET 探测后中止，不上传数据；下一段仍可接续。
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	old := sender.next("range-start")
	cancel()
	response.Body.Close()
	h := f.app.transfers
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.mu.Lock()
		released := h.relays == 0
		h.mu.Unlock()
		if released {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("aborted request retained relay")
		}
		time.Sleep(10 * time.Millisecond)
	}
	late := transferPut(t, server.URL, id+"/"+old["request_id"].(string), old["token"].(string), strings.NewReader(""), 0)
	late.Body.Close()
	if late.StatusCode != 404 {
		t.Fatal("old range nonce survived abort")
	}
	request, _ = http.NewRequest("GET", url, nil)
	request.Header.Set("Range", "bytes=-3")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	start := sender.next("range-start")
	if start["start"] != float64(97) || start["end"] != float64(100) {
		t.Fatal("suffix mismatch")
	}
	up := transferPut(t, server.URL, id+"/"+start["request_id"].(string), start["token"].(string), strings.NewReader("end"), 3)
	up.Body.Close()
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(data) != "end" || up.StatusCode != 200 {
		t.Fatal("resume failed", err)
	}
	// 并发遵守现有中继设置，超额请求不破坏已有下载或消耗上传凭证。
	h.mu.Lock()
	h.maxRelays = 1
	h.mu.Unlock()
	response, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	sender.next("range-start")
	extra, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	extra.Body.Close()
	if extra.StatusCode != 429 {
		t.Fatal("concurrency guard missing")
	}
	receiver.send(map[string]any{"type": "cancel", "id": id})
	if receiver.next("state")["state"] != "cancelled" {
		t.Fatal("cancel failed")
	}
	response.Body.Close()
	response, err = http.Head(url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("cancelled tool link usable")
	}
}

func TestTransferRequestedRange(t *testing.T) {
	for _, row := range []struct {
		header     string
		first, end int64
	}{
		{"", 0, 100}, {"bytes=5-", 5, 100}, {"bytes=5-9223372036854775807", 5, 100}, {"bytes=-999", 0, 100},
	} {
		span, _, err := transferRequestedRange(row.header, 100)
		if err != nil || span.start != row.first || span.end != row.end {
			t.Fatal(row, span, err)
		}
	}
	if span, _, err := transferRequestedRange("", 0); err != nil || span.end != 0 {
		t.Fatal("empty full file")
	}
	ranges := mergeTransferRange([]transferByteRange{{0, 10}, {20, 30}}, transferByteRange{5, 25})
	if len(ranges) != 1 || ranges[0] != (transferByteRange{0, 30}) {
		t.Fatal("overlap double counted", ranges)
	}
}

func TestTransferToolAuthorizationExpiryAndBudget(t *testing.T) {
	f, server, sender, receiver := transferPair(t)
	id, link := offerTool(t, sender, receiver, 100)
	url := server.URL + link
	for _, target := range []string{strings.Replace(url, "ticket=", "ticket=wrong", 1), strings.Replace(url, id, randomID(), 1)} {
		response, err := http.Head(target)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatal("tool ticket not bound to file")
		}
	}
	request, _ := http.NewRequest("HEAD", url, nil)
	request.Header.Set("Origin", "https://untrusted.example")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("foreign origin accepted")
	}
	h := f.app.transfers
	h.mu.Lock()
	var file *transferFile
	for _, room := range h.rooms {
		if room.files[id] != nil {
			file = room.files[id]
		}
	}
	file.tool.sent = 1 << 20
	h.mu.Unlock()
	response, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 429 {
		t.Fatal("transfer byte budget not enforced")
	}
	h.mu.Lock()
	file.tool.sent = 0
	file.tool.attempts = 256
	h.mu.Unlock()
	response, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 429 {
		t.Fatal("request budget not enforced")
	}
	response, err = http.Head(url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("HEAD consumed transfer budget")
	}
	h.mu.Lock()
	file.tool.expires = time.Now().Add(-time.Second)
	h.mu.Unlock()
	response, err = http.Head(url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("expired ticket accepted")
	}
}
