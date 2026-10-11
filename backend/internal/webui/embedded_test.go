//go:build with_frontend

package webui

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// 用真实构建产物验证内嵌根目录和首页引用，避免只验证内存中的测试资源。
func TestEmbeddedFrontendEntryAndAssets(t *testing.T) {
	handler := Handler(http.NotFoundHandler())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/login", nil))
	if w.Code != http.StatusOK || !strings.Contains(strings.ToLower(w.Body.String()), "<!doctype html>") {
		t.Fatalf("内嵌首页未提供: status=%d body=%s", w.Code, w.Body.String())
	}
	// Vite 输出的脚本和样式应是同源文件，当前网页 CSP 不允许内联脚本。
	for _, script := range regexp.MustCompile(`(?i)<script\b[^>]*>`).FindAllString(w.Body.String(), -1) {
		if !strings.Contains(script, "src=") {
			t.Fatal("首页包含被网页 CSP 阻止的内联脚本")
		}
	}
	references := regexp.MustCompile(`(?:src|href)="(/[^"?]+)(?:\?[^\"]*)?"`).FindAllStringSubmatch(w.Body.String(), -1)
	if len(references) == 0 {
		t.Fatal("内嵌首页没有静态资源引用")
	}
	for _, match := range references {
		asset := httptest.NewRecorder()
		handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, match[1], nil))
		if asset.Code != http.StatusOK {
			t.Errorf("首页资源缺失: %s status=%d", match[1], asset.Code)
		}
		if strings.HasSuffix(match[1], ".js") && !strings.HasPrefix(asset.Header().Get("Content-Type"), "text/javascript") {
			t.Errorf("首页脚本 MIME 不正确: %s %s", match[1], asset.Header().Get("Content-Type"))
		}
	}
}
