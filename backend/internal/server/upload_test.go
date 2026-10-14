package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func (f *fixture) upload(t *testing.T, method, target, auth string, body io.Reader, headers map[string]string, status int) *httptest.ResponseRecorder {
	t.Helper()
	// 管理员业务上传使用新入口，原始入口的隔离由独立安全测试验证。
	if auth != "" && auth == f.admin && strings.HasPrefix(target, "/api/") && !isAdminPath(strings.SplitN(target, "?", 2)[0]) {
		target = adminAPIPrefix + strings.TrimPrefix(target, "/api")
	}
	r := httptest.NewRequest(method, target, body)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("upload %s: status=%d want=%d %s", strings.Split(target, "?")[0], w.Code, status, w.Body.String())
	}
	return w
}
func uploadHeaders(name string, fields map[string]any, share bool) map[string]string {
	prefix := "X-FS-"
	if share {
		prefix = "X-Share-"
	}
	b, _ := json.Marshal(fields)
	return map[string]string{prefix + "Filename": url.PathEscape(name), prefix + "Options": base64.StdEncoding.EncodeToString(b), "Content-Type": "text/plain"}
}

// 当前页面通过原始 PUT 上传，权限、中文元信息与临时额度必须一起校验。
func TestUploadsScopeLimitsNoOverwriteAndCleanup(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	storage, _ := f.local(t, root, "/disk")
	writer, _ := f.key(t, "upload", "/disk/public", MountUpload|FileShare)
	reader, _ := f.key(t, "reader", "/", MountView)
	fields := map[string]any{"storage_config_id": storage, "path": "/disk/public", "original_filename": true}
	headers := uploadHeaders("中文.txt", fields, true)
	f.upload(t, "PUT", "/api/share/upload", "", strings.NewReader("内容"), headers, 401)
	f.upload(t, "PUT", "/api/share/upload", reader, strings.NewReader("内容"), headers, 403)
	denied := uploadHeaders("中文.txt", map[string]any{"storage_config_id": storage, "path": "/disk/private"}, true)
	f.upload(t, "PUT", "/api/share/upload", writer, strings.NewReader("内容"), denied, 403)
	share := dataMap(t, f.upload(t, "PUT", "/api/share/upload", writer, strings.NewReader("内容"), headers, 201))
	f.upload(t, "PUT", "/api/share/upload", writer, strings.NewReader("overwrite"), headers, 409)
	view := dataMap(t, f.request(t, "GET", "/api/share/get/"+share["slug"].(string), "", nil, nil, 200))
	if got := f.request(t, "GET", view["contentUrl"].(string), "", nil, nil, 200).Body.String(); got != "内容" {
		t.Fatal(got)
	}
	for _, name := range []string{"../escape", "sub/file", "sub\\file", ".cloudpaste-upload-secret", ".CLOUDPASTE-UPLOAD-secret", "bad\x00name", "bad\nname", "bad:stream"} {
		f.upload(t, "PUT", "/api/share/upload", writer, strings.NewReader("bad"), uploadHeaders(name, fields, true), 400)
	}
	invalid := uploadHeaders("invalid.txt", map[string]any{"storage_config_id": storage, "path": "/disk/public/../private"}, true)
	f.upload(t, "PUT", "/api/share/upload", writer, strings.NewReader("bad"), invalid, 400)
	f.app.cfg.MaxUploadBytes = 8
	r := httptest.NewRequest("PUT", "/api/share/upload", strings.NewReader("more than eight bytes"))
	r.ContentLength = -1
	r.Header.Set("Authorization", writer)
	for key, value := range uploadHeaders("large.txt", fields, true) {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "public/large.txt")); !os.IsNotExist(err) {
		t.Fatal("oversized file published", err)
	}
	f.request(t, "PUT", "/api/storage/"+storage, f.admin, map[string]any{"readonly": true}, nil, 200)
	f.upload(t, "PUT", "/api/share/upload", writer, strings.NewReader("ok"), uploadHeaders("readonly.txt", fields, true), 403)
	entries, err := os.ReadDir(f.app.uploadDir)
	if err != nil || len(entries) != 0 || f.app.rawReserved != 0 {
		t.Fatal("temporary files leaked", entries, err)
	}
}

func TestUploadShareUnicodeAndUnmappedStorage(t *testing.T) {
	f := newFixture(t)
	storage := dataMap(t, f.request(t, "POST", "/api/storage", f.admin, map[string]any{"name": "unmounted", "storage_type": "LOCAL", "root_path": t.TempDir(), "is_public": true}, nil, 201))["id"].(string)
	fields := map[string]any{"storage_config_id": storage, "remark": "中文备注", "password": "中文密码", "max_views": 1}
	share := dataMap(t, f.upload(t, "PUT", "/api/share/upload", f.admin, strings.NewReader("unmapped"), uploadHeaders("原名.txt", fields, true), 201))
	if share["remark"] != "中文备注" || share["storage_key"] != nil {
		t.Fatal("分享响应泄露来源或损坏中文元信息")
	}
	f.request(t, "GET", "/api/s/"+share["slug"].(string), "", nil, nil, 403)
	view := dataMap(t, f.request(t, "POST", "/api/share/verify/"+share["slug"].(string), "", map[string]any{"password": "中文密码"}, nil, 200))
	if f.request(t, "GET", view["contentUrl"].(string), "", nil, nil, 200).Body.String() != "unmapped" {
		t.Fatal("unmapped share failed")
	}
	f.request(t, "GET", "/api/share/get/"+share["slug"].(string), "", nil, nil, 404)
	scoped, _ := f.key(t, "scoped", "/disk", MountUpload|FileShare)
	f.upload(t, "PUT", "/api/share/upload", scoped, strings.NewReader("blocked"), uploadHeaders("a.txt", fields, true), 403)
}

// 旧版本的目录密码仍限制当前上传，不能因移除配置接口而绕过。
func TestUploadRespectsStoredDirectoryPasswords(t *testing.T) {
	f := newFixture(t)
	storage, _ := f.local(t, t.TempDir(), "/disk")
	writer, _ := f.key(t, "writer", "/disk", MountUpload|FileShare)
	hash, err := passwordHash("directory-password")
	if err != nil {
		t.Fatal(err)
	}
	meta := FsMeta{ID: randomID(), Path: "/disk/locked", Password: hash, PasswordInherit: true}
	if err = f.app.saveResource(context.Background(), "fs_meta", meta.ID, "", "", meta); err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{"storage_config_id": storage, "path": "/disk/locked"}
	f.upload(t, "PUT", "/api/share/upload", writer, strings.NewReader("blocked"), uploadHeaders("a.txt", fields, true), 403)
}

func TestUploadCannotEscapeThroughDirectoryLinks(t *testing.T) {
	f := newFixture(t)
	root, outside := t.TempDir(), t.TempDir()
	storage, _ := f.local(t, root, "/disk")
	directoryLink(t, filepath.Join(root, "escape"), outside)
	fields := map[string]any{"storage_config_id": storage, "path": "/disk/escape", "original_filename": true}
	f.upload(t, "PUT", "/api/share/upload", f.admin, strings.NewReader("bad"), uploadHeaders("outside.txt", fields, true), 403)
	if _, err := os.Stat(filepath.Join(outside, "outside.txt")); !os.IsNotExist(err) {
		t.Fatal("upload escaped storage root")
	}
}

func TestConcurrentUploadNeverOverwrites(t *testing.T) {
	f := newFixture(t)
	storage, _ := f.local(t, t.TempDir(), "/disk")
	var group sync.WaitGroup
	statuses := make(chan int, 2)
	for _, content := range []string{"first", "second"} {
		group.Go(func() {
			r := httptest.NewRequest("PUT", "/api/admin/share/upload", strings.NewReader(content))
			r.Header.Set("Authorization", f.admin)
			for k, v := range uploadHeaders("same.txt", map[string]any{"storage_config_id": storage, "path": "/disk", "original_filename": true}, true) {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			statuses <- w.Code
		})
	}
	group.Wait()
	close(statuses)
	successes, conflicts := 0, 0
	for status := range statuses {
		if status == 201 {
			successes++
		}
		if status == 409 {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("concurrent publication did not preserve one file", successes, conflicts)
	}
}

type callbackReader struct {
	io.Reader
	once     sync.Once
	callback func()
}

func (r *callbackReader) Read(data []byte) (int, error) {
	r.once.Do(r.callback)
	return r.Reader.Read(data)
}

func TestUploadRevocationWhileReceivingBody(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	storage, _ := f.local(t, root, "/disk")
	writer, id := f.key(t, "writer", "/disk", MountUpload|FileShare)
	// 初始认证后撤销权限，完整接收也不能继续发布文件。
	body := &callbackReader{Reader: strings.NewReader("contents"), callback: func() {
		f.request(t, "PUT", "/api/admin/api-keys/"+id, f.admin, map[string]any{"permissions": MountView}, nil, 200)
	}}
	fields := map[string]any{"storage_config_id": storage, "path": "/disk", "original_filename": true}
	f.upload(t, "PUT", "/api/share/upload", writer, body, uploadHeaders("revoked.txt", fields, true), 403)
	if _, err := os.Stat(filepath.Join(root, "revoked.txt")); !os.IsNotExist(err) {
		t.Fatal("revoked upload published")
	}
}

func TestS3PublishWithNoOverwrite(t *testing.T) {
	t.Run("S3", func(t *testing.T) {
		var received string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" || r.Header.Get("If-None-Match") != "*" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
				t.Error("missing conditional signed S3 upload")
				w.WriteHeader(400)
				return
			}
			b, _ := io.ReadAll(r.Body)
			received = string(b)
			if r.URL.Path == "/bucket/existing.txt" {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(412)
				io.WriteString(w, `<Error><Code>PreconditionFailed</Code></Error>`)
				return
			}
			w.Header().Set("ETag", `"remote-etag"`)
		}))
		defer upstream.Close()
		driver, err := newS3Driver(Storage{Endpoint: upstream.URL, Bucket: "bucket", Region: "us-east-1", AccessKey: "test", SecretKey: "secret", PathStyle: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer driver.Close()
		file := stageTestFile(t, "contents")
		if err = driver.(uploadWriter).PutNew(context.Background(), "new.txt", file, 8, "text/plain"); err != nil {
			t.Fatal(err)
		}
		if received != "contents" {
			t.Fatal(received)
		}
		_, _ = file.Seek(0, io.SeekStart)
		var api *apiError
		if err = driver.(uploadWriter).PutNew(context.Background(), "existing.txt", file, 8, "text/plain"); !isAPIStatus(err, 409, &api) {
			t.Fatal("S3 overwrite not rejected", err)
		}
	})
}

func isAPIStatus(err error, status int, api **apiError) bool {
	if err == nil {
		return false
	}
	switch e := err.(type) {
	case *apiError:
		*api = e
		return e.Status == status
	default:
		return false
	}
}

func stageTestFile(t *testing.T, content string) *os.File {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "staged")
	if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// 上游不支持条件发布时，应失败并清理分片，不能悄悄降级为覆盖写入。
func TestS3LargeMultipartFailureAborts(t *testing.T) {
	parts, aborts := 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		q := r.URL.Query()
		switch {
		case r.Method == "POST" && q.Has("uploads"):
			io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>remote-upload</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == "PUT" && q.Get("uploadId") == "remote-upload":
			parts++
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, parts))
		case r.Method == "POST" && q.Get("uploadId") == "remote-upload":
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("multipart completion lacks no-overwrite")
			}
			w.WriteHeader(412)
			io.WriteString(w, `<Error><Code>PreconditionFailed</Code></Error>`)
		case r.Method == "DELETE":
			aborts++
			w.WriteHeader(204)
		default:
			t.Error("unexpected S3 multipart operation", r.Method, q)
			w.WriteHeader(400)
		}
	}))
	defer upstream.Close()
	driver, err := newS3Driver(Storage{Endpoint: upstream.URL, Bucket: "bucket", Region: "us-east-1", AccessKey: "test", SecretKey: "secret", PathStyle: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	f, err := os.CreateTemp(t.TempDir(), "large-")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	const size = 65 << 20
	if err = f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	var api *apiError
	err = driver.(uploadWriter).PutNew(context.Background(), "large.bin", f, size, "application/octet-stream")
	if !isAPIStatus(err, 409, &api) || parts != 9 || aborts != 1 {
		t.Fatal("large multipart failure not safely aborted", err, parts, aborts)
	}
}
