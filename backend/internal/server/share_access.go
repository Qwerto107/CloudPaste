package server

import (
	"net/http"
	"time"
)

const shareAccessDuration = 10 * time.Minute

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
