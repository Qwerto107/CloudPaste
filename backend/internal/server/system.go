package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type setting struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Type        string `json:"type"`
	Group       int    `json:"group_id"`
	Flags       int    `json:"flags"`
	Description string `json:"description"`
}

// 公开设置采用白名单；凭证、管理员密码和运行环境变量不会进入返回值。
var defaultSettings = []setting{
	{"proxy_sign_all", "true", "bool", 1, 2, "Go 后端始终验证下载票据"},
	{"proxy_sign_expires", "600", "number", 1, 2, "公开分享下载票据有效期（秒）"},
	{"max_upload_size", "100", "number", 1, 0, "上传上限（MiB，不超过环境配置）"},
	{"share_default_expiry_hours", "24", "number", 1, 0, "新分享默认有效期（小时，0 表示不过期）"},
	{"preview_max_text_kb", "512", "number", 2, 0, "文本文件预览上限（KiB，16–2048）"},
	{"preview_text_types", "txt,md,markdown,json,xml,csv,log,yaml,yml,html,css,js,go,rs,py,toml", "text", 2, 0, "文本预览扩展名（逗号分隔）"},
	{"preview_image_types", "jpg,jpeg,png,gif,webp,avif,bmp,svg", "text", 2, 0, "图片预览扩展名"},
	{"preview_video_types", "mp4,webm,ogv,mov", "text", 2, 0, "视频预览扩展名（浏览器原生播放）"},
	{"preview_audio_types", "mp3,wav,ogg,flac,m4a,aac,opus", "text", 2, 0, "音频预览扩展名（浏览器原生播放）"},
	{"preview_allow_external_images", "false", "bool", 2, 0, "允许 Markdown 加载站外图片（默认关闭）"},
	{"site_title", "CloudPaste", "text", 4, 0, "站点标题"},
	{"site_favicon_url", "", "text", 4, 0, "站点图标 URL"},
	{"site_footer_markdown", "", "textarea", 4, 0, "站点页脚"},
	{"site_custom_head", "", "textarea", 4, 0, "页面顶部 HTML（过滤脚本与事件属性）"},
	{"site_custom_body", "", "textarea", 4, 0, "页面底部 HTML（过滤脚本与事件属性）"},
	{"site_home_editor_enabled", "true", "bool", 4, 0, "显示文本编辑入口"},
	{"site_upload_page_enabled", "true", "bool", 4, 0, "显示文件上传入口"},
	{"site_announcement_enabled", "false", "bool", 4, 0, "启用公告"},
	{"site_announcement_content", "", "textarea", 4, 0, "公告内容"},
}

// 环境变量给出硬上限，站点设置只能收紧；并发上传读取原子快照。
func (a *App) maxUploadBytes() int64 {
	if value := a.uploadLimit.Load(); value > 0 {
		return min(value, a.cfg.MaxUploadBytes)
	}
	return a.cfg.MaxUploadBytes
}

func (a *App) defaultShareExpiry(r *http.Request) (*string, error) {
	hours := int64(24)
	var value string
	err := a.db.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE key=?", "share_default_expiry_hours").Scan(&value)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		parsed, e := strconv.ParseInt(value, 10, 64)
		if e == nil && parsed >= 0 && parsed <= 876000 {
			hours = parsed
		}
	}
	if hours == 0 {
		return nil, nil
	}
	expiry := time.Now().Add(time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339)
	return &expiry, nil
}

func (a *App) registerSystem(mux *apiMux) {
	mux.HandleFunc("GET /api/health", a.handle(func(w http.ResponseWriter, r *http.Request) error {
		if err := a.db.PingContext(r.Context()); err != nil {
			return err
		}
		a.write(w, 200, map[string]any{"status": "ok", "uploads": true, "readOnlyStorage": false}, "服务正常")
		return nil
	}))
	mux.HandleFunc("GET /api/version", a.handle(func(w http.ResponseWriter, r *http.Request) error {
		a.write(w, 200, map[string]any{"version": "0.5.1", "name": "cloudpaste", "environment": "Docker / standalone", "storage": "SQLite", "goVersion": runtime.Version(), "uptime": int(time.Since(a.started).Seconds()), "features": map[string]bool{"upload": true, "webdav": false, "readOnlyStorage": false}}, "获取版本成功")
		return nil
	}))
	mux.HandleFunc("GET /api/system/max-upload-size", a.handle(func(w http.ResponseWriter, r *http.Request) error {
		a.write(w, 200, map[string]any{"max_upload_size": a.maxUploadBytes() / (1 << 20), "upload_enabled": true}, "获取上传限制成功")
		return nil
	}))
	mux.HandleFunc("GET /api/public/settings", a.handle(a.settings))
	mux.HandleFunc("GET /api/admin/settings", a.protected(0, true, a.settings))
	mux.HandleFunc("GET /api/admin/settings/groups", a.protected(0, true, func(w http.ResponseWriter, r *http.Request) error {
		groups := []map[string]any{}
		for id, name := range map[int]string{1: "全局设置", 2: "预览设置", 4: "站点设置"} {
			count, readOnly := 0, 0
			for _, s := range defaultSettings {
				if s.Group == id {
					count++
					if s.Flags == 2 {
						readOnly++
					}
				}
			}
			groups = append(groups, map[string]any{"id": id, "name": name, "settingCount": count, "readonlyCount": readOnly})
		}
		a.write(w, 200, groups, "获取分组成功")
		return nil
	}))
	mux.HandleFunc("GET /api/admin/settings/metadata", a.protected(0, true, func(w http.ResponseWriter, r *http.Request) error {
		items, err := a.loadSettings(r)
		if err != nil {
			return err
		}
		for _, s := range items {
			if s.Key == r.URL.Query().Get("key") {
				a.write(w, 200, s, "获取成功")
				return nil
			}
		}
		return notFound()
	}))
	mux.HandleFunc("PUT /api/admin/settings/group/{group}", a.protected(0, true, a.updateSettings))
	mux.HandleFunc("GET /api/storage-types/{type}/capabilities", a.protected(MountView, false, func(w http.ResponseWriter, r *http.Request) error {
		for _, m := range storageMetadata() {
			if m["type"] == r.PathValue("type") {
				m["storageType"] = m["type"]
				a.write(w, 200, m, "获取成功")
				return nil
			}
		}
		return notFound()
	}))
	mux.HandleFunc("POST /api/admin/cache/clear", a.protected(0, true, func(w http.ResponseWriter, r *http.Request) error {
		a.clientsMu.Lock()
		for id, entry := range a.storageClients {
			_ = entry.Driver.Close()
			delete(a.storageClients, id)
		}
		a.clientsMu.Unlock()
		a.write(w, 200, map[string]int{"cleared": 0}, "未缓存文件内容；连接缓存已清理")
		return nil
	}))
}
func (a *App) loadSettings(r *http.Request) ([]setting, error) {
	items := append([]setting(nil), defaultSettings...)
	rows, err := a.db.QueryContext(r.Context(), "SELECT key,value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		if v, ok := values[items[i].Key]; ok && items[i].Flags != 2 {
			items[i].Value = v
		}
		if items[i].Key == "max_upload_size" {
			items[i].Value = strconv.FormatInt(a.maxUploadBytes()/(1<<20), 10)
		}

	}
	return items, nil
}
func (a *App) settings(w http.ResponseWriter, r *http.Request) error {
	items, err := a.loadSettings(r)
	if err != nil {
		return err
	}
	group := r.URL.Query().Get("group")
	if group != "" {
		id, err := strconv.Atoi(group)
		if err != nil || (id != 1 && id != 2 && id != 4) {
			return badRequest("分组编号无效")
		}
		filtered := []setting{}
		for _, s := range items {
			if s.Group == id {
				filtered = append(filtered, s)
			}
		}
		a.write(w, 200, filtered, "获取设置成功")
		return nil
	}
	groups := map[int][]setting{2: {}}
	for _, s := range items {
		groups[s.Group] = append(groups[s.Group], s)
	}
	a.write(w, 200, groups, "获取设置成功")
	return nil
}
func (a *App) updateSettings(w http.ResponseWriter, r *http.Request) error {
	group, err := strconv.Atoi(r.PathValue("group"))
	if err != nil || (group != 1 && group != 2 && group != 4) {
		return badRequest("分组编号无效")
	}
	var body map[string]json.RawMessage
	if err = decode(w, r, &body); err != nil {
		return err
	}
	values := map[string]string{}
	for key, raw := range body {
		var definition *setting
		for i := range defaultSettings {
			if defaultSettings[i].Key == key {
				definition = &defaultSettings[i]
				break
			}
		}
		if definition == nil || definition.Group != group || definition.Flags == 2 {
			return badRequest("包含未知、只读或不属于此分组的设置")
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			if definition.Type == "bool" {
				var flag bool
				if json.Unmarshal(raw, &flag) != nil {
					return badRequest("布尔值无效")
				}
				value = strconv.FormatBool(flag)
			} else if definition.Type == "number" {
				// 浏览器数字输入会提交 JSON 数字；同时兼容旧客户端的字符串值。
				var number json.Number
				if json.Unmarshal(raw, &number) != nil {
					return badRequest("数值无效")
				}
				value = number.String()
			} else {
				return badRequest("设置值必须为字符串")
			}
		}
		if len(value) > 65536 {
			return badRequest("设置值过长")
		}
		if definition.Type == "bool" && value != "true" && value != "false" {
			return badRequest("布尔值必须为 true 或 false")
		}
		if definition.Type == "number" {
			number, err := strconv.ParseInt(value, 10, 64)
			minimum, maximum := int64(0), int64(876000)
			if key == "max_upload_size" {
				minimum, maximum = 1, a.cfg.MaxUploadBytes/(1<<20)
			}
			if key == "preview_max_text_kb" {
				minimum, maximum = 16, 2048
			}
			if err != nil || number < minimum || number > maximum {
				return badRequest("数值超出允许范围")
			}
		}
		if strings.HasPrefix(key, "preview_") && strings.HasSuffix(key, "_types") {
			if len(value) > 2048 {
				return badRequest("扩展名列表过长")
			}
			for _, extension := range strings.Split(value, ",") {
				for _, char := range strings.TrimSpace(extension) {
					if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
						return badRequest("扩展名只允许字母和数字")
					}
				}
			}
		}
		values[key] = value
	}
	// 请求解码与响应写入不占用全局写锁；只序列化提交及运行时限制更新。
	err = func() error {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for key, value := range values {
			if _, err = tx.ExecContext(r.Context(), "INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value); err != nil {
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		if value, ok := values["max_upload_size"]; ok {
			limit, _ := strconv.ParseInt(value, 10, 64)
			a.uploadLimit.Store(limit << 20)
		}
		return nil
	}()
	if err != nil {
		return err
	}
	a.write(w, 200, map[string]any{"groupId": group, "updatedCount": len(values), "totalCount": len(values), "errors": []string{}, "success": true}, "设置已更新")
	return nil
}
