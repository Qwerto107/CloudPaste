package server

import (
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
)

// 覆盖默认权限、独立开关，以及开放创建后仍不可访问管理入口。
func TestGuestPermissionSwitches(t *testing.T) {
	f := newFixture(t)
	f.request(t, "POST", "/api/paste", "", map[string]any{"content": "hello"}, nil, 401)
	f.request(t, "POST", "/api/transfer/create", "", map[string]any{"name": "guest"}, nil, 401)
	f.request(t, "POST", "/api/transfer/join", "", map[string]any{"name": "guest", "code": "A1234"}, nil, 404)
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{guestText: true, guestJoinTransfer: false}, nil, 200)
	f.request(t, "POST", "/api/paste", "", map[string]any{"content": "hello"}, nil, 201)
	f.request(t, "POST", "/api/paste", "", map[string]any{"content": "private", "is_public": false}, nil, 400)
	f.request(t, "POST", "/api/transfer/create", "", map[string]any{"name": "guest"}, nil, 401)
	f.request(t, "POST", "/api/transfer/join", "", map[string]any{"name": "guest", "code": "A1234"}, nil, 403)
	key, _ := f.key(t, "text", "/", TextShare)
	f.request(t, "POST", "/api/transfer/join", key, map[string]any{"name": "user", "code": "A1234"}, nil, 404)
	f.request(t, "POST", "/api/transfer/join", f.admin, map[string]any{"name": "admin", "code": "A1234"}, nil, 404)
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{guestText: false, guestCreateTransfer: true}, nil, 200)
	f.request(t, "POST", "/api/paste", "", map[string]any{"content": "hello"}, nil, 401)
	f.request(t, "POST", "/api/paste", key, map[string]any{"content": "user"}, nil, 201)
	created := dataMap(t, f.request(t, "POST", "/api/transfer/create", "", map[string]any{"name": "guest"}, nil, 201))
	f.app.transfers.mu.Lock()
	for _, room := range f.app.transfers.rooms {
		if !room.owner.Guest || !f.app.transfers.ownerValid(room.owner) {
			t.Error("访客连接身份无效")
		}
		room.members[0].connected = true
	}
	f.app.transfers.mu.Unlock()
	f.request(t, "POST", "/api/transfer/join", key, map[string]any{"name": "user", "code": created["code"]}, nil, 200)
	for _, target := range []string{"/api/pastes", "/api/files", "/api/owner/paste/missing", "/api/admin/settings"} {
		f.request(t, "GET", target, "", nil, nil, 401)
	}
}

// 上传凭证只能合并同一批文件，不能借用其他访客的归属或取得管理权限。
func TestGuestUploadsAndCollectionIsolation(t *testing.T) {
	f := newFixture(t)
	storage, _ := f.local(t, t.TempDir(), "/disk")
	fields := map[string]any{"storage_config_id": storage, "path": "/"}
	f.upload(t, "PUT", "/api/share/upload", "", strings.NewReader("denied"), uploadHeaders("a.txt", fields, true), 401)
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{guestUpload: true}, nil, 200)
	f.request(t, "GET", "/api/storage", "", nil, nil, 200)
	token := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	headers := uploadHeaders("a.txt", fields, true)
	headers["X-Guest-Token"] = token
	first := dataMap(t, f.upload(t, "PUT", "/api/share/upload", "", strings.NewReader("first"), headers, 201))
	second := dataMap(t, f.upload(t, "PUT", "/api/share/upload", "", strings.NewReader("second"), headers, 201))
	var stored Share
	r := httptest.NewRequest("GET", "/", nil)
	if err := f.app.resource(r.Context(), "shares", "id", first["id"].(string), &stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored.Owner, "guest:") || !strings.HasPrefix(stored.StorageKey, "guest/"+secretHash(token)+"/") {
		t.Fatal("访客文件未隔离")
	}
	body := map[string]any{"ids": []string{first["id"].(string), second["id"].(string)}}
	other := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("b", 32)))
	f.request(t, "POST", "/api/share/collection", "", body, map[string]string{"X-Guest-Token": other}, 403)
	f.request(t, "POST", "/api/share/collection", "", body, map[string]string{"X-Guest-Token": "invalid"}, 400)
	collection := dataMap(t, f.request(t, "POST", "/api/share/collection", "", body, map[string]string{"X-Guest-Token": token}, 201))
	f.request(t, "GET", "/api/owner/share/get/"+collection["slug"].(string), "", nil, map[string]string{"X-Guest-Token": token}, 401)
	fields["path"] = "/disk"
	f.upload(t, "PUT", "/api/share/upload", "", strings.NewReader("denied"), uploadHeaders("a.txt", fields, true), 403)
	fields["path"] = "/"
	var s Storage
	if err := f.app.resource(r.Context(), "storage_configs", "id", storage, &s); err != nil {
		t.Fatal(err)
	}
	s.Public = 0
	if err := f.app.saveResource(r.Context(), "storage_configs", s.ID, "", "", s); err != nil {
		t.Fatal(err)
	}
	f.upload(t, "PUT", "/api/share/upload", "", strings.NewReader("denied"), uploadHeaders("a.txt", fields, true), 403)
	s.Public = 1
	if err := f.app.saveResource(r.Context(), "storage_configs", s.ID, "", "", s); err != nil {
		t.Fatal(err)
	}
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{guestUpload: false}, nil, 200)
	// 撤销新上传仍保留已有合集的正常访问和下载。
	view := dataMap(t, f.request(t, "GET", "/api/share/get/"+collection["slug"].(string), "", nil, nil, 200))
	f.request(t, "GET", view["archiveUrl"].(string), "", nil, nil, 200)
	f.request(t, "GET", "/api/storage", "", nil, nil, 401)
}
