package server

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type limitChangingReader struct {
	io.Reader
	change func()
}

func (r *limitChangingReader) Read(buffer []byte) (int, error) {
	if r.change != nil {
		r.change()
		r.change = nil
	}
	return r.Reader.Read(buffer)
}

// 开始接收后提高上限不能让文件突破原有暂存预留，避免并发上传耗尽磁盘。
func TestChangingUploadLimitKeepsReservationBound(t *testing.T) {
	f := newFixture(t)
	storage, _ := f.local(t, t.TempDir(), "/disk")
	f.app.uploadLimit.Store(1 << 20)
	body := &limitChangingReader{Reader: strings.NewReader(strings.Repeat("a", (1<<20)+1)), change: func() { f.app.uploadLimit.Store(2 << 20) }}
	f.upload(t, "PUT", "/api/share/upload", f.admin, body, uploadHeaders("bounded.txt", map[string]any{"storage_config_id": storage}, true), 413)
	if f.app.rawReserved != 0 {
		t.Fatal("失败上传未释放暂存额度")
	}
}

// 设置必须控制实际上传，并在数据库重开后继续生效。
func TestSharingSettingsLimitsAndPersistence(t *testing.T) {
	f := newFixture(t)
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_upload_size": "1", "share_default_expiry_hours": 1}, nil, 200)
	if f.app.maxUploadBytes() != 1<<20 {
		t.Fatal("上传限制未生效")
	}
	for _, body := range []map[string]any{{"max_upload_size": "101"}, {"max_upload_size": "-1"}, {"share_default_expiry_hours": "NaN"}} {
		f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, body, nil, 400)
	}
	f.request(t, "PUT", "/api/admin/settings/group/2", f.admin, map[string]any{"preview_max_text_kb": "2049"}, nil, 400)
	f.request(t, "PUT", "/api/admin/settings/group/2", f.admin, map[string]any{"preview_image_types": "png,../secret"}, nil, 400)
	f.request(t, "PUT", "/api/admin/settings/group/2", f.admin, map[string]any{"preview_max_text_kb": "64", "preview_image_types": "png,jpg"}, nil, 200)
	paste := dataMap(t, f.request(t, "POST", "/api/paste", f.admin, map[string]any{"content": "temporary"}, nil, 201))
	expiry, err := time.Parse(time.RFC3339, paste["expires_at"].(string))
	if err != nil || time.Until(expiry) < 59*time.Minute || time.Until(expiry) > time.Hour {
		t.Fatal("默认有效期未生效")
	}
	permanent := dataMap(t, f.request(t, "POST", "/api/paste", f.admin, map[string]any{"content": "permanent", "expires_at": nil}, nil, 201))
	if permanent["expires_at"] != nil {
		t.Fatal("显式不过期设置被覆盖")
	}
	storage, _ := f.local(t, t.TempDir(), "/disk")
	request := httptest.NewRequest("PUT", "/api/admin/share/upload", strings.NewReader(strings.Repeat("a", (1<<20)+1)))
	request.Header.Set("Authorization", f.admin)
	request.Header.Set("X-Share-Filename", "large.txt")
	request.Header.Set("X-Share-Options", uploadHeaders("large.txt", map[string]any{"storage_config_id": storage}, true)["X-Share-Options"])
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	if response.Code != 413 {
		t.Fatalf("超限上传得到 %d: %s", response.Code, response.Body.String())
	}
	second, err := New(f.app.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.maxUploadBytes() != 1<<20 {
		t.Fatal("重启后丢失上传限制")
	}
}

func TestRemovedFeaturesAndStorage(t *testing.T) {
	f := newFixture(t)
	namespaceRequest(t, f, "GET", "/api/admin/dashboard/stats", f.admin, nil, 404)
	for _, route := range []string{"/dav", "/dav/x", "/api/admin/dav/x"} {
		for _, method := range []string{"GET", "PROPFIND", "PUT"} {
			namespaceRequest(t, f, method, route, "", nil, 404)
		}
	}
	f.request(t, "POST", "/api/admin/storage", f.admin, map[string]any{"name": "removed", "storage_type": "WEBDAV", "endpoint_url": "https://example.com"}, nil, 400)
	f.request(t, "GET", "/api/storage-types", f.admin, nil, nil, 404)
}
