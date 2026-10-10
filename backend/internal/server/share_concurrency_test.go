package server

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type pausedShareDriver struct {
	Driver
	entered, release       chan struct{}
	enterOnce, releaseOnce sync.Once
}

// 模拟慢存储，暂停查询但允许管理接口修改当前授权状态。
func (d *pausedShareDriver) Stat(ctx context.Context, key string) (FileInfo, error) {
	d.enterOnce.Do(func() { close(d.entered) })
	select {
	case <-d.release:
	case <-ctx.Done():
		return FileInfo{}, ctx.Err()
	}
	return d.Driver.Stat(ctx, key)
}
func (d *pausedShareDriver) resume() { d.releaseOnce.Do(func() { close(d.release) }) }

func pauseShareStorage(t *testing.T, f *fixture, storageID string) *pausedShareDriver {
	t.Helper()
	d := &pausedShareDriver{entered: make(chan struct{}), release: make(chan struct{})}
	f.app.clientsMu.Lock()
	entry, ok := f.app.storageClients[storageID]
	if !ok {
		f.app.clientsMu.Unlock()
		t.Fatal("share driver not cached")
	}
	d.Driver = entry.Driver
	entry.Driver = d
	f.app.storageClients[storageID] = entry
	f.app.clientsMu.Unlock()
	t.Cleanup(d.resume)
	return d
}

func asyncVisitorRead(f *fixture, target string) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r := httptest.NewRequest("GET", target, nil).WithContext(ctx)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		done <- w
	}()
	return done
}

func waitForShareStorage(t *testing.T, d *pausedShareDriver) {
	t.Helper()
	select {
	case <-d.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("share did not reach storage")
	}
}

func TestSlowPublicShareDoesNotBlockUnrelatedText(t *testing.T) {
	for _, prefix := range []string{"/api/share/get/", "/api/s/"} {
		t.Run(prefix, func(t *testing.T) {
			f := newFixture(t)
			root := t.TempDir()
			writeFile(t, root, "file.txt", "shared content")
			storage, _ := f.local(t, root, "/disk")
			share := dataMap(t, f.request(t, "POST", "/api/fs/create-share", f.admin, map[string]any{"path": "/disk/file.txt"}, nil, 201))
			f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "independent", "content": "independent text"}, nil, 201)
			d := pauseShareStorage(t, f, storage)
			shareDone := asyncVisitorRead(f, prefix+share["slug"].(string))
			waitForShareStorage(t, d)
			textDone := asyncVisitorRead(f, "/api/paste/independent")
			select {
			case w := <-textDone:
				if w.Code != 200 {
					t.Fatalf("text status=%d body=%s", w.Code, w.Body.String())
				}
			case <-time.After(time.Second):
				d.resume()
				<-shareDone
				<-textDone
				t.Fatal("slow share blocked unrelated text")
			}
			d.resume()
			if w := <-shareDone; w.Code != 200 {
				t.Fatalf("share status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestShareStateChangesDuringStorageQueryRejectRead(t *testing.T) {
	for _, prefix := range []string{"/api/share/get/", "/api/s/"} {
		for _, change := range []string{"password", "source", "permission", "deleted"} {
			t.Run(prefix+change, func(t *testing.T) {
				f := newFixture(t)
				root := t.TempDir()
				writeFile(t, root, "file.txt", "shared secret")
				storage, mount := f.local(t, root, "/disk")
				creator, keyID := f.key(t, "creator", "/disk", MountView|FileShare)
				share := dataMap(t, f.request(t, "POST", "/api/fs/create-share", creator, map[string]any{"path": "/disk/file.txt"}, nil, 201))
				d := pauseShareStorage(t, f, storage)
				shareDone := asyncVisitorRead(f, prefix+share["slug"].(string))
				waitForShareStorage(t, d)
				changed := make(chan struct{})
				go func() {
					defer close(changed)
					switch change {
					case "password":
						f.request(t, "PUT", "/api/files/"+share["id"].(string), f.admin, map[string]any{"password": "new-password"}, nil, 200)
					case "source":
						f.request(t, "PUT", "/api/mount/"+mount, f.admin, map[string]any{"root_path": "/replacement"}, nil, 200)
					case "permission":
						f.request(t, "PUT", "/api/admin/api-keys/"+keyID, f.admin, map[string]any{"permissions": 0}, nil, 200)
					case "deleted":
						f.request(t, "DELETE", "/api/files/batch-delete", f.admin, map[string]any{"ids": []string{share["id"].(string)}, "delete_mode": "record_only"}, nil, 200)
					}
				}()
				select {
				case <-changed:
				case <-time.After(time.Second):
					d.resume()
					<-shareDone
					<-changed
					t.Fatal("slow share blocked management change")
				}
				d.resume()
				w := <-shareDone
				expected := 403
				if change == "source" {
					expected = 410
				}
				if change == "deleted" {
					expected = 404
				}
				if w.Code != expected {
					t.Fatalf("status=%d want=%d body=%s", w.Code, expected, w.Body.String())
				}
			})
		}
	}
}

func TestConcurrentPublicShareHonorsLastView(t *testing.T) {
	for _, prefix := range []string{"/api/share/get/", "/api/s/"} {
		t.Run(prefix, func(t *testing.T) {
			f := newFixture(t)
			root := t.TempDir()
			writeFile(t, root, "file.txt", "shared content")
			f.local(t, root, "/disk")
			share := dataMap(t, f.request(t, "POST", "/api/fs/create-share", f.admin, map[string]any{"path": "/disk/file.txt", "max_views": 1}, nil, 201))
			var successes atomic.Int32
			var group sync.WaitGroup
			for i := 0; i < 8; i++ {
				group.Go(func() {
					w := <-asyncVisitorRead(f, prefix+share["slug"].(string))
					if w.Code == 200 {
						successes.Add(1)
					} else if w.Code != 410 {
						t.Errorf("unexpected share status=%d body=%s", w.Code, w.Body.String())
					}
				})
			}
			group.Wait()
			if successes.Load() != 1 {
				t.Fatalf("max_views=1 allowed %d reads", successes.Load())
			}
		})
	}
}

func TestShareTicketExpiringDuringStorageQueryIsRejected(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "file.txt", "shared secret")
	storage, _ := f.local(t, root, "/disk")
	view := dataMap(t, f.request(t, "POST", "/api/fs/create-share", f.admin, map[string]any{"path": "/disk/file.txt", "password": "share-password"}, nil, 201))
	var share Share
	if err := f.app.resource(context.Background(), "shares", "id", view["id"].(string), &share); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Unix() + 2
	ticket := f.app.ticket(Ticket{Purpose: "share", Path: share.Slug, Subject: share.ID, Version: shareVersion(share), Expires: expires})
	d := pauseShareStorage(t, f, storage)
	done := asyncVisitorRead(f, "/api/s/"+share.Slug+"?token="+ticket)
	waitForShareStorage(t, d)
	// 模拟存储查询跨过票据到期时间，返回时不能继续使用旧授权。
	time.Sleep(time.Until(time.Unix(expires, 0)) + 20*time.Millisecond)
	d.resume()
	if w := <-done; w.Code != 403 {
		t.Fatalf("expired ticket status=%d body=%s", w.Code, w.Body.String())
	}
}
