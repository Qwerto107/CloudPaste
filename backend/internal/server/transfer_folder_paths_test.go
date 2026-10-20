package server

import "testing"

func TestFolderDuplicatePaths(t *testing.T) {
	f := newFixture(t)
	room := securityTransferRoom(t, f)
	h := f.app.transfers
	h.mu.Lock()
	defer h.mu.Unlock()
	folder := securityFolderLocked(t, h, room, 10000, 16, 0)
	offer := func(path string, directory bool) error {
		return h.messageLocked(room.members[0], transferMessage{Type: "offer", ClientID: randomID(), Name: path, Folder: &transferFolderEntry{ID: folder.id, Path: path, Directory: directory}})
	}
	if err := offer("Data", true); err != nil {
		t.Fatal(err)
	}
	for _, file := range room.files {
		h.finishLocked(file, "complete")
	}
	for _, directory := range []bool{true, false} {
		if err := offer("data", directory); err == nil {
			t.Fatal("completed path or file/directory collision was accepted")
		}
	}
	if folder.offered != 1 || len(folder.paths) != 1 {
		t.Fatal("duplicate consumed queue credit or hash entries")
	}
	if err := offer("different", true); err != nil {
		t.Fatal(err)
	}
	h.finishFolderLocked(room, folder, "cancelled")
	if folder.paths != nil {
		t.Fatal("completed folder retained path hashes")
	}
	for _, pair := range [][2]string{{"A/File.txt", "a/FILE.TXT"}, {"Σ.txt", "ς.TXT"}, {"K.txt", "K.TXT"}} {
		if transferPathHash(pair[0]) != transferPathHash(pair[1]) {
			t.Fatal("Unicode case equivalent paths have different keys", pair)
		}
	}
}
