package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTransferSTUNSettings(t *testing.T) {
	f := newFixture(t)
	const key = "transfer_stun"
	if f.app.transferSettingValues()[key] != defaultTransferSTUN {
		t.Fatal("default STUN pool missing")
	}
	if normalized, valid := normalizeTransferSTUN(defaultTransferSTUN); !valid || normalized != defaultTransferSTUN || len(strings.Split(normalized, "\n")) != 5 {
		t.Fatal("default STUN pool is invalid")
	}
	userKey, _ := f.key(t, "transfer-user", "/", TextShare|FileShare)
	f.request(t, "PUT", "/api/admin/settings/group/3", userKey, map[string]any{key: "stun:example.com:3478"}, nil, 403)
	for _, invalid := range []any{
		true, 3478, "stun:", "https://example.com:3478", "turn:example.com:3478",
		"stun:example.com", "stun::3478", "stun:example.com:0", "stun:example.com:65536",
		"stun:example.com:abc", "stun:user@example.com:3478", "stun:example.com:3478/path",
		"stun:example.com:3478?transport=udp", "stun:example.com:3478#fragment",
		"stun:a\nb:3478", "stun:a b:3478", "stun:2001:db8::1:3478",
		"stun:-invalid.example:3478", "stun:example..com:3478", "stun:" + strings.Repeat("a", 257) + ":3478",
	} {
		f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, map[string]any{key: invalid, "transfer_max_relays": 1}, nil, 400)
	}
	if f.app.transferSettingValues()["transfer_max_relays"] != "64" {
		t.Fatal("invalid STUN update partially changed other settings")
	}
	for _, valid := range []string{"stun:192.0.2.1:3478", "stun:[2001:db8::1]:3478", "stun:stun.example.com:65535"} {
		f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, map[string]any{key: valid}, nil, 200)
	}
	const first = "stun:first.example.com:3478"
	f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, map[string]any{key: " " + first + " "}, nil, 200)
	admin := f.request(t, "GET", "/api/admin/settings?group=3", f.admin, nil, nil, 200)
	var result struct{ Data []setting }
	if err := json.Unmarshal(admin.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range result.Data {
		if item.Key == key && item.Value == first {
			found = true
		}
	}
	if !found {
		t.Fatal("admin settings did not return normalized STUN address")
	}
	for _, target := range []string{"/api/public/settings?group=3", "/api/public/settings"} {
		if strings.Contains(f.request(t, "GET", target, "", nil, nil, 200).Body.String(), key) {
			t.Fatal("STUN configuration leaked through public settings")
		}
	}
	// 直接核对真实配对消息，避免测试依赖浏览器向外部 STUN 服务器发起请求。
	pair := func(want string) *transferRoom {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		room := &transferRoom{id: randomID(), ctx: ctx, cancel: cancel, created: time.Now(), expires: time.Now().Add(time.Hour), files: map[string]*transferFile{}}
		h := f.app.transfers
		h.mu.Lock()
		defer h.mu.Unlock()
		for i := range room.members {
			room.members[i] = &transferMember{room: room, index: i, connected: true, out: make(chan any, 8)}
		}
		h.rooms[room.id] = room
		if err := h.messageLocked(room.members[0], transferMessage{Type: "confirm"}); err != nil {
			t.Fatal(err)
		}
		for _, member := range room.members {
			select {
			case raw := <-member.out:
				message := raw.(map[string]any)
				first, servers := "", []string{}
				if want != "" {
					servers = strings.Split(want, "\n")
					first = servers[0]
				}
				actual := message["stun_servers"].([]string)
				if message["type"] != "paired" || message["stun"] != first || strings.Join(actual, "\n") != strings.Join(servers, "\n") {
					t.Fatal("pairing used wrong STUN address", message)
				}
			default:
				t.Fatal("pairing message missing")
			}
		}
		return room
	}
	room := pair(first)
	const second = "stun:second.example.com:3478\nstun:third.example.com:3478"
	f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, map[string]any{key: second}, nil, 200)
	for _, member := range room.members {
		if !member.connected || len(member.out) != 0 {
			t.Fatal("saved setting interrupted or renegotiated existing pairing")
		}
	}
	pair(second)
	if err := f.app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(f.app.cfg, f.app.logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if restarted.transferSettingValues()[key] != second {
		t.Fatal("STUN address was not restored after restart")
	}
	f = &fixture{app: restarted, handler: restarted.Handler(), admin: f.admin}
	f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, map[string]any{key: ""}, nil, 200)
	pair("")
	if err := restarted.restoreTransferSettings(); err != nil || restarted.transferSettingValues()[key] != "" {
		t.Fatal("disabled STUN setting was not persisted", err)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
	disabled, err := New(restarted.cfg, restarted.logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disabled.Close() })
	if disabled.transferSettingValues()[key] != "" {
		t.Fatal("restart replaced explicitly disabled STUN with defaults")
	}
}

func TestTransferSTUNServerPool(t *testing.T) {
	const want = "stun:first.example.com:3478\nstun:[2001:db8::1]:3478"
	if actual, valid := normalizeTransferSTUN(" \r\n stun:first.example.com:3478 \r\nSTUN:first.example.com:3478"); valid || actual != "" {
		t.Fatal("invalid scheme accepted")
	}
	actual, valid := normalizeTransferSTUN(" \r\n stun:first.example.com:3478 \r\nstun:FIRST.example.com:3478\n\n stun:[2001:db8::1]:3478 \n")
	if !valid || actual != want {
		t.Fatal("server pool was not normalized", actual)
	}
	servers := []string{}
	for i := 0; i < 5; i++ {
		servers = append(servers, "stun:server"+string(rune('a'+i))+".example.com:3478")
	}
	if _, valid := normalizeTransferSTUN(strings.Join(servers, "\n")); !valid {
		t.Fatal("five servers rejected")
	}
	for _, invalid := range []string{strings.Join(append(servers, "stun:six.example.com:3478"), "\n"), want + "\nturn:example.com:3478", want + "\nstun:user@example.com:3478", strings.Repeat(" ", 1285)} {
		if _, valid := normalizeTransferSTUN(invalid); valid {
			t.Fatal("unsafe pool accepted", invalid)
		}
	}
	f := newFixture(t)
	f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, map[string]any{"transfer_stun": "\n" + want + "\n"}, nil, 200)
	f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, map[string]any{"transfer_stun": want + "\nturn:example.com:3478"}, nil, 400)
	if f.app.transferSettingValues()["transfer_stun"] != want {
		t.Fatal("invalid pool changed stored configuration")
	}
}

func TestTransferSTUNEnvironmentRemoved(t *testing.T) {
	// 旧变量即使包含非法地址也应被完全忽略，不影响启动或后台设置。
	t.Setenv("TRANSFER_STUN", "https://invalid.example")
	if _, err := ConfigFromEnv(); err != nil {
		t.Fatal("removed STUN environment variable still affects startup", err)
	}
	f := newFixture(t)
	items, err := f.app.loadSettings(httptest.NewRequest("GET", "/api/admin/settings", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Key == "transfer_stun" && item.Value != defaultTransferSTUN {
			t.Fatal("removed environment variable replaced default STUN pool")
		}
	}
}
