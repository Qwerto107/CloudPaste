package server

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 真实创建、读取和下载覆盖三种分享，其他用户与公开入口不能借创建者权限。
func TestOwnerExpiredShareRead(t *testing.T) {
	for _, kind := range []string{"text", "file", "collection"} {
		for _, cause := range []string{"expired", "exhausted"} {
			t.Run(kind+"/"+cause, func(t *testing.T) {
				f := newFixture(t)
				permissions := TextShare | TextManage | FileShare | FileManage | MountUpload
				owner, keyID := f.key(t, "owner", "/", permissions)
				other, _ := f.key(t, "other", "/", permissions)
				storage := ""
				if kind != "text" {
					storage, _ = f.local(t, t.TempDir(), "/disk")
				}
				created := createSlugTestShare(t, f, kind, owner, storage, "owner-preview", 201)
				if created["password"] != "share-password" {
					t.Fatal("creation did not return own password")
				}
				get, update, verify := "/api/paste/owner-preview", "/api/pastes/owner-preview", "/api/paste/owner-preview"
				if kind != "text" {
					get, update, verify = "/api/share/get/owner-preview", "/api/files/"+created["id"].(string), "/api/share/verify/owner-preview"
				}
				wantViews, reason := float64(0), "已过期"
				if cause == "expired" {
					f.request(t, "PUT", update, owner, map[string]any{"expires_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}, nil, 200)
				} else {
					public := dataMap(t, f.request(t, "POST", verify, "", map[string]any{"password": "share-password"}, nil, 200))
					if _, exists := public["password"]; exists {
						t.Fatal("public response exposed password")
					}
					if _, exists := public["password_encrypted"]; exists {
						t.Fatal("public response exposed ciphertext")
					}
					wantViews, reason = 1, "查看次数已用完"
				}
				preview := strings.Replace(get, "/api/", "/api/owner/", 1)
				f.request(t, "GET", get, "", nil, nil, 404)
				f.request(t, "GET", preview, other, nil, nil, 404)
				f.request(t, "GET", preview, "", nil, nil, 401)
				view := dataMap(t, f.request(t, "GET", preview, owner, nil, nil, 200))
				if view["owner_preview"] != true || view["expiry_reason"] != reason || view["password"] != nil || view["views"] != wantViews {
					t.Fatalf("incorrect owner preview: %v", view)
				}
				adminView := dataMap(t, f.request(t, "GET", get, f.admin, nil, nil, 200))
				if adminView["password"] != nil {
					t.Fatal("preview exposed password to admin")
				}
				if kind != "text" {
					link := view["downloadUrl"]
					if kind == "collection" {
						link = view["archiveUrl"]
						child := view["files"].([]any)[0].(map[string]any)
						f.request(t, "GET", child["contentUrl"].(string), "", nil, nil, 200)
					}
					f.request(t, "GET", link.(string), "", nil, nil, 200)
					f.request(t, "GET", link.(string), other, nil, nil, 404)
					publicStatus := 404
					if cause == "exhausted" {
						publicStatus = 403
					}
					f.request(t, "GET", strings.Replace(link.(string), "/api/owner/s/", "/api/s/", 1), "", nil, nil, publicStatus)
					f.request(t, "GET", strings.Replace(link.(string), "owner-preview", "another", 1), "", nil, nil, 403)
				}
				again := dataMap(t, f.request(t, "GET", preview, owner, nil, nil, 200))
				if again["views"] != wantViews {
					t.Fatal("owner download or refresh consumed views")
				}
				f.request(t, "PUT", "/api/admin/api-keys/"+keyID, f.admin, map[string]any{"is_enable": false}, nil, 200)
				if kind != "text" {
					link := view["downloadUrl"]
					if kind == "collection" {
						link = view["archiveUrl"]
					}
					f.request(t, "GET", link.(string), "", nil, nil, 403)
				}
			})
		}
	}
}

func TestManagedSharePasswords(t *testing.T) {
	for _, kind := range []string{"text", "file", "collection"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			owner, _ := f.key(t, "owner", "/", TextShare|TextManage|FileShare|FileManage|MountUpload)
			storage := ""
			if kind != "text" {
				storage, _ = f.local(t, t.TempDir(), "/disk")
			}
			created := createSlugTestShare(t, f, kind, owner, storage, "password-roundtrip", 201)
			get, update, table, list, listField := "/api/owner/paste/password-roundtrip", "/api/pastes/password-roundtrip", "pastes", "/api/pastes", "results"
			if kind != "text" {
				get, update, table, list, listField = "/api/owner/share/get/password-roundtrip", "/api/files/"+created["id"].(string), "shares", "/api/files", "files"
			}
			var raw string
			if err := f.app.db.QueryRow("SELECT data FROM "+table+" WHERE id=?", created["id"]).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw, "share-password") || !strings.Contains(raw, "gcm:v1:") {
				t.Fatal("password not encrypted at rest")
			}
			items := dataMap(t, f.request(t, "GET", list, owner, nil, nil, 200))[listField].([]any)
			if items[0].(map[string]any)["password"] != "share-password" {
				t.Fatal("management list cannot see password")
			}
			updated := dataMap(t, f.request(t, "PUT", update, owner, map[string]any{"password": "中文 new-password", "max_views": 0}, nil, 200))
			if updated["password"] != "中文 new-password" {
				t.Fatal("password not updated")
			}
			view := dataMap(t, f.request(t, "GET", get, owner, nil, nil, 200))
			if view["password"] != nil {
				t.Fatal("preview exposed new password")
			}
			items = dataMap(t, f.request(t, "GET", list, owner, nil, nil, 200))[listField].([]any)
			if items[0].(map[string]any)["password"] != "中文 new-password" {
				t.Fatal("management did not recover new password")
			}
			f.request(t, "PUT", update, owner, map[string]any{"password": ""}, nil, 200)
			view = dataMap(t, f.request(t, "GET", get, owner, nil, nil, 200))
			if view["password"] != nil || view["has_password"] != false {
				t.Fatal("clearing password did not remove protection")
			}
		})
	}
}

func TestLegacySharePasswordRemainsProtected(t *testing.T) {
	f := newFixture(t)
	created := createSlugTestShare(t, f, "text", f.admin, "", "legacy-password", 201)
	var p Paste
	if err := f.app.resource(context.Background(), "pastes", "id", created["id"].(string), &p); err != nil {
		t.Fatal(err)
	}
	p.EncryptedPassword = ""
	if err := f.app.saveResource(context.Background(), "pastes", p.ID, p.Slug, p.Owner, p); err != nil {
		t.Fatal(err)
	}
	view := dataMap(t, f.request(t, "GET", "/api/pastes/"+p.ID, f.admin, nil, nil, 200))
	if view["password_available"] != false || view["has_password"] != true || view["password"] != nil {
		t.Fatal("legacy password incorrectly displayed or removed")
	}
	f.request(t, "POST", "/api/paste/legacy-password", "", map[string]any{"password": "share-password"}, nil, 200)
	f.request(t, "PUT", "/api/pastes/legacy-password", f.admin, map[string]any{"password": "replacement"}, nil, 200)
	view = dataMap(t, f.request(t, "GET", "/api/pastes/"+p.ID, f.admin, nil, nil, 200))
	if view["password"] != "replacement" || view["password_available"] != true {
		t.Fatal("legacy reset did not become readable")
	}
}

func TestOwnerReadNeedsNoAdditionalManagePermission(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.key(t, "creator", "/", TextShare|FileShare|MountUpload)
	storage, _ := f.local(t, t.TempDir(), "/disk")
	for _, kind := range []string{"text", "file"} {
		created := createSlugTestShare(t, f, kind, owner, storage, "only-create-"+kind, 201)
		get, update := "/api/owner/paste/only-create-text", "/api/pastes/only-create-text"
		if kind == "file" {
			get, update = "/api/owner/share/get/only-create-file", "/api/files/"+created["id"].(string)
		}
		f.request(t, "PUT", update, f.admin, map[string]any{"expires_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}, nil, 200)
		view := dataMap(t, f.request(t, "GET", get, owner, nil, nil, 200))
		if view["owner_preview"] != true {
			t.Fatal("creator without manage permission could not preview")
		}
		if kind == "file" {
			f.request(t, "GET", view["downloadUrl"].(string), "", nil, nil, 200)
		}
	}
}
