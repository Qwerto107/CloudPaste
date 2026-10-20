package server

import "testing"

func TestFolderEntryCountLimit(t *testing.T) {
	f := newFixture(t)
	room := securityTransferRoom(t, f)
	h := f.app.transfers
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, count := range []int{0, 10001, 1000000} {
		if err := h.messageLocked(room.members[0], transferMessage{Type: "folder-offer", ClientID: randomID(), Name: "folder", Count: count}); err == nil {
			t.Fatal("unsafe count accepted", count)
		}
	}
	if folder := securityFolderLocked(t, h, room, 10000, 16, 0); folder.count != 10000 {
		t.Fatal("maximum folder size rejected")
	}
}
