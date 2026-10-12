package server

import (
	"errors"
	"net/http"
	"time"
)

const shareAccessDuration = 10 * time.Minute

// 公开分享统一隐藏失效原因，避免通过状态码或文案区分记录是否存在。
func (a *App) handleShare(fn endpoint) http.HandlerFunc {
	return a.handle(func(w http.ResponseWriter, r *http.Request) error {
		err := fn(w, r)
		var api *apiError
		if !adminRequest(r) && errors.As(err, &api) && (api.Status == 404 || api.Status == 410) {
			return &apiError{404, "NOT_FOUND", "分享已失效或不存在"}
		}
		return err
	})
}

// 分享解锁凭证与下载票据使用不同用途，且绑定记录、链接和当前版本。
func (a *App) readShareAccess(r *http.Request, purpose, slug, id, version string) Ticket {
	access, err := a.parseTicket(r.Header.Get("X-Share-Access"), purpose, slug)
	if err != nil || access.Subject != id || access.Version != version {
		return Ticket{}
	}
	return access
}

func (a *App) addShareAccess(view map[string]any, access Ticket) {
	view["access_token"] = a.ticket(access)
	view["access_expires_at"] = time.Unix(access.Expires, 0).UTC().Format(time.RFC3339)
}
