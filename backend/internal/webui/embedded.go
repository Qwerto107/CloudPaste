//go:build with_frontend

package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

// 前端产物由构建脚本复制至本包；缺少产物或首页时直接使构建失败。
//
//go:embed dist/index.html dist/*
var frontend embed.FS

// Handler 将内嵌网页与 API 放在同一监听端口，静态根始终固定为构建产物。
func Handler(api http.Handler) http.Handler {
	files, err := fs.Sub(frontend, "dist")
	if err != nil {
		panic("内嵌前端目录无效: " + err.Error())
	}
	return newHandler(api, files)
}
