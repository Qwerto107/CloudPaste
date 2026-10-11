//go:build !with_frontend

package webui

import "net/http"

// Handler 保持普通后端构建的行为，运行测试和开发 API 不需要前端产物。
func Handler(api http.Handler) http.Handler { return api }
