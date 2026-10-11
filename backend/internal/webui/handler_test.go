package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const testPage = "<!doctype html><html><body>CloudPaste</body></html>"

func testFiles() fstest.MapFS {
	return fstest.MapFS{
		"index.html":               {Data: []byte(testPage)},
		"config.js":                {Data: []byte(`window.appConfig = { backendUrl: "" };`)},
		"sw.js":                    {Data: []byte("self.registration.unregister();")},
		"assets/main-a1b2c3d4.js":  {Data: []byte("console.log('ready');")},
		"assets/main-a1b2c3d4.css": {Data: []byte("body { color: black; }")},
		"chain-cloud.svg":          {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		".secret":                  {Data: []byte("hidden-content")},
		"data/private.txt":         {Data: []byte("upload-content")},
	}
}

// 前端包装不得接管未知 API 或 DAV，也不能更改其凭据、请求体和安全头。
func TestAPIAndDAVRemainWithBackend(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "ApiKey test" {
			t.Error("API 凭据未保留")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "request-body" {
			t.Errorf("API 请求体未保留: body=%q err=%v", body, err)
		}
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"source":"api"}`))
	})
	handler := newHandler(api, testFiles())
	for _, target := range []string{"/api", "/api/health", "/api/admin/keys", "/api/unknown.js", "/api/%2e%2e/index.html", "/dav", "/dav/document"} {
		for _, method := range []string{"GET", "POST", "PROPFIND"} {
			r := httptest.NewRequest(method, target, strings.NewReader("request-body"))
			r.Header.Set("Authorization", "ApiKey test")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized || w.Body.String() != `{"source":"api"}` || w.Header().Get("Content-Security-Policy") != "sandbox; default-src 'none'" {
				t.Fatalf("%s %s 被静态路由接管: status=%d headers=%v body=%s", method, target, w.Code, w.Header(), w.Body.String())
			}
		}
	}
}

// 深链接可直接打开，网页不继承会禁用脚本的 API 沙箱策略。
func TestPagesAndStaticResourceResponses(t *testing.T) {
	handler := newHandler(http.NotFoundHandler(), testFiles())
	tests := []struct {
		path, contentType, cache string
	}{
		{"/", "text/html", "no-store"},
		{"/admin/settings/site", "text/html", "no-store"},
		{"/manage/account/", "text/html", "no-store"},
		{"/paste/example-slug", "text/html", "no-store"},
		{"/file/example-slug", "text/html", "no-store"},
		{"/config.js", "text/javascript", "no-store"},
		{"/sw.js", "text/javascript", "no-store"},
		{"/assets/main-a1b2c3d4.js", "text/javascript", "public, max-age=31536000, immutable"},
		{"/assets/main-a1b2c3d4.css", "text/css", "public, max-age=31536000, immutable"},
		{"/chain-cloud.svg", "image/svg+xml", "no-cache"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(method, test.path, nil))
				if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), test.contentType) || w.Header().Get("Cache-Control") != test.cache {
					t.Fatalf("资源响应错误: status=%d headers=%v", w.Code, w.Header())
				}
				csp := w.Header().Get("Content-Security-Policy")
				if strings.Contains(csp, "sandbox") || !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "no-referrer" {
					t.Fatalf("网页安全头错误: %v", w.Header())
				}
				if w.Header().Get("Content-Length") == "" || (method == http.MethodHead && w.Body.Len() != 0) {
					t.Fatalf("HEAD/资源长度错误: headers=%v body=%s", w.Header(), w.Body.String())
				}
				if method == http.MethodGet && test.contentType == "text/html" && w.Body.String() != testPage {
					t.Fatalf("深链接未返回页面: %s", w.Body.String())
				}
			}
		})
	}
	request := httptest.NewRequest(http.MethodGet, "/assets/main-a1b2c3d4.js", nil)
	request.Header.Set("Range", "bytes=0-6")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != http.StatusPartialContent || w.Body.String() != "console" {
		t.Fatalf("静态资源范围读取错误: status=%d body=%s", w.Code, w.Body.String())
	}
}

// 缺失资源、目录列举、隐藏文件和数据目录均不能回退为成功的网页响应。
func TestStaticRoutesRejectUnsafeOrMissingResources(t *testing.T) {
	handler := newHandler(http.NotFoundHandler(), testFiles())
	for _, target := range []string{
		"/assets", "/assets/", "/assets/missing", "/missing.js", "/missing.png",
		"/.secret", "/.git/config", "/data", "/data/private.txt", "/data/missing",
		"/../index.html", "/assets/%2e%2e/index.html", "/%2e%2e/index.html",
		"/assets/%5cindex.html", "//index.html", "/assets//main-a1b2c3d4.js",
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "upload-content") || w.Body.String() == testPage {
			t.Fatalf("不安全路径可访问: %s status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/index.html", strings.NewReader("replace-page")))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("静态路径接受了写入: status=%d headers=%v", w.Code, w.Header())
	}
}
