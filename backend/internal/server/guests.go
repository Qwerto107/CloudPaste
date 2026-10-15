package server

import (
	"database/sql"
	"encoding/base64"
	"net/http"
)

const (
	guestText           = "guest_text_share_enabled"
	guestUpload         = "guest_file_upload_enabled"
	guestCreateTransfer = "guest_transfer_create_enabled"
	guestJoinTransfer   = "guest_transfer_join_enabled"
)

// 访客开关只作用于指定入口，不为访客授予通用认证或管理权限。
func (a *App) guestAllowed(r *http.Request, key string) (bool, error) {
	var value string
	err := a.db.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE key=?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return key == guestJoinTransfer, nil
	}
	return value == "true" && err == nil, err
}

func guestPrincipal(id string, permission int) *Principal {
	return &Principal{ID: id, Guest: true, BasicPath: "/", Permissions: permission}
}

func (a *App) creationPrincipal(r *http.Request, key string, permission int) (*Principal, error) {
	p, err := a.authenticate(r)
	if err != nil || p != nil {
		return p, err
	}
	allowed, err := a.guestAllowed(r, key)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, &apiError{401, "UNAUTHORIZED", "访客无此操作权限，请登录后重试"}
	}
	// 随机凭证仅用于隔离同一批上传和合集归属，不能用来登录或管理分享。
	id := randomID()
	if previous := principal(r); previous != nil && previous.Guest {
		id = previous.ID
	}
	if token := r.Header.Get("X-Guest-Token"); token != "" {
		decoded, e := base64.RawURLEncoding.Strict().DecodeString(token)
		if e != nil || len(decoded) != 32 {
			return nil, badRequest("访客上传凭证无效")
		}
		id = secretHash(token)
	}
	if key == guestUpload {
		permission |= FileShare | MountUpload
	}
	return guestPrincipal(id, permission), nil
}

func (a *App) guestOrProtected(key string, permission int, fn endpoint) http.HandlerFunc {
	return a.handle(func(w http.ResponseWriter, r *http.Request) error {
		p, err := a.creationPrincipal(r, key, permission)
		if err != nil {
			return err
		}
		if !p.Has(permission) {
			return forbidden()
		}
		return fn(w, r.WithContext(withPrincipal(r.Context(), p)))
	})
}
