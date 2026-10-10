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
		{"GET", "/api/admin/test/api-key", f.admin, 200},
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
	// 管理员凭据不能从普通存储详情入口读取密钥，API Key 也不能通过 reveal 提权。
	storage := dataMap(t, f.request(t, "POST", "/api/admin/storage", f.admin, map[string]any{
		"name": "secret", "storage_type": "S3", "bucket_name": "bucket", "is_public": true,
		"access_key_id": "namespace-access-secret", "secret_access_key": "namespace-signing-secret",
	}, nil, 201))
	id := storage["id"].(string)
	namespaceRequest(t, f, "GET", "/api/storage/"+id+"?reveal=plain", f.admin, nil, 403)
	user := namespaceRequest(t, f, "GET", "/api/storage/"+id+"?reveal=plain", key, nil, 200)
	if strings.Contains(user.Body.String(), "namespace-access-secret") || strings.Contains(user.Body.String(), "namespace-signing-secret") {
		t.Fatal("storage credentials leaked to API Key")
	}
	admin := namespaceRequest(t, f, "GET", "/api/admin/storage/"+id+"?reveal=plain", f.admin, nil, 200)
	if !strings.Contains(admin.Body.String(), "namespace-signing-secret") {
		t.Fatal("admin credential reveal unavailable")
	}
}

func TestAdminDownloadTicketsCannotCrossNamespaces(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "file.txt", "namespace file")
	f.local(t, root, "/disk")
	key, _ := f.key(t, "reader", "/disk", MountView|WebDAVRead)
	adminLink := dataMap(t, f.request(t, "GET", "/api/admin/fs/file-link?path=/disk/file.txt", f.admin, nil, nil, 200))["url"].(string)
	if !strings.HasPrefix(adminLink, "/api/admin/p/") {
		t.Fatal("admin download link left protected prefix")
	}
	namespaceRequest(t, f, "GET", adminLink, "", nil, 200)
	namespaceRequest(t, f, "GET", strings.Replace(adminLink, "/api/admin/p/", "/api/p/", 1), "", nil, 403)
	userLink := dataMap(t, f.request(t, "GET", "/api/fs/file-link?path=/disk/file.txt", key, nil, nil, 200))["url"].(string)
	namespaceRequest(t, f, "GET", userLink, "", nil, 200)
	namespaceRequest(t, f, "GET", strings.Replace(userLink, "/api/p/", "/api/admin/p/", 1), "", nil, 403)
	// 已删除的 DAV 入口不能用 Basic 认证探测管理密码。
	for _, target := range []string{"/dav/disk/file.txt", "/api/admin/dav/disk/file.txt"} {
		namespaceRequest(t, f, "GET", target, "", nil, 404)
	}
	// 管理员主动创建的公开分享仍可由访客读取，不要求访客访问管理前缀。
	share := dataMap(t, f.request(t, "POST", "/api/admin/fs/create-share", f.admin, map[string]any{"path": "/disk/file.txt"}, nil, 201))
	namespaceRequest(t, f, "GET", "/api/s/"+share["slug"].(string), "", nil, 200)
}

func TestMultipartTicketsCannotCrossNamespaces(t *testing.T) {
	f := newFixture(t)
	f.local(t, t.TempDir(), "/disk")
	key, _ := f.key(t, "writer", "/disk", MountUpload)
	for _, credential := range []string{f.admin, key} {
		name := "user-empty.txt"
		if credential == f.admin {
			name = "admin-empty.txt"
		}
		session := dataMap(t, f.request(t, "POST", "/api/fs/multipart/init", credential, map[string]any{
			"path": "/disk", "fileName": name, "fileSize": 0, "partSize": 5 << 20,
		}, nil, 200))
		signed := dataMap(t, f.request(t, "POST", "/api/fs/multipart/sign-parts", credential, map[string]any{
			"uploadId": session["uploadId"], "partNumbers": []int{1},
		}, nil, 200))
		link := signed["presignedUrls"].([]any)[0].(map[string]any)["url"].(string)
		other := strings.Replace(link, "/api/", "/api/admin/", 1)
		if credential == f.admin {
			if !strings.HasPrefix(link, "/api/admin/") {
				t.Fatal("admin multipart URL left protected prefix")
			}
			other = strings.Replace(link, "/api/admin/", "/api/", 1)
		}
		namespaceRequest(t, f, "PUT", other, "", nil, 403)
		namespaceRequest(t, f, "PUT", link, "", nil, 200)
	}
}
