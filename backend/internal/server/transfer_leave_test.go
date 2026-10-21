package server

import (
	"context"
	"testing"
	"time"
)

func TestTransferPageLeaveEndsOfflinePair(t *testing.T) {
	f, _, a, b := transferPair(t)
	id, download := offerRelay(t, a, b, 10)
	_ = b.conn.CloseNow()
	a.next("peer-offline")
	headers := map[string]string{"Origin": "https://allowed.test", "Content-Type": "text/plain;charset=UTF-8"}
	f.request(t, "POST", "/api/transfer/leave", "", map[string]any{"token": b.token}, headers, 204)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := a.conn.Read(ctx); err == nil {
		t.Fatal("peer remained connected after page leave")
	}
	h := f.app.transfers
	h.mu.Lock()
	if len(h.rooms) != 0 || h.relays != 0 || h.folderReservations != 0 {
		t.Error("page leave retained room or transfer capacity", id)
	}
	h.mu.Unlock()
	f.request(t, "GET", download, "", nil, nil, 404)
	// WebSocket leave 和页面离开通知可以乱序到达，重复通知必须无害。
	f.request(t, "POST", "/api/transfer/leave", "", map[string]any{"token": b.token}, headers, 204)
}

func TestTransferPageLeaveRequiresOriginAndMemberToken(t *testing.T) {
	f := newFixture(t)
	room, other := securityTransferRoom(t, f), securityTransferRoom(t, f)
	token := transferSecret()
	room.members[0].token = secretHash(token)
	other.members[0].token = secretHash(transferSecret())
	body := map[string]any{"token": token}
	headers := map[string]string{"Origin": "https://allowed.test"}
	f.request(t, "POST", "/api/transfer/leave", "", body, nil, 403)
	f.request(t, "POST", "/api/transfer/leave", "", body, map[string]string{"Origin": "https://evil.test"}, 403)
	f.request(t, "POST", "/api/transfer/leave", "", map[string]any{"token": "short"}, headers, 400)
	f.request(t, "POST", "/api/transfer/leave", "", map[string]any{"token": token, "room": other.id}, headers, 400)
	f.request(t, "POST", "/api/transfer/leave", "", map[string]any{"token": transferSecret()}, headers, 204)
	if room.ctx.Err() != nil || other.ctx.Err() != nil {
		t.Fatal("invalid notification ended a room")
	}
	f.request(t, "POST", "/api/transfer/leave", "", body, headers, 204)
	if room.ctx.Err() == nil || other.ctx.Err() != nil {
		t.Fatal("page leave did not isolate the token's room")
	}
}

func TestTransferPageLeaveRateLimit(t *testing.T) {
	f := newFixture(t)
	limit := f.app.runtimeLimits().RateLimits.TransferSocketPerIP
	h := f.app.transfers
	h.mu.Lock()
	for range limit {
		if !h.allowIPLocked("leave", "127.0.0.1", limit) {
			t.Fatal("unexpected initial rate limit")
		}
	}
	h.mu.Unlock()
	f.request(t, "POST", "/api/transfer/leave", "", map[string]any{"token": transferSecret()}, map[string]string{"Origin": "http://example.com"}, 429)
}
