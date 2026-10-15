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
	// 直接准备旧版挂载数据，避免测试依赖已移除的管理接口。
	mount := Mount{ID: randomID(), Name: "disk", StorageID: id, Path: mountPath, RootPath: "/", Active: 1, UpdatedAt: nowText()}
	if err := f.app.saveResource(context.Background(), "mounts", mount.ID, "", "", mount); err != nil {
		t.Fatal(err)
	}
	return id, mount.ID
}

// 构造旧版已有文件分享，验证升级后仍能读取并复核历史来源权限。
func (f *fixture) legacyShare(t *testing.T, auth string, fields map[string]any) map[string]any {
	t.Helper()
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Authorization", auth)
	actor, err := f.app.authenticate(r)
	if err != nil || actor == nil {
		t.Fatalf("准备分享身份失败: %v", err)
	}
	r = r.WithContext(withPrincipal(r.Context(), actor))
	target := fields["path"].(string)
	m, storage, driver, key, err := f.app.resolveMount(r, target, MountView, false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := driver.Stat(r.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := f.app.effectiveMeta(r.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	share := Share{ID: randomID(), Slug: randomID(), Path: target, Filename: info.Name, Size: *info.Size, MimeType: info.MimeType, Owner: actor.Owner(), CreatedAt: nowText(), UpdatedAt: nowText(), SourceVersion: sourceVersion(m, storage), MetaVersion: secretHash(meta.Password)}
	if value, ok := fields["slug"].(string); ok {
		share.Slug = value
	}
	if value, ok := fields["max_views"].(int); ok {
		share.MaxViews = value
	}
	if value, ok := fields["password"].(string); ok {
		share.Password, err = passwordHash(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = f.app.saveResource(r.Context(), "shares", share.ID, share.Slug, share.Owner, share); err != nil {
		t.Fatal(err)
	}
	return shareView(share)
}

func (f *fixture) moveMount(t *testing.T, id, root string) {
	t.Helper()
	var mount Mount
	if err := f.app.resource(context.Background(), "mounts", "id", id, &mount); err != nil {
		t.Fatal(err)
	}
	mount.RootPath, mount.UpdatedAt = root, nowText()
	if err := f.app.saveResource(context.Background(), "mounts", id, "", mount.Owner, mount); err != nil {
		t.Fatal(err)
	}
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
	_, err := New(Config{DataDir: t.TempDir(), AdminUsername: "admin", AdminPassword: "12345"}, nil)
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

func TestAdminPasswordLengthBoundary(t *testing.T) {
	for _, password := range []string{"12345", "中文", strings.Repeat("a", 73)} {
		if _, err := New(Config{DataDir: t.TempDir(), AdminPassword: password}, nil); err == nil {
			t.Fatal("invalid initial password accepted")
		}
	}
	for _, password := range []string{"123456", "中文密码六位"} {
		app, err := New(Config{DataDir: t.TempDir(), AdminUsername: "admin", AdminPassword: password}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Close() })
		f := &fixture{app: app, handler: app.Handler()}
		f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "admin", "password": password}, nil, 200)
	}
	f := newFixture(t)
	// 后台修改遵守同一规则；成功后旧会话失效，并能用六位新密码登录。
	for _, password := range []string{"12345", "中文", strings.Repeat("a", 73)} {
		f.request(t, "POST", "/api/admin/change-password", f.admin, map[string]any{"currentPassword": testPassword, "newPassword": password}, nil, 400)
	}
	f.request(t, "POST", "/api/admin/change-password", f.admin, map[string]any{"currentPassword": testPassword, "newPassword": "123456"}, nil, 200)
	f.request(t, "GET", "/api/admin/settings", f.admin, nil, nil, 401)
	f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "admin", "password": "123456"}, nil, 200)
}

func TestSourceChangeRevokesLinksAndShares(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "file.txt", "original")
	writeFile(t, root, "replacement/file.txt", "replacement")
	_, mountID := f.local(t, root, "/disk")
	share := f.legacyShare(t, f.admin, map[string]any{"path": "/disk/file.txt"})
	f.moveMount(t, mountID, "/replacement")
	f.request(t, "GET", "/api/share/get/"+share["slug"].(string), "", nil, nil, 404)
}

func TestSharePasswordAndLastViewLease(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "file.txt", "file contents")
	f.local(t, root, "/disk")
	s := f.legacyShare(t, f.admin, map[string]any{"path": "/disk/file.txt", "password": "share-password", "max_views": 1})
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
	f.request(t, "POST", "/api/share/verify/"+slug, "", map[string]any{"password": "share-password"}, nil, 404)
	f.request(t, "PUT", "/api/files/"+s["id"].(string), f.admin, map[string]any{"password": "new-share-password"}, nil, 200)
	f.request(t, "GET", link, "", nil, nil, 403)
	f.request(t, "DELETE", "/api/files/batch-delete", f.admin, map[string]any{"ids": []string{s["id"].(string)}, "delete_mode": "invalid"}, nil, 400)
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
	f.request(t, "GET", "/api/raw/"+slug, "", nil, nil, 404)
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
			} else if w.Code != 404 {
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

func TestHTTPDefaultsAndUnsupportedUploads(t *testing.T) {
	f := newFixture(t)
	f.request(t, "POST", "/api/share/upload", f.admin, nil, nil, 404)
	f.request(t, "POST", "/api/proxy/link", "", map[string]any{"url": "http://localhost"}, nil, 404)
	f.request(t, "POST", "/api/proxy/link", f.admin, map[string]any{"url": "http://localhost"}, nil, 404)
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
