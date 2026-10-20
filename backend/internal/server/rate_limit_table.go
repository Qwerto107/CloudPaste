package server

import (
	"container/list"
	"net/netip"
	"time"
)

type rateLimitEntry struct {
	attemptWindow
	key      string
	position *list.Element
}

// 调用方持锁；固定容量的 LRU 表在满载时仍接纳新来源，不增加后台任务。
type rateLimitTable struct {
	capacity int
	byKey    map[string]*rateLimitEntry
	order    list.List
}

func newRateLimitTable(capacity int) *rateLimitTable {
	return &rateLimitTable{capacity: capacity, byKey: make(map[string]*rateLimitEntry)}
}

func (t *rateLimitTable) remove(entry *rateLimitEntry) {
	delete(t.byKey, entry.key)
	t.order.Remove(entry.position)
}

func (t *rateLimitTable) entry(key string, now time.Time) *rateLimitEntry {
	entry := t.byKey[key]
	if entry != nil {
		if now.Sub(entry.Started) >= time.Minute {
			t.remove(entry)
			return nil
		}
		t.order.MoveToFront(entry.position)
	}
	return entry
}

func (t *rateLimitTable) blocked(key string, limit int, now time.Time) bool {
	entry := t.entry(key, now)
	return entry != nil && entry.Count >= limit
}

func (t *rateLimitTable) allow(key string, limit int, now time.Time) bool {
	entry := t.entry(key, now)
	if entry == nil {
		if len(t.byKey) >= t.capacity {
			t.remove(t.order.Back().Value.(*rateLimitEntry))
		}
		entry = &rateLimitEntry{attemptWindow: attemptWindow{Started: now}, key: key}
		entry.position = t.order.PushFront(entry)
		t.byKey[key] = entry
	}
	if entry.Count >= limit {
		return false
	}
	entry.Count++
	return true
}

func (t *rateLimitTable) prune(now time.Time) {
	for _, entry := range t.byKey {
		if now.Sub(entry.Started) >= time.Minute {
			t.remove(entry)
		}
	}
}

func rateLimitIP(value string) string {
	address, err := netip.ParseAddr(value)
	if err != nil {
		return value
	}
	address = address.Unmap().WithZone("")
	if address.Is6() {
		// 防止在同一 IPv6 网段轮换地址，绕过按来源计数并占满限频表。
		return netip.PrefixFrom(address, 64).Masked().String()
	}
	return address.String()
}
