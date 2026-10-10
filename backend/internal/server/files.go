package server

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

func (a *App) registerFiles(mux *apiMux) {
	mux.HandleFunc("GET /api/fs/list", a.protected(MountView, false, a.listDirectory))
	mux.HandleFunc("GET /api/fs/get", a.protected(MountView, false, a.fileDetail))
	mux.HandleFunc("GET /api/fs/content", a.protected(MountView, false, a.fileContent))
	mux.HandleFunc("GET /api/fs/download", a.protected(MountView, false, a.fileContent))
	mux.HandleFunc("GET /api/fs/file-link", a.protected(MountView, false, a.fileLink))
	mux.HandleFunc("POST /api/fs/create-share", a.protected(FileShare|MountView, false, a.createShare))
	mux.HandleFunc("GET /api/p/{path...}", a.handle(a.signedContent))
	mux.HandleFunc("GET /api/share/get/{slug}", a.handle(a.publicShare))
	mux.HandleFunc("POST /api/share/verify/{slug}", a.handle(a.publicShare))
	mux.HandleFunc("GET /api/s/{slug}", a.handle(a.shareContent))
	mux.HandleFunc("GET /api/s/{slug}/{filename}", a.handle(a.shareContent))
	mux.HandleFunc("GET /api/share/content/{slug}", a.handle(a.shareContent))
	mux.HandleFunc("GET /api/share/content/{slug}/{filename}", a.handle(a.shareContent))
	mux.HandleFunc("GET /api/files", a.protected(FileManage, false, a.listShares))
	mux.HandleFunc("GET /api/files/{id}", a.protected(FileManage, false, a.shareDetail))
	mux.HandleFunc("PUT /api/files/{id}", a.protected(FileManage, false, a.updateShare))
	mux.HandleFunc("DELETE /api/files/batch-delete", a.protected(FileManage, false, a.deleteShares))
	// 安全默认：不向任何调用者返回存储认证头或预签名上游 URL。
	mux.HandleFunc("POST /api/proxy/link", a.protected(MountView, false, func(w http.ResponseWriter, r *http.Request) error { return unsupported() }))
}

func decorated(info FileInfo, m Mount, base string) FileInfo {
	info.Path = path.Join(base, info.Name)
	if info.Directory {
		info.Path += "/"
	}
	info.MountID = m.ID
	info.StorageType = m.StorageType
	return info
}

func (a *App) listDirectory(w http.ResponseWriter, r *http.Request) error {
	target, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	actor := principal(r)
	if !actor.Admin && !within(actor.BasicPath, target) && !within(target, actor.BasicPath) {
		return forbidden()
	}
	if err = a.checkPathPassword(r, target); err != nil {
		return err
	}
	mounts, err := a.allMounts(r.Context())
	if err != nil {
		return err
	}
	// 根目录和挂载的父目录只显示允许访问的虚拟入口。
	virtual := true
	for _, m := range mounts {
		if m.Active == 1 && within(m.Path, target) {
			virtual = false
			break
		}
	}
	if virtual {
		items := []FileInfo{}
		seen := map[string]bool{}
		for _, m := range mounts {
			if m.Active != 1 || !within(target, m.Path) || m.Path == target {
				continue
			}
			var s Storage
			if err = a.resource(r.Context(), "storage_configs", "id", m.StorageID, &s); err != nil {
				return err
			}
			if s.Status != "ENABLED" || !a.storageAllowed(actor, s) {
				continue
			}
			if !actor.Admin && !within(actor.BasicPath, m.Path) && !within(m.Path, actor.BasicPath) {
				continue
			}
			relative := strings.TrimPrefix(strings.TrimPrefix(m.Path, target), "/")
			name := strings.Split(relative, "/")[0]
			if seen[name] {
				continue
			}
			seen[name] = true
			info := FileInfo{Name: name, Directory: true, IsMount: path.Join(target, name) == m.Path}
			classify(&info)
			items = append(items, decorated(info, m, target))
		}
		if target != "/" && len(items) == 0 {
			return notFound()
		}
		meta, err := a.effectiveMeta(r.Context(), target)
		if err != nil {
			return err
		}
		a.write(w, 200, map[string]any{"path": target, "items": items, "isVirtual": true, "hasMore": false, "meta": map[string]any{"headerMarkdown": meta.Header, "footerMarkdown": meta.Footer}}, "获取目录成功")
		return nil
	}
	listingRequest := r
	if !actor.Admin && !within(actor.BasicPath, target) && within(target, actor.BasicPath) {
		// 父目录只用于导航；下面仍按原始 basic_path 过滤每个条目。
		navigationActor := *actor
		navigationActor.BasicPath = target
		listingRequest = r.WithContext(withPrincipal(r.Context(), &navigationActor))
	}
	m, _, d, key, err := a.resolveMount(listingRequest, target, MountView, true)
	if err != nil {
		return err
	}
	limit, _ := paging(r)
	result, err := d.List(r.Context(), key, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		return err
	}
	meta, err := a.effectiveMeta(r.Context(), target)
	if err != nil {
		return err
	}
	visible := []FileInfo{}
	for _, item := range result.Items {
		info := decorated(item, m, target)
		canonical, err := cleanPath(info.Path)
		if err != nil {
			continue
		}
		if !actor.Admin && !within(actor.BasicPath, canonical) && !(info.Directory && within(canonical, actor.BasicPath)) {
			continue
		}
		hidden := false
		if !actor.Admin {
			for _, pattern := range meta.HidePatterns {
				if matched, _ := path.Match(pattern, info.Name); matched {
					hidden = true
					break
				}
			}
		}
		if !hidden {
			visible = append(visible, info)
		}
	}
	result.Items = visible
	view := toMap(result)
	view["path"] = target
	view["mount_id"] = m.ID
	view["storage_type"] = m.StorageType
	view["meta"] = map[string]any{"headerMarkdown": meta.Header, "footerMarkdown": meta.Footer}
	a.write(w, 200, view, "获取目录成功")
	return nil
}

func (a *App) fileInfo(r *http.Request, target string) (FileInfo, Driver, string, error) {
	m, _, d, key, err := a.resolveMount(r, target, MountView, true)
	if err != nil {
		return FileInfo{}, nil, "", err
	}
	info, err := d.Stat(r.Context(), key)
	if err != nil {
		return info, nil, "", err
	}
	info.Path = target
	info.Name = path.Base(target)
	info.MountID = m.ID
	info.StorageType = m.StorageType
	return info, d, key, nil
}
func (a *App) fsLink(r *http.Request, target string) (string, error) {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	if err := a.checkPathPassword(r, target); err != nil {
		return "", err
	}
	meta, err := a.effectiveMeta(r.Context(), target)
	if err != nil {
		return "", err
	}
	actor := principal(r)
	m, storage, _, _, err := a.resolveMount(r, target, MountView, false)
	if err != nil {
		return "", err
	}
	token := a.ticket(Ticket{Purpose: "file", Path: target, Subject: actor.ID, Version: actor.Owner(), Credential: actor.CredentialHash, MetaVersion: secretHash(meta.Password), SourceVersion: sourceVersion(m, storage), Expires: time.Now().Add(5 * time.Minute).Unix()})
	return scopedAPIPath(r, "/api/p") + escapePath(target) + "?sign=" + url.QueryEscape(token), nil
}
func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
func (a *App) fileDetail(w http.ResponseWriter, r *http.Request) error {
	target, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	info, _, _, err := a.fileInfo(r, target)
	if err != nil {
		return err
	}
	view := toMap(info)
	if !info.Directory {
		link, err := a.fsLink(r, target)
		if err != nil {
			return err
		}
		view["previewUrl"] = link
		view["downloadUrl"] = link + "&download=true"
		view["linkType"] = "proxy"
		view["previewSelection"] = previewSelection(info)
	}
	a.write(w, 200, view, "获取文件信息成功")
	return nil
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
func (a *App) fileLink(w http.ResponseWriter, r *http.Request) error {
	target, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	info, _, _, err := a.fileInfo(r, target)
	if err != nil {
		return err
	}
	if info.Directory {
		return badRequest("目录不支持生成下载链接")
	}
	link, err := a.fsLink(r, target)
	if err != nil {
		return err
	}
	if r.URL.Query().Get("force_download") == "true" {
		link += "&download=true"
	}
	a.write(w, 200, map[string]any{"url": link, "linkType": "proxy"}, "获取链接成功")
	return nil
}
func (a *App) fileContent(w http.ResponseWriter, r *http.Request) error {
	target, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	info, d, key, err := a.fileInfo(r, target)
	if err != nil {
		return err
	}
	return a.deliver(w, r, d, key, info, strings.HasSuffix(r.URL.Path, "/download"))
}

func (a *App) signedContent(w http.ResponseWriter, r *http.Request) error {
	target, err := cleanPath("/" + r.PathValue("path"))
	if err != nil {
		return err
	}
	// 从签名载荷读取主体只是为了查询当前权限，信任前必须完整验证签名。
	actor, err := a.ticketPrincipal(r, r.URL.Query().Get("sign"), "file", target)
	if err != nil {
		return err
	}
	r = r.WithContext(withPrincipal(r.Context(), actor))
	_, _, d, key, err := a.resolveMount(r, target, MountView, false)
	if err != nil {
		return err
	}
	info, err := d.Stat(r.Context(), key)
	if err != nil {
		return err
	}
	info.Name = path.Base(target)
	return a.deliver(w, r, d, key, info, r.URL.Query().Get("download") == "true")
}

func (a *App) deliver(w http.ResponseWriter, r *http.Request, d Driver, key string, info FileInfo, download bool) error {
	if info.Directory {
		return badRequest("目录不支持下载")
	}
	select {
	case a.downloads <- struct{}{}:
		defer func() { <-a.downloads }()
	default:
		return &apiError{503, "BUSY", "下载并发已满，请稍后重试"}
	}
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
