// Package webui 为单文件发行版提供只读的内嵌前端，不读取数据目录或外部静态目录。
package webui

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// 与 docker/frontend/nginx.conf 的网页策略保持一致；API 继续使用原有内容沙箱。
// blob 仅用于本地图片预览；脚本、网络连接和嵌入对象的策略保持独立限制。
const pageCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob: https: http:; media-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'"

func newHandler(api http.Handler, files fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 即使 API 不存在或方法不匹配，也必须由后端认证和错误处理负责。
		if under(r.URL.Path, "/api") || under(r.URL.Path, "/dav") {
			api.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", pageCSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		name, ok := staticName(r.URL.Path)
		if !ok || under(r.URL.Path, "/data") {
			http.NotFound(w, r)
			return
		}
		if name == "" {
			name = "index.html"
		}
		file, err := files.Open(name)
		// 仅页面路径回退到首页；缺失的脚本、图片和 assets 不能返回 HTML。
		if errors.Is(err, fs.ErrNotExist) && path.Ext(name) == "" && !under("/"+name, "/assets") {
			name = "index.html"
			file, err = files.Open(name)
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			// 不调用目录文件服务器，避免目录列举及目录索引自动暴露。
			http.NotFound(w, r)
			return
		}
		if name == "index.html" {
			// 每次页面响应使用独立 nonce，仅允许可信组件生成的样式，不放宽脚本策略。
			var random [18]byte
			if _, err := rand.Read(random[:]); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			nonce := base64.RawStdEncoding.EncodeToString(random[:])
			html, err := io.ReadAll(io.LimitReader(file, 1<<20))
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			html = bytes.ReplaceAll(html, []byte("__CLOUDPASTE_STYLE_NONCE__"), []byte(nonce))
			w.Header().Set("Content-Security-Policy", strings.Replace(pageCSP, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// 替换 nonce 后重新计算长度，GET 与 HEAD 都提供正确的资源元信息。
			w.Header().Set("Content-Length", strconv.Itoa(len(html)))
			// HTML 不返回 304 或局部内容，避免旧页面 nonce 与新响应策略不一致。
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(html)
			}
			return
		}
		content, ok := file.(io.ReadSeeker)
		if !ok {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if under("/"+name, "/assets") {
			// Vite 构建的 assets 文件名带内容哈希，升级时使用新 URL。
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else if name != "index.html" && name != "config.js" && name != "sw.js" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		// Windows 的系统 MIME 映射可能不同，明确网页运行所需的类型。
		switch path.Ext(name) {
		case ".html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case ".js", ".mjs":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		// 标准库处理 HEAD、Range 和资源长度；内容仅来自编译进二进制的文件系统。
		http.ServeContent(w, r, name, info.ModTime(), content)
	})
}

func under(value, prefix string) bool {
	return value == prefix || strings.HasPrefix(value, prefix+"/")
}

// URL.Path 已由 HTTP 层解码；直接拒绝歧义路径，不清理后再寻找资源。
func staticName(value string) (string, bool) {
	if !strings.HasPrefix(value, "/") || len(value) > 4096 || strings.ContainsAny(value, "\\\x00\r\n") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(value, "/"), "/")
	if name == "" {
		return "", value == "/"
	}
	if !fs.ValidPath(name) {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	return name, true
}
