package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 文本、单文件与合集都必须支持刷新，同时保持密码、期限与查看额度约束。
func TestProtectedShareAccessRefresh(t *testing.T) {
	for _, kind := range []string{"text", "file", "collection"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			var created map[string]any
			var get, verify, update, remove, purpose string
			if kind == "text" {
				created = dataMap(t, f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "protected", "content": "protected contents", "password": "share-password", "max_views": 3}, nil, 201))
				get, verify, update, remove, purpose = "/api/paste/protected", "/api/paste/protected", "/api/pastes/protected", "/api/pastes/batch-delete", "paste-access"
			} else {
				storage, _ := f.local(t, t.TempDir(), "/disk")
				fields := map[string]any{"storage_config_id": storage, "password": "share-password", "max_views": 3, "slug": "protected"}
				if kind == "collection" {
					fields = map[string]any{"storage_config_id": storage}
				}
				created = dataMap(t, f.upload(t, "PUT", "/api/share/upload", f.admin, strings.NewReader("protected contents"), uploadHeaders("first.txt", fields, true), 201))
				if kind == "collection" {
					second := dataMap(t, f.upload(t, "PUT", "/api/share/upload", f.admin, strings.NewReader("second contents"), uploadHeaders("second.txt", fields, true), 201))
					created = dataMap(t, f.request(t, "POST", "/api/share/collection", f.admin, map[string]any{"ids": []string{created["id"].(string), second["id"].(string)}, "slug": "protected", "password": "share-password", "max_views": 3}, nil, 201))
				}
				get, verify, update, remove, purpose = "/api/share/get/protected", "/api/share/verify/protected", "/api/files/"+created["id"].(string), "/api/files/batch-delete", "share-access"
			}
			missing := f.request(t, "GET", get+"-missing", "", nil, nil, 404).Body.String()
			// 不存在、过期和次数耗尽应返回相同响应，不泄露是否有密码。
			expectUnavailable := func(method, target string, body any, headers map[string]string) {
				t.Helper()
				w := f.request(t, method, target, "", body, headers, 404)
				if w.Body.String() != missing {
					t.Fatalf("失效分享响应不一致: got=%s want=%s", w.Body.String(), missing)
				}
			}
			initial := dataMap(t, f.request(t, "POST", verify, "", map[string]any{"password": "share-password"}, nil, 200))
			token := initial["access_token"].(string)
			access, err := f.app.parseTicket(token, purpose, "protected")
			if err != nil || access.Expires <= time.Now().Unix() || access.Expires > time.Now().Add(shareAccessDuration).Unix() {
				t.Fatalf("访问期限不正确: access=%+v err=%v", access, err)
			}
			lastView := initial
			for count := 2; count <= 3; count++ {
				refreshed := dataMap(t, f.request(t, "GET", get, "", nil, map[string]string{"X-Share-Access": token}, 200))
				if refreshed["views"] != float64(count) || refreshed["access_token"] != token || refreshed["access_expires_at"] != initial["access_expires_at"] {
					t.Fatalf("刷新未正确计数或延长了访问期限: %v", refreshed)
				}
				lastView = refreshed
			}
			// 凭证不能绕过上限，拒绝请求也不能继续增加次数。
			expectUnavailable("GET", get, nil, map[string]string{"X-Share-Access": token})
			expectUnavailable("GET", get, nil, nil)
			expectUnavailable("POST", verify, map[string]any{"password": "share-password"}, nil)
			expectUnavailable("POST", verify, map[string]any{"password": "wrong-password"}, nil)
			expectUnavailable("POST", verify, map[string]any{}, nil)
			if lastView["isLastView"] != true {
				t.Fatal("最后一次查看缺少提示")
			}
			if kind == "text" {
				expectUnavailable("GET", "/api/raw/protected", nil, map[string]string{"X-Share-Access": token})
				var current Paste
				if err := f.app.resource(context.Background(), "pastes", "id", created["id"].(string), &current); err != nil || current.Views != 3 {
					t.Fatalf("文本查看次数不正确: views=%d err=%v", current.Views, err)
				}
			} else {
				// 最后一次查看的文件仍可预览、下载或打包，不重复消耗查看次数。
				if kind == "collection" {
					for _, item := range lastView["files"].([]any) {
						file := item.(map[string]any)
						f.request(t, "GET", file["contentUrl"].(string), "", nil, nil, 200)
						f.request(t, "GET", file["downloadUrl"].(string), "", nil, nil, 200)
					}
					f.request(t, "GET", lastView["archiveUrl"].(string), "", nil, nil, 200)
				} else {
					link := lastView["previewUrl"].(string)
					f.request(t, "GET", link, "", nil, map[string]string{"Range": "bytes=0-3"}, 206)
					f.request(t, "GET", link, "", nil, map[string]string{"Range": "bytes=4-7"}, 206)
					f.request(t, "GET", lastView["downloadUrl"].(string), "", nil, nil, 200)
				}
				var current Share
				if err := f.app.resource(context.Background(), "shares", "id", created["id"].(string), &current); err != nil || current.Views != 3 {
					t.Fatalf("文件读取重复计数: views=%d err=%v", current.Views, err)
				}
			}
			if kind != "text" {
				// 解锁凭证不能替代文件下载票据。
				f.request(t, "GET", "/api/s/protected?token="+token, "", nil, nil, 403)
			}
			f.request(t, "PUT", update, f.admin, map[string]any{"max_views": 0}, nil, 200)
			invalid := []string{token + "tampered"}
			for _, change := range []func(*Ticket){
				func(v *Ticket) { v.Expires = time.Now().Unix() - 1 },
				func(v *Ticket) { v.Path = "another-share" },
				func(v *Ticket) { v.Subject = "another-record" },
				func(v *Ticket) { v.Purpose = "share" },
			} {
				other := access
				change(&other)
				invalid = append(invalid, f.app.ticket(other))
			}
			for _, badToken := range invalid {
				view := dataMap(t, f.request(t, "GET", get, "", nil, map[string]string{"X-Share-Access": badToken}, 200))
				if view["requiresPassword"] != true && view["requires_password"] != true {
					t.Fatal("无效凭证绕过密码验证")
				}
			}
			// 多个请求携带同一凭证并发刷新，也只能消耗剩余的一次额度。
			f.request(t, "PUT", update, f.admin, map[string]any{"max_views": 2}, nil, 200)
			concurrentView := dataMap(t, f.request(t, "POST", verify, "", map[string]any{"password": "share-password"}, nil, 200))
			concurrentToken := concurrentView["access_token"].(string)
			var successes atomic.Int32
			var group sync.WaitGroup
			for range 8 {
				group.Go(func() {
					r := httptest.NewRequest("GET", get, nil)
					r.Header.Set("X-Share-Access", concurrentToken)
					w := httptest.NewRecorder()
					f.handler.ServeHTTP(w, r)
					if w.Code == 200 {
						successes.Add(1)
					} else if w.Code != 404 {
						t.Errorf("并发刷新返回意外状态: %d %s", w.Code, w.Body.String())
					}
				})
			}
			group.Wait()
			if successes.Load() != 1 {
				t.Fatalf("剩余一次额度允许了 %d 次刷新", successes.Load())
			}
			f.request(t, "PUT", update, f.admin, map[string]any{"password": "replacement-password", "max_views": 0}, nil, 200)
			view := dataMap(t, f.request(t, "GET", get, "", nil, map[string]string{"X-Share-Access": token}, 200))
			if view["requiresPassword"] != true && view["requires_password"] != true {
				t.Fatal("改密码后旧凭证仍可访问")
			}
			newView := dataMap(t, f.request(t, "POST", verify, "", map[string]any{"password": "replacement-password"}, nil, 200))
			newHeaders := map[string]string{"X-Share-Access": newView["access_token"].(string)}
			if kind == "text" {
				f.request(t, "PUT", update, f.admin, map[string]any{"is_public": false}, nil, 200)
				f.request(t, "GET", get, "", nil, newHeaders, 404)
				f.request(t, "PUT", update, f.admin, map[string]any{"is_public": true}, nil, 200)
			}
			past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			// 已显示密码框后分享失效，再提交也必须转为统一提示，而非密码错误。
			f.request(t, "GET", get, "", nil, nil, 200)
			f.request(t, "PUT", update, f.admin, map[string]any{"expires_at": past}, nil, 200)
			expectUnavailable("GET", get, nil, newHeaders)
			expectUnavailable("GET", get, nil, nil)
			expectUnavailable("POST", verify, map[string]any{"password": "wrong-password"}, nil)
			if kind == "text" {
				expectUnavailable("GET", "/api/raw/protected", nil, nil)
			} else {
				expectUnavailable("GET", "/api/s/protected", nil, nil)
				expectUnavailable("GET", "/api/share/content/protected", nil, nil)
			}
			f.request(t, "DELETE", remove, f.admin, map[string]any{"ids": []string{created["id"].(string)}, "delete_mode": "record_only"}, nil, 200)
			expectUnavailable("GET", get, nil, newHeaders)
			expectUnavailable("POST", verify, map[string]any{"password": "wrong-password"}, nil)
		})
	}
}

// 存储查询较慢时，锁内也需要再次检查凭证是否已过期。
func TestShareAccessExpiresDuringStorageQuery(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "first.txt", "contents")
	storage, _ := f.local(t, root, "/disk")
	created := dataMap(t, f.request(t, "POST", "/api/fs/create-share", f.admin, map[string]any{"path": "/disk/first.txt", "password": "share-password"}, nil, 201))
	var share Share
	if err := f.app.resource(context.Background(), "shares", "id", created["id"].(string), &share); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Unix() + 2
	token := f.app.ticket(Ticket{Purpose: "share-access", Path: share.Slug, Subject: share.ID, Version: shareVersion(share), Expires: expires})
	d := pauseShareStorage(t, f, storage)
	done := make(chan int, 1)
	go func() {
		r := httptest.NewRequest("GET", "/api/share/get/"+share.Slug, nil)
		r.Header.Set("X-Share-Access", token)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		done <- w.Code
	}()
	waitForShareStorage(t, d)
	time.Sleep(time.Until(time.Unix(expires, 0)) + 20*time.Millisecond)
	d.resume()
	if status := <-done; status != 403 {
		t.Fatalf("过期的刷新凭证仍获准访问: %d", status)
	}
}
