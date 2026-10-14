package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
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
