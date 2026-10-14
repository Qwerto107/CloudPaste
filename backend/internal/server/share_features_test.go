package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdminExpiredSharePreview(t *testing.T) {
	for _, kind := range []string{"text", "file", "collection"} {
		for _, reason := range []string{"expired", "exhausted"} {
			t.Run(kind+"/"+reason, func(t *testing.T) {
				f := newFixture(t)
				storage := ""
				if kind != "text" {
					storage, _ = f.local(t, t.TempDir(), "/disk")
				}
				created := createSlugTestShare(t, f, kind, f.admin, storage, "admin-preview", 201)
				get, verify, update := "/api/paste/admin-preview", "/api/paste/admin-preview", "/api/pastes/admin-preview"
				if kind != "text" {
					get, verify, update = "/api/share/get/admin-preview", "/api/share/verify/admin-preview", "/api/files/"+created["id"].(string)
				}
				if reason == "expired" {
					past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
					f.request(t, "PUT", update, f.admin, map[string]any{"expires_at": past}, nil, 200)
				} else {
					f.request(t, "POST", verify, "", map[string]any{"password": "share-password"}, nil, 200)
				}
				f.request(t, "GET", get, "", nil, nil, 404)
				key, _ := f.key(t, "ordinary", "/", TextManage|FileManage)
				adminGet := "/api/admin" + strings.TrimPrefix(get, "/api")
				namespaceRequest(t, f, "GET", adminGet, key, nil, 403)
				namespaceRequest(t, f, "GET", adminGet, "", nil, 401)
				view := dataMap(t, f.request(t, "GET", get, f.admin, nil, nil, 200))
				views := float64(0)
				message := "已过期"
				if reason == "exhausted" {
					views, message = 1, "查看次数已用完"
				}
				if view["views"] != views || view["expiry_reason"] != message || view["admin_preview"] != true {
					t.Fatalf("管理员查看未标明失效原因或消耗了查看次数: %v", view)
				}
				if kind == "text" {
					if view["content"] != "text contents" || view["requiresPassword"] != false {
						t.Fatal("管理员没有读取到文本")
					}
				} else {
					link := view["downloadUrl"]
					if kind == "collection" {
						link = view["archiveUrl"]
						file := view["files"].([]any)[0].(map[string]any)
						w := f.request(t, "GET", file["contentUrl"].(string), "", nil, nil, 200)
						if w.Body.String() != "collection contents" {
							t.Fatal("失效合集文件无法读取")
						}
					} else {
						f.request(t, "GET", view["contentUrl"].(string), "", nil, map[string]string{"Range": "bytes=0-3"}, 206)
					}
					f.request(t, "GET", link.(string), "", nil, nil, 200)
					// 管理员票据不能转成公开读取票据，也不能授权另一分享。
					publicStatus := 404
					if reason == "exhausted" {
						publicStatus = 403
					}
					f.request(t, "GET", strings.Replace(link.(string), "/api/admin/s/", "/api/s/", 1), "", nil, nil, publicStatus)
					f.request(t, "GET", strings.Replace(link.(string), "admin-preview", "another-share", 1), "", nil, nil, 403)
					f.request(t, "POST", "/api/admin/logout", f.admin, nil, nil, 200)
					f.request(t, "GET", link.(string), "", nil, nil, 403)
				}
			})
		}
	}
}

func TestAdminShareTicketRevokedByReplacement(t *testing.T) {
	f := newFixture(t)
	storage, _ := f.local(t, t.TempDir(), "/disk")
	old := createSlugTestShare(t, f, "file", f.admin, storage, "reused-admin", 201)
	view := dataMap(t, f.request(t, "GET", "/api/share/get/reused-admin", f.admin, nil, nil, 200))
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	f.request(t, "PUT", "/api/files/"+old["id"].(string), f.admin, map[string]any{"expires_at": past}, nil, 200)
	createSlugTestShare(t, f, "file", f.admin, storage, "reused-admin", 201)
	f.request(t, "GET", view["downloadUrl"].(string), "", nil, nil, 403)
}

// 验证真正的文件删除和记录删除两个模式，合集需要处理所有来源快照。
func TestShareFileDeletionModes(t *testing.T) {
	for _, kind := range []string{"file", "collection"} {
		for _, mode := range []string{"", "both", "record_only"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				f := newFixture(t)
				root := t.TempDir()
				storage, _ := f.local(t, root, "/disk")
				created := createSlugTestShare(t, f, kind, f.admin, storage, "deletable", 201)
				var share Share
				if err := f.app.resource(context.Background(), "shares", "id", created["id"].(string), &share); err != nil {
					t.Fatal(err)
				}
				files := share.Files
				if len(files) == 0 {
					files = []Share{share}
				}
				// 失效也不妨碍本人清理原文件。
				past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
				f.request(t, "PUT", "/api/files/"+share.ID, f.admin, map[string]any{"expires_at": past}, nil, 200)
				f.request(t, "DELETE", "/api/files/batch-delete", f.admin, map[string]any{"ids": []string{share.ID}, "delete_mode": mode}, nil, 200)
				var remaining Share
				if err := f.app.resource(context.Background(), "shares", "id", share.ID, &remaining); err == nil {
					t.Fatal("分享记录没有删除")
				}
				for _, file := range files {
					_, err := os.Stat(filepath.Join(root, filepath.FromSlash(file.StorageKey)))
					if mode == "record_only" && err != nil {
						t.Fatal("仅删除记录却删除了原文件", err)
					}
					if mode != "record_only" && !errors.Is(err, os.ErrNotExist) {
						t.Fatal("删除记录后原文件仍存在", err)
					}
				}
			})
		}
	}
}

func TestShareFileDeletionAuthorizationAndRetry(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	storage, _ := f.local(t, root, "/disk")
	owner, _ := f.key(t, "owner", "/", FileShare|FileManage|MountUpload)
	other, _ := f.key(t, "other", "/", FileShare|FileManage|MountUpload)
	created := createSlugTestShare(t, f, "file", owner, storage, "owned-delete", 201)
	f.request(t, "DELETE", "/api/files/batch-delete", other, map[string]any{"ids": []string{created["id"].(string)}}, nil, 404)
	var share Share
	if err := f.app.resource(context.Background(), "shares", "id", created["id"].(string), &share); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, filepath.FromSlash(share.StorageKey))
	if _, err := os.Stat(filename); err != nil {
		t.Fatal("其他用户删除了原文件", err)
	}
	// 只读存储不能物理删除；失败必须保留记录，以便改选仅删除记录。
	var config Storage
	if err := f.app.resource(context.Background(), "storage_configs", "id", storage, &config); err != nil {
		t.Fatal(err)
	}
	config.ReadOnly = 1
	if err := f.app.saveResource(context.Background(), "storage_configs", storage, "", "", config); err != nil {
		t.Fatal(err)
	}
	f.request(t, "DELETE", "/api/files/batch-delete", owner, map[string]any{"ids": []string{share.ID}}, nil, 403)
	if err := f.app.resource(context.Background(), "shares", "id", share.ID, &share); err != nil {
		t.Fatal("物理删除失败却删除了记录", err)
	}
	f.request(t, "DELETE", "/api/files/batch-delete", owner, map[string]any{"ids": []string{share.ID}, "delete_mode": "record_only"}, nil, 200)
	config.ReadOnly = 0
	if err := f.app.saveResource(context.Background(), "storage_configs", storage, "", "", config); err != nil {
		t.Fatal(err)
	}
	created = createSlugTestShare(t, f, "file", owner, storage, "missing-file", 201)
	if err := f.app.resource(context.Background(), "shares", "id", created["id"].(string), &share); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(share.StorageKey))); err != nil {
		t.Fatal(err)
	}
	f.request(t, "DELETE", "/api/files/batch-delete", owner, map[string]any{"ids": []string{share.ID}}, nil, 200)
}

func TestLocalDeletionRejectsLinksAndDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "locked/keep.txt", "keep")
	d := &localDriver{rootPath: root}
	if err := d.Delete(context.Background(), "locked"); err == nil {
		t.Fatal("删除接受了目录")
	}
	if err := os.Symlink(filepath.Join(root, "locked", "keep.txt"), filepath.Join(root, "alias.txt")); err != nil {
		t.Skipf("无法创建测试链接: %v", err)
	}
	if err := d.Delete(context.Background(), "alias.txt"); err == nil {
		t.Fatal("删除接受了符号链接")
	}
	if _, err := os.Stat(filepath.Join(root, "locked", "keep.txt")); err != nil {
		t.Fatal("删除了链接目标", err)
	}
}

func TestS3FileDeletion(t *testing.T) {
	var requests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/test-bucket/file.txt" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Errorf("错误或未签名的删除请求: %s %s", r.Method, r.URL.Path)
		}
		requests++
		if requests == 1 {
			w.WriteHeader(403)
			return
		}
		w.WriteHeader(204)
	}))
	defer upstream.Close()
	driver, err := newS3Driver(Storage{Endpoint: upstream.URL, Bucket: "test-bucket", AccessKey: "access", SecretKey: "secret", PathStyle: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	deleter := driver.(fileDeleter)
	if err := deleter.Delete(context.Background(), "file.txt"); err == nil {
		t.Fatal("忽略了 S3 删除错误")
	}
	if err := deleter.Delete(context.Background(), "file.txt"); err != nil {
		t.Fatal("S3 删除重试失败", err)
	}
}
