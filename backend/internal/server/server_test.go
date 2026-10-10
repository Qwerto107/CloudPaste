package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testPassword = "a-long-test-password"

type fixture struct {
	app     *App
	handler http.Handler
	admin   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	app, err := New(Config{DataDir: t.TempDir(), AdminUsername: "admin", AdminPassword: testPassword, MaxDownloads: 8, AllowedOrigins: []string{"https://allowed.test"}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	f := &fixture{app: app, handler: app.Handler()}
	login := f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "admin", "password": testPassword}, nil, 200)
	f.admin = "Bearer " + dataMap(t, login)["token"].(string)
	return f
}
func (f *fixture) request(t *testing.T, method, target, auth string, body any, headers map[string]string, status int) *httptest.ResponseRecorder {
	t.Helper()
	var content io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		content = bytes.NewReader(b)
	}
	// 业务回归中的管理员请求使用新入口；边界测试直接调用 handler，不使用此映射。
	if auth != "" && auth == f.admin {
		if strings.HasPrefix(target, "/api/") && !isAdminPath(strings.SplitN(target, "?", 2)[0]) {
			target = adminAPIPrefix + strings.TrimPrefix(target, "/api")
		} else if target == "/dav" || strings.HasPrefix(target, "/dav/") {
			target = adminAPIPrefix + target
		}
	}
	r := httptest.NewRequest(method, target, content)
	r.RemoteAddr = "127.0.0.1:1234"
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, target, w.Code, status, w.Body.String())
	}
	return w
}
func dataMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result struct{ Data map[string]any }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data == nil {
		t.Fatalf("missing data: %s", w.Body.String())
	}
	return result.Data
}
func (f *fixture) key(t *testing.T, name, basicPath string, permissions int) (string, string) {
	t.Helper()
	w := f.request(t, "POST", "/api/admin/api-keys", f.admin, map[string]any{"name": name, "basic_path": basicPath, "permissions": permissions}, nil, 201)
	data := dataMap(t, w)
	return "ApiKey " + data["key"].(string), data["id"].(string)
}
func (f *fixture) local(t *testing.T, root, mountPath string) (string, string) {
	t.Helper()
	storage := dataMap(t, f.request(t, "POST", "/api/storage", f.admin, map[string]any{"name": "local", "storage_type": "LOCAL", "root_path": root, "is_public": true}, nil, 201))
	id := storage["id"].(string)
	mount := dataMap(t, f.request(t, "POST", "/api/mount/create", f.admin, map[string]any{"name": "disk", "storage_config_id": id, "mount_path": mountPath}, nil, 201))
	return id, mount["id"].(string)
}
func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapAndEncryptedConfiguration(t *testing.T) {
	_, err := New(Config{DataDir: t.TempDir(), AdminUsername: "admin", AdminPassword: "admin123"}, nil)
	if err == nil {
		t.Fatal("weak initial password accepted")
	}
	f := newFixture(t)
	encrypted, err := f.app.encrypt("remote-secret")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := f.app.decrypt(encrypted)
	if err != nil || plain != "remote-secret" {
		t.Fatalf("decrypt: %q %v", plain, err)
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encrypted, "gcm:v1:"))
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 1
	if _, err = f.app.decrypt("gcm:v1:" + base64.RawURLEncoding.EncodeToString(b)); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err = f.app.decrypt("plaintext"); err == nil {
		t.Fatal("plaintext credential accepted")
	}
	token := f.app.ticket(Ticket{Purpose: "share", Path: "abc", Subject: "id", Expires: time.Now().Add(time.Minute).Unix()})
	if f.app.verifyTicket(token, "file", "abc", "id", "") {
		t.Fatal("ticket reused across purposes")
	}
	if f.app.verifyTicket(token, "share", "other", "id", "") {
		t.Fatal("ticket reused across paths")
	}
}

func TestLocalAuthorizationRangeAndCredentialRevocation(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "public/file.txt", "0123456789")
	writeFile(t, root, "private/secret.txt", "secret")
	f.local(t, root, "/disk")
	auth, id := f.key(t, "limited", "/disk/public", MountView|FileShare|FileManage|WebDAVRead)
	f.request(t, "GET", "/api/fs/content?path=/disk/private/secret.txt", auth, nil, nil, 403)
	f.request(t, "GET", "/api/fs/content?path="+url.QueryEscape("/disk/public/../private/secret.txt"), auth, nil, nil, 400)
	f.request(t, "GET", "/api/fs/list?path=/disk", auth, nil, nil, 200)
	f.request(t, "PROPFIND", "/dav", auth, nil, map[string]string{"Depth": "1"}, 404)
	info := dataMap(t, f.request(t, "GET", "/api/fs/get?path=/disk/public/file.txt", auth, nil, nil, 200))
	link := info["previewUrl"].(string)
	w := f.request(t, "GET", link, "", nil, map[string]string{"Range": "bytes=2-5"}, 206)
	if w.Body.String() != "2345" || w.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatal("range response incorrect", w.Body.String(), w.Header())
	}
	f.request(t, "HEAD", link, "", nil, nil, 200)
	f.request(t, "GET", strings.Split(link, "?")[0], "", nil, nil, 403)
	share := dataMap(t, f.request(t, "POST", "/api/fs/create-share", auth, map[string]any{"path": "/disk/public/file.txt"}, nil, 201))
	f.request(t, "PUT", "/api/admin/api-keys/"+id, f.admin, map[string]any{"is_enable": false}, nil, 200)
	f.request(t, "GET", link, "", nil, nil, 403)
	f.request(t, "GET", "/api/share/get/"+share["slug"].(string), "", nil, nil, 403)
	// 管理员退出后，由该会话生成的临时文件链接也立即失效。
	adminLink := dataMap(t, f.request(t, "GET", "/api/fs/file-link?path=/disk/public/file.txt", f.admin, nil, nil, 200))["url"].(string)
	f.request(t, "POST", "/api/admin/logout", f.admin, nil, nil, 200)
	f.request(t, "GET", adminLink, "", nil, nil, 403)
}

func TestSourceChangeRevokesLinksAndShares(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "file.txt", "original")
	writeFile(t, root, "replacement/file.txt", "replacement")
	_, mountID := f.local(t, root, "/disk")
	link := dataMap(t, f.request(t, "GET", "/api/fs/file-link?path=/disk/file.txt", f.admin, nil, nil, 200))["url"].(string)
	share := dataMap(t, f.request(t, "POST", "/api/fs/create-share", f.admin, map[string]any{"path": "/disk/file.txt"}, nil, 201))
	f.request(t, "PUT", "/api/mount/"+mountID, f.admin, map[string]any{"root_path": "/replacement"}, nil, 200)
	f.request(t, "GET", link, "", nil, nil, 403)
	f.request(t, "GET", "/api/share/get/"+share["slug"].(string), "", nil, nil, 410)
	w := f.request(t, "GET", "/api/fs/content?path=/disk/file.txt", f.admin, nil, nil, 200)
	if w.Body.String() != "replacement" {
		t.Fatal("new mount root was not applied")
	}
}

func TestDirectoryPasswordCoversAllFileEntries(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "locked/file.txt", "protected")
	f.local(t, root, "/disk")
	auth, _ := f.key(t, "reader", "/", MountView|FileShare|WebDAVRead)
	meta := dataMap(t, f.request(t, "POST", "/api/fs-meta/create", f.admin, map[string]any{"path": "/disk/locked", "password": "directory-password", "passwordInherit": true}, nil, 201))
	for _, endpoint := range []string{"list", "get", "content", "download", "file-link"} {
		target := "/disk/locked/file.txt"
		if endpoint == "list" {
			target = "/disk/locked"
		}
		f.request(t, "GET", "/api/fs/"+endpoint+"?path="+target, auth, nil, nil, 403)
	}
	f.request(t, "POST", "/api/fs/create-share", auth, map[string]any{"path": "/disk/locked/file.txt"}, nil, 403)
	f.request(t, "GET", "/dav/disk/locked/file.txt", auth, nil, nil, 404)
	verified := dataMap(t, f.request(t, "POST", "/api/fs/meta/password/verify", auth, map[string]any{"path": "/disk/locked", "password": "directory-password"}, nil, 200))
	headers := map[string]string{"X-FS-Path-Token": verified["token"].(string)}
	f.request(t, "GET", "/api/fs/content?path=/disk/locked/file.txt", auth, nil, headers, 200)
	link := dataMap(t, f.request(t, "GET", "/api/fs/file-link?path=/disk/locked/file.txt", auth, nil, headers, 200))["url"].(string)
	f.request(t, "GET", link, "", nil, nil, 200)
	share := dataMap(t, f.request(t, "POST", "/api/fs/create-share", auth, map[string]any{"path": "/disk/locked/file.txt"}, headers, 201))
	f.request(t, "PUT", "/api/fs-meta/"+meta["id"].(string), f.admin, map[string]any{"password": "replacement-password"}, nil, 200)
	f.request(t, "GET", link, "", nil, nil, 403)
	f.request(t, "GET", "/api/fs/content?path=/disk/locked/file.txt", auth, nil, headers, 403)
	f.request(t, "GET", "/api/share/get/"+share["slug"].(string), "", nil, nil, 410)
}

func TestSharePasswordAndLastViewLease(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "file.txt", "file contents")
	f.local(t, root, "/disk")
	s := dataMap(t, f.request(t, "POST", "/api/fs/create-share", f.admin, map[string]any{"path": "/disk/file.txt", "password": "share-password", "max_views": 1}, nil, 201))
	slug := s["slug"].(string)
	w := f.request(t, "GET", "/api/share/get/"+slug, "", nil, nil, 200)
	if strings.Contains(w.Body.String(), "previewUrl") || strings.Contains(w.Body.String(), "file contents") {
		t.Fatal("protected share exposed download details")
	}
	f.request(t, "GET", "/api/s/"+slug, "", nil, nil, 403)
	f.request(t, "POST", "/api/share/verify/"+slug, "", map[string]any{"password": "wrong"}, nil, 403)
	verified := dataMap(t, f.request(t, "POST", "/api/share/verify/"+slug, "", map[string]any{"password": "share-password"}, nil, 200))
	link := verified["previewUrl"].(string)
	f.request(t, "GET", link, "", nil, map[string]string{"Range": "bytes=0-3"}, 206)
	f.request(t, "GET", link, "", nil, map[string]string{"Range": "bytes=4-7"}, 206)
	f.request(t, "POST", "/api/share/verify/"+slug, "", map[string]any{"password": "share-password"}, nil, 410)
	f.request(t, "PUT", "/api/files/"+s["id"].(string), f.admin, map[string]any{"password": "new-share-password"}, nil, 200)
	f.request(t, "GET", link, "", nil, nil, 403)
	f.request(t, "DELETE", "/api/files/batch-delete", f.admin, map[string]any{"ids": []string{s["id"].(string)}, "delete_mode": "both"}, nil, 501)
	f.request(t, "DELETE", "/api/files/batch-delete", f.admin, map[string]any{"ids": []string{s["id"].(string)}, "delete_mode": "record_only"}, nil, 200)
	if _, err := os.Stat(filepath.Join(root, "file.txt")); err != nil {
		t.Fatal("original file removed", err)
	}
}

func TestPastePasswordOwnershipAndConcurrentViews(t *testing.T) {
	f := newFixture(t)
	auth, _ := f.key(t, "writer", "/", TextShare|TextManage)
	other, _ := f.key(t, "other", "/", TextShare|TextManage)
	p := dataMap(t, f.request(t, "POST", "/api/paste", auth, map[string]any{"content": "private text", "password": "paste-password", "max_views": 2}, nil, 201))
	slug := p["slug"].(string)
	f.request(t, "GET", "/api/pastes/"+p["id"].(string), other, nil, nil, 404)
	f.request(t, "GET", "/api/raw/"+slug, "", nil, nil, 403)
	w := f.request(t, "GET", "/api/paste/"+slug, "", nil, nil, 200)
	if strings.Contains(w.Body.String(), "private text") {
		t.Fatal("password bypass")
	}
	f.request(t, "POST", "/api/paste/"+slug, "", map[string]any{"password": "paste-password"}, nil, 200)
	plain := dataMap(t, f.request(t, "POST", "/api/paste", f.admin, map[string]any{"content": "last view", "max_views": 1}, nil, 201))
	target := "/api/paste/" + plain["slug"].(string)
	var successes atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Go(func() {
			r := httptest.NewRequest("GET", target, nil)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code == 200 {
				successes.Add(1)
			} else if w.Code != 410 {
				t.Errorf("unexpected concurrent status: %d %s", w.Code, w.Body.String())
			}
		})
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("max_views=1 allowed %d successes", successes.Load())
	}
	f.request(t, "GET", "/api/pastes", auth, nil, nil, 200)
}

func directoryLink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	// Windows 普通权限可能无法建立符号链接，用目录联接测试同样的逃逸路径。
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("无法创建测试链接: %v %s", err, output)
	}
}
func TestLocalLinksCannotEscapeStorageOrAuthorizedSubdirectory(t *testing.T) {
	f := newFixture(t)
	root, outside := t.TempDir(), t.TempDir()
	writeFile(t, outside, "secret.txt", "outside")
	writeFile(t, root, "private/secret.txt", "private")
	writeFile(t, root, "public/file.txt", "public")
	directoryLink(t, filepath.Join(root, "public", "escape"), outside)
	directoryLink(t, filepath.Join(root, "public", "sibling"), filepath.Join(root, "private"))
	f.local(t, root, "/disk")
	auth, _ := f.key(t, "scoped", "/disk/public", MountView)
	f.request(t, "GET", "/api/fs/content?path=/disk/public/escape/secret.txt", auth, nil, nil, 403)
	f.request(t, "GET", "/api/fs/content?path=/disk/public/sibling/secret.txt", auth, nil, nil, 403)
}

func TestStorageACLAndExpiredDownloadCredential(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "file.txt", "body")
	storage, _ := f.local(t, root, "/disk")
	auth, id := f.key(t, "reader", "/", MountView)
	f.request(t, "PUT", "/api/admin/api-keys/"+id+"/storage-acl", f.admin, map[string]any{"storage_config_ids": []string{storage}}, nil, 200)
	f.local(t, t.TempDir(), "/other")
	f.request(t, "GET", "/api/fs/list?path=/other", auth, nil, nil, 403)
	link := dataMap(t, f.request(t, "GET", "/api/fs/file-link?path=/disk/file.txt", auth, nil, nil, 200))["url"].(string)
	f.request(t, "PUT", "/api/admin/api-keys/"+id, f.admin, map[string]any{"expires_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}, nil, 200)
	f.request(t, "GET", link, "", nil, nil, 403)
}

func TestHTTPDefaultsAndUnsupportedUploads(t *testing.T) {
	f := newFixture(t)
	f.request(t, "POST", "/api/share/upload", f.admin, nil, nil, 400)
	f.request(t, "POST", "/api/proxy/link", "", map[string]any{"url": "http://localhost"}, nil, 401)
	f.request(t, "POST", "/api/proxy/link", f.admin, map[string]any{"url": "http://localhost"}, nil, 501)
	w := f.request(t, "OPTIONS", "/api/fs/get", "", nil, map[string]string{"Origin": "https://evil.test"}, 204)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("untrusted CORS origin allowed")
	}
	w = f.request(t, "OPTIONS", "/api/fs/get", "", nil, map[string]string{"Origin": "https://allowed.test"}, 204)
	if w.Header().Get("Access-Control-Allow-Origin") != "https://allowed.test" {
		t.Fatal("allowed CORS origin missing")
	}
	f.request(t, "GET", "/api/p/x/../secret", "", nil, nil, 400)
	f.request(t, "PUT", "/api/admin/settings/group/4", f.admin, map[string]any{"site_custom_body": "<script>alert(1)</script>"}, nil, 200)
	w = f.request(t, "GET", "/api/public/settings?group=4", "", nil, nil, 200)
	if !strings.Contains(w.Body.String(), `"site_upload_page_enabled","value":"true"`) {
		t.Fatal("upload page missing")
	}
	r := httptest.NewRequest("POST", "/api/admin/paste", strings.NewReader(`{"content":"`+strings.Repeat("a", 2<<20)+`"}`))
	r.Header.Set("Authorization", f.admin)
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("oversized body accepted", w.Code)
	}
}

func TestDatabaseAndEncryptionKeySurviveRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, AdminUsername: "admin", AdminPassword: testPassword}
	app, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := passwordHash("saved-password")
	if err != nil {
		t.Fatal(err)
	}
	p := Paste{ID: "id", Slug: "saved", Content: "persisted", Password: hash, Public: 1}
	if err = app.saveResource(context.Background(), "pastes", p.ID, p.Slug, "owner", p); err != nil {
		t.Fatal(err)
	}
	encrypted, err := app.encrypt("credential")
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.AdminPassword = ""
	app, err = New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var saved Paste
	if err = app.resource(context.Background(), "pastes", "slug", "saved", &saved); err != nil || saved.Content != "persisted" {
		t.Fatal("data did not persist", err)
	}
	value, err := app.decrypt(encrypted)
	if err != nil || value != "credential" {
		t.Fatal("key did not persist", err)
	}
}
