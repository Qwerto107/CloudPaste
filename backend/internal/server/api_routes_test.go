package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// 边界测试直接发送指定路径，绝不经过业务回归辅助函数的管理员前缀映射。
func namespaceRequest(t *testing.T, f *fixture, method, target, auth string, body any, status int) *httptest.ResponseRecorder {
	t.Helper()
	var content string
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		content = string(data)
	}
	r := httptest.NewRequest(method, target, strings.NewReader(content))
	r.RemoteAddr = "127.0.0.1:1234"
	if method == "PROPFIND" {
		r.Header.Set("Depth", "1")
	}
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	// 来源或证书头不能改变后端判定的入口和身份。
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	r.Header.Set("X-SSL-Client-Verify", "SUCCESS")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, target, w.Code, status, w.Body.String())
	}
	return w
}

func TestAdminNamespaceIsolation(t *testing.T) {
	f := newFixture(t)
	key, _ := f.key(t, "user", "/", TextShare|TextManage|MountView|MountUpload|FileShare|FileManage|WebDAVRead)
	for _, test := range []struct {
		method, target, auth string
		status               int
	}{
		{"GET", "/api/public/settings?group=4", "", 200},
		{"GET", "/api/admin/settings", "", 401},
		{"GET", "/api/storage", "", 401},
		{"GET", "/api/storage", key, 200},
		{"GET", "/api/admin/storage", key, 403},
		{"GET", "/api/storage", f.admin, 403},
		{"GET", "/api/admin/storage", f.admin, 200},
		{"GET", "/api/test/api-key", f.admin, 403},
		{"GET", "/api/admin/test/api-key", f.admin, 404},
		{"GET", "/api/test/admin-token", key, 404},
		{"GET", "/api/admin/test/admin-token", f.admin, 200},
		{"POST", "/api/storage", key, 404},
		{"POST", "/api/storage", f.admin, 403},
		{"POST", "/api/admin/storage", key, 403},
		{"POST", "/api/mount/create", key, 404},
		{"POST", "/api/fs-meta/create", key, 404},
		{"POST", "/api/pastes/clear-expired", key, 404},
		{"GET", "/api/%61dmin/settings", f.admin, 400},
		{"GET", "/api/admin%2Fsettings", f.admin, 400},
	} {
		namespaceRequest(t, f, test.method, test.target, test.auth, nil, test.status)
	}
}

// 已移除的入口及其管理员镜像必须都不可达，防止后续重构重新暴露旧协议。
func TestRemovedAPIsAreNotRegistered(t *testing.T) {
	f := newFixture(t)
	for _, endpoint := range []string{
		"GET /api/version", "GET /api/system/max-upload-size", "GET /api/public/guest-config",
		"GET /api/fs/list", "GET /api/fs/get", "GET /api/fs/content", "GET /api/fs/download", "GET /api/fs/file-link", "POST /api/fs/create-share", "GET /api/p/removed",
		"GET /api/share/content/removed", "GET /api/share/content/removed/file.txt", "GET /api/files/removed", "GET /api/raw/removed", "POST /api/proxy/link",
		"GET /api/admin/api-keys/removed/storage-acl", "PUT /api/admin/api-keys/removed/storage-acl",
		"GET /api/admin/fs-meta/list", "POST /api/admin/fs-meta/create", "GET /api/admin/fs-meta/removed", "PUT /api/admin/fs-meta/removed", "DELETE /api/admin/fs-meta/removed", "POST /api/fs/meta/password/verify",
		"GET /api/storage/removed", "GET /api/mount/list", "POST /api/admin/mount/create", "PUT /api/admin/mount/removed", "DELETE /api/admin/mount/removed",
		"GET /api/storage-types", "GET /api/storage-types/LOCAL/capabilities", "GET /api/admin/mount-schema", "POST /api/admin/pastes/clear-expired",
		"GET /api/admin/settings/groups", "GET /api/admin/settings/metadata", "POST /api/admin/cache/clear",
		"POST /api/fs/multipart/init", "POST /api/fs/multipart/sign-parts", "POST /api/fs/multipart/list-parts", "POST /api/fs/multipart/list-uploads", "POST /api/fs/multipart/abort", "POST /api/fs/multipart/complete", "PUT /api/fs/multipart/part/removed/1",
		"PUT /api/fs/upload", "POST /api/fs/upload",
	} {
		method, route, _ := strings.Cut(endpoint, " ")
		if isAdminPath(route) {
			namespaceRequest(t, f, method, route, f.admin, nil, 404)
			continue
		}
		namespaceRequest(t, f, method, route, "", nil, 404)
		namespaceRequest(t, f, method, adminAPIPrefix+strings.TrimPrefix(route, "/api"), f.admin, nil, 404)
	}
	namespaceRequest(t, f, "POST", "/api/share/upload", "", nil, 404)
	namespaceRequest(t, f, "POST", "/api/admin/share/upload", f.admin, nil, 404)
	for _, route := range []string{"/api/admin/public/settings", "/api/admin/health"} {
		namespaceRequest(t, f, "GET", route, f.admin, nil, 404)
	}
}
