package server

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ipLocationSetting = "ip_location_database"
const ipLocationMaxBytes = 512 << 20

// 数据库只由管理员手动上传，不接受下载 URL、客户端路径或任意服务端文件路径。
func (a *App) registerIPLocation(mux *apiMux) {
	mux.HandleFunc("GET /api/admin/ip-location", a.protected(0, true, a.getIPLocation))
	mux.HandleFunc("PUT /api/admin/ip-location", a.protected(0, true, a.loadIPLocation))
	mux.HandleFunc("DELETE /api/admin/ip-location", a.protected(0, true, a.unloadIPLocation))
}

func ipLocationFilename(name string) bool {
	if !strings.HasPrefix(name, "ip-location-") || !strings.HasSuffix(name, ".bin") {
		return false
	}
	number := strings.TrimSuffix(strings.TrimPrefix(name, "ip-location-"), ".bin")
	if number == "" {
		return false
	}
	for _, c := range number {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (a *App) restoreIPLocation() error {
	var name string
	err := a.db.QueryRow("SELECT value FROM settings WHERE key=?", ipLocationSetting).Scan(&name)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if name != "" && ipLocationFilename(name) {
		database, err := openIPLocation(filepath.Join(a.cfg.DataDir, name))
		if err != nil {
			// 可选定位库不可用时只关闭归属地，不阻止站点服务启动。
			a.logger.Warn("已选择的 IP 归属地数据库不可用", "error", err)
		} else {
			database.filename = name
			a.ipLocationDB = database
		}
	}
	// 仅清理本模块生成的随机文件，回收上传中断或进程退出留下的文件。
	entries, err := os.ReadDir(a.cfg.DataDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != name && ipLocationFilename(entry.Name()) {
			_ = os.Remove(filepath.Join(a.cfg.DataDir, entry.Name()))
		}
	}
	return nil
}

func (a *App) ipLocationStatus() map[string]any {
	a.ipLocationMu.RLock()
	defer a.ipLocationMu.RUnlock()
	if a.ipLocationDB == nil {
		return map[string]any{"loaded": false}
	}
	return map[string]any{"loaded": true, "date": a.ipLocationDB.date, "size": a.ipLocationDB.size}
}

func (a *App) getIPLocation(w http.ResponseWriter, r *http.Request) error {
	a.write(w, 200, a.ipLocationStatus(), "")
	return nil
}

func (a *App) loadIPLocation(w http.ResponseWriter, r *http.Request) error {
	// 上传和卸载最多执行一个，不建立等待队列，不影响正在查询的连接。
	if !a.ipLocationEditMu.TryLock() {
		return &apiError{409, "IP_LOCATION_BUSY", "数据库正在更新，请稍后重试"}
	}
	defer a.ipLocationEditMu.Unlock()
	if r.ContentLength > ipLocationMaxBytes {
		return uploadTooLarge()
	}
	control := http.NewResponseController(w)
	if err := control.SetReadDeadline(time.Now().Add(10 * time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	defer func() { _ = control.SetReadDeadline(time.Time{}) }()
	r.Body = http.MaxBytesReader(w, r.Body, ipLocationMaxBytes)
	f, err := os.CreateTemp(a.cfg.DataDir, "ip-location-*.bin")
	if err != nil {
		return err
	}
	path := f.Name()
	keep := false
	defer func() {
		_ = f.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	// 不解析 multipart 或构建整文件缓冲；原始 BIN 请求体直接流式落盘。
	_, err = io.CopyBuffer(f, r.Body, make([]byte, 32<<10))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return uploadTooLarge()
		}
		return badRequest("数据库上传未完成")
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	database, err := openIPLocation(path)
	if err != nil {
		return badRequest("数据库无效，请选择解压后的 IP2Location LITE DB3 IPv4 BIN 文件")
	}
	defer func() {
		if !keep {
			database.db.Close()
		}
	}()
	// 上传耗时较长，发布前再次确认管理员会话仍然有效。
	p, err := a.authenticate(r)
	if err != nil || p == nil || !p.Admin {
		return forbidden()
	}
	database.filename = filepath.Base(path)
	if _, err = a.db.ExecContext(r.Context(), "INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", ipLocationSetting, database.filename); err != nil {
		return err
	}
	keep = true
	a.replaceIPLocation(database)
	a.transfers.refreshLocations()
	setAuditDetails(r, map[string]any{"loaded": true, "database_date": database.date, "database_size": database.size})
	a.write(w, 200, a.ipLocationStatus(), "数据库已加载")
	return nil
}

func (a *App) unloadIPLocation(w http.ResponseWriter, r *http.Request) error {
	if !a.ipLocationEditMu.TryLock() {
		return &apiError{409, "IP_LOCATION_BUSY", "数据库正在更新，请稍后重试"}
	}
	defer a.ipLocationEditMu.Unlock()
	if _, err := a.db.ExecContext(r.Context(), "DELETE FROM settings WHERE key=?", ipLocationSetting); err != nil {
		return err
	}
	a.replaceIPLocation(nil)
	a.transfers.refreshLocations()
	setAuditDetails(r, map[string]any{"loaded": false})
	a.write(w, 200, a.ipLocationStatus(), "数据库已卸载，已停止显示归属地")
	return nil
}

func (a *App) replaceIPLocation(database *ipLocationDatabase) {
	a.ipLocationMu.Lock()
	previous := a.ipLocationDB
	a.ipLocationDB = database
	// 写锁等待已有查询完成后再关闭旧句柄，避免切换时出现文件读取错误。
	if previous != nil {
		previous.db.Close()
	}
	a.ipLocationMu.Unlock()
	if previous != nil && ipLocationFilename(previous.filename) {
		_ = os.Remove(filepath.Join(a.cfg.DataDir, previous.filename))
	}
}

func (h *transferHub) refreshLocations() {
	type snapshot struct {
		room *transferRoom
		ips  [2]string
	}
	h.mu.Lock()
	rooms := make([]snapshot, 0, len(h.rooms))
	for _, room := range h.rooms {
		item := snapshot{room: room}
		for i, member := range room.members {
			if member != nil && member.connected {
				item.ips[i] = member.ip
			}
		}
		rooms = append(rooms, item)
	}
	h.mu.Unlock()
	// 只在管理员加载或卸载时刷新，磁盘查询不持有互传房间锁。
	for _, item := range rooms {
		locations := []*ipLocation{h.app.locateIP(item.ips[0]), h.app.locateIP(item.ips[1])}
		h.mu.Lock()
		room := item.room
		if h.rooms[room.id] == room && room.ctx.Err() == nil {
			for i, member := range room.members {
				if member != nil && member.ip == item.ips[i] {
					member.location = locations[i]
				}
			}
			if room.paired {
				h.broadcastLocked(room, map[string]any{"type": "locations", "locations": []*ipLocation{room.members[0].location, room.members[1].location}})
			} else if room.members[1] != nil && room.members[1].connected {
				h.sendLocked(room.members[0], map[string]any{"type": "locations", "joining_location": room.members[1].location})
			}
		}
		h.mu.Unlock()
	}
}
