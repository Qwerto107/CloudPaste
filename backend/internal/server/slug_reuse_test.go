package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 通过实际创建入口准备三种分享，合集也必须先完成文件上传。
func createSlugTestShare(t *testing.T, f *fixture, kind, auth, storage, slug string, status int) map[string]any {
	t.Helper()
	fields := map[string]any{"slug": slug, "password": "share-password", "max_views": 1}
	if kind == "text" {
		fields["content"] = "text contents"
		return dataMap(t, f.request(t, "POST", "/api/paste", auth, fields, nil, status))
	}
	fields["storage_config_id"] = storage
	if kind == "file" {
		w := f.upload(t, "PUT", "/api/share/upload", auth, strings.NewReader("file contents"), uploadHeaders("first.txt", fields, true), status)
		if status != 201 {
			return nil
		}
		return dataMap(t, w)
	}
	file := dataMap(t, f.upload(t, "PUT", "/api/share/upload", auth, strings.NewReader("collection contents"), uploadHeaders("first.txt", map[string]any{"storage_config_id": storage}, true), 201))
	second := dataMap(t, f.upload(t, "PUT", "/api/share/upload", auth, strings.NewReader("second contents"), uploadHeaders("second.txt", map[string]any{"storage_config_id": storage}, true), 201))
	fields["ids"] = []string{file["id"].(string), second["id"].(string)}
	w := f.request(t, "POST", "/api/share/collection", auth, fields, nil, status)
	if status != 201 {
		return nil
	}
	return dataMap(t, w)
}

// 期限和查看额度都应释放后缀；旧记录、文件来源和旧凭证不能混入新分享。
func TestExpiredShareSlugReuse(t *testing.T) {
	for _, kind := range []string{"text", "file", "collection"} {
		for _, reason := range []string{"expired", "exhausted"} {
			t.Run(kind+"/"+reason, func(t *testing.T) {
				f := newFixture(t)
				storage := ""
				if kind != "text" {
					storage, _ = f.local(t, t.TempDir(), "/disk")
				}
				old := createSlugTestShare(t, f, kind, f.admin, storage, "reusable", 201)
				get, verify, update, table := "/api/paste/reusable", "/api/paste/reusable", "/api/pastes/reusable", "pastes"
				if kind != "text" {
					get, verify, update, table = "/api/share/get/reusable", "/api/share/verify/reusable", "/api/files/"+old["id"].(string), "shares"
				}
				// 密码保护本身不会释放后缀。
				if kind == "text" {
					f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "reusable", "content": "duplicate"}, nil, 400)
				} else {
					createSlugTestShare(t, f, kind, f.admin, storage, "reusable", 400)
				}
				lease := dataMap(t, f.request(t, "POST", verify, "", map[string]any{"password": "share-password"}, nil, 200))
				if reason == "expired" {
					past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
					f.request(t, "PUT", update, f.admin, map[string]any{"expires_at": past, "max_views": 0}, nil, 200)
				}
				f.request(t, "GET", get, "", nil, nil, 404)
				var oldPaste Paste
				var oldShare Share
				var previous any = &oldPaste
				if kind != "text" {
					previous = &oldShare
					if reason == "exhausted" {
						// 最后一次访问的读取票据在后缀回收前仍能完成下载。
						link := "downloadUrl"
						if kind == "collection" {
							link = "archiveUrl"
						}
						f.request(t, "GET", lease[link].(string), "", nil, nil, 200)
					}
				}
				if err := f.app.resource(context.Background(), table, "id", old["id"].(string), previous); err != nil {
					t.Fatal(err)
				}
				// 失效后的后缀也可由另一创建者使用。
				other, _ := f.key(t, "replacement", "/", TextShare|TextManage|MountUpload|FileShare|FileManage)
				current := createSlugTestShare(t, f, kind, other, storage, "reusable", 201)
				if current["id"] == old["id"] {
					t.Fatal("后缀回收复用了旧记录 ID")
				}
				var storedSlug string
				if err := f.app.db.QueryRow("SELECT slug FROM "+table+" WHERE id=?", old["id"]).Scan(&storedSlug); err != nil || storedSlug == "reusable" || !validSlug.MatchString(storedSlug) {
					t.Fatalf("旧后缀未释放或旧记录丢失: slug=%s err=%v", storedSlug, err)
				}
				if kind == "text" {
					var retained Paste
					if err := f.app.resource(context.Background(), table, "id", oldPaste.ID, &retained); err != nil {
						t.Fatal(err)
					}
					oldPaste.Slug = storedSlug
					if !reflect.DeepEqual(retained, oldPaste) {
						t.Fatalf("回收改动了旧文本内容或失效状态: %+v", retained)
					}
					// 即使管理员保留着旧编辑窗口，也不能误改后缀上的新记录。
					f.request(t, "PUT", update, f.admin, map[string]any{"id": oldPaste.ID, "content": "stale edit"}, nil, 404)
				} else {
					var retained Share
					if err := f.app.resource(context.Background(), table, "id", oldShare.ID, &retained); err != nil {
						t.Fatal(err)
					}
					oldShare.Slug = storedSlug
					if !reflect.DeepEqual(retained, oldShare) {
						t.Fatal("回收改动了旧文件来源或失效状态")
					}
					link := "downloadUrl"
					if kind == "collection" {
						link = "archiveUrl"
					}
					f.request(t, "GET", lease[link].(string), "", nil, nil, 403)
					if kind == "collection" {
						file := lease["files"].([]any)[0].(map[string]any)
						f.request(t, "GET", file["contentUrl"].(string), "", nil, nil, 403)
					}
				}
				locked := dataMap(t, f.request(t, "GET", get, "", nil, map[string]string{"X-Share-Access": lease["access_token"].(string)}, 200))
				if locked["requiresPassword"] != true && locked["requires_password"] != true {
					t.Fatal("旧解锁凭证可以读取新记录")
				}
				opened := dataMap(t, f.request(t, "POST", verify, "", map[string]any{"password": "share-password"}, nil, 200))
				if opened["id"] != current["id"] || opened["views"] != float64(1) {
					t.Fatalf("原地址没有指向独立的新记录: %v", opened)
				}
			})
		}
	}
}

func TestRenameToExpiredShareSlug(t *testing.T) {
	for _, kind := range []string{"text", "file", "collection"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			storage := ""
			if kind != "text" {
				storage, _ = f.local(t, t.TempDir(), "/disk")
			}
			old := createSlugTestShare(t, f, kind, f.admin, storage, "target", 201)
			current := createSlugTestShare(t, f, kind, f.admin, storage, "current", 201)
			oldUpdate, currentUpdate, field := "/api/pastes/target", "/api/pastes/current", "newSlug"
			if kind != "text" {
				oldUpdate, currentUpdate, field = "/api/files/"+old["id"].(string), "/api/files/"+current["id"].(string), "slug"
			}
			f.request(t, "PUT", currentUpdate, f.admin, map[string]any{field: "target"}, nil, 400)
			past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			f.request(t, "PUT", oldUpdate, f.admin, map[string]any{"expires_at": past}, nil, 200)
			renamed := dataMap(t, f.request(t, "PUT", currentUpdate, f.admin, map[string]any{field: "target"}, nil, 200))
			if renamed["id"] != current["id"] || renamed["slug"] != "target" {
				t.Fatal("改名没有回收失效后缀")
			}
		})
	}
}

// 人为让新记录写入失败，确认事务同时回滚后缀回收；文件和合集共用后缀空间。
func TestShareSlugReuseRollback(t *testing.T) {
	for _, kind := range []string{"text", "file", "collection"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			storage, table := "", "pastes"
			if kind != "text" {
				storage, _ = f.local(t, t.TempDir(), "/disk")
				table = "shares"
			}
			oldKind := kind
			if kind == "collection" {
				oldKind = "file"
			}
			old := createSlugTestShare(t, f, oldKind, f.admin, storage, "rollback", 201)
			update := "/api/pastes/rollback"
			if kind != "text" {
				update = "/api/files/" + old["id"].(string)
			}
			past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			f.request(t, "PUT", update, f.admin, map[string]any{"expires_at": past}, nil, 200)
			if kind == "collection" {
				f.request(t, "POST", "/api/share/collection", f.admin, map[string]any{"slug": "rollback", "ids": []string{"missing-file"}}, nil, 404)
			}
			if _, err := f.app.db.Exec("CREATE TRIGGER reject_replacement BEFORE INSERT ON " + table + " WHEN NEW.slug='rollback' BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
				t.Fatal(err)
			}
			status := 500
			if kind == "file" {
				status = 409
			}
			if kind == "text" {
				f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "rollback", "content": "replacement"}, nil, status)
			} else {
				createSlugTestShare(t, f, kind, f.admin, storage, "rollback", status)
			}
			var retained struct{ ID, Slug string }
			if err := f.app.resource(context.Background(), table, "slug", "rollback", &retained); err != nil || retained.ID != old["id"] || retained.Slug != "rollback" {
				t.Fatalf("失败创建释放了旧后缀: %+v err=%v", retained, err)
			}
			if _, err := f.app.db.Exec("DROP TRIGGER reject_replacement"); err != nil {
				t.Fatal(err)
			}
			// 失败后仍能正常重试，也验证文件后缀可以被合集复用。
			createSlugTestShare(t, f, kind, f.admin, storage, "rollback", 201)
		})
	}
}

func TestConcurrentExpiredPasteSlugReuse(t *testing.T) {
	f := newFixture(t)
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "concurrent", "content": "old", "expires_at": past}, nil, 201)
	var successes atomic.Int32
	var group sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		group.Go(func() {
			<-start
			body, _ := json.Marshal(map[string]any{"slug": "concurrent", "content": "new", "expires_at": nil})
			r := httptest.NewRequest("POST", "/api/admin/paste", bytes.NewReader(body))
			r.Header.Set("Authorization", f.admin)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code == 201 {
				successes.Add(1)
			} else if w.Code != 400 {
				t.Errorf("并发回收返回意外状态: %d %s", w.Code, w.Body.String())
			}
		})
	}
	close(start)
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("同一后缀成功创建了 %d 条记录", successes.Load())
	}
	var count int
	if err := f.app.db.QueryRow("SELECT count(*) FROM pastes").Scan(&count); err != nil || count != 2 {
		t.Fatalf("并发回收丢失旧记录或创建了重复记录: %d %v", count, err)
	}
}

func TestPrivatePasteSlugRemainsOccupied(t *testing.T) {
	f := newFixture(t)
	f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "private", "content": "hidden", "is_public": false, "expires_at": nil}, nil, 201)
	f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "private", "content": "replacement"}, nil, 400)
}
