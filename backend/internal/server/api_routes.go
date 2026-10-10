package server

import (
	"net/http"
	"strings"
)

const adminAPIPrefix = "/api/admin"

type adminScopeKey struct{}

// apiMux 为通用业务接口提供独立管理员路径，权限检查仍由原处理函数完成。
// 管理员专属接口直接注册到 /api/admin，普通路径不保留旧的管理入口。
type apiMux struct{ *http.ServeMux }

func (mux *apiMux) HandleFunc(pattern string, handler http.HandlerFunc) {
	mux.ServeMux.HandleFunc(pattern, handler)
	method, route, ok := strings.Cut(pattern, " ")
	if !ok || !strings.HasPrefix(route, "/api/") || isAdminPath(route) {
		return
	}
	adminPattern := method + " " + adminAPIPrefix + strings.TrimPrefix(route, "/api")
	mux.ServeMux.HandleFunc(adminPattern, func(w http.ResponseWriter, r *http.Request) {
		// 克隆请求，只恢复处理函数使用的业务路径，保留已验证的入口身份及路径参数。
		request := r.Clone(r.Context())
		request.URL.Path = "/api" + strings.TrimPrefix(r.URL.Path, adminAPIPrefix)
		request.URL.RawPath = ""
		handler(w, request)
	})
}

func isAdminPath(value string) bool {
	return value == adminAPIPrefix || strings.HasPrefix(value, adminAPIPrefix+"/")
}

func adminRequest(r *http.Request) bool {
	value, _ := r.Context().Value(adminScopeKey{}).(bool)
	return value
}

func scopedAPIPath(r *http.Request, value string) string {
	if adminRequest(r) {
		return adminAPIPrefix + strings.TrimPrefix(value, "/api")
	}
	return value
}

func isDAVPath(value string) bool {
	return value == "/dav" || strings.HasPrefix(value, "/dav/") || value == adminAPIPrefix+"/dav" || strings.HasPrefix(value, adminAPIPrefix+"/dav/")
}

// 无登录头的管理员下载及分片请求必须在专用路径中验证绑定原凭证的票据。
func adminTicketRequest(r *http.Request) bool {
	return ((r.Method == "GET" || r.Method == "HEAD") && strings.HasPrefix(r.URL.Path, adminAPIPrefix+"/p/")) ||
		(r.Method == "PUT" && strings.HasPrefix(r.URL.Path, adminAPIPrefix+"/fs/multipart/part/"))
}
