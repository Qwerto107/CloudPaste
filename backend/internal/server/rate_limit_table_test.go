package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRateLimitTableEvictionAndExpiry(t *testing.T) {
	table := newRateLimitTable(2)
	now := time.Now()
	if !table.allow("a", 2, now) || !table.allow("b", 2, now) || !table.allow("a", 2, now.Add(time.Second)) {
		t.Fatal("fresh budget rejected")
	}
	if !table.allow("c", 2, now.Add(2*time.Second)) || table.byKey["b"] != nil || len(table.byKey) != 2 {
		t.Fatal("LRU did not evict the least recently used source")
	}
	if table.allow("a", 2, now.Add(3*time.Second)) || !table.blocked("a", 2, now.Add(4*time.Second)) {
		t.Fatal("live budget was reset by another source")
	}
	table.prune(now.Add(time.Minute))
	if table.byKey["a"] != nil || table.byKey["c"] == nil || table.order.Len() != len(table.byKey) {
		t.Fatal("expiry did not keep LRU and counters consistent")
	}
	if !table.allow("c", 2, now.Add(time.Minute+2*time.Second)) || table.byKey["c"].Count != 1 {
		t.Fatal("expired counter was not reset on access")
	}
	for i := 0; i < 4096; i++ {
		if !table.allow(fmt.Sprint(i), 1, now.Add(2*time.Minute)) || len(table.byKey) > 2 || table.order.Len() != len(table.byKey) {
			t.Fatal("new sources blocked or table became unbounded")
		}
	}
}

func TestRateLimitIP(t *testing.T) {
	for value, want := range map[string]string{
		"198.51.100.1": "198.51.100.1", "::ffff:198.51.100.1": "198.51.100.1",
		"2001:db8:1:2::1": "2001:db8:1:2::/64", "2001:db8:1:2:ffff::abcd": "2001:db8:1:2::/64",
		"fe80::1%eth0": "fe80::/64", "unknown": "unknown",
	} {
		if got := rateLimitIP(value); got != want {
			t.Fatalf("%s: got %s, want %s", value, got, want)
		}
	}
}

func TestTransferIPv6RatePrefix(t *testing.T) {
	f := newFixture(t)
	f.app.cfg.RateLimits.TransferJoinPerIP = 2
	for _, step := range []struct {
		ip     string
		status int
	}{
		{"2001:db8:1:2::1", 404}, {"2001:db8:1:2::2", 404},
		{"2001:db8:1:2:ffff::abcd", 429}, {"2001:db8:1:3::1", 404},
	} {
		w := rateLimitRequest(t, f, "POST", "/api/transfer/join", step.ip, "", map[string]any{"code": "A1234", "name": "test"})
		requireRateStatus(t, w, step.status)
	}
}

func TestTransferInvalidTicketsCannotFillNormalRateTable(t *testing.T) {
	f := newFixture(t)
	h := f.app.transfers
	ticket := strings.Repeat("a", 43)
	for i := 0; i < 300; i++ {
		ip := fmt.Sprintf("198.18.%d.%d", i/256, i%256)
		requireRateStatus(t, rateLimitRequest(t, f, "PUT", "/api/transfer/upload/missing", ip, "Transfer "+ticket, nil), 404)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	room := &transferRoom{id: randomID(), ctx: ctx, cancel: cancel, created: time.Now(), expires: time.Now().Add(time.Hour), files: map[string]*transferFile{}}
	room.members[0] = &transferMember{room: room, index: 0, out: make(chan any, 4)}
	room.members[1] = &transferMember{room: room, index: 1, out: make(chan any, 4)}
	file := &transferFile{id: randomID(), room: room, ctx: ctx, mode: "relay", state: "active", size: 1, downloadToken: secretHash(ticket)}
	room.files[file.id] = file
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if len(h.limits.byKey) != 0 || len(h.probes.byKey) != 256 {
			t.Fatal("invalid ticket probes consumed normal entries or grew unbounded")
		}
		h.rooms[room.id] = room
		// 同一来源的探测额度耗尽后，合法票据仍有独立的正常传输额度。
		for i := 1; i < f.app.cfg.RateLimits.TransferRelayPerIP; i++ {
			h.invalidRelayLocked("198.18.1.43")
		}
		if err := h.invalidRelayLocked("198.18.1.43"); err.(*apiError).Status != 429 {
			t.Fatal("probe limit not enforced", err)
		}
	}()
	r := httptest.NewRequest("GET", "/api/transfer/download/"+file.id+"?ticket="+ticket, nil)
	r.RemoteAddr = "198.18.1.43:1234"
	r.Header.Set("Origin", "https://allowed.test")
	r.SetPathValue("id", file.id)
	if _, err := f.app.claimTransfer(r, true); err != nil {
		t.Fatal("valid ticket blocked by probe traffic", err)
	}
}
