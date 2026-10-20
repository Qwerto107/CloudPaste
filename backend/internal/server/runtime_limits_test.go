package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func setRuntimeLimitsForTest(a *App, update func(*runtimeLimits)) {
	limits := *a.runtimeLimits()
	update(&limits)
	a.limits.Store(&limits)
}

func TestRuntimeLimitsSettingsPersistence(t *testing.T) {
	f := newFixture(t)
	key, _ := f.key(t, "user", "/", 0)
	f.request(t, "PUT", "/api/admin/settings/group/6", "", map[string]any{"password_attempts_per_minute": 2}, nil, 401)
	f.request(t, "PUT", "/api/admin/settings/group/6", key, map[string]any{"password_attempts_per_minute": 2}, nil, 403)
	f.request(t, "GET", "/api/public/settings?group=6", "", nil, nil, 403)
	public := f.request(t, "GET", "/api/public/settings", "", nil, nil, 200).Body.String()
	for _, name := range []string{"password_attempts_per_minute", "max_uploads", "max_downloads", "max_staged_size_mb"} {
		if strings.Contains(public, `"`+name+`"`) {
			t.Fatal("private operating settings leaked", name)
		}
	}
	for _, body := range []map[string]any{
		{"max_uploads": 0}, {"max_downloads": 65536}, {"max_staged_size_mb": 102401},
		{"max_uploads": 1.5}, {"max_upload_size": 10241}, {"max_upload_size": 2, "max_staged_size_mb": 1},
	} {
		f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, body, nil, 400)
	}
	rates := map[string]any{}
	for name := range rateLimitTargets(&RateLimitConfig{}) {
		for _, invalid := range []any{0, -1, 1000001, "NaN"} {
			f.request(t, "PUT", "/api/admin/settings/group/6", f.admin, map[string]any{name: invalid}, nil, 400)
		}
		rates[name] = 2
	}
	f.request(t, "PUT", "/api/admin/settings/group/6", f.admin, rates, nil, 200)
	for i := 0; i < 2; i++ {
		requireRateStatus(t, rateLimitRequest(t, f, "GET", "/api/test/api-key", "198.51.100.9", "ApiKey wrong", nil), 401)
	}
	requireRateStatus(t, rateLimitRequest(t, f, "GET", "/api/test/api-key", "198.51.100.9", "ApiKey wrong", nil), 429)
	// 放宽限频沿用原窗口和计数，不允许保存设置清空已有失败次数。
	f.request(t, "PUT", "/api/admin/settings/group/6", f.admin, map[string]any{"password_attempts_per_minute": 3}, nil, 200)
	requireRateStatus(t, rateLimitRequest(t, f, "GET", "/api/test/api-key", "198.51.100.9", "ApiKey wrong", nil), 401)
	requireRateStatus(t, rateLimitRequest(t, f, "GET", "/api/test/api-key", "198.51.100.9", "ApiKey wrong", nil), 429)
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_uploads": 3, "max_downloads": 5, "max_upload_size": 2, "max_staged_size_mb": 3}, nil, 200)
	ctx := context.Background()
	if err := f.app.reserveUpload(ctx, 2<<20); err != nil {
		t.Fatal(err)
	}
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_staged_size_mb": 2}, nil, 200)
	if err := f.app.reserveUpload(ctx, 1); err == nil {
		t.Fatal("lowered staging quota ignored existing reservations")
	}
	f.app.releaseUpload(2 << 20)
	if err := f.app.reserveUpload(ctx, 1); err != nil {
		t.Fatal("released reservation still blocks upload", err)
	}
	f.app.releaseUpload(1)
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_upload_size": 3, "max_uploads": 4}, nil, 400)
	want := *f.app.runtimeLimits()
	if want.MaxUploads != 3 || want.MaxUploadBytes != 2<<20 {
		t.Fatal("inconsistent upload limits partially committed", want)
	}
	if err := f.app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(f.app.cfg, f.app.logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if got := *restarted.runtimeLimits(); !reflect.DeepEqual(got, want) {
		t.Fatalf("operating settings lost after restart: %+v, want %+v", got, want)
	}
}

func TestDynamicDownloadConcurrency(t *testing.T) {
	f := newFixture(t)
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_downloads": 2}, nil, 200)
	begin := func(ip string) (func(), error) {
		r := httptest.NewRequest("GET", "/file", nil)
		r.RemoteAddr = ip + ":1234"
		return f.app.beginDownload(r)
	}
	one, err := begin("192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	one = sync.OnceFunc(one)
	defer one()
	two, err := begin("192.0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	two = sync.OnceFunc(two)
	defer two()
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_downloads": 1}, nil, 200)
	if _, err := begin("192.0.2.3"); err == nil {
		t.Fatal("lowered download limit ignored active requests")
	}
	one()
	if _, err := begin("192.0.2.3"); err == nil {
		t.Fatal("new download started before active count dropped below limit")
	}
	two()
	three, err := begin("192.0.2.3")
	if err != nil {
		t.Fatal("finished downloads leaked capacity", err)
	}
	defer three()
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_downloads": 2}, nil, 200)
	four, err := begin("192.0.2.4")
	if err != nil {
		t.Fatal("raised download limit did not take effect", err)
	}
	defer four()
}

func TestDynamicUploadConcurrency(t *testing.T) {
	f := newFixture(t)
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_uploads": 2}, nil, 200)
	entered, resume, done := make(chan struct{}, 3), make(chan struct{}), make(chan error, 3)
	finish := sync.OnceFunc(func() { close(resume) })
	defer finish()
	endpoint := f.app.uploadEndpoint(func(http.ResponseWriter, *http.Request) error {
		entered <- struct{}{}
		<-resume
		return nil
	})
	start := func() {
		go func() {
			done <- endpoint(httptest.NewRecorder(), httptest.NewRequest("PUT", "/", strings.NewReader("x")))
		}()
		<-entered
	}
	start()
	start()
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_uploads": 1}, nil, 200)
	if err := endpoint(httptest.NewRecorder(), httptest.NewRequest("PUT", "/", nil)); err == nil {
		t.Fatal("lowered upload limit ignored active requests")
	}
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"max_uploads": 3}, nil, 200)
	start()
	finish()
	for range 3 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if f.app.uploadCount != 0 {
		t.Fatal("uploads leaked capacity after limit change")
	}
}

func TestTransferDisabledKeepsExistingRelay(t *testing.T) {
	f, server, sender, receiver := transferPair(t)
	f.request(t, "PUT", "/api/admin/settings/group/3", f.admin, map[string]any{"transfer_enabled": false}, nil, 200)
	f.request(t, "POST", "/api/transfer/create", f.admin, map[string]any{"name": "new"}, nil, 404)
	f.request(t, "POST", "/api/transfer/join", "", map[string]any{"code": "A1234"}, nil, 404)
	id, download := offerRelay(t, sender, receiver, 3)
	response, err := http.Get(server.URL + download)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	start := sender.next("start")
	done := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(response.Body); done <- data }()
	upload := transferPut(t, server.URL, id, start["token"].(string), strings.NewReader("abc"), 3)
	upload.Body.Close()
	if upload.StatusCode != 200 || string(<-done) != "abc" {
		t.Fatal("disabled transfer interrupted existing relay")
	}
	if err := f.app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(f.app.cfg, f.app.logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if restarted.transfers.enabled.Load() {
		t.Fatal("transfer switch lost after restart")
	}
}

func TestOperatingEnvironmentRemoved(t *testing.T) {
	for _, name := range append(append([]string{}, rateLimitEnvNames...), "MAX_UPLOADS", "MAX_DOWNLOADS", "MAX_UPLOAD_SIZE_MB", "MAX_STAGED_SIZE_MB", "TRANSFER_ENABLED", "TRANSFER_RATE_MB", "TRANSFER_MAX_FILE_MB") {
		t.Setenv(name, "invalid-removed-value")
	}
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.MaxDownloads != 48 || cfg.MaxUploads != 16 || cfg.MaxUploadBytes != 10<<30 || cfg.MaxStagedBytes != 20<<30 || cfg.TransferDisabled || cfg.TransferBytesPerSecond != 64<<20 || cfg.TransferMaxFileBytes != 10<<30 {
		t.Fatal("removed operating environment variables still affect startup", cfg, err)
	}
}
