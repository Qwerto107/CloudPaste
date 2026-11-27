package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func reconnectSocket(t *testing.T, server *httptest.Server, token string) transferTestPeer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/transfer/socket", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	peer := transferTestPeer{conn: conn, t: t, token: token}
	peer.send(map[string]any{"type": "auth", "token": token, "resume": true})
	return peer
}

func TestTransferReconnectRestoresPairAndRevokesOldDownload(t *testing.T) {
	f, server, a, b := transferPair(t)
	id, download := offerRelay(t, a, b, 10)
	_ = b.conn.CloseNow()
	offline := a.next("peer-offline")
	deadline, err := time.Parse(time.RFC3339Nano, offline["reconnect_until"].(string))
	if err != nil || time.Until(deadline) < 4*time.Minute || time.Until(deadline) > transferReconnect {
		t.Fatal("invalid recovery deadline", offline)
	}
	h := f.app.transfers
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if len(h.rooms) != 1 || h.relays != 0 || h.folderReservations != 0 {
			t.Fatal("offline room retained transfer slots")
		}
		for _, room := range h.rooms {
			if file := room.files[id]; file.state != "failed" || file.downloadToken != "" || file.uploadToken != "" {
				t.Fatal("old transfer remained usable")
			}
		}
	}()
	f.request(t, "GET", download, "", nil, nil, 404)
	a.send(map[string]any{"type": "text", "text": "while-offline"})
	a.next("error")
	resumed := reconnectSocket(t, server, b.token)
	if resumed.next("ready")["paired"] != true {
		t.Fatal("recovery lost pairing")
	}
	state := resumed.next("sync")
	if state["files"].([]any)[0].(map[string]any)["state"] != "failed" {
		t.Fatal("missing terminal task snapshot", state)
	}
	resumed.next("paired")
	a.next("paired")
	resumed.send(map[string]any{"type": "text", "text": "restored"})
	if a.next("text")["text"] != "restored" {
		t.Fatal("restored message failed")
	}
	resumed.next("text")
	// 恢复不是续传：重新提供文件会生成新的编号和票据。
	newID, newDownload := offerRelay(t, a, resumed, 10)
	if newID == id || newDownload == download {
		t.Fatal("recovery reused old transfer")
	}
}

func TestTransferReconnectReplacesHalfOpenSocket(t *testing.T) {
	f, server, a, b := transferPair(t)
	for i := 0; i < 3; i++ {
		resumed := reconnectSocket(t, server, b.token)
		resumed.next("ready")
		resumed.next("sync")
		resumed.next("paired")
		a.next("peer-offline")
		a.next("paired")
		resumed.send(map[string]any{"type": "text", "text": "replacement"})
		a.next("text")
		resumed.next("text")
		b = resumed
	}
	f.app.transfers.mu.Lock()
	defer f.app.transfers.mu.Unlock()
	if len(f.app.transfers.rooms) != 1 {
		t.Fatal("old socket cleanup removed replacement")
	}
}

func TestTransferReconnectAfterBothDevicesDisconnect(t *testing.T) {
	f, server, a, b := transferPair(t)
	_ = a.conn.CloseNow()
	b.next("peer-offline")
	_ = b.conn.CloseNow()
	h := f.app.transfers
	// 等待第二条真实网络连接的读循环完成清理，不依赖五分钟的墙钟等待。
	deadline := time.Now().Add(time.Second)
	for {
		h.mu.Lock()
		offline := false
		for _, room := range h.rooms {
			offline = !room.members[0].connected && !room.members[1].connected
		}
		h.mu.Unlock()
		if offline {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second device did not disconnect")
		}
		time.Sleep(time.Millisecond)
	}
	one := reconnectSocket(t, server, a.token)
	one.next("ready")
	one.next("sync")
	one.next("peer-offline")
	two := reconnectSocket(t, server, b.token)
	two.next("ready")
	two.next("sync")
	two.next("paired")
	one.next("paired")
	one.send(map[string]any{"type": "text", "text": "both-restored"})
	if two.next("text")["text"] != "both-restored" {
		t.Fatal("both devices failed to restore")
	}
}

func TestTransferReconnectExpiredAndRevokedCredentials(t *testing.T) {
	for _, scenario := range []string{"expired", "revoked", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			f, server, a, b := transferPair(t)
			_ = b.conn.CloseNow()
			a.next("peer-offline")
			token := b.token
			switch scenario {
			case "expired":
				h := f.app.transfers
				h.mu.Lock()
				for _, room := range h.rooms {
					room.members[1].disconnectedAt = time.Now().Add(-transferReconnect)
				}
				h.mu.Unlock()
			case "revoked":
				if _, err := f.app.db.Exec("DELETE FROM sessions"); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				token = transferSecret()
			}
			resumed := reconnectSocket(t, server, token)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _, err := resumed.conn.Read(ctx)
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatal("invalid recovery accepted", err)
			}
		})
	}
}

func TestTransferReconnectReclaimsFolderAndKeepsFixedDeadline(t *testing.T) {
	f := newFixture(t)
	room := securityTransferRoom(t, f)
	h := f.app.transfers
	h.mu.Lock()
	defer h.mu.Unlock()
	folder := securityFolderLocked(t, h, room, 100, 128, 100)
	h.disconnectMemberLocked(room.members[1], nil)
	first := room.members[1].disconnectedAt
	h.disconnectMemberLocked(room.members[1], nil)
	if room.ctx.Err() != nil || h.folderReservations != 0 || h.relays != 0 || folder.state != "failed" || first.IsZero() || first != room.members[1].disconnectedAt {
		t.Fatal("repeated cleanup changed deadline or retained resources")
	}
	if h.reconnectExpiredLocked(room, first.Add(transferReconnect-time.Nanosecond)) || !h.reconnectExpiredLocked(room, first.Add(transferReconnect)) {
		t.Fatal("recovery is not bounded to five minutes")
	}
}

func TestTransferLeaveDoesNotRetainPair(t *testing.T) {
	f, _, a, b := transferPair(t)
	a.send(map[string]any{"type": "leave"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := b.conn.Read(ctx); err == nil {
		t.Fatal("peer remained connected after leave")
	}
	f.app.transfers.mu.Lock()
	defer f.app.transfers.mu.Unlock()
	if len(f.app.transfers.rooms) != 0 {
		t.Fatal("explicit leave retained room")
	}
}

func TestTransferReconnectConfigurableWindow(t *testing.T) {
	f := newFixture(t)
	// 默认 5 分钟
	if got := f.app.transferSettingValues()["transfer_reconnect_minutes"]; got != "5" {
		t.Fatalf("默认断线恢复时长应为 5 分钟，实际 %q", got)
	}
	// 更新为 30 分钟
	f.app.applyTransferSettings(map[string]string{"transfer_reconnect_minutes": "30"})
	if got := f.app.transferSettingValues()["transfer_reconnect_minutes"]; got != "30" {
		t.Fatalf("更新后断线恢复时长应为 30 分钟，实际 %q", got)
	}
	now := time.Now()
	room := &transferRoom{
		expires: now.Add(2 * time.Hour),
		members: [2]*transferMember{{disconnectedAt: now}, {}},
	}
	f.app.transfers.mu.Lock()
	if f.app.transfers.reconnectExpiredLocked(room, now.Add(29*time.Minute)) {
		f.app.transfers.mu.Unlock()
		t.Fatal("29 分钟不应判定为超时")
	}
	if !f.app.transfers.reconnectExpiredLocked(room, now.Add(30*time.Minute)) {
		f.app.transfers.mu.Unlock()
		t.Fatal("30 分钟应判定为超时")
	}
	f.app.transfers.mu.Unlock()

	// 超出范围的值应被忽略，保持原值 30
	f.app.applyTransferSettings(map[string]string{"transfer_reconnect_minutes": "61"})
	if got := f.app.transferSettingValues()["transfer_reconnect_minutes"]; got != "30" {
		t.Fatalf("非法值不应覆盖原设置，实际 %q", got)
	}
	// 设为 0 表示断线立即结束
	f.app.applyTransferSettings(map[string]string{"transfer_reconnect_minutes": "0"})
	if got := f.app.transferSettingValues()["transfer_reconnect_minutes"]; got != "0" {
		t.Fatalf("应允许设为 0，实际 %q", got)
	}
}

func TestTransferRoomQuotaConfigurableAndWSLinkage(t *testing.T) {
	f := newFixture(t)
	vals := f.app.transferSettingValues()
	if vals["transfer_max_rooms"] != "32" || vals["transfer_max_rooms_per_owner"] != "5" {
		t.Fatalf("默认房间配额应为 32/5，实际 %s/%s", vals["transfer_max_rooms"], vals["transfer_max_rooms_per_owner"])
	}
	f.app.applyTransferSettings(map[string]string{
		"transfer_max_rooms":           "64",
		"transfer_max_rooms_per_owner": "8",
	})
	f.app.transfers.mu.Lock()
	maxConns := f.app.transfers.maxConnectionsLocked()
	maxPerIP := f.app.transfers.maxConnectionsPerIPLocked()
	f.app.transfers.mu.Unlock()
	if maxConns != 64*2+8 {
		t.Fatalf("WebSocket 总连接上限应联动为 %d，实际 %d", 64*2+8, maxConns)
	}
	if maxPerIP != 8*4 {
		t.Fatalf("WebSocket 单 IP 上限应联动为 %d，实际 %d", 8*4, maxPerIP)
	}
	// 非法越界值应被忽略
	f.app.applyTransferSettings(map[string]string{
		"transfer_max_rooms":           "0",
		"transfer_max_rooms_per_owner": "99",
	})
	vals = f.app.transferSettingValues()
	if vals["transfer_max_rooms"] != "64" || vals["transfer_max_rooms_per_owner"] != "8" {
		t.Fatalf("越界值不应覆盖原配额，实际 %s/%s", vals["transfer_max_rooms"], vals["transfer_max_rooms_per_owner"])
	}
}
