package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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
func formUpload(t *testing.T, name, content string, fields map[string]string) (*bytes.Buffer, map[string]string) {
	t.Helper()
	b := new(bytes.Buffer)
	writer := multipart.NewWriter(b)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	for k, v := range fields {
		if err = writer.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return b, map[string]string{"Content-Type": writer.FormDataContentType()}
}
func TestUploadsScopeLimitsNoOverwriteAndCleanup(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	id, _ := f.local(t, root, "/disk")
	writer, _ := f.key(t, "upload", "/disk/public", MountUpload|MountView|FileShare)
	reader, _ := f.key(t, "reader", "/", MountView)
	headers := uploadHeaders("中文.txt", nil, false)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/public", "", strings.NewReader("内容"), headers, 401)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/public", reader, strings.NewReader("内容"), headers, 403)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/private", writer, strings.NewReader("内容"), headers, 403)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/public", writer, strings.NewReader("内容"), headers, 201)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/public", writer, strings.NewReader("overwrite"), headers, 409)
	if got := f.request(t, "GET", "/api/fs/content?path="+url.QueryEscape("/disk/public/中文.txt"), writer, nil, nil, 200).Body.String(); got != "内容" {
		t.Fatal(got)
	}
	for _, name := range []string{"../escape", "sub/file", "sub\\file", ".cloudpaste-upload-secret", ".CLOUDPASTE-UPLOAD-secret", "bad\x00name", "bad\nname", "bad:stream"} {
		f.upload(t, "PUT", "/api/fs/upload?path=/disk/public", writer, strings.NewReader("bad"), uploadHeaders(name, nil, false), 400)
	}
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/public/../private", writer, strings.NewReader("bad"), headers, 400)
	f.request(t, "GET", "/api/fs/content?path=/disk/public/.CLOUDPASTE-UPLOAD-secret", writer, nil, nil, 400)
	body, formHeaders := formUpload(t, "form.txt", "form data", map[string]string{"path": "/disk/public"})
	f.upload(t, "POST", "/api/fs/upload", writer, body, formHeaders, 201)
	f.app.cfg.MaxUploadBytes = 8
	// 未声明长度的请求也校验实际字节，不只相信 Content-Length。
	r := httptest.NewRequest("PUT", "/api/fs/upload?path=/disk/public", strings.NewReader("more than eight bytes"))
	r.ContentLength = -1
	r.Header.Set("Authorization", writer)
	for k, v := range uploadHeaders("large.txt", nil, false) {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "public/large.txt")); !os.IsNotExist(err) {
		t.Fatal("oversized file published", err)
	}
	f.request(t, "PUT", "/api/storage/"+id, f.admin, map[string]any{"readonly": true}, nil, 200)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/public", writer, strings.NewReader("ok"), uploadHeaders("readonly.txt", nil, false), 403)
	mounts := f.request(t, "GET", "/api/mount/list", f.admin, nil, nil, 200)
	if strings.Contains(mounts.Body.String(), `"writer"`) {
		t.Fatal("readonly mount advertised upload")
	}
	entries, err := os.ReadDir(f.app.uploadDir)
	if err != nil || len(entries) != 0 || f.app.rawReserved != 0 {
		t.Fatal("temporary files leaked", entries, err)
	}
}

func TestUploadShareFormOrderUnicodeAndUnmappedStorage(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	id, _ := f.local(t, root, "/disk")
	writer, keyID := f.key(t, "writer", "/disk", MountUpload|FileShare|FileManage)
	body, headers := formUpload(t, "分享.txt", "secret upload", map[string]string{"storage_config_id": id, "path": "/disk", "password": "分享密码", "remark": "中文备注", "max_views": "1"})
	record := dataMap(t, f.upload(t, "POST", "/api/share/upload", writer, body, headers, 201))
	slug := record["slug"].(string)
	if _, ok := record["storage_key"]; ok {
		t.Fatal("physical storage key leaked")
	}
	if record["remark"] != "中文备注" {
		t.Fatal("Unicode metadata lost")
	}
	f.request(t, "GET", "/api/s/"+slug, "", nil, nil, 403)
	verified := dataMap(t, f.request(t, "POST", "/api/share/verify/"+slug, "", map[string]any{"password": "分享密码"}, nil, 200))
	link := verified["previewUrl"].(string)
	if got := f.request(t, "GET", link, "", nil, nil, 200).Body.String(); got != "secret upload" {
		t.Fatal(got)
	}
	f.request(t, "POST", "/api/share/verify/"+slug, "", map[string]any{"password": "分享密码"}, nil, 404)
	f.request(t, "PUT", "/api/admin/api-keys/"+keyID, f.admin, map[string]any{"is_enable": false}, nil, 200)
	f.request(t, "GET", link, "", nil, nil, 403)
	// 上传页面可直接选择未挂载的存储，但受限路径账号不能绕过挂载边界。
	unmounted := dataMap(t, f.request(t, "POST", "/api/storage", f.admin, map[string]any{"name": "unmounted", "storage_type": "LOCAL", "root_path": t.TempDir(), "is_public": true}, nil, 201))["id"].(string)
	scoped, _ := f.key(t, "scoped", "/disk", MountUpload|FileShare)
	f.upload(t, "PUT", "/api/share/upload", scoped, strings.NewReader("blocked"), uploadHeaders("a.txt", map[string]any{"storage_config_id": unmounted}, true), 403)
	fields := map[string]any{"storage_config_id": unmounted, "remark": "中文备注", "password": "中文密码"}
	share := dataMap(t, f.upload(t, "PUT", "/api/share/upload", f.admin, strings.NewReader("unmapped"), uploadHeaders("原名.txt", fields, true), 201))
	v := dataMap(t, f.request(t, "POST", "/api/share/verify/"+share["slug"].(string), "", map[string]any{"password": "中文密码"}, nil, 200))
	if f.request(t, "GET", v["previewUrl"].(string), "", nil, nil, 200).Body.String() != "unmapped" {
		t.Fatal("unmapped share failed")
	}
}

func TestUploadDirectoryPasswordsAndSessionRevocation(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	f.local(t, root, "/disk")
	writer, keyID := f.key(t, "writer", "/disk", MountUpload|MountView|FileShare)
	meta := dataMap(t, f.request(t, "POST", "/api/fs-meta/create", f.admin, map[string]any{"path": "/disk/locked", "password": "directory-password", "passwordInherit": false}, nil, 201))
	headers := uploadHeaders("a.txt", nil, false)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/locked", writer, strings.NewReader("ok"), headers, 403)
	verified := dataMap(t, f.request(t, "POST", "/api/fs/meta/password/verify", writer, map[string]any{"path": "/disk/locked", "password": "directory-password"}, nil, 200))
	headers["X-FS-Path-Token"] = verified["token"].(string)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/locked", writer, strings.NewReader("ok"), headers, 201)
	body := map[string]any{"path": "/disk/locked", "fileName": "parts.txt", "fileSize": 3, "partSize": 5 << 20}
	f.request(t, "POST", "/api/fs/multipart/init", writer, body, nil, 403)
	s := dataMap(t, f.request(t, "POST", "/api/fs/multipart/init", writer, body, headers, 200))
	id := s["uploadId"].(string)
	partURL := signedPart(t, f, writer, id, 1)
	f.request(t, "PUT", "/api/fs-meta/"+meta["id"].(string), f.admin, map[string]any{"password": "replacement-password"}, nil, 200)
	f.upload(t, "PUT", partURL, "", strings.NewReader("abc"), nil, 410)
	f.request(t, "POST", "/api/fs/multipart/abort", writer, map[string]any{"uploadId": id}, nil, 200)
	body["path"] = "/disk"
	s = dataMap(t, f.request(t, "POST", "/api/fs/multipart/init", writer, body, nil, 200))
	id = s["uploadId"].(string)
	partURL = signedPart(t, f, writer, id, 1)
	f.request(t, "PUT", "/api/admin/api-keys/"+keyID, f.admin, map[string]any{"is_enable": false}, nil, 200)
	f.upload(t, "PUT", partURL, "", strings.NewReader("abc"), nil, 403)
}
func signedPart(t *testing.T, f *fixture, auth, id string, n int) string {
	t.Helper()
	v := dataMap(t, f.request(t, "POST", "/api/fs/multipart/sign-parts", auth, map[string]any{"uploadId": id, "partNumbers": []int{n}}, nil, 200))
	return v["presignedUrls"].([]any)[0].(map[string]any)["url"].(string)
}
func TestMultipartIntegrityRetryRestartAndQuota(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	f.local(t, root, "/disk")
	writer, _ := f.key(t, "writer", "/disk", MountUpload|MountView)
	other, _ := f.key(t, "other", "/disk", MountUpload)
	const partSize = 5 << 20
	size := int64(partSize + 3)
	init := map[string]any{"path": "/disk", "fileName": "large.bin", "fileSize": size, "partSize": partSize, "partCount": 2}
	s := dataMap(t, f.request(t, "POST", "/api/fs/multipart/init", writer, init, nil, 200))
	id := s["uploadId"].(string)
	f.request(t, "POST", "/api/fs/multipart/sign-parts", other, map[string]any{"uploadId": id, "partNumbers": []int{1}}, nil, 403)
	url1, url2 := signedPart(t, f, writer, id, 1), signedPart(t, f, writer, id, 2)
	f.upload(t, "PUT", strings.Replace(url1, "/1?", "/2?", 1), "", strings.NewReader("end"), nil, 403)
	f.upload(t, "PUT", url2, "", strings.NewReader("bad-size"), nil, 400)
	f.request(t, "POST", "/api/fs/multipart/complete", writer, map[string]any{"uploadId": id, "parts": []uploadPart{}}, nil, 400)
	data := bytes.Repeat([]byte("x"), partSize)
	etag1 := f.upload(t, "PUT", url1, "", bytes.NewReader(data), nil, 200).Header().Get("ETag")
	etag2 := f.upload(t, "PUT", url2, "", strings.NewReader("old"), nil, 200).Header().Get("ETag")
	// 分片重试更新校验记录，旧清单不能完成新内容。
	newETag := f.upload(t, "PUT", url2, "", strings.NewReader("end"), nil, 200).Header().Get("ETag")
	f.request(t, "POST", "/api/fs/multipart/complete", writer, map[string]any{"uploadId": id, "parts": []uploadPart{{PartNumber: 1, ETag: etag1}, {PartNumber: 2, ETag: etag2}}}, nil, 400)
	orphan := filepath.Join(f.app.uploadDir, randomID())
	if err := os.WriteFile(orphan, []byte("interrupted request"), 0600); err != nil {
		t.Fatal(err)
	}
	// 关闭并重新打开数据库，恢复不能依赖旧进程里的任何内存状态。
	cfg, logger := f.app.cfg, f.app.logger
	if err := f.app.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	f.app, f.handler = restored, restored.Handler()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan staging file survived restart")
	}
	listed := dataMap(t, f.request(t, "POST", "/api/fs/multipart/list-parts", writer, map[string]any{"uploadId": id}, nil, 200))
	if len(listed["parts"].([]any)) != 2 {
		t.Fatal("parts not recovered")
	}
	parts := []uploadPart{{PartNumber: 1, ETag: etag1}, {PartNumber: 2, ETag: newETag}}
	f.request(t, "POST", "/api/fs/multipart/complete", writer, map[string]any{"uploadId": id, "parts": parts}, nil, 200)
	contents, err := os.ReadFile(filepath.Join(root, "large.bin"))
	if err != nil || int64(len(contents)) != size || string(contents[len(contents)-3:]) != "end" {
		t.Fatal("merged file incorrect", err)
	}
	if _, err = os.Stat(filepath.Join(f.app.uploadDir, id)); !os.IsNotExist(err) {
		t.Fatal("completed staging session leaked")
	}
	f.app.cfg.MaxStagedBytes = size
	init["fileName"] = "pending.bin"
	s = dataMap(t, f.request(t, "POST", "/api/fs/multipart/init", writer, init, nil, 200))
	id = s["uploadId"].(string)
	f.request(t, "POST", "/api/fs/multipart/init", writer, init, nil, 507)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk", writer, strings.NewReader("a"), uploadHeaders("quota.txt", nil, false), 507)
	f.request(t, "POST", "/api/fs/multipart/abort", writer, map[string]any{"uploadId": id}, nil, 200)
	s = dataMap(t, f.request(t, "POST", "/api/fs/multipart/init", writer, init, nil, 200))
	id = s["uploadId"].(string)
	var session uploadSession
	if err = f.app.resource(context.Background(), "uploads", "id", id, &session); err != nil {
		t.Fatal(err)
	}
	session.Expires = time.Now().Add(-time.Second).Unix()
	if err = f.app.saveResource(context.Background(), "uploads", id, "", session.Owner, session); err != nil {
		t.Fatal(err)
	}
	f.request(t, "POST", "/api/fs/multipart/list-uploads", writer, map[string]any{"path": "/disk"}, nil, 200)
	if _, err = os.Stat(filepath.Join(f.app.uploadDir, id)); !os.IsNotExist(err) {
		t.Fatal("expired upload file leaked")
	}
}

func TestUploadCannotEscapeThroughDirectoryLinks(t *testing.T) {
	f := newFixture(t)
	root, outside := t.TempDir(), t.TempDir()
	f.local(t, root, "/disk")
	link := filepath.Join(root, "escape")
	if runtime.GOOS == "windows" {
		output, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if err != nil {
			t.Skipf("junction unavailable: %v %s", err, output)
		}
	} else {
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
	}
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/escape", f.admin, strings.NewReader("bad"), uploadHeaders("outside.txt", nil, false), 403)
	if _, err := os.Stat(filepath.Join(outside, "outside.txt")); !os.IsNotExist(err) {
		t.Fatal("upload escaped storage root")
	}
	// API Key 的授权子目录也构成独立文件系统边界。
	if err := os.MkdirAll(filepath.Join(root, "public"), 0700); err != nil {
		t.Fatal(err)
	}
	insideLink := filepath.Join(root, "public/to-private")
	if runtime.GOOS == "windows" {
		output, err := exec.Command("cmd", "/c", "mklink", "/J", insideLink, outside).CombinedOutput()
		if err != nil {
			t.Skipf("junction unavailable: %v %s", err, output)
		}
	} else {
		if err := os.Symlink(outside, insideLink); err != nil {
			t.Fatal(err)
		}
	}
	writer, _ := f.key(t, "writer", "/disk/public", MountUpload)
	f.upload(t, "PUT", "/api/fs/upload?path=/disk/public/to-private", writer, strings.NewReader("bad"), uploadHeaders("private.txt", nil, false), 403)
}

func TestConcurrentUploadNeverOverwrites(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	f.local(t, root, "/disk")
	var group sync.WaitGroup
	statuses := make(chan int, 2)
	for _, content := range []string{"first", "second"} {
		group.Go(func() {
			r := httptest.NewRequest("PUT", "/api/admin/fs/upload?path=/disk", strings.NewReader(content))
			r.Header.Set("Authorization", f.admin)
			for k, v := range uploadHeaders("same.txt", nil, false) {
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
	f.local(t, root, "/disk")
	writer, id := f.key(t, "writer", "/disk", MountUpload)
	// 请求初始认证成功后撤销权限，收到完整文件也不能继续发布。
	body := &callbackReader{Reader: strings.NewReader("contents"), callback: func() {
		f.request(t, "PUT", "/api/admin/api-keys/"+id, f.admin, map[string]any{"permissions": MountView}, nil, 200)
	}}
	f.upload(t, "PUT", "/api/fs/upload?path=/disk", writer, body, uploadHeaders("revoked.txt", nil, false), 403)
	if _, err := os.Stat(filepath.Join(root, "revoked.txt")); !os.IsNotExist(err) {
		t.Fatal("revoked upload published")
	}
	f.request(t, "PUT", "/api/admin/api-keys/"+id, f.admin, map[string]any{"permissions": MountUpload}, nil, 200)
	session := dataMap(t, f.request(t, "POST", "/api/fs/multipart/init", writer, map[string]any{"path": "/disk", "fileName": "parts.txt", "fileSize": 3}, nil, 200))
	uploadID := session["uploadId"].(string)
	partURL := signedPart(t, f, writer, uploadID, 1)
	body = &callbackReader{Reader: strings.NewReader("abc"), callback: func() {
		f.request(t, "PUT", "/api/admin/api-keys/"+id, f.admin, map[string]any{"is_enable": false}, nil, 200)
	}}
	f.upload(t, "PUT", partURL, "", body, nil, 403)
	var stored uploadSession
	if err := f.app.resource(context.Background(), "uploads", "id", uploadID, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Parts) != 0 {
		t.Fatal("revoked chunk registered")
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
