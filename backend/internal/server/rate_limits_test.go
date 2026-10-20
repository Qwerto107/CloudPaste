package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

var rateLimitEnvNames = []string{
	"PASSWORD_ATTEMPTS_PER_MINUTE", "TRANSFER_CREATE_PER_MINUTE",
	"TRANSFER_JOIN_PER_MINUTE", "TRANSFER_RELAY_PER_MINUTE",
	"TRANSFER_WS_CONNECT_PER_MINUTE", "TRANSFER_WS_MESSAGES_PER_MINUTE",
	"TRANSFER_TOOL_PER_MINUTE", "TRANSFER_FOLDER_WS_MESSAGES_PER_MINUTE",
	"TRANSFER_FOLDER_RELAY_PER_MINUTE",
}

func TestRateLimitEnvironment(t *testing.T) {
	for _, name := range rateLimitEnvNames {
		t.Setenv(name, "")
	}
	cfg, err := ConfigFromEnv()
	want := RateLimitConfig{10, 30, 30, 180, 30, 240, 240, 12000, 12000}
	if err != nil || !reflect.DeepEqual(cfg.RateLimits, want) {
		t.Fatalf("defaults: got %+v, error %v", cfg.RateLimits, err)
	}
	for _, name := range rateLimitEnvNames {
		t.Setenv(name, "invalid-removed-setting")
	}
	cfg, err = ConfigFromEnv()
	if err != nil || !reflect.DeepEqual(cfg.RateLimits, want) {
		t.Fatalf("removed environment variables still affect limits: %+v %v", cfg.RateLimits, err)
	}
}

// 用真实连接来源模拟不同 IP，避免依赖可被客户端伪造的转发头。
func rateLimitRequest(t *testing.T, f *fixture, method, target, ip, auth string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, target, bytes.NewReader(raw))
	r.RemoteAddr = net.JoinHostPort(ip, "1234")
	r.Header.Set("Origin", "https://allowed.test")
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func requireRateStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
	}
}

func TestTransferCreateRatePerIP(t *testing.T) {
	for _, limit := range []int{30, 2} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			f := newFixture(t)
			if limit != 30 {
				setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferCreatePerIP = limit })
			}
			create := func(ip string) *httptest.ResponseRecorder {
				return rateLimitRequest(t, f, "POST", "/api/admin/transfer/create", ip, f.admin, map[string]any{"name": "测试设备"})
			}
			clearRooms := func() {
				// 已结束的连接不占房间容量，才能独立验证一分钟内的创建次数。
				h := f.app.transfers
				h.mu.Lock()
				defer h.mu.Unlock()
				for _, room := range h.rooms {
					h.removeLocked(room)
				}
			}
			for i := 0; i < limit; i++ {
				requireRateStatus(t, create("198.51.100.1"), 201)
				clearRooms()
			}
			requireRateStatus(t, create("198.51.100.1"), 429)
			// 超过旧全站 30 次后，另一个 IP 仍能创建。
			requireRateStatus(t, create("198.51.100.2"), 201)
		})
	}
}

func TestTransferJoinRatePerIP(t *testing.T) {
	f := newFixture(t)
	join := func(ip string) *httptest.ResponseRecorder {
		return rateLimitRequest(t, f, "POST", "/api/transfer/join", ip, "", map[string]any{"code": "A1234", "name": "测试设备"})
	}
	// 无效配对码也计数；不同 IP 总请求超过旧全站 120 次后仍可尝试。
	for ip := 1; ip <= 5; ip++ {
		for i := 0; i < 30; i++ {
			requireRateStatus(t, join(fmt.Sprintf("198.51.100.%d", ip)), 404)
		}
		requireRateStatus(t, join(fmt.Sprintf("198.51.100.%d", ip)), 429)
	}
	setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferJoinPerIP = 2 })
	for i := 0; i < 2; i++ {
		requireRateStatus(t, join("198.51.100.6"), 404)
	}
	requireRateStatus(t, join("198.51.100.6"), 429)
}

func TestTransferRelayUploadDownloadShareRate(t *testing.T) {
	for _, limit := range []int{180, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			f := newFixture(t)
			if limit != 180 {
				setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferRelayPerIP = limit })
			}
			request := func(i int, ip string) *httptest.ResponseRecorder {
				ticket := strings.Repeat("a", 43)
				if i%2 == 0 {
					return rateLimitRequest(t, f, "GET", "/api/transfer/download/missing?ticket="+ticket, ip, "", nil)
				}
				return rateLimitRequest(t, f, "PUT", "/api/transfer/upload/missing", ip, "Transfer "+ticket, nil)
			}
			for i := 0; i < limit; i++ {
				requireRateStatus(t, request(i, "198.51.100.1"), 404)
			}
			requireRateStatus(t, request(limit, "198.51.100.1"), 429)
			requireRateStatus(t, request(limit+1, "198.51.100.1"), 429)
			requireRateStatus(t, request(limit, "198.51.100.2"), 404)
		})
	}
}

func TestPasswordCustomRatePerIP(t *testing.T) {
	f := newFixture(t)
	setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.PasswordPerIP = 2 })
	login := func(ip string) *httptest.ResponseRecorder {
		return rateLimitRequest(t, f, "POST", "/api/admin/login", ip, "", map[string]any{"username": "missing", "password": "wrong"})
	}
	for i := 0; i < 2; i++ {
		requireRateStatus(t, login("198.51.100.1"), 401)
	}
	requireRateStatus(t, login("198.51.100.1"), 429)
	requireRateStatus(t, login("198.51.100.2"), 401)
}

func TestTransferSocketCustomRatePerIP(t *testing.T) {
	f := newFixture(t)
	setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferSocketPerIP = 2 })
	// 合法 Origin、失败的升级尝试也占次数，但应释放并发连接槽。
	for i := 0; i < 2; i++ {
		w := rateLimitRequest(t, f, "GET", "/api/transfer/socket", "198.51.100.1", "", nil)
		if w.Code < 400 || w.Code == 429 {
			t.Fatalf("unexpected handshake status: %d", w.Code)
		}
	}
	requireRateStatus(t, rateLimitRequest(t, f, "GET", "/api/transfer/socket", "198.51.100.1", "", nil), 429)
	if w := rateLimitRequest(t, f, "GET", "/api/transfer/socket", "198.51.100.2", "", nil); w.Code == 429 {
		t.Fatal("socket attempts blocked a different IP")
	}
}

func TestTransferMessagesCustomRatePerMember(t *testing.T) {
	f, _, sender, receiver := transferPair(t)
	h := f.app.transfers
	h.mu.Lock()
	setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferMessagesPerMember = 3 })
	h.mu.Unlock()
	// 创建者的 confirm 已计一次；两个成员的消息额度相互独立。
	for i := 0; i < 2; i++ {
		sender.send(map[string]any{"type": "text", "text": "测试"})
		sender.next("text")
		receiver.next("text")
	}
	receiver.send(map[string]any{"type": "text", "text": "另一设备"})
	sender.next("text")
	receiver.next("text")
	sender.send(map[string]any{"type": "text", "text": "超限"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := sender.conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("message limit did not promptly disconnect: %v", err)
	}
}

func TestTransferToolCustomRatePerIP(t *testing.T) {
	f, _, sender, receiver := transferPair(t)
	setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferToolPerIP = 2 })
	_, link := offerTool(t, sender, receiver, 10)
	for i := 0; i < 2; i++ {
		requireRateStatus(t, rateLimitRequest(t, f, "HEAD", link, "198.51.100.1", "", nil), http.StatusOK)
	}
	// GET 与 HEAD 共享次数；另一个 IP 可继续探测。
	requireRateStatus(t, rateLimitRequest(t, f, "GET", link, "198.51.100.1", "", nil), 429)
	requireRateStatus(t, rateLimitRequest(t, f, "HEAD", link, "198.51.100.2", "", nil), http.StatusOK)
}

// 文件夹有效票据使用独立额度；无效票据与重放只消耗探测额度。
func TestTransferFolderRelaySeparateRate(t *testing.T) {
	f := newFixture(t)
	h := f.app.transfers
	setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferRelayPerIP = 1 })
	setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferFolderRelayPerIP = 2 })
	ctx, cancel := context.WithCancel(context.Background())
	room := &transferRoom{id: randomID(), ctx: ctx, cancel: cancel, created: time.Now(), expires: time.Now().Add(time.Hour), files: map[string]*transferFile{}, folders: map[string]*transferFolder{}}
	room.members[0] = &transferMember{room: room, index: 0}
	room.members[1] = &transferMember{room: room, index: 1}
	folder := &transferFolder{id: randomID(), state: "active", mode: "relay", count: 3, size: 3, current: map[string]bool{}}
	room.folders[folder.id] = folder
	secret := strings.Repeat("a", 43)
	for _, id := range []string{"one", "two", "three"} {
		room.files[id] = &transferFile{id: id, room: room, folder: folder, folderEntry: &transferFolderEntry{ID: folder.id, Path: id}, mode: "relay", state: "active", ctx: ctx, size: 1, downloadToken: secretHash(secret)}
		folder.current[id] = true
	}
	h.mu.Lock()
	h.rooms[room.id] = room
	h.allowIPLocked("relay", "198.51.100.1", 1)
	h.mu.Unlock()
	claim := func(id, ticket string) (*transferFile, error) {
		r := httptest.NewRequest("GET", "/api/transfer/download/"+id+"?ticket="+ticket, nil)
		r.SetPathValue("id", id)
		r.RemoteAddr = "198.51.100.1:1234"
		r.Header.Set("Origin", "https://allowed.test")
		return f.app.claimTransfer(r, true)
	}
	for _, id := range []string{"one", "two"} {
		if _, err := claim(id, secret); err != nil {
			t.Fatalf("valid folder blocked: %v", err)
		}
	}
	for i, item := range [][2]string{{"three", secret}, {"one", secret}, {"three", strings.Repeat("b", 43)}, {"missing", secret}} {
		want := 429
		if i == 1 {
			want = 404 // 第一次重放占用探测额度，后续无效票据才超限。
		}
		if _, err := claim(item[0], item[1]); err == nil || err.(*apiError).Status != want {
			t.Fatalf("limit bypass: %v", err)
		}
	}
	if room.files["three"].downloading || h.limits.byKey["folder-relay:198.51.100.1"].Count != 2 || h.probes.byKey["198.51.100.1"].Count != 1 {
		t.Fatal("failed request consumed a ticket or bypassed quota")
	}
}

// 独立消息额度也必须执行，不能把文件夹授权当作不限频许可。
func TestTransferFolderMessagesCustomRate(t *testing.T) {
	f, _, a, b := transferPair(t)
	setRuntimeLimitsForTest(f.app, func(limits *runtimeLimits) { limits.RateLimits.TransferFolderMessagesPerMember = 2 })
	id := startFolderTest(t, a, b, 3, 0)
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("empty-%d", i)
		a.send(map[string]any{"type": "offer", "client_id": randomID(), "name": name, "size": 0, "folder": map[string]any{"id": id, "path": name, "directory": true}})
		file := a.next("file")
		b.next("file")
		b.send(map[string]any{"type": "accept", "id": file["id"], "mode": "directory"})
		a.next("state")
		b.next("state")
		a.next("folder-state")
		b.next("folder-state")
		a.next("folder-next")
	}
	a.send(map[string]any{"type": "offer", "client_id": randomID(), "name": "over", "size": 0, "folder": map[string]any{"id": id, "path": "over", "directory": true}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := a.conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("folder quota did not disconnect: %v", err)
	}
}
