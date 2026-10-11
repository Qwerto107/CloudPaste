//go:build !with_frontend

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 不带构建标签时保留独立后端语义，根路径也由原处理器负责。
func TestDefaultBuildKeepsAPIOnly(t *testing.T) {
	handler := Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"source":"backend"}`))
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusNotFound || w.Body.String() != `{"source":"backend"}` || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("纯 API 构建行为变化: status=%d body=%s", w.Code, w.Body.String())
	}
}
