package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"
)

var validSlug = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

func pasteVersion(p Paste) string { return secretHash(p.ID + ":" + p.Password + ":" + p.UpdatedAt) }

func expired(expires *string) bool {
	if expires == nil || *expires == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, *expires)
	return err != nil || !t.After(time.Now())
}

func validateExpiry(expires *string, maxViews int) error {
	if maxViews < 0 || maxViews > 1000000 {
		return badRequest("max_views 必须为 0-1000000")
	}
	if expires != nil && *expires != "" {
		if _, err := time.Parse(time.RFC3339, *expires); err != nil {
			return badRequest("expires_at 必须为 RFC3339 时间")
		}
	}
	return nil
}

func (a *App) registerPastes(mux *apiMux) {
	mux.HandleFunc("POST /api/paste", a.guestOrProtected(guestText, TextShare, a.createPaste))
	mux.ServeMux.HandleFunc("GET /api/paste/{slug}", a.handleShare(a.publicPaste))
	mux.ServeMux.HandleFunc("POST /api/paste/{slug}", a.handleShare(a.publicPaste))
	// 管理员通过独立入口查看失效内容，公开入口仍保持原有约束。
	mux.HandleFunc("GET /api/admin/paste/{slug}", a.protected(0, true, a.publicPaste))
	mux.ServeMux.HandleFunc("GET /api/owner/paste/{slug}", a.protected(0, false, a.publicPaste))
	mux.HandleFunc("GET /api/pastes", a.protected(TextManage, false, a.listPastes))
	mux.HandleFunc("GET /api/pastes/{id}", a.protected(TextManage, false, a.pasteDetail))
	mux.HandleFunc("PUT /api/pastes/{slug}", a.protected(TextManage, false, a.updatePaste))
	mux.HandleFunc("DELETE /api/pastes/batch-delete", a.protected(TextManage, false, a.deletePastes))
}

func (a *App) createPaste(w http.ResponseWriter, r *http.Request) error {
	var p Paste
	p.Public = 1
	// 省略有效期采用站点默认值；显式 null 仍表示不过期。
	var body struct {
		Paste
		Expiry json.RawMessage `json:"expires_at"`
	}
	body.Paste = p
	if err := decode(w, r, &body); err != nil {
		return err
	}
	p = body.Paste
	// 访客没有认证的私有内容入口，只能创建通过链接访问的分享。
	if principal(r).Guest && p.Public != 1 {
		return badRequest("访客不能创建私有文本")
	}
	// 客户端不能提交加密副本；必须由服务端使用当前密钥生成。
	p.EncryptedPassword = ""
	if body.Expiry == nil {
		expiry, err := a.defaultShareExpiry(r)
		if err != nil {
			return err
		}
		p.ExpiresAt = expiry
	} else if err := json.Unmarshal(body.Expiry, &p.ExpiresAt); err != nil {
		return badRequest("过期时间无效")
	}
	if p.Content == "" {
		return badRequest("文本内容不能为空")
	}
	if len(p.Title) > 1000 || len(p.Remark) > 4000 {
		return badRequest("标题或备注过长")
	}
	if p.Slug == "" {
		p.Slug = randomID()
	}
	if !validSlug.MatchString(p.Slug) {
		return badRequest("链接后缀格式无效")
	}
	if err := validateExpiry(p.ExpiresAt, p.MaxViews); err != nil {
		return err
	}
	if p.Password != "" {
		var err error
		p.Password, p.EncryptedPassword, err = a.protectSharePassword(p.Password)
		if err != nil {
			return err
		}
	}
	p.ID = randomID()
	p.Owner = principal(r).Owner()
	p.CreatedAt = nowText()
	p.UpdatedAt = p.CreatedAt
	p.Views = 0
	// 与查看次数更新串行，回收时只能看到已提交的失效状态。
	a.mutationMu.Lock()
	err := a.saveShareResource(r.Context(), "pastes", p.ID, p.Slug, p.Owner, p)
	a.mutationMu.Unlock()
	if err != nil {
		return err
	}
	view, err := a.managedPasteView(p, false)
	if err != nil {
		return err
	}
	view["url"] = "/paste/" + p.Slug
	a.write(w, 201, view, "文本创建成功")
	return nil
}

func pasteView(p Paste, content bool) map[string]any {
	view := toMap(p)
	delete(view, "password")
	delete(view, "password_encrypted")
	view["has_password"] = p.Password != ""
	view["hasPassword"] = p.Password != ""
	if !content {
		delete(view, "content")
	}
	return view
}

func (a *App) publicPaste(w http.ResponseWriter, r *http.Request) error {
	var p Paste
	if err := a.resource(r.Context(), "pastes", "slug", r.PathValue("slug"), &p); err != nil {
		return err
	}
	actor := principal(r)
	admin := adminShareRead(r)
	if ownerShareRoute(r) && !ownerShareRead(r, p.Owner, TextManage) {
		return notFound()
	}
	managed := admin || ownerShareRead(r, p.Owner, TextManage)
	if p.Public != 1 && (actor == nil || (!actor.Admin && actor.Owner() != p.Owner)) {
		return notFound()
	}
	// 先检查有效期和查看上限，失效分享无需再验证密码。
	if !managed && (expired(p.ExpiresAt) || (p.MaxViews > 0 && p.Views >= p.MaxViews)) {
		return &apiError{410, "PASTE_GONE", "文本分享已过期或达到最大查看次数"}
	}
	password := r.URL.Query().Get("password")
	if r.Method == "POST" {
		var body struct{ Password string }
		if err := decode(w, r, &body); err != nil {
			return err
		}
		password = body.Password
	}
	access := a.readShareAccess(r, "paste-access", p.Slug, p.ID, pasteVersion(p))
	hasAccess := p.Password != "" && access.Expires > 0
	if !managed && p.Password != "" && !hasAccess {
		if password == "" {
			a.write(w, 200, map[string]any{"slug": p.Slug, "hasPassword": true, "requiresPassword": true}, "需要密码验证")
			return nil
		}
		if !a.allowPasswordAttempt(r) {
			return &apiError{429, "RATE_LIMITED", "密码尝试过多"}
		}
		if err := a.checkPassword(p.Password, password); err != nil {
			return err
		}
	}
	// 锁内重新读取，防止并发访问在最后一次查看时越过限制。
	verifiedVersion := pasteVersion(p)
	a.mutationMu.Lock()
	if err := a.resource(r.Context(), "pastes", "slug", p.Slug, &p); err != nil {
		a.mutationMu.Unlock()
		return err
	}
	if pasteVersion(p) != verifiedVersion || (hasAccess && access.Expires <= time.Now().Unix()) {
		a.mutationMu.Unlock()
		return forbidden()
	}
	if p.Public != 1 && (actor == nil || (!actor.Admin && actor.Owner() != p.Owner)) {
		a.mutationMu.Unlock()
		return notFound()
	}
	if !managed && (expired(p.ExpiresAt) || (p.MaxViews > 0 && p.Views >= p.MaxViews)) {
		a.mutationMu.Unlock()
		return &apiError{410, "PASTE_GONE", "文本分享已过期或达到最大查看次数"}
	}
	// 解锁凭证只免除密码验证，每次成功打开或刷新仍消耗查看额度。
	var err error
	if !managed {
		p.Views++
		err = a.saveResource(r.Context(), "pastes", p.ID, p.Slug, p.Owner, p)
	}
	a.mutationMu.Unlock()
	if err != nil {
		return err
	}
	view := pasteView(p, true)
	view["requiresPassword"] = false
	if managed {
		view["admin_preview"] = admin
		view["owner_preview"] = !admin
		view["expiry_reason"] = shareExpiryReason(p.ExpiresAt, p.MaxViews, p.Views)
	}
	view["isLastView"] = p.MaxViews > 0 && p.Views == p.MaxViews
	if !managed && p.Password != "" {
		if !hasAccess {
			access = Ticket{Purpose: "paste-access", Path: p.Slug, Subject: p.ID, Version: pasteVersion(p), Expires: time.Now().Add(shareAccessDuration).Unix()}
		}
		a.addShareAccess(view, access)
	}
	a.write(w, 200, view, "获取文本成功")
	return nil
}

func (a *App) listPastes(w http.ResponseWriter, r *http.Request) error {
	limit, offset := paging(r)
	owner := ""
	if !principal(r).Admin {
		owner = principal(r).Owner()
	}
	items, total, err := a.resources(r.Context(), "pastes", owner, limit, offset)
	if err != nil {
		return err
	}
	views := []map[string]any{}
	for _, raw := range items {
		var p Paste
		if err = json.Unmarshal(raw, &p); err != nil {
			return err
		}
		view, err := a.managedPasteView(p, true)
		if err != nil {
			return err
		}
		views = append(views, view)
	}
	a.write(w, 200, map[string]any{"results": views, "pagination": pagination(total, limit, offset)}, "获取成功")
	return nil
}

func (a *App) pasteDetail(w http.ResponseWriter, r *http.Request) error {
	var p Paste
	if err := a.resource(r.Context(), "pastes", "id", r.PathValue("id"), &p); err != nil {
		return err
	}
	if !principal(r).Admin && principal(r).Owner() != p.Owner {
		return notFound()
	}
	view, err := a.managedPasteView(p, true)
	if err != nil {
		return err
	}
	a.write(w, 200, view, "获取成功")
	return nil
}

func (a *App) updatePaste(w http.ResponseWriter, r *http.Request) error {
	var p Paste
	if err := a.resource(r.Context(), "pastes", "slug", r.PathValue("slug"), &p); err != nil {
		return err
	}
	if !principal(r).Admin && principal(r).Owner() != p.Owner {
		return notFound()
	}
	var body struct {
		ID                                        string `json:"id"`
		Content, Title, Remark, Password, NewSlug *string
		ClearPassword                             bool
		ExpiresAt                                 json.RawMessage `json:"expires_at"`
		MaxViews                                  *int            `json:"max_views"`
		Public                                    *Flag           `json:"is_public"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	// 后缀可能已被新分享复用，旧编辑窗口不能修改新记录。
	if body.ID != "" && body.ID != p.ID {
		return notFound()
	}
	// 接收请求和计算密码哈希都在锁外，慢客户端不能持有全局写锁。
	var encryptedPassword string
	if body.ClearPassword {
		empty := ""
		body.Password = &empty
	} else if body.Password != nil && *body.Password != "" {
		hash, encrypted, err := a.protectSharePassword(*body.Password)
		if err != nil {
			return err
		}
		body.Password = &hash
		encryptedPassword = encrypted
	}
	id := p.ID
	p, err := func() (Paste, error) {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
		// 慢请求期间权限、记录或查看次数可能变化，必须读取当前状态再应用字段。
		actor, err := a.authenticate(r)
		if err != nil {
			return Paste{}, err
		}
		if !actor.Has(TextManage) {
			return Paste{}, forbidden()
		}
		var p Paste
		if err := a.resource(r.Context(), "pastes", "slug", r.PathValue("slug"), &p); err != nil {
			return p, err
		}
		if p.ID != id || (!actor.Admin && actor.Owner() != p.Owner) {
			return p, notFound()
		}
		if body.Content != nil {
			p.Content = *body.Content
		}
		if p.Content == "" {
			return p, badRequest("内容不能为空")
		}
		if body.Title != nil {
			p.Title = *body.Title
		}
		if body.Remark != nil {
			p.Remark = *body.Remark
		}
		if len(p.Title) > 1000 || len(p.Remark) > 4000 {
			return p, badRequest("标题或备注过长")
		}
		if body.NewSlug != nil {
			p.Slug = *body.NewSlug
			if p.Slug == "" {
				p.Slug = randomID()
			}
		}
		if !validSlug.MatchString(p.Slug) {
			return p, badRequest("链接后缀格式无效")
		}
		if body.Password != nil {
			p.Password = *body.Password
			p.EncryptedPassword = encryptedPassword
		}
		if body.MaxViews != nil {
			p.MaxViews = *body.MaxViews
			p.Views = 0
		}
		if body.Public != nil {
			p.Public = *body.Public
		}
		if body.ExpiresAt != nil {
			if err := json.Unmarshal(body.ExpiresAt, &p.ExpiresAt); err != nil {
				return p, badRequest("过期时间无效")
			}
		}
		if err := validateExpiry(p.ExpiresAt, p.MaxViews); err != nil {
			return p, err
		}
		p.UpdatedAt = nowText()
		if err := a.saveShareResource(r.Context(), "pastes", p.ID, p.Slug, p.Owner, p); err != nil {
			return p, err
		}
		return p, nil
	}()
	if err != nil {
		return err
	}
	// 响应写入也可能阻塞，释放写锁后再返回完整内容。
	view, err := a.managedPasteView(p, true)
	if err != nil {
		return err
	}
	a.write(w, 200, view, "文本已更新")
	return nil
}

func (a *App) deletePastes(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if len(body.IDs) == 0 || len(body.IDs) > 200 {
		return badRequest("一次删除 1-200 条记录")
	}
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	count := 0
	for _, id := range body.IDs {
		var p Paste
		if err := a.resource(r.Context(), "pastes", "id", id, &p); err != nil {
			continue
		}
		if !principal(r).Admin && principal(r).Owner() != p.Owner {
			continue
		}
		if err := a.deleteResource(r.Context(), "pastes", id); err != nil {
			return err
		}
		count++
	}
	a.write(w, 200, map[string]int{"success": count}, "删除完成")
	return nil
}
