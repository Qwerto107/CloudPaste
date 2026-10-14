package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func sourceVersion(m Mount, s Storage) string {
	return secretHash(m.ID + ":" + m.UpdatedAt + ":" + s.ID + ":" + s.UpdatedAt)
}
func shareVersion(s Share) string { return secretHash(s.ID + ":" + s.Password + ":" + s.UpdatedAt) }
func shareGone(s Share) error {
	if expired(s.ExpiresAt) {
		return &apiError{410, "SHARE_GONE", "文件分享已过期"}
	}
	return nil
}

func shareView(s Share) map[string]any {
	v := toMap(s)
	delete(v, "password")
	delete(v, "source_version")
	delete(v, "meta_version")
	delete(v, "file_path")
	delete(v, "storage_key")
	// 子文件的来源与密码快照仅留在服务端，公开列表只返回展示信息。
	delete(v, "files")
	v["has_password"] = s.Password != ""
	v["requires_password"] = s.Password != ""
	v["shareUrl"] = "/file/" + s.Slug
	info := FileInfo{Name: s.Filename, MimeType: s.MimeType}
	classify(&info)
	v["type"] = info.Type
	v["typeName"] = info.TypeName
	v["previewSelection"] = previewSelection(info)
	v["previewUrl"] = nil
	v["downloadUrl"] = nil
	v["linkType"] = "proxy"
	if len(s.Files) > 0 {
		items := make([]map[string]any, 0, len(s.Files))
		for _, file := range s.Files {
			items = append(items, map[string]any{"id": file.ID, "filename": file.Filename, "size": file.Size, "mimetype": file.MimeType})
		}
		v["files"] = items
		v["type"] = "collection"
		v["typeName"] = "文件合集"
	}
	return v
}

// 分享不复制文件；来源配置、目录密码或创建者权限发生变化后必须重新创建分享。
// 此函数只复核授权与来源配置，不向远程存储发出文件查询。
func (a *App) shareDriver(r *http.Request, s Share) (Driver, string, error) {
	var actor *Principal
	if strings.HasPrefix(s.Owner, "apikey:") {
		var raw, credential string
		err := a.db.QueryRowContext(r.Context(), "SELECT data,key_hash FROM api_keys WHERE id=?", strings.TrimPrefix(s.Owner, "apikey:")).Scan(&raw, &credential)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, "", notFound()
			}
			return nil, "", err
		}
		var k APIKey
		if err = json.Unmarshal([]byte(raw), &k); err != nil {
			return nil, "", err
		}
		if k.Enabled != 1 || expired(k.ExpiresAt) {
			return nil, "", forbidden()
		}
		actor = keyPrincipal(k, credential)
	} else {
		var id string
		if err := a.db.QueryRowContext(r.Context(), "SELECT id FROM admins WHERE id=?", s.Owner).Scan(&id); err != nil {
			return nil, "", notFound()
		}
		actor = &Principal{ID: id, Admin: true, BasicPath: "/"}
	}
	ownerRequest := r.WithContext(withPrincipal(r.Context(), actor))
	if s.StorageID != "" {
		return a.uploadedShareDriver(ownerRequest, s)
	}
	m, storage, d, key, err := a.resolveMount(ownerRequest, s.Path, MountView|FileShare, false)
	if err != nil {
		return nil, "", err
	}
	meta, err := a.effectiveMeta(r.Context(), s.Path)
	if err != nil {
		return nil, "", err
	}
	if s.SourceVersion != sourceVersion(m, storage) || s.MetaVersion != secretHash(meta.Password) {
		return nil, "", &apiError{410, "SHARE_GONE", "来源配置或目录保护已变化，请重新创建分享"}
	}
	return d, key, nil
}

func (a *App) shareSource(r *http.Request, s Share) (FileInfo, Driver, string, error) {
	d, key, err := a.shareDriver(r, s)
	if err != nil {
		return FileInfo{}, nil, "", err
	}
	// 存储查询可能耗时几十秒，调用方必须在全局写锁之外执行。
	info, err := d.Stat(r.Context(), key)
	info.Name = s.Filename
	return info, d, key, err
}

// 调用方持有写锁，存储查询完成后重新核对当前分享及创建者权限。
func (a *App) revalidateShare(r *http.Request, previous Share) (Share, error) {
	var current Share
	if err := a.resource(r.Context(), "shares", "slug", previous.Slug, &current); err != nil {
		return current, err
	}
	if shareVersion(current) != shareVersion(previous) {
		return current, forbidden()
	}
	if err := shareGone(current); err != nil {
		return current, err
	}
	return current, a.validateShareSources(r, current)
}

func (a *App) publicShare(w http.ResponseWriter, r *http.Request) error {
	var s Share
	if err := a.resource(r.Context(), "shares", "slug", r.PathValue("slug"), &s); err != nil {
		return err
	}
	if err := shareGone(s); err != nil {
		return err
	}
	// 查看上限在密码提示前检查，失效分享不再要求输入密码。
	if s.MaxViews > 0 && s.Views >= s.MaxViews {
		return &apiError{410, "SHARE_GONE", "文件分享已达到最大查看次数"}
	}
	access := a.readShareAccess(r, "share-access", s.Slug, s.ID, shareVersion(s))
	hasAccess := s.Password != "" && access.Expires > 0
	if s.Password != "" && !hasAccess {
		if r.Method == "GET" {
			a.write(w, 200, map[string]any{"slug": s.Slug, "requires_password": true}, "需要密码验证")
			return nil
		}
		var body struct{ Password string }
		if err := decode(w, r, &body); err != nil {
			return err
		}
		if !a.allowPasswordAttempt(r) {
			return &apiError{429, "RATE_LIMITED", "密码尝试过多"}
		}
		if err := a.checkPassword(s.Password, body.Password); err != nil {
			return err
		}
	}
	if len(s.Files) > 0 {
		// 打开合集只复核来源权限；每个文件在实际读取时再查询存储。
		if err := a.validateShareSources(r, s); err != nil {
			return err
		}
	} else {
		if _, _, _, err := a.shareSource(r, s); err != nil {
			return err
		}
	}
	// 锁只覆盖当前授权复核和次数更新，不覆盖存储查询或响应写入。
	current, err := func() (Share, error) {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
		current, err := a.revalidateShare(r, s)
		if err != nil {
			return current, err
		}
		if hasAccess && access.Expires <= time.Now().Unix() {
			return current, forbidden()
		}
		if current.MaxViews > 0 && current.Views >= current.MaxViews {
			return current, &apiError{410, "SHARE_GONE", "文件分享已达到最大查看次数"}
		}
		// 解锁凭证只免除密码验证，每次成功打开或刷新仍消耗查看额度。
		current.Views++
		err = a.saveResource(r.Context(), "shares", current.ID, current.Slug, current.Owner, current)
		return current, err
	}()
	if err != nil {
		return err
	}
	s = current
	// 一次查看签发短期读取票据，视频分段请求不会反复消耗查看次数。
	leaseExpires := time.Now().Add(shareAccessDuration).Unix()
	if hasAccess {
		// 刷新不能延长凭证寿命，下载链接也受同一访问期限约束。
		leaseExpires = access.Expires
	}
	token := a.ticket(Ticket{Purpose: "share", Path: s.Slug, Subject: s.ID, Version: shareVersion(s), Expires: leaseExpires})
	link := "/api/s/" + s.Slug + "/" + url.PathEscape(s.Filename) + "?token=" + url.QueryEscape(token)
	v := shareView(s)
	v["requires_password"] = false
	v["previewUrl"] = link
	v["downloadUrl"] = link + "&download=true"
	v["contentUrl"] = link
	v["isLastView"] = s.MaxViews > 0 && s.Views == s.MaxViews
	if s.Password != "" {
		if !hasAccess {
			access = Ticket{Purpose: "share-access", Path: s.Slug, Subject: s.ID, Version: shareVersion(s), Expires: leaseExpires}
		}
		a.addShareAccess(v, access)
	}
	if len(s.Files) > 0 {
		items := v["files"].([]map[string]any)
		for _, item := range items {
			fileLink := "/api/s/" + s.Slug + "?file=" + url.QueryEscape(item["id"].(string)) + "&token=" + url.QueryEscape(token)
			item["contentUrl"] = fileLink
			item["downloadUrl"] = fileLink + "&download=true"
		}
		v["archiveUrl"] = "/api/s/" + s.Slug + "?archive=true&token=" + url.QueryEscape(token)
		delete(v, "contentUrl")
		v["previewUrl"] = nil
		v["downloadUrl"] = nil
	}
	a.write(w, 200, v, "获取文件成功")
	return nil
}

// 内容入口先检查票据或匿名查看额度，避免无效请求触发远程存储查询。
func (a *App) shareContentAccess(r *http.Request, s Share) (bool, error) {
	if err := shareGone(s); err != nil {
		return false, err
	}
	token := r.URL.Query().Get("token")
	if a.verifyTicket(token, "share", s.Slug, s.ID, shareVersion(s)) {
		return false, nil
	}
	if s.Password != "" || token != "" {
		return false, forbidden()
	}
	if s.MaxViews > 0 && s.Views >= s.MaxViews {
		return false, &apiError{410, "SHARE_GONE", "文件分享已达到最大查看次数"}
	}
	return r.Method != "HEAD", nil
}

func (a *App) shareContent(w http.ResponseWriter, r *http.Request) error {
	var s Share
	if err := a.resource(r.Context(), "shares", "slug", r.PathValue("slug"), &s); err != nil {
		return err
	}
	if _, err := a.shareContentAccess(r, s); err != nil {
		return err
	}
	if len(s.Files) > 0 {
		return a.collectionContent(w, r, s)
	}
	info, d, key, err := a.shareSource(r, s)
	if err != nil {
		return err
	}
	err = func() error {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
		current, err := a.revalidateShare(r, s)
		if err != nil {
			return err
		}
		// 查询期间票据也可能过期，完成后必须再次验证。
		consumeView, err := a.shareContentAccess(r, current)
		if err != nil {
			return err
		}
		if consumeView {
			current.Views++
			return a.saveResource(r.Context(), "shares", current.ID, current.Slug, current.Owner, current)
		}
		return nil
	}()
	if err != nil {
		return err
	}
	return a.deliver(w, r, d, key, info, r.URL.Query().Get("download") == "true")
}

func (a *App) listShares(w http.ResponseWriter, r *http.Request) error {
	limit, offset := paging(r)
	owner := ""
	if !principal(r).Admin {
		owner = principal(r).Owner()
	}
	items, total, err := a.resources(r.Context(), "shares", owner, limit, offset)
	if err != nil {
		return err
	}
	views := []map[string]any{}
	for _, raw := range items {
		var s Share
		if err = json.Unmarshal(raw, &s); err != nil {
			return err
		}
		views = append(views, shareView(s))
	}
	a.write(w, 200, map[string]any{"files": views, "pagination": pagination(total, limit, offset)}, "获取成功")
	return nil
}
func (a *App) ownedShare(r *http.Request) (Share, error) {
	var s Share
	err := a.resource(r.Context(), "shares", "id", r.PathValue("id"), &s)
	if err != nil {
		return s, err
	}
	if !principal(r).Admin && s.Owner != principal(r).Owner() {
		return s, notFound()
	}
	return s, nil
}
func (a *App) updateShare(w http.ResponseWriter, r *http.Request) error {
	s, err := a.ownedShare(r)
	if err != nil {
		return err
	}
	var body struct {
		Remark, Password, Slug, Filename *string
		ClearPassword                    bool
		ExpiresAt                        json.RawMessage `json:"expires_at"`
		MaxViews                         *int            `json:"max_views"`
	}
	if err = decode(w, r, &body); err != nil {
		return err
	}
	// 密码哈希与请求体读取不占用全局写锁。
	if body.ClearPassword {
		empty := ""
		body.Password = &empty
	} else if body.Password != nil && *body.Password != "" {
		hash, err := passwordHash(*body.Password)
		if err != nil {
			return err
		}
		body.Password = &hash
	}
	s, err = func() (Share, error) {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
		// 重新认证并读取最新记录，保留接收请求期间已发生的查看和其他更新。
		actor, err := a.authenticate(r)
		if err != nil {
			return Share{}, err
		}
		if !actor.Has(FileManage) {
			return Share{}, forbidden()
		}
		currentRequest := r.WithContext(withPrincipal(r.Context(), actor))
		s, err := a.ownedShare(currentRequest)
		if err != nil {
			return s, err
		}
		// 只修改显示名，不移动原文件；继续拒绝穿越与控制字符。
		if body.Filename != nil {
			if err := validateUploadName(*body.Filename); err != nil {
				return s, err
			}
			s.Filename = *body.Filename
		}
		if body.Remark != nil {
			s.Remark = *body.Remark
		}
		if len(s.Remark) > 4000 {
			return s, badRequest("备注过长")
		}
		if body.Slug != nil {
			s.Slug = *body.Slug
		}
		if !validSlug.MatchString(s.Slug) {
			return s, badRequest("链接后缀格式无效")
		}
		if body.Password != nil {
			s.Password = *body.Password
		}
		if body.ExpiresAt != nil {
			if err = json.Unmarshal(body.ExpiresAt, &s.ExpiresAt); err != nil {
				return s, badRequest("过期时间无效")
			}
		}
		if body.MaxViews != nil {
			s.MaxViews = *body.MaxViews
			s.Views = 0
		}
		if err = validateExpiry(s.ExpiresAt, s.MaxViews); err != nil {
			return s, err
		}
		s.UpdatedAt = nowText()
		if err = a.saveResource(r.Context(), "shares", s.ID, s.Slug, s.Owner, s); err != nil {
			return s, badRequest("链接后缀已被占用")
		}
		return s, nil
	}()
	if err != nil {
		return err
	}
	a.write(w, 200, shareView(s), "分享已更新")
	return nil
}
func (a *App) deleteShares(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		IDs        []string `json:"ids"`
		DeleteMode string   `json:"delete_mode"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if len(body.IDs) == 0 || len(body.IDs) > 200 {
		return badRequest("一次删除 1-200 条记录")
	}
	// 第一版存储只读，明确拒绝“同时删除文件”，防止界面误报文件已经删除。
	if body.DeleteMode != "" && body.DeleteMode != "record_only" {
		return unsupported()
	}
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	count := 0
	for _, id := range body.IDs {
		r.SetPathValue("id", id)
		s, err := a.ownedShare(r)
		if err != nil {
			var api *apiError
			if errors.As(err, &api) && api.Status == 404 {
				continue
			}
			return err
		}
		if err = a.deleteResource(r.Context(), "shares", s.ID); err != nil {
			return err
		}
		count++
	}
	a.write(w, 200, map[string]int{"success": count, "deleted": count}, "分享记录已删除，原文件保留")
	return nil
}
