package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func ownerShareRoute(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/owner/")
}
func ownerShareRead(r *http.Request, owner string, permission int) bool {
	p := principal(r)
	// 创建权限或管理权限均可读取本人内容，不要求创建者额外获得管理权限。
	createPermission := TextShare
	if permission == FileManage {
		createPermission = FileShare
	}
	return ownerShareRoute(r) && p != nil && !p.Admin && (p.Has(permission) || p.Has(createPermission)) && p.Owner() == owner
}
func managedFileRead(r *http.Request, s Share) bool {
	return adminShareRead(r) || ownerShareRead(r, s.Owner, FileManage)
}

// 原生媒体和下载请求不能附带 API Key，短期票据绑定凭据摘要并逐次复核权限。
func (a *App) ownerDownloadPrincipal(r *http.Request) (*Principal, error) {
	slug := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/owner/s/"), "/", 2)[0]
	lease, err := a.parseTicket(r.URL.Query().Get("token"), "owner-share", slug)
	if err != nil || lease.Session == "" {
		return nil, forbidden()
	}
	var raw string
	if err := a.db.QueryRowContext(r.Context(), "SELECT data FROM api_keys WHERE key_hash=?", lease.Session).Scan(&raw); err != nil {
		return nil, forbidden()
	}
	var key APIKey
	if json.Unmarshal([]byte(raw), &key) != nil || key.Enabled != 1 || expired(key.ExpiresAt) {
		return nil, forbidden()
	}
	actor := keyPrincipal(key, lease.Session)
	if !actor.Has(FileManage) && !actor.Has(FileShare) {
		return nil, forbidden()
	}
	return actor, nil
}

func adminShareRead(r *http.Request) bool {
	p := principal(r)
	return adminRequest(r) && p != nil && p.Admin
}

func shareExpiryReason(expires *string, maxViews, views int) string {
	if expired(expires) {
		return "已过期"
	}
	if maxViews > 0 && views >= maxViews {
		return "查看次数已用完"
	}
	return ""
}

// 票据只用于管理员文件读取，且每次请求都复核原登录会话是否仍有效。
func (a *App) adminDownloadPrincipal(r *http.Request) (*Principal, error) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/s/")
	slug := strings.SplitN(path, "/", 2)[0]
	lease, err := a.parseTicket(r.URL.Query().Get("token"), "admin-share", slug)
	if err != nil || lease.Session == "" {
		return nil, forbidden()
	}
	var id, name string
	if err := a.db.QueryRowContext(r.Context(), "SELECT a.id,a.username FROM sessions s JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=? AND s.expires_at>?", lease.Session, time.Now().Unix()).Scan(&id, &name); err != nil {
		return nil, forbidden()
	}
	// 原生文件请求通过票据恢复会话时，也保留管理员用户名供操作日志使用。
	return &Principal{ID: id, Name: name, Admin: true, BasicPath: "/", CredentialHash: lease.Session}, nil
}
