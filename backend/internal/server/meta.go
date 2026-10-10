package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

type effectiveMeta struct {
	OwnerPath, Password, Header, Footer string
	HidePatterns                        []string
}

func (a *App) effectiveMeta(ctx context.Context, target string) (effectiveMeta, error) {
	// 只读取当前路径的祖先，避免每次下载都载入所有目录的 Markdown。
	rows, err := a.db.QueryContext(ctx, `SELECT data FROM fs_meta WHERE json_extract(data,'$.path')='/' OR json_extract(data,'$.path')=? OR substr(?,1,length(json_extract(data,'$.path'))+1)=json_extract(data,'$.path')||'/'`, target, target)
	if err != nil {
		return effectiveMeta{}, err
	}
	defer rows.Close()
	metas := []FsMeta{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return effectiveMeta{}, err
		}
		var m FsMeta
		if err = json.Unmarshal([]byte(raw), &m); err != nil {
			return effectiveMeta{}, err
		}
		if within(m.Path, target) {
			metas = append(metas, m)
		}
	}
	if err = rows.Err(); err != nil {
		return effectiveMeta{}, err
	}
	slices.SortFunc(metas, func(a, b FsMeta) int { return len(a.Path) - len(b.Path) })
	result := effectiveMeta{HidePatterns: []string{}}
	for _, m := range metas {
		self := m.Path == target
		if m.Password != "" && (self || m.PasswordInherit) {
			result.Password = m.Password
			result.OwnerPath = m.Path
		}
		if self || m.HeaderInherit {
			result.Header = m.Header
		}
		if self || m.FooterInherit {
			result.Footer = m.Footer
		}
		if self || m.HideInherit {
			result.HidePatterns = append(result.HidePatterns, m.HidePatterns...)
		}
	}
	return result, nil
}

func (a *App) checkPathPassword(r *http.Request, target string) error {
	actor := principal(r)
	if actor != nil && actor.Admin {
		return nil
	}
	meta, err := a.effectiveMeta(r.Context(), target)
	if err != nil {
		return err
	}
	if meta.Password == "" {
		return nil
	}
	if actor == nil {
		return forbidden()
	}
	tokens := []string{r.Header.Get("X-FS-Path-Token"), r.URL.Query().Get("path_token")}
	extra := r.Header.Get("X-FS-Path-Tokens")
	if extra == "" {
		extra = r.URL.Query().Get("path_tokens")
	}
	if len(extra) <= 32768 {
		tokens = append(tokens, strings.Split(extra, ",")...)
	}
	for _, token := range tokens {
		if a.verifyTicket(strings.TrimSpace(token), "directory", meta.OwnerPath, actor.ID, secretHash(meta.Password)) {
			return nil
		}
	}
	return &apiError{403, "FS_PATH_PASSWORD_REQUIRED", "该目录需要密码访问"}
}

func (a *App) registerMeta(mux *apiMux) {
	mux.HandleFunc("GET /api/admin/fs-meta/list", a.protected(0, true, a.listMeta))
	mux.HandleFunc("POST /api/admin/fs-meta/create", a.protected(0, true, a.saveMeta))
	mux.HandleFunc("GET /api/admin/fs-meta/{id}", a.protected(0, true, a.metaDetail))
	mux.HandleFunc("PUT /api/admin/fs-meta/{id}", a.protected(0, true, a.saveMeta))
	mux.HandleFunc("DELETE /api/admin/fs-meta/{id}", a.protected(0, true, func(w http.ResponseWriter, r *http.Request) error {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
		if err := a.deleteResource(r.Context(), "fs_meta", r.PathValue("id")); err != nil {
			return err
		}
		a.write(w, 200, nil, "元信息已删除")
		return nil
	}))
	mux.HandleFunc("POST /api/fs/meta/password/verify", a.protected(MountView, false, a.verifyPathPassword))
}
func metaView(m FsMeta) map[string]any {
	v := toMap(m)
	v["hasPassword"] = m.Password != ""
	delete(v, "password")
	return v
}
func (a *App) listMeta(w http.ResponseWriter, r *http.Request) error {
	items, _, err := a.resources(r.Context(), "fs_meta", "", 1000, 0)
	if err != nil {
		return err
	}
	views := []map[string]any{}
	for _, raw := range items {
		var m FsMeta
		if err = json.Unmarshal(raw, &m); err != nil {
			return err
		}
		views = append(views, metaView(m))
	}
	a.write(w, 200, views, "获取成功")
	return nil
}
func (a *App) metaDetail(w http.ResponseWriter, r *http.Request) error {
	var m FsMeta
	if err := a.resource(r.Context(), "fs_meta", "id", r.PathValue("id"), &m); err != nil {
		return err
	}
	a.write(w, 200, metaView(m), "获取成功")
	return nil
}
func (a *App) saveMeta(w http.ResponseWriter, r *http.Request) error {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	var m FsMeta
	if r.Method == "PUT" {
		if err := a.resource(r.Context(), "fs_meta", "id", r.PathValue("id"), &m); err != nil {
			return err
		}
	}
	id := m.ID
	var fields map[string]json.RawMessage
	if err := decode(w, r, &fields); err != nil {
		return err
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(body, &m); err != nil {
		return badRequest("元信息格式无效")
	}
	m.Path, err = cleanPath(m.Path)
	if err != nil {
		return err
	}
	if len(m.Header) > 16384 || len(m.Footer) > 16384 || len(m.HidePatterns) > 100 {
		return badRequest("元信息超过大小限制")
	}
	for _, pattern := range m.HidePatterns {
		if len(pattern) > 200 {
			return badRequest("隐藏规则过长")
		}
		if _, err = path.Match(pattern, "example"); err != nil {
			return badRequest("隐藏规则无效")
		}
	}
	if raw, present := fields["password"]; present {
		// null 或空字符串清除密码；缺省字段保留已有哈希。
		var password *string
		if err = json.Unmarshal(raw, &password); err != nil {
			return badRequest("密码格式无效")
		}
		m.Password = ""
		if password != nil && *password != "" {
			m.Password, err = passwordHash(*password)
			if err != nil {
				return err
			}
		}
	}
	items, total, err := a.resources(r.Context(), "fs_meta", "", 1000, 0)
	if err != nil {
		return err
	}
	if id == "" && total >= 1000 {
		return badRequest("最多配置 1000 条目录元信息")
	}
	for _, raw := range items {
		var other FsMeta
		if err = json.Unmarshal(raw, &other); err != nil {
			return err
		}
		if other.ID != id && other.Path == m.Path {
			return badRequest("目录元信息已存在")
		}
	}
	if id == "" {
		m.ID = randomID()
	} else {
		m.ID = id
	}
	if err = a.saveResource(r.Context(), "fs_meta", m.ID, "", principal(r).ID, m); err != nil {
		return err
	}
	status := 200
	if id == "" {
		status = 201
	}
	a.write(w, status, metaView(m), "元信息已保存")
	return nil
}
func (a *App) verifyPathPassword(w http.ResponseWriter, r *http.Request) error {
	var body struct{ Path, Password string }
	if err := decode(w, r, &body); err != nil {
		return err
	}
	target, err := cleanPath(body.Path)
	if err != nil {
		return err
	}
	actor := principal(r)
	if !actor.Admin && !within(actor.BasicPath, target) {
		return forbidden()
	}
	meta, err := a.effectiveMeta(r.Context(), target)
	if err != nil {
		return err
	}
	if meta.Password == "" {
		a.write(w, 200, map[string]any{"verified": true, "requiresPassword": false, "token": nil, "path": target}, "该路径不需要密码")
		return nil
	}
	if !a.allowPasswordAttempt(r) {
		return &apiError{429, "RATE_LIMITED", "密码尝试过多"}
	}
	if err = a.checkPassword(meta.Password, body.Password); err != nil {
		return err
	}
	// 票据绑定身份、密码哈希版本和有效期，不包含明文密码。
	token := a.ticket(Ticket{Purpose: "directory", Path: meta.OwnerPath, Subject: actor.ID, Version: secretHash(meta.Password), Expires: time.Now().Add(30 * time.Minute).Unix()})
	a.write(w, 200, map[string]any{"verified": true, "requiresPassword": true, "token": token, "path": meta.OwnerPath}, "路径密码验证成功")
	return nil
}
