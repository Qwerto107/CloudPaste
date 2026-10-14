package server

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"
)

func (a *App) registerFiles(mux *apiMux) {
	mux.HandleFunc("POST /api/share/collection", a.protected(FileShare, false, a.createCollection))
	// 公开分享只提供普通路径，前端不会附带管理身份。
	mux.ServeMux.HandleFunc("GET /api/share/get/{slug}", a.handleShare(a.publicShare))
	mux.ServeMux.HandleFunc("POST /api/share/verify/{slug}", a.handleShare(a.publicShare))
	mux.ServeMux.HandleFunc("GET /api/s/{slug}", a.handleShare(a.shareContent))
	mux.ServeMux.HandleFunc("GET /api/s/{slug}/{filename}", a.handleShare(a.shareContent))
	mux.HandleFunc("GET /api/admin/share/get/{slug}", a.protected(0, true, a.publicShare))
	mux.HandleFunc("GET /api/admin/s/{slug}", a.protected(0, true, a.shareContent))
	mux.HandleFunc("GET /api/admin/s/{slug}/{filename}", a.protected(0, true, a.shareContent))
	// 创建者入口与公开入口隔离，不能凭其他人的票据查看失效内容。
	mux.ServeMux.HandleFunc("GET /api/owner/share/get/{slug}", a.protected(0, false, a.publicShare))
	mux.ServeMux.HandleFunc("GET /api/owner/s/{slug}", a.protected(0, false, a.shareContent))
	mux.ServeMux.HandleFunc("GET /api/owner/s/{slug}/{filename}", a.protected(0, false, a.shareContent))
	mux.HandleFunc("GET /api/files", a.protected(FileManage, false, a.listShares))
	mux.HandleFunc("PUT /api/files/{id}", a.protected(FileManage, false, a.updateShare))
	mux.HandleFunc("DELETE /api/files/batch-delete", a.protected(FileManage, false, a.deleteShares))
}

func previewSelection(info FileInfo) map[string]any {
	key := "download"
	switch info.Type {
	case 2:
		key = "video"
	case 3:
		key = "audio"
	case 4:
		key = "text"
	case 5:
		key = "image"
	case 6:
		key = "office"
	case 7:
		key = "pdf"
	}
	// HTML 使用文本预览，避免原前端不安全的同源脚本预览模式。
	return map[string]any{"key": key, "kind": "component", "providers": map[string]string{"native": "native"}}
}

func (a *App) deliver(w http.ResponseWriter, r *http.Request, d Driver, key string, info FileInfo, download bool) error {
	if info.Directory {
		return badRequest("目录不支持下载")
	}
	release, err := a.beginDownload(r)
	if err != nil {
		return err
	}
	defer release()
	w.Header().Set("Content-Type", info.MimeType)
	w.Header().Set("Cache-Control", "private, no-store")
	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": info.Name}))
	if r.Method == "HEAD" {
		if info.Size != nil {
			w.Header().Set("Content-Length", fmt.Sprint(*info.Size))
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(200)
		return nil
	}
	stream, err := d.Open(r.Context(), key, r)
	if err != nil {
		return err
	}
	if stream.Body != nil {
		defer stream.Body.Close()
	}
	response, err := newDownloadResponse(w, downloadWriteTimeout)
	if err != nil {
		return err
	}
	defer response.finish()
	w = response
	if seek, ok := stream.Body.(io.ReadSeeker); ok {
		modified := time.Time{}
		if info.Modified != nil {
			modified = *info.Modified
		}
		http.ServeContent(w, r, info.Name, modified, seek)
		return nil
	}
	for name, values := range stream.Header {
		w.Header()[name] = values
	}
	w.WriteHeader(stream.Status)
	if stream.Body != nil && stream.Status != 304 {
		// 固定 32 KiB 缓冲区，文件大小不会决定后端的内存分配量。
		if _, err = io.CopyBuffer(w, stream.Body, make([]byte, 32<<10)); err != nil && r.Context().Err() == nil {
			a.logger.Warn("文件传输中断", "error", err)
		}
	}
	return nil
}
