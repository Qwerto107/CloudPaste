package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Listing struct {
	Items      []FileInfo `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
	HasMore    bool       `json:"hasMore"`
}
type Stream struct {
	Body   io.ReadCloser
	Status int
	Header http.Header
}
type Driver interface {
	List(context.Context, string, string, int) (Listing, error)
	Stat(context.Context, string) (FileInfo, error)
	Open(context.Context, string, *http.Request) (Stream, error)
	Close() error
}
type cachedDriver struct {
	Driver  Driver
	Updated string
	Used    time.Time
}

// cleanPath 拒绝穿越和歧义分隔符，不先清理再授权。
func cleanPath(value string) (string, error) {
	if len(value) > 4096 || strings.Count(value, "/") > 64 {
		return "", badRequest("路径超过长度或层级限制")
	}
	if value == "" {
		return "/", nil
	}
	if strings.ContainsAny(value, "\\\x00\r\n") {
		return "", badRequest("路径包含非法字符")
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." || isUploadTemporary(part) {
			return "", badRequest("路径不允许包含 . 或 ..")
		}
	}
	if strings.Contains(value, "//") {
		return "", badRequest("路径包含重复分隔符")
	}
	return path.Clean(value), nil
}
func within(base, target string) bool {
	return base == "/" || target == base || strings.HasPrefix(target, base+"/")
}

func classify(info *FileInfo) {
	if info.Directory {
		info.Type = 1
		info.TypeName = "folder"
		info.MimeType = "application/x-directory"
		info.Size = nil
		return
	}
	if info.MimeType == "" {
		info.MimeType = mime.TypeByExtension(strings.ToLower(path.Ext(info.Name)))
	}
	if info.MimeType == "" {
		info.MimeType = "application/octet-stream"
	}
	info.TypeName = "unknown"
	switch {
	case strings.HasPrefix(info.MimeType, "image/"):
		info.Type = 5
		info.TypeName = "image"
	case strings.HasPrefix(info.MimeType, "video/"):
		info.Type = 2
		info.TypeName = "video"
	case strings.HasPrefix(info.MimeType, "audio/"):
		info.Type = 3
		info.TypeName = "audio"
	case info.MimeType == "application/pdf":
		info.Type = 7
		info.TypeName = "document"
	case strings.HasPrefix(info.MimeType, "text/") || slices.Contains([]string{".json", ".yaml", ".yml", ".toml", ".md", ".js", ".go", ".rs", ".xml"}, strings.ToLower(path.Ext(info.Name))):
		info.Type = 4
		info.TypeName = "text"
	case slices.Contains([]string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx"}, strings.ToLower(path.Ext(info.Name))):
		info.Type = 6
		info.TypeName = "office"
	}
}

func (a *App) storageAllowed(p *Principal, s Storage) bool {
	if p == nil {
		return false
	}
	if p.Admin {
		return true
	}
	return s.Public == 1 && (len(p.StorageIDs) == 0 || slices.Contains(p.StorageIDs, s.ID))
}
func (a *App) allMounts(ctx context.Context) ([]Mount, error) {
	raw, total, err := a.resources(ctx, "mounts", "", 1001, 0)
	if err != nil {
		return nil, err
	}
	if total > 1000 {
		return nil, errors.New("挂载数量超过上限")
	}
	mounts := []Mount{}
	for _, b := range raw {
		var m Mount
		if err = json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		mounts = append(mounts, m)
	}
	slices.SortFunc(mounts, func(a, b Mount) int {
		if len(a.Path) != len(b.Path) {
			return len(b.Path) - len(a.Path)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return mounts, nil
}

func (a *App) resolveMount(r *http.Request, value string, permission int, checkPassword bool) (Mount, Storage, Driver, string, error) {
	var m Mount
	var s Storage
	canonical, err := cleanPath(value)
	if err != nil {
		return m, s, nil, "", err
	}
	p := principal(r)
	if !p.Has(permission) {
		return m, s, nil, "", forbidden()
	}
	if !p.Admin && !within(p.BasicPath, canonical) {
		return m, s, nil, "", forbidden()
	}
	if checkPassword {
		if err = a.checkPathPassword(r, canonical); err != nil {
			return m, s, nil, "", err
		}
	}
	mounts, err := a.allMounts(r.Context())
	if err != nil {
		return m, s, nil, "", err
	}
	for _, candidate := range mounts {
		if candidate.Active == 1 && within(candidate.Path, canonical) {
			m = candidate
			break
		}
	}
	if m.ID == "" {
		return m, s, nil, "", notFound()
	}
	if err = a.resource(r.Context(), "storage_configs", "id", m.StorageID, &s); err != nil {
		return m, s, nil, "", err
	}
	if s.Status != "ENABLED" || !a.storageAllowed(p, s) {
		return m, s, nil, "", forbidden()
	}
	key, err := cleanPath(path.Join(m.RootPath, strings.TrimPrefix(canonical, m.Path)))
	if err != nil {
		return m, s, nil, "", err
	}
	driver, err := a.driver(s)
	if local, ok := driver.(*localDriver); ok && err == nil {
		bound := m.RootPath
		if !p.Admin && within(m.Path, p.BasicPath) {
			bound = path.Join(bound, strings.TrimPrefix(p.BasicPath, m.Path))
		}
		bound = strings.TrimPrefix(bound, "/")
		key = strings.TrimPrefix(key, "/")
		if bound != "" {
			if key != bound && !strings.HasPrefix(key, bound+"/") {
				return m, s, nil, "", forbidden()
			}
			key = strings.TrimPrefix(strings.TrimPrefix(key, bound), "/")
		}
		driver = &localDriver{rootPath: local.rootPath, scope: bound}
	}
	return m, s, driver, strings.TrimPrefix(key, "/"), err
}

func (a *App) driver(s Storage) (Driver, error) {
	a.clientsMu.Lock()
	defer a.clientsMu.Unlock()
	if entry, ok := a.storageClients[s.ID]; ok && entry.Updated == s.UpdatedAt {
		entry.Used = time.Now()
		a.storageClients[s.ID] = entry
		return entry.Driver, nil
	}
	// 驱动最多缓存 16 个，不缓存完整目录或文件内容。
	if len(a.storageClients) >= 16 {
		var oldestID string
		var oldest time.Time
		for id, entry := range a.storageClients {
			if oldestID == "" || entry.Used.Before(oldest) {
				oldestID = id
				oldest = entry.Used
			}
		}
		_ = a.storageClients[oldestID].Driver.Close()
		delete(a.storageClients, oldestID)
	}
	if entry, ok := a.storageClients[s.ID]; ok {
		_ = entry.Driver.Close()
		delete(a.storageClients, s.ID)
	}
	var err error
	for _, field := range []*string{&s.AccessKey, &s.SecretKey, &s.Password} {
		*field, err = a.decrypt(*field)
		if err != nil {
			return nil, err
		}
	}
	var d Driver
	switch s.Type {
	case "LOCAL":
		d, err = newLocalDriver(s)
	case "S3":
		d, err = newS3Driver(s)
	default:
		return nil, badRequest("不支持的存储类型")
	}
	if err != nil {
		return nil, err
	}
	a.storageClients[s.ID] = cachedDriver{Driver: d, Updated: s.UpdatedAt, Used: time.Now()}
	return d, nil
}

func (a *App) invalidateDriver(id string) {
	a.clientsMu.Lock()
	defer a.clientsMu.Unlock()
	if entry, ok := a.storageClients[id]; ok {
		_ = entry.Driver.Close()
		delete(a.storageClients, id)
	}
}

func (a *App) registerStorage(mux *apiMux) {
	mux.HandleFunc("GET /api/storage", a.protected(0, false, a.listStorage))
	mux.HandleFunc("POST /api/admin/storage", a.protected(0, true, a.saveStorage))
	mux.HandleFunc("PUT /api/admin/storage/{id}", a.protected(0, true, a.saveStorage))
	mux.HandleFunc("DELETE /api/admin/storage/{id}", a.protected(0, true, a.removeStorage))
	mux.HandleFunc("PUT /api/admin/storage/{id}/set-default", a.protected(0, true, a.defaultStorage))
	mux.HandleFunc("POST /api/admin/storage/{id}/test", a.protected(0, true, a.testStorage))
}

// 普通存储查询需要实际文件能力，纯文本或分享记录管理权限不能读取配置。
func canReadStorage(p *Principal) bool {
	return p != nil && (p.Admin || p.Permissions&(MountView|MountUpload) != 0)
}

func storageView(s Storage, admin bool) map[string]any {
	if !admin {
		// 使用字段白名单，后续新增管理配置不会意外进入普通用户响应。
		return map[string]any{
			"id": s.ID, "name": s.Name, "storage_type": s.Type,
			"is_public": s.Public, "is_default": s.Default, "status": s.Status,
			"readonly": s.ReadOnly, "capabilities": storageCapabilities(s), "upload_protocol": "go-proxy",
		}
	}
	v := toMap(s)
	delete(v, "access_key_id")
	delete(v, "secret_access_key")
	delete(v, "password")
	v["capabilities"] = storageCapabilities(s)
	v["upload_protocol"] = "go-proxy"
	return v
}
func (a *App) listStorage(w http.ResponseWriter, r *http.Request) error {
	actor := principal(r)
	if !canReadStorage(actor) {
		return forbidden()
	}
	limit, offset := paging(r)
	raw, _, err := a.resources(r.Context(), "storage_configs", "", 1001, 0)
	if err != nil {
		return err
	}
	items := []map[string]any{}
	for _, b := range raw {
		var s Storage
		if err = json.Unmarshal(b, &s); err != nil {
			return err
		}
		if a.storageAllowed(actor, s) {
			items = append(items, storageView(s, actor.Admin))
		}
	}
	total := len(items)
	offset = min(offset, total)
	end := min(offset+limit, total)
	if r.URL.Query().Get("limit") == "" {
		offset = 0
		end = total
	}
	a.write(w, 200, map[string]any{"items": items[offset:end], "total": total}, "获取成功")
	return nil
}
func (a *App) saveStorage(w http.ResponseWriter, r *http.Request) error {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	var old, s Storage
	s.Status = "ENABLED"
	s.Region = "us-east-1"
	s.PathStyle = 1
	if r.Method == "PUT" {
		if err := a.resource(r.Context(), "storage_configs", "id", r.PathValue("id"), &old); err != nil {
			return err
		}
		s = old
		s.AccessKey = ""
		s.SecretKey = ""
		s.Password = ""
	}
	if err := decode(w, r, &s); err != nil {
		return err
	}
	if len(s.Name) == 0 || len(s.Name) > 100 {
		return badRequest("存储名称必须为 1-100 字节")
	}
	s.Type = strings.ToUpper(s.Type)
	if s.Status != "ENABLED" && s.Status != "DISABLED" {
		return badRequest("存储状态无效")
	}
	if r.Method == "PUT" && s.Type != old.Type {
		return badRequest("修改存储类型请创建新配置")
	}
	switch s.Type {
	case "LOCAL":
		if !filepath.IsAbs(s.RootPath) {
			return badRequest("root_path 必须为绝对路径")
		}
		root, err := filepath.EvalSymlinks(s.RootPath)
		if err != nil {
			return badRequest("本地存储目录不存在")
		}
		stat, err := os.Stat(root)
		if err != nil || !stat.IsDir() {
			return badRequest("本地存储必须为目录")
		}
		s.RootPath = root
	case "S3":
		if s.Endpoint != "" {
			u, err := url.Parse(s.Endpoint)
			if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return badRequest("存储地址必须为不含凭证、查询或片段的 HTTP/HTTPS 地址")
			}
		}
		if s.Type == "S3" && (s.Bucket == "" || len(s.Bucket) > 255) {
			return badRequest("需要有效的 bucket_name")
		}
	default:
		return badRequest("仅支持 LOCAL 和 S3 存储")
	}
	for _, pair := range []struct {
		Current *string
		Old     string
	}{{&s.AccessKey, old.AccessKey}, {&s.SecretKey, old.SecretKey}, {&s.Password, old.Password}} {
		if *pair.Current == "" || strings.HasPrefix(*pair.Current, "****") {
			*pair.Current = pair.Old
		} else {
			encrypted, err := a.encrypt(*pair.Current)
			if err != nil {
				return err
			}
			*pair.Current = encrypted
		}
	}
	if s.Type == "S3" && (s.AccessKey == "" || s.SecretKey == "") {
		return badRequest("需要 S3 访问密钥")
	}
	if old.ID == "" {
		s.ID = randomID()
		s.CreatedAt = nowText()
	} else {
		s.ID = old.ID
		s.CreatedAt = old.CreatedAt
	}
	s.UpdatedAt = nowText()
	s.Default = old.Default
	var count int
	if old.ID == "" {
		if err := a.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM storage_configs").Scan(&count); err != nil {
			return err
		}
		if count >= 1000 {
			return badRequest("最多配置 1000 个存储")
		}
	}
	if err := a.saveResource(r.Context(), "storage_configs", s.ID, "", principal(r).ID, s); err != nil {
		return err
	}
	a.invalidateDriver(s.ID)
	status := 200
	if old.ID == "" {
		status = 201
	}
	a.write(w, status, storageView(s, true), "存储已保存")
	return nil
}
func (a *App) removeStorage(w http.ResponseWriter, r *http.Request) error {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	// 挂载管理入口已移除；删除存储时一并撤销历史挂载，原文件保持不动。
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM mounts WHERE json_extract(data,'$.storage_config_id')=?`, r.PathValue("id")); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM storage_configs WHERE id=?", r.PathValue("id")); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.invalidateDriver(r.PathValue("id"))
	a.write(w, 200, nil, "存储已删除")
	return nil
}
func (a *App) defaultStorage(w http.ResponseWriter, r *http.Request) error {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	var s Storage
	if err := a.resource(r.Context(), "storage_configs", "id", r.PathValue("id"), &s); err != nil {
		return err
	}
	_, err := a.db.ExecContext(r.Context(), `UPDATE storage_configs SET data=json_set(data,'$.is_default',CASE WHEN id=? THEN 1 ELSE 0 END)`, s.ID)
	if err != nil {
		return err
	}
	a.write(w, 200, nil, "默认存储已更新")
	return nil
}
func (a *App) testStorage(w http.ResponseWriter, r *http.Request) error {
	var s Storage
	if err := a.resource(r.Context(), "storage_configs", "id", r.PathValue("id"), &s); err != nil {
		return err
	}
	d, err := a.driver(s)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if _, err = d.List(ctx, "", "", 1); err != nil {
		return err
	}
	a.write(w, 200, map[string]any{"success": true, "storageType": s.Type, "message": "读取测试成功"}, "连接成功")
	return nil
}
