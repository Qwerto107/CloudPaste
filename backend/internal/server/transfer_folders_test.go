package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTransferFolderPaths(t *testing.T) {
	for _, path := range []string{"../a", "/a", "a//b", "C:/a", `a\b`, "a/CON.txt", "a/b.", "a/b ", "a\x00b", strings.Repeat("a/", 16) + "b"} {
		if validTransferPath(path, false) {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
	if !validTransferPath("子目录/数据.txt", false) || !validTransferPath("", true) || validTransferPath("", false) {
		t.Fatal("valid path handling")
	}
}

func startFolderTest(t *testing.T, a, b transferTestPeer, count int, size int64) string {
	t.Helper()
	a.send(map[string]any{"type": "folder-offer", "client_id": randomID(), "name": "目录测试", "count": count, "size": size})
	folder := a.next("folder")
	b.next("folder")
	id := folder["id"].(string)
	b.send(map[string]any{"type": "accept-folder", "id": id, "mode": "relay", "concurrency": 1})
	a.next("folder-state")
	b.next("folder-state")
	a.next("folder-next")
	return id
}

func TestTransferFolderQueueBeyond32AndBoundedState(t *testing.T) {
	f, _, a, b := transferPair(t)
	// 条目队列超过旧的 32 个累计上限，但服务端始终只保留当前一个子任务。
	setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferMessagesPerMember = 3 })
	id := startFolderTest(t, a, b, 64, 0)
	for index := 0; index < 64; index++ {
		name := fmt.Sprintf("empty-%03d", index)
		a.send(map[string]any{"type": "offer", "client_id": randomID(), "name": name, "size": 0, "folder": map[string]any{"id": id, "path": "nested/" + name, "directory": true}})
		file := a.next("file")
		b.next("file")
		b.send(map[string]any{"type": "accept", "id": file["id"], "mode": "directory"})
		a.next("state")
		b.next("state")
		state := a.next("folder-state")
		b.next("folder-state")
		if index < 63 {
			a.next("folder-next")
		} else if state["state"] != "complete" || state["completed"] != float64(64) {
			t.Fatal("folder did not complete", state)
		}
	}
	f.app.transfers.mu.Lock()
	defer f.app.transfers.mu.Unlock()
	for _, room := range f.app.transfers.rooms {
		if len(room.files) != 0 || len(room.folders) != 1 {
			t.Fatal("folder children accumulated in room")
		}
	}
}

func TestTransferFolderRelayWaitsForSaved(t *testing.T) {
	f, s, a, b := transferPair(t)
	payload := []byte("目录流式接收")
	id := startFolderTest(t, a, b, 1, int64(len(payload)))
	a.send(map[string]any{"type": "offer", "client_id": randomID(), "name": "data.txt", "size": len(payload), "folder": map[string]any{"id": id, "path": "nested/data.txt"}})
	file := a.next("file")
	b.next("file")
	fileID := file["id"].(string)
	b.send(map[string]any{"type": "accept", "id": fileID, "mode": "relay"})
	download := b.next("download")["url"].(string)
	a.next("state")
	b.next("state")
	response, err := http.Get(s.URL + download)
	if err != nil {
		t.Fatal(err)
	}
	start := a.next("start")
	done := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(response.Body); response.Body.Close(); done <- data }()
	up := transferPut(t, s.URL, fileID, start["token"].(string), bytes.NewReader(payload), int64(len(payload)))
	up.Body.Close()
	if ready := b.next("relay-ready"); ready["id"] != fileID {
		t.Fatal("missing authenticated upload-ready notification", ready)
	}
	if !bytes.Equal(<-done, payload) {
		t.Fatal("content changed")
	}
	f.app.transfers.mu.Lock()
	for _, room := range f.app.transfers.rooms {
		if room.folders[id].state != "active" {
			t.Fatal("folder completed before local save acknowledgement")
		}
	}
	f.app.transfers.mu.Unlock()
	b.send(map[string]any{"type": "complete", "id": fileID, "bytes": len(payload)})
	if a.next("state")["state"] != "complete" || b.next("state")["state"] != "complete" {
		t.Fatal("entry incomplete")
	}
	if a.next("folder-state")["state"] != "complete" || b.next("folder-state")["state"] != "complete" {
		t.Fatal("folder incomplete")
	}
}

func TestTransferFolderCancelRevokesCurrentTicket(t *testing.T) {
	f, s, a, b := transferPair(t)
	id := startFolderTest(t, a, b, 2, 10)
	a.send(map[string]any{"type": "offer", "client_id": randomID(), "name": "data.txt", "size": 10, "folder": map[string]any{"id": id, "path": "data.txt"}})
	file := a.next("file")
	b.next("file")
	b.send(map[string]any{"type": "accept", "id": file["id"], "mode": "relay"})
	download := b.next("download")["url"].(string)
	a.next("state")
	b.next("state")
	b.send(map[string]any{"type": "cancel-folder", "id": id})
	a.next("state")
	b.next("state")
	if a.next("folder-state")["state"] != "cancelled" || b.next("folder-state")["state"] != "cancelled" {
		t.Fatal("queue was not cancelled")
	}
	response, err := http.Get(s.URL + download)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("cancelled folder ticket still works")
	}
	f.app.transfers.mu.Lock()
	defer f.app.transfers.mu.Unlock()
	if f.app.transfers.relays != 0 {
		t.Fatal("relay slot not released")
	}
	for _, room := range f.app.transfers.rooms {
		if len(room.files) != 0 {
			t.Fatal("cancelled child retained")
		}
	}
}

// 并发窗口预留不能超过全站额度，断开时未使用的额度也必须归还。
func TestTransferFolderParallelReservations(t *testing.T) {
	f, _, a, b := transferPair(t)
	a.send(map[string]any{"type": "folder-offer", "client_id": randomID(), "name": "parallel", "count": 40, "size": 40})
	folder := a.next("folder")
	b.next("folder")
	if folder["concurrency"] != float64(16) {
		t.Fatal("default concurrency", folder)
	}
	h := f.app.transfers
	h.mu.Lock()
	h.maxRelays = 4
	h.mu.Unlock()
	b.send(map[string]any{"type": "accept-folder", "id": folder["id"], "mode": "relay", "concurrency": 32})
	a.next("folder-state")
	b.next("folder-state")
	if next := a.next("folder-next"); next["count"] != float64(2) {
		t.Fatal("global limit", next)
	}
	h.mu.Lock()
	if h.folderReservations != 2 {
		t.Fatal("reservation count", h.folderReservations)
	}
	for _, room := range h.rooms {
		h.removeLocked(room)
	}
	if h.folderReservations != 0 || h.relays != 0 {
		t.Fatal("disconnect leaked slots", h.folderReservations, h.relays)
	}
	h.mu.Unlock()
}

func TestTransferFolderRelayLimitAbove64AndUnlimited(t *testing.T) {
	for _, limit := range []int{64, 128, 4096, 0} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			f, _, a, b := transferPair(t)
			f.app.applyTransferSettings(map[string]string{"transfer_max_relays": fmt.Sprint(limit)})
			a.send(map[string]any{"type": "folder-offer", "client_id": randomID(), "name": "parallel", "count": 256, "size": 256})
			folder := a.next("folder")
			b.next("folder")
			b.send(map[string]any{"type": "accept-folder", "id": folder["id"], "mode": "relay", "concurrency": 128})
			a.next("folder-state")
			b.next("folder-state")
			want := 128
			if limit > 0 {
				want = min(want, max(1, limit/2))
			}
			if next := a.next("folder-next"); next["count"] != float64(want) {
				t.Fatalf("relay limit %d: %v", limit, next)
			}
			b.send(map[string]any{"type": "cancel-folder", "id": folder["id"]})
			a.next("folder-state")
			b.next("folder-state")
			f.app.transfers.mu.Lock()
			reserved := f.app.transfers.folderReservations
			f.app.transfers.mu.Unlock()
			if reserved != 0 {
				t.Fatal("cancel leaked relay reservations", reserved)
			}
		})
	}
}

// 合并进度必须整批授权，不能用后一项的非法编号留下前一项的写入。
func TestTransferFolderParallelProgressAtomic(t *testing.T) {
	f, _, a, b := transferPair(t)
	f.app.transfers.mu.Lock()
	for _, room := range f.app.transfers.rooms {
		room.relayOnly = false
	}
	f.app.transfers.mu.Unlock()
	a.send(map[string]any{"type": "folder-offer", "client_id": randomID(), "name": "parallel", "count": 128, "size": 1280})
	folder := a.next("folder")
	b.next("folder")
	b.send(map[string]any{"type": "accept-folder", "id": folder["id"], "mode": "p2p", "concurrency": 129})
	b.next("error")
	b.send(map[string]any{"type": "accept-folder", "id": folder["id"], "mode": "p2p", "concurrency": 128})
	a.next("folder-state")
	b.next("folder-state")
	if next := a.next("folder-next"); next["count"] != float64(128) {
		t.Fatal(next)
	}
	ids := []string{}
	for i := 0; i < 128; i++ {
		name := fmt.Sprintf("file-%d", i)
		a.send(map[string]any{"type": "offer", "client_id": randomID(), "name": name, "size": 10, "folder": map[string]any{"id": folder["id"], "path": name}})
		file := a.next("file")
		b.next("file")
		ids = append(ids, file["id"].(string))
		b.send(map[string]any{"type": "accept", "id": file["id"], "mode": "p2p"})
		a.next("start")
		a.next("state")
		b.next("state")
	}
	b.send(map[string]any{"type": "folder-progress", "progress": []map[string]any{{"id": ids[0], "bytes": 5}, {"id": "invalid", "bytes": 1}}})
	b.next("error")
	h := f.app.transfers
	h.mu.Lock()
	for _, room := range h.rooms {
		if len(room.files) != 128 || room.files[ids[0]].bytes != 0 {
			t.Fatal("parallel tasks or atomic progress")
		}
	}
	h.mu.Unlock()
	b.send(map[string]any{"type": "folder-progress", "progress": []map[string]any{{"id": ids[0], "bytes": 5}, {"id": ids[1], "bytes": 7}}})
	a.next("folder-progress")
	b.next("folder-progress")
	b.send(map[string]any{"type": "cancel-folder", "id": folder["id"]})
}

// 调低时先结清发送端在途消息，再回收额度，已经安排的文件不能被取消。
func TestTransferFolderConcurrencyUpdate(t *testing.T) {
	f, _, a, b := transferPair(t)
	a.send(map[string]any{"type": "folder-offer", "client_id": randomID(), "name": "adjust", "count": 10, "size": 10})
	folder := a.next("folder")
	b.next("folder")
	id := folder["id"].(string)
	b.send(map[string]any{"type": "accept-folder", "id": id, "mode": "relay", "concurrency": 4})
	a.next("folder-state")
	b.next("folder-state")
	a.next("folder-next")
	ids := []string{}
	offer := func() {
		name := fmt.Sprintf("f-%d", len(ids))
		a.send(map[string]any{"type": "offer", "client_id": randomID(), "name": name, "size": 1, "folder": map[string]any{"id": id, "path": name}})
		child := a.next("file")
		b.next("file")
		ids = append(ids, child["id"].(string))
	}
	offer()
	offer()
	a.send(map[string]any{"type": "folder-concurrency", "id": id, "concurrency": 1})
	if a.next("error")["action"] != "folder-concurrency" {
		t.Fatal("missing action")
	}
	for _, value := range []int{0, 129} {
		b.send(map[string]any{"type": "folder-concurrency", "id": id, "concurrency": value})
		b.next("error")
	}
	b.send(map[string]any{"type": "folder-concurrency", "id": id, "concurrency": 1})
	a.next("folder-reset")
	// 模拟收到重置之前已经发出的条目，必须先于确认被接纳。
	offer()
	b.send(map[string]any{"type": "folder-ready", "id": id})
	b.next("error")
	a.send(map[string]any{"type": "folder-ready", "id": id})
	a.next("folder-state")
	b.next("folder-state")
	if b.next("folder-concurrency")["concurrency"] != float64(1) {
		t.Fatal("lower limit")
	}
	h := f.app.transfers
	h.mu.Lock()
	var room *transferRoom
	for _, current := range h.rooms {
		room = current
	}
	pending, reservations, children := room.folders[id].pending, h.folderReservations, len(room.files)
	// 这里只验证调度，内部完成前两个条目后仍不能启动下一项。
	h.finishLocked(room.files[ids[0]], "complete")
	h.finishLocked(room.files[ids[1]], "complete")
	afterTwo := room.folders[id].pending
	h.finishLocked(room.files[ids[2]], "complete")
	h.mu.Unlock()
	if pending != 0 || reservations != 3 || children != 3 || afterTwo != 0 {
		t.Fatalf("lowering did not drain safely: %d %d %d %d", pending, reservations, children, afterTwo)
	}
	if a.next("folder-next")["count"] != float64(1) {
		t.Fatal("new lower allowance")
	}
	b.send(map[string]any{"type": "folder-concurrency", "id": id, "concurrency": 5})
	a.next("folder-reset")
	a.send(map[string]any{"type": "folder-ready", "id": id})
	a.next("folder-state")
	b.next("folder-state")
	if b.next("folder-concurrency")["concurrency"] != float64(5) {
		t.Fatal("higher limit")
	}
	if a.next("folder-next")["count"] != float64(5) {
		t.Fatal("new higher allowance")
	}
	b.send(map[string]any{"type": "cancel-folder", "id": id})
}

// 队列还在推进时，等待浏览器连接的文件不能提前超时；已接入的停滞文件不能借用其他文件的进度。
func TestTransferFolderQueuedActivity(t *testing.T) {
	now := time.Now()
	old := now.Add(-10 * time.Minute)
	folder := &transferFolder{state: "active", mode: "relay", progress: now}
	file := &transferFile{state: "active", mode: "relay", folder: folder, created: old, progress: old, downloading: true}
	if !file.lastActivity().Equal(now) {
		t.Fatal("queued file did not inherit folder progress")
	}
	file.uploading = true
	if !file.lastActivity().Equal(old) {
		t.Fatal("active stalled file borrowed sibling progress")
	}
	file.state = "offered"
	if !file.lastActivity().Equal(now) {
		t.Fatal("unaccepted queued child expired while folder was progressing")
	}
	file.state = "active"
	file.folder = nil
	if !file.lastActivity().Equal(old) {
		t.Fatal("ordinary file idle protection changed")
	}
	folder.progress = old
	file.folder = folder
	file.uploading = false
	if !file.lastActivity().Equal(old) {
		t.Fatal("fully idle folder lost timeout protection")
	}
}
