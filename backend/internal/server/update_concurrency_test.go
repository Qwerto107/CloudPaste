package server

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pausedUpdateBody struct {
	reader           io.Reader
	entered, release chan struct{}
	paused           bool
}

// 暂停接收 JSON，模拟客户端已经通过入口认证但请求体仍未发送完成。
func (b *pausedUpdateBody) Read(p []byte) (int, error) {
	if !b.paused {
		b.paused = true
		close(b.entered)
		<-b.release
	}
	return b.reader.Read(p)
}

func pausedUpdate(t *testing.T, f *fixture, target, auth, body string) (func(), <-chan *httptest.ResponseRecorder) {
	t.Helper()
	reader := &pausedUpdateBody{reader: strings.NewReader(body), entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan *httptest.ResponseRecorder, 1)
	r := httptest.NewRequest("PUT", target, reader)
	r.Header.Set("Authorization", auth)
	go func() {
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		done <- w
	}()
	resume := func() {
		select {
		case <-reader.release:
		default:
			close(reader.release)
		}
	}
	t.Cleanup(resume)
	select {
	case <-reader.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("更新请求未开始接收请求体")
	}
	return resume, done
}

func awaitUpdate(t *testing.T, done <-chan *httptest.ResponseRecorder, status int) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case w := <-done:
		if w.Code != status {
			t.Fatalf("更新返回 %d，期望 %d: %s", w.Code, status, w.Body.String())
		}
		return w
	case <-time.After(2 * time.Second):
		t.Fatal("更新请求未结束")
		return nil
	}
}

func updateFixture(t *testing.T, kind string) (*fixture, string, string, string, string) {
	t.Helper()
	f := newFixture(t)
	f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "independent", "content": "independent text"}, nil, 201)
	if kind == "paste" {
		auth, keyID := f.key(t, "editor", "/", TextShare|TextManage)
		view := dataMap(t, f.request(t, "POST", "/api/paste", auth, map[string]any{"slug": "owned-text", "content": "original"}, nil, 201))
		return f, auth, keyID, "/api/pastes/owned-text", view["id"].(string)
	}
	root := t.TempDir()
	writeFile(t, root, "file.txt", "shared content")
	f.local(t, root, "/disk")
	auth, keyID := f.key(t, "editor", "/disk", MountView|FileShare|FileManage)
	view := dataMap(t, f.request(t, "POST", "/api/fs/create-share", auth, map[string]any{"path": "/disk/file.txt", "slug": "owned-file"}, nil, 201))
	return f, auth, keyID, "/api/files/" + view["id"].(string), view["id"].(string)
}

func TestSlowUpdatesDoNotBlockReadsOrLoseConcurrentChanges(t *testing.T) {
	for _, kind := range []string{"paste", "share"} {
		t.Run(kind, func(t *testing.T) {
			f, auth, _, target, _ := updateFixture(t, kind)
			resume, done := pausedUpdate(t, f, target, auth, `{"remark":"slow-update","password":"new-password"}`)
			read := asyncVisitorRead(f, "/api/paste/independent")
			select {
			case w := <-read:
				if w.Code != 200 {
					t.Fatalf("无关文本读取返回 %d", w.Code)
				}
			case <-time.After(time.Second):
				resume()
				awaitUpdate(t, done, 200)
				<-read
				t.Fatal("慢更新仍阻塞无关文本")
			}
			// 另一次更新和公开查看必须被保留，不能被慢请求的旧快照覆盖。
			fields := map[string]any{"max_views": 9}
			public := "/api/share/get/owned-file"
			if kind == "paste" {
				fields = map[string]any{"title": "concurrent-title"}
				public = "/api/paste/owned-text"
			}
			f.request(t, "PUT", target, auth, fields, nil, 200)
			f.request(t, "GET", public, "", nil, nil, 200)
			resume()
			view := dataMap(t, awaitUpdate(t, done, 200))
			if view["views"] != float64(1) || view["remark"] != "slow-update" {
				t.Fatalf("慢更新覆盖了查看次数或未应用字段: %v", view)
			}
			if kind == "paste" && view["title"] != "concurrent-title" {
				t.Fatal("慢更新覆盖了并发标题修改")
			}
			if kind == "share" && view["max_views"] != float64(9) {
				t.Fatal("慢更新覆盖了并发查看上限修改")
			}
		})
	}
}

func TestSlowUpdatesRecheckRevokedPermissionAndDeletedRecord(t *testing.T) {
	for _, kind := range []string{"paste", "share"} {
		for _, change := range []string{"permission", "deleted"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				f, auth, keyID, target, id := updateFixture(t, kind)
				resume, done := pausedUpdate(t, f, target, auth, `{"remark":"unauthorized-change"}`)
				status := 403
				if change == "permission" {
					f.request(t, "PUT", "/api/admin/api-keys/"+keyID, f.admin, map[string]any{"permissions": 0}, nil, 200)
				} else {
					entry := "/api/pastes/batch-delete"
					if kind == "share" {
						entry = "/api/files/batch-delete"
					}
					f.request(t, "DELETE", entry, f.admin, map[string]any{"ids": []string{id}}, nil, 200)
					status = 404
				}
				resume()
				awaitUpdate(t, done, status)
			})
		}
	}
}

func TestSlowShareCreationDoesNotHoldGlobalLockAndRechecksSource(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "file.txt", "shared content")
	storage, mount := f.local(t, root, "/disk")
	auth, _ := f.key(t, "creator", "/disk", MountView|FileShare)
	// 先初始化驱动，再暂停真实文件查询。
	f.request(t, "GET", "/api/fs/get?path=/disk/file.txt", auth, nil, nil, 200)
	d := pauseShareStorage(t, f, storage)
	done := make(chan *httptest.ResponseRecorder, 1)
	r := httptest.NewRequest("POST", "/api/fs/create-share", bytes.NewBufferString(`{"path":"/disk/file.txt","slug":"pending-share"}`))
	r.Header.Set("Authorization", auth)
	go func() {
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		done <- w
	}()
	waitForShareStorage(t, d)
	changed := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest("PUT", "/api/admin/mount/"+mount, bytes.NewBufferString(`{"root_path":"/replacement"}`))
		request.Header.Set("Authorization", f.admin)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, request)
		changed <- w
	}()
	select {
	case w := <-changed:
		if w.Code != 200 {
			t.Fatalf("挂载更新返回 %d: %s", w.Code, w.Body.String())
		}
	case <-time.After(time.Second):
		d.resume()
		<-done
		<-changed
		t.Fatal("创建分享仍阻塞挂载更新")
	}
	d.resume()
	awaitUpdate(t, done, 410)
	var s Share
	if err := f.app.resource(context.Background(), "shares", "slug", "pending-share", &s); err == nil {
		t.Fatal("来源已变化却仍保存了分享")
	}
}
