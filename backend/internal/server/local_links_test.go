package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 根内相对链接也不能绕过目录密码，覆盖浏览、读取、分享和上传入口。
func TestLocalRelativeLinksCannotBypassDirectoryPassword(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "locked/secret.txt", "protected-content")
	writeFile(t, root, "public/file.txt", "public-content")
	if err := os.Symlink(filepath.FromSlash("../locked"), filepath.Join(root, "public", "alias")); err != nil {
		t.Skipf("无法创建相对目录链接: %v", err)
	}
	if err := os.Symlink(filepath.FromSlash("../locked/secret.txt"), filepath.Join(root, "public", "alias.txt")); err != nil {
		t.Skipf("无法创建相对文件链接: %v", err)
	}
	f.local(t, root, "/disk")
	auth, _ := f.key(t, "reader-writer", "/disk", MountView|FileShare|MountUpload|WebDAVRead)
	f.request(t, "POST", "/api/fs-meta/create", f.admin, map[string]any{"path": "/disk/locked", "password": "directory-password", "passwordInherit": true}, nil, 201)
	f.request(t, "GET", "/api/fs/content?path=/disk/locked/secret.txt", auth, nil, nil, 403)
	f.request(t, "GET", "/api/fs/list?path=/disk/public/alias", auth, nil, nil, 403)
	for _, target := range []string{"/disk/public/alias/secret.txt", "/disk/public/alias.txt"} {
		for _, entry := range []string{"get", "content", "download", "file-link"} {
			f.request(t, "GET", "/api/fs/"+entry+"?path="+target, auth, nil, nil, 403)
		}
		f.request(t, "GET", "/dav"+target, auth, nil, nil, 404)
		f.request(t, "POST", "/api/fs/create-share", auth, map[string]any{"path": target}, nil, 403)
	}
	listing := f.request(t, "GET", "/api/fs/list?path=/disk/public", auth, nil, nil, 200)
	if strings.Contains(listing.Body.String(), `"name":"alias"`) || strings.Contains(listing.Body.String(), `"name":"alias.txt"`) {
		t.Fatal("目录列表暴露了被拒绝的链接")
	}
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/public/alias", auth, strings.NewReader("bad"), uploadHeaders("new.txt", nil, false), 403)
	if _, err := os.Stat(filepath.Join(root, "locked", "new.txt")); !os.IsNotExist(err) {
		t.Fatal("上传经链接写入了受保护目录")
	}
	// 链接位于授权根自身时也必须拒绝，不能在切换作用域时跟随它。
	scoped, _ := f.key(t, "scoped", "/disk/public/alias", MountView|MountUpload)
	f.request(t, "GET", "/api/fs/content?path=/disk/public/alias/secret.txt", scoped, nil, nil, 403)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/public/alias", scoped, strings.NewReader("bad"), uploadHeaders("scoped.txt", nil, false), 403)
}

// 原文件被换成链接后，已经签发的下载链接和匿名分享也不能读取链接目标。
func TestLocalReplacingFileWithLinkRejectsExistingTickets(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "locked/secret.txt", "protected-content")
	writeFile(t, root, "public/file.txt", "public-content")
	f.local(t, root, "/disk")
	auth, _ := f.key(t, "reader", "/disk", MountView|FileShare)
	link := dataMap(t, f.request(t, "GET", "/api/fs/file-link?path=/disk/public/file.txt", auth, nil, nil, 200))["url"].(string)
	share := dataMap(t, f.request(t, "POST", "/api/fs/create-share", auth, map[string]any{"path": "/disk/public/file.txt"}, nil, 201))
	filename := filepath.Join(root, "public", "file.txt")
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.FromSlash("../locked/secret.txt"), filename); err != nil {
		t.Skipf("无法创建相对文件链接: %v", err)
	}
	f.request(t, "GET", link, "", nil, nil, 403)
	f.request(t, "GET", "/api/s/"+share["slug"].(string), "", nil, nil, 403)
}
