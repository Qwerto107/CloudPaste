package server

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 分享读取仍拒绝根内相对链接，不能通过链接跨入另一个受保护目录。
func TestLocalRelativeLinksCannotBypassDirectoryPassword(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "locked/secret.txt", "protected-content")
	writeFile(t, root, "public/file.txt", "public-content")
	if err := os.Symlink(filepath.FromSlash("../locked"), filepath.Join(root, "public", "alias")); err != nil {
		t.Skipf("无法创建相对目录链接: %v", err)
	}
	if err := os.Symlink(filepath.FromSlash("../locked/secret.txt"), filepath.Join(root, "public", "alias.txt")); err != nil {
		t.Skipf("无法创建相对文件链接: %v", err)
	}
	driver := &localDriver{rootPath: root, scope: "public"}
	for _, key := range []string{"alias/secret.txt", "alias.txt"} {
		if _, err := driver.Stat(context.Background(), key); err == nil {
			t.Fatalf("链接被当作普通文件: %s", key)
		}
		if stream, err := driver.Open(context.Background(), key, httptest.NewRequest("GET", "/", nil)); err == nil {
			stream.Body.Close()
			t.Fatalf("链接目标被读取: %s", key)
		}
	}
}

// 原文件被换成链接后，已创建的匿名分享也不能读取链接目标。
func TestLocalReplacingFileWithLinkRejectsExistingTickets(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "locked/secret.txt", "protected-content")
	writeFile(t, root, "public/file.txt", "public-content")
	f.local(t, root, "/disk")
	auth, _ := f.key(t, "reader", "/disk", MountView|FileShare)
	share := f.legacyShare(t, auth, map[string]any{"path": "/disk/public/file.txt"})
	filename := filepath.Join(root, "public", "file.txt")
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.FromSlash("../locked/secret.txt"), filename); err != nil {
		t.Skipf("无法创建相对文件链接: %v", err)
	}
	f.request(t, "GET", "/api/s/"+share["slug"].(string), "", nil, nil, 403)
}
