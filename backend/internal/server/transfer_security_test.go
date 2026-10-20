package server

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func securityTransferRoom(t *testing.T, f *fixture) *transferRoom {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	room := &transferRoom{id: randomID(), ctx: ctx, cancel: cancel, paired: true, relayOnly: true, maxFile: 1 << 20, created: time.Now(), expires: time.Now().Add(time.Hour), files: map[string]*transferFile{}, folders: map[string]*transferFolder{}}
	for i := range room.members {
		room.members[i] = &transferMember{room: room, index: i, connected: true, out: make(chan any, 1024)}
	}
	f.app.transfers.mu.Lock()
	f.app.transfers.rooms[room.id] = room
	f.app.transfers.mu.Unlock()
	return room
}

func securityFolderLocked(t *testing.T, h *transferHub, room *transferRoom, count, concurrency int, size int64) *transferFolder {
	t.Helper()
	if err := h.messageLocked(room.members[0], transferMessage{Type: "folder-offer", ClientID: randomID(), Name: "folder", Count: count, Size: size}); err != nil {
		t.Fatal(err)
	}
	var folder *transferFolder
	for _, item := range room.folders {
		folder = item
	}
	if err := h.messageLocked(room.members[1], transferMessage{Type: "accept-folder", ID: folder.id, Mode: "relay", Concurrency: concurrency}); err != nil {
		t.Fatal(err)
	}
	return folder
}

func TestFolderReservationLeavesCapacityAndExpires(t *testing.T) {
	f := newFixture(t)
	room, ordinary := securityTransferRoom(t, f), securityTransferRoom(t, f)
	h := f.app.transfers
	h.mu.Lock()
	defer h.mu.Unlock()
	h.maxRelays = 4
	folder := securityFolderLocked(t, h, room, 100, 128, 100)
	if h.folderReservations != 2 || folder.pending != 2 {
		t.Fatal("folder reserved all capacity")
	}
	file := &transferFile{id: randomID(), room: ordinary, state: "offered", size: 1, sender: 0}
	ordinary.files[file.id] = file
	if err := h.messageLocked(ordinary.members[1], transferMessage{Type: "accept", ID: file.id, Mode: "relay"}); err != nil {
		t.Fatal("ordinary transfer blocked by idle folder", err)
	}
	folder.queuedAt, folder.progress = time.Now().Add(-2*time.Minute), time.Now().Add(-2*time.Minute)
	h.expireFoldersLocked(room, time.Now())
	if folder.state != "failed" || folder.pending != 0 || h.folderReservations != 0 || h.relays != 1 || file.state != "active" {
		t.Fatal("expiry did not reclaim only stale folder reservations")
	}
	h.expireFoldersLocked(room, time.Now())
	if h.folderReservations != 0 {
		t.Fatal("repeated expiry released reservations twice")
	}
}

func TestFolderReservationBudgetSharedAcrossRooms(t *testing.T) {
	f := newFixture(t)
	one, two := securityTransferRoom(t, f), securityTransferRoom(t, f)
	h := f.app.transfers
	h.mu.Lock()
	defer h.mu.Unlock()
	h.maxRelays = 8
	first := securityFolderLocked(t, h, one, 100, 128, 100)
	second := securityFolderLocked(t, h, two, 100, 128, 100)
	if first.pending+second.pending != 4 || h.folderReservations != 4 {
		t.Fatal("multiple folders bypassed shared reservation cap")
	}
	second.progress = time.Now().Add(-time.Hour)
	h.expireFoldersLocked(two, time.Now())
	if second.state != "active" {
		t.Fatal("folder waiting for capacity was cancelled")
	}
	h.finishFolderLocked(one, first, "cancelled")
	h.dispatchFolderQueuesLocked()
	h.expireFoldersLocked(two, time.Now())
	if second.state != "active" || second.pending == 0 {
		t.Fatal("newly granted credits had no idle grace period")
	}
}

func TestFolderReservationDoesNotReduceActiveConcurrency(t *testing.T) {
	for _, maximum := range []int{1, 64, 128, 0} {
		t.Run(fmt.Sprint(maximum), func(t *testing.T) {
			f := newFixture(t)
			room := securityTransferRoom(t, f)
			h := f.app.transfers
			h.mu.Lock()
			defer h.mu.Unlock()
			h.maxRelays = maximum
			folder := securityFolderLocked(t, h, room, 256, 128, 256)
			want := 128
			if maximum > 0 {
				want = min(want, maximum)
			}
			for i := 0; i < want; i++ {
				name := fmt.Sprintf("file-%d", i)
				if err := h.messageLocked(room.members[0], transferMessage{Type: "offer", ClientID: name, Name: name, Size: 1, Folder: &transferFolderEntry{ID: folder.id, Path: name}}); err != nil {
					t.Fatal(err)
				}
				var offered *transferFile
				for _, file := range room.files {
					if file.state == "offered" {
						offered = file
						break
					}
				}
				if offered == nil {
					t.Fatal("rolling window failed to refill")
				}
				if err := h.messageLocked(room.members[1], transferMessage{Type: "accept", ID: offered.id, Mode: "relay"}); err != nil {
					t.Fatal(err)
				}
			}
			if h.relays != want {
				t.Fatalf("active concurrency reduced: got %d, want %d", h.relays, want)
			}
			folder.progress, folder.queuedAt = time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)
			h.expireFoldersLocked(room, time.Now())
			if folder.state != "active" {
				t.Fatal("healthy in-flight files were cancelled")
			}
		})
	}
}

func TestFolderAdjustmentExpiresDespiteOtherActivity(t *testing.T) {
	f := newFixture(t)
	room := securityTransferRoom(t, f)
	h := f.app.transfers
	h.mu.Lock()
	defer h.mu.Unlock()
	folder := securityFolderLocked(t, h, room, 20, 8, 20)
	if err := h.messageLocked(room.members[1], transferMessage{Type: "folder-concurrency", ID: folder.id, Concurrency: 2}); err != nil {
		t.Fatal(err)
	}
	folder.adjustingAt = time.Now().Add(-2 * time.Minute)
	folder.progress = time.Now()
	h.expireFoldersLocked(room, time.Now())
	if folder.state != "failed" || h.folderReservations != 0 || folder.adjusting {
		t.Fatal("stuck adjustment retained reservations")
	}
}
