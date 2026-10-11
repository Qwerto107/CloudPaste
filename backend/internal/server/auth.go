package server

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (a *App) bootstrapAdmin() error {
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if len(a.cfg.AdminPassword) < 12 {
		return errors.New("首次启动请设置至少 12 字节的 ADMIN_PASSWORD")
	}
	hash, err := passwordHash(a.cfg.AdminPassword)
	if err != nil {
		return err
	}
	_, err = a.db.Exec("INSERT INTO admins(id,username,password) VALUES(?,?,?)", randomID(), a.cfg.AdminUsername, hash)
	return err
}

func (a *App) checkPassword(hash, password string) error {
	select {
	case a.passwordChecks <- struct{}{}:
		defer func() { <-a.passwordChecks }()
	default:
		return &apiError{429, "RATE_LIMITED", "密码验证繁忙，请稍后重试"}
	}
	if !passwordMatches(hash, password) {
		return &apiError{403, "INVALID_PASSWORD", "密码错误"}
	}
	return nil
}

func (a *App) allowPasswordAttempt(r *http.Request) bool {
	// 只有明确受信的代理可以提供来源地址，直连客户端不能用转发头绕过限流。
	ip := a.clientIP(r)
	a.passwordAttemptMu.Lock()
	defer a.passwordAttemptMu.Unlock()
	// 使用后端已判定的入口范围，不接受客户端指定管理限流类别。
	budget := &a.guestPasswordAttempts
	if adminRequest(r) {
		budget = &a.adminPasswordAttempts
	}
	if budget.byIP == nil {
		budget.byIP = make(map[string]attemptWindow)
	}
	now := time.Now()
	for key, entry := range budget.byIP {
		if now.Sub(entry.Started) > time.Minute {
			delete(budget.byIP, key)
		}
	}
	if now.Sub(budget.total.Started) > time.Minute {
		budget.total = attemptWindow{Started: now}
	}
	entry, exists := budget.byIP[ip]
	if !exists {
		if len(budget.byIP) >= 2048 {
			return false
		}
		entry = attemptWindow{Started: now}
	}
	if entry.Count >= 10 || budget.total.Count >= 60 {
		return false
	}
	entry.Count++
	budget.total.Count++
	budget.byIP[ip] = entry
	return true
}

func (a *App) authenticate(r *http.Request) (*Principal, error) {
	header := r.Header.Get("Authorization")
	if header == "" && r.Header.Get("X-Custom-Auth-Key") != "" {
		header = "ApiKey " + r.Header.Get("X-Custom-Auth-Key")
	}
	if header == "" {
		return nil, nil
	}
	parts := strings.Fields(header)
	if len(parts) != 2 {
		return nil, &apiError{401, "UNAUTHORIZED", "认证信息无效"}
	}
	scheme, value := strings.ToLower(parts[0]), parts[1]

	if scheme == "bearer" {
		var id, name string
		err := a.db.QueryRowContext(r.Context(), "SELECT a.id,a.username FROM sessions s JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=? AND s.expires_at>?", secretHash(value), time.Now().Unix()).Scan(&id, &name)
		if err == nil {
			return &Principal{ID: id, Name: name, Admin: true, BasicPath: "/", CredentialHash: secretHash(value)}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if scheme == "apikey" {
		return a.apiKeyPrincipal(r, value)
	}
	if scheme == "apikeybase64" {
		// 编码传输保留中文、空格和控制字符，避免请求头改变原始密钥。
		key, err := base64.StdEncoding.Strict().DecodeString(value)
		if err != nil || len(key) == 0 {
			return nil, &apiError{401, "UNAUTHORIZED", "认证信息无效"}
		}
		return a.apiKeyPrincipal(r, string(key))
	}
	return nil, &apiError{401, "UNAUTHORIZED", "认证信息无效或已过期"}
}

func (a *App) apiKeyPrincipal(r *http.Request, key string) (*Principal, error) {
	var raw, credential string
	err := a.db.QueryRowContext(r.Context(), "SELECT data,key_hash FROM api_keys WHERE key_hash=?", secretHash(key)).Scan(&raw, &credential)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &apiError{401, "UNAUTHORIZED", "API Key 无效"}
	}
	if err != nil {
		return nil, err
	}
	var k APIKey
	if err = json.Unmarshal([]byte(raw), &k); err != nil {
		return nil, err
	}
	if expired(k.ExpiresAt) || k.Enabled != 1 {
		return nil, &apiError{401, "UNAUTHORIZED", "API Key 已停用或过期"}
	}
	return keyPrincipal(k, credential), nil
}

func keyPrincipal(k APIKey, credential string) *Principal {
	return &Principal{ID: k.ID, Name: k.Name, Role: k.Role, Permissions: k.Permissions, BasicPath: k.BasicPath, StorageIDs: k.StorageIDs, CredentialHash: credential}
}
func withPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// 文件票据只证明曾通过验证；每次下载仍检查当前凭证、权限和目录密码版本。
func (a *App) ticketPrincipal(r *http.Request, token, purpose, target string) (*Principal, error) {
	t, err := a.parseTicket(token, purpose, target)
	if err != nil {
		return nil, err
	}
	var actor *Principal
	if t.Version == t.Subject {
		var id, name string
		err = a.db.QueryRowContext(r.Context(), "SELECT a.id,a.username FROM sessions s JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=? AND s.admin_id=? AND s.expires_at>?", t.Credential, t.Subject, time.Now().Unix()).Scan(&id, &name)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, forbidden()
			}
			return nil, err
		}
		actor = &Principal{ID: id, Name: name, Admin: true, BasicPath: "/", CredentialHash: t.Credential}
	} else if t.Version == "apikey:"+t.Subject {
		var raw, credential string
		err = a.db.QueryRowContext(r.Context(), "SELECT data,key_hash FROM api_keys WHERE id=?", t.Subject).Scan(&raw, &credential)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, forbidden()
			}
			return nil, err
		}
		var k APIKey
		if err = json.Unmarshal([]byte(raw), &k); err != nil {
			return nil, err
		}
		if expired(k.ExpiresAt) || k.Enabled != 1 || credential != t.Credential {
			return nil, forbidden()
		}
		actor = keyPrincipal(k, credential)
	} else {
		return nil, forbidden()
	}
	// 票据的原身份必须与当前入口一致，不能把管理员票据换到普通路径使用。
	if actor.Admin != adminRequest(r) {
		return nil, forbidden()
	}
	// 上传票据的目录与来源版本由上传会话统一验证，包含目标及父目录密码。
	if purpose == "upload-part" {
		return actor, nil
	}
	meta, err := a.effectiveMeta(r.Context(), target)
	if err != nil {
		return nil, err
	}
	if t.MetaVersion != secretHash(meta.Password) {
		return nil, forbidden()
	}
	if purpose == "file" {
		ownerRequest := r.WithContext(withPrincipal(r.Context(), actor))
		m, s, _, _, err := a.resolveMount(ownerRequest, target, MountView, false)
		if err != nil {
			return nil, err
		}
		if t.SourceVersion != sourceVersion(m, s) {
			return nil, forbidden()
		}
	}
	return actor, nil
}

func permissionMap(p *Principal) map[string]bool {
	return map[string]bool{"text_share": p.Has(TextShare), "text_manage": p.Has(TextManage), "file_share": p.Has(FileShare), "file_manage": p.Has(FileManage), "mount_view": p.Has(MountView), "mount_upload": p.Has(MountUpload), "mount_copy": false, "mount_rename": false, "mount_delete": false, "webdav_read": false, "webdav_manage": false}
}

func (a *App) registerAuth(mux *apiMux) {
	mux.HandleFunc("POST /api/admin/login", a.handle(a.login))
	mux.HandleFunc("POST /api/admin/logout", a.handle(func(w http.ResponseWriter, r *http.Request) error {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) == 2 {
			_, err := a.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", secretHash(parts[1]))
			if err != nil {
				return err
			}
		}
		a.write(w, 200, nil, "登出成功")
		return nil
	}))
	mux.HandleFunc("POST /api/admin/change-password", a.protected(0, true, a.changePassword))
	mux.HandleFunc("GET /api/admin/test/admin-token", a.protected(0, true, func(w http.ResponseWriter, r *http.Request) error { a.write(w, 200, nil, "令牌有效"); return nil }))
	mux.HandleFunc("GET /api/test/api-key", a.protected(0, false, func(w http.ResponseWriter, r *http.Request) error {
		p := principal(r)
		a.write(w, 200, map[string]any{"name": p.Name, "basic_path": p.BasicPath, "permissions": permissionMap(p), "is_admin": p.Admin, "key_info": map[string]any{"id": p.ID, "name": p.Name, "basic_path": p.BasicPath, "role": p.Role}}, "认证成功")
		return nil
	}))
	mux.HandleFunc("GET /api/public/guest-config", a.handle(func(w http.ResponseWriter, r *http.Request) error {
		a.write(w, 200, map[string]any{"enabled": false, "key": nil}, "游客模式未启用")
		return nil
	}))
	mux.HandleFunc("GET /api/admin/api-keys", a.protected(0, true, a.listKeys))
	mux.HandleFunc("GET /api/admin/api-keys/{id}/reveal", a.protected(0, true, a.revealKey))
	mux.HandleFunc("POST /api/admin/api-keys", a.protected(0, true, a.createKey))
	mux.HandleFunc("PUT /api/admin/api-keys/{id}", a.protected(0, true, a.updateKey))
	mux.HandleFunc("DELETE /api/admin/api-keys/{id}", a.protected(0, true, func(w http.ResponseWriter, r *http.Request) error {
		_, err := a.db.ExecContext(r.Context(), "DELETE FROM api_keys WHERE id=?", r.PathValue("id"))
		if err != nil {
			return err
		}
		a.write(w, 200, nil, "密钥已删除")
		return nil
	}))
	mux.HandleFunc("GET /api/admin/api-keys/{id}/storage-acl", a.protected(0, true, a.keyACL))
	mux.HandleFunc("PUT /api/admin/api-keys/{id}/storage-acl", a.protected(0, true, a.keyACL))
}

func (a *App) login(w http.ResponseWriter, r *http.Request) error {
	if !a.allowPasswordAttempt(r) {
		return &apiError{429, "RATE_LIMITED", "登录尝试过多，请稍后重试"}
	}
	var body struct{ Username, Password string }
	if err := decode(w, r, &body); err != nil {
		return err
	}
	var id, hash string
	err := a.db.QueryRowContext(r.Context(), "SELECT id,password FROM admins WHERE username=?", body.Username).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return &apiError{401, "UNAUTHORIZED", "用户名或密码错误"}
	}
	if err != nil {
		return err
	}
	if err = a.checkPassword(hash, body.Password); err != nil {
		return &apiError{401, "UNAUTHORIZED", "用户名或密码错误"}
	}
	token := randomID()
	expires := time.Now().Add(7 * 24 * time.Hour)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE admin_id=? AND token_hash NOT IN (SELECT token_hash FROM sessions WHERE admin_id=? ORDER BY created_at DESC LIMIT 4)", id, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO sessions VALUES(?,?,?,?)", secretHash(token), id, expires.Unix(), time.Now().UnixNano()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.write(w, 200, map[string]any{"username": body.Username, "token": token, "expiresAt": expires.UTC().Format(time.RFC3339)}, "登录成功")
	return nil
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) error {
	var body struct{ CurrentPassword, NewPassword, NewUsername string }
	if err := decode(w, r, &body); err != nil {
		return err
	}
	var hash string
	if err := a.db.QueryRowContext(r.Context(), "SELECT password FROM admins WHERE id=?", principal(r).ID).Scan(&hash); err != nil {
		return err
	}
	if !a.allowPasswordAttempt(r) {
		return &apiError{429, "RATE_LIMITED", "密码验证尝试过多"}
	}
	if err := a.checkPassword(hash, body.CurrentPassword); err != nil {
		return err
	}
	if body.NewPassword == "" && body.NewUsername == "" {
		return badRequest("需要提供新密码或用户名")
	}
	if body.NewPassword != "" {
		if len(body.NewPassword) < 12 {
			return badRequest("管理员密码至少 12 字节")
		}
		var err error
		hash, err = passwordHash(body.NewPassword)
		if err != nil {
			return err
		}
	}
	name := body.NewUsername
	if name == "" {
		name = principal(r).Name
	}
	if len(name) > 100 {
		return badRequest("用户名过长")
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "UPDATE admins SET username=?,password=? WHERE id=?", name, hash, principal(r).ID); err != nil {
		return badRequest("用户名不可用")
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE admin_id=?", principal(r).ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.write(w, 200, nil, "信息更新成功，请重新登录")
	return nil
}

func (a *App) keyByID(r *http.Request) (APIKey, error) {
	var k APIKey
	var raw string
	err := a.db.QueryRowContext(r.Context(), "SELECT data,key_hash,key_encrypted FROM api_keys WHERE id=?", r.PathValue("id")).Scan(&raw, &k.KeyHash, &k.EncryptedKey)
	if errors.Is(err, sql.ErrNoRows) {
		return k, notFound()
	}
	if err != nil {
		return k, err
	}
	err = json.Unmarshal([]byte(raw), &k)
	return k, err
}
func (a *App) saveKey(r *http.Request, k APIKey) error {
	raw, err := json.Marshal(k)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(r.Context(), "INSERT INTO api_keys VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", k.ID, k.KeyHash, k.EncryptedKey, string(raw))
	return err
}
func validateKey(k *APIKey) error {
	if strings.TrimSpace(k.Name) == "" || len(k.Name) > 100 {
		return badRequest("密钥名称必须为 1-100 字节")
	}
	p, err := cleanPath(k.BasicPath)
	if err != nil {
		return err
	}
	k.BasicPath = p
	if k.Role == "" {
		k.Role = "GENERAL"
	}
	if k.Role != "GENERAL" && k.Role != "GUEST" {
		return badRequest("不支持的角色")
	}
	if k.Permissions < 0 || k.Permissions&^(TextShare|FileShare|TextManage|FileManage|MountView|MountUpload|WebDAVRead) != 0 {
		return badRequest("只支持分享管理、挂载兼容及上传权限位")
	}
	return validateExpiry(k.ExpiresAt, 0)
}
func (a *App) listKeys(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.db.QueryContext(r.Context(), "SELECT data FROM api_keys ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []APIKey{}
	for rows.Next() {
		var raw string
		var k APIKey
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(raw), &k); err != nil {
			return err
		}
		// 固定掩码不会泄露短密钥，也不按字节截断中文字符。
		k.Key = "********"
		items = append(items, k)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	a.write(w, 200, items, "获取成功")
	return nil
}
func (a *App) revealKey(w http.ResponseWriter, r *http.Request) error {
	k, err := a.keyByID(r)
	if err != nil {
		return err
	}
	key, err := a.decrypt(k.EncryptedKey)
	if err != nil {
		return err
	}
	a.write(w, 200, map[string]string{"key": key}, "获取成功")
	return nil
}
func (a *App) createKey(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		APIKey
		CustomKey string `json:"custom_key"`
	}
	body.Enabled = 1
	body.BasicPath = "/"
	expires := time.Now().AddDate(1, 0, 0).UTC().Format(time.RFC3339)
	body.ExpiresAt = &expires
	if err := decode(w, r, &body); err != nil {
		return err
	}
	k := body.APIKey
	k.ID = randomID()
	k.CreatedAt = nowText()
	k.Key = ""
	if err := validateKey(&k); err != nil {
		return err
	}
	key := body.CustomKey
	if key == "" {
		key = randomID()
	}
	k.KeyHash = secretHash(key)
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	var count int
	if err := a.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM api_keys").Scan(&count); err != nil {
		return err
	}
	if count >= 1000 {
		return badRequest("最多配置 1000 个 API Key")
	}
	var err error
	k.EncryptedKey, err = a.encrypt(key)
	if err != nil {
		return err
	}
	if err = a.saveKey(r, k); err != nil {
		return badRequest("密钥不可用")
	}
	k.Key = key
	a.write(w, 201, k, "密钥创建成功，请保存完整密钥")
	return nil
}
func (a *App) updateKey(w http.ResponseWriter, r *http.Request) error {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	k, err := a.keyByID(r)
	if err != nil {
		return err
	}
	id := k.ID
	if err = decode(w, r, &k); err != nil {
		return err
	}
	k.ID = id
	k.Key = ""
	if err = validateKey(&k); err != nil {
		return err
	}
	if err = a.saveKey(r, k); err != nil {
		return err
	}
	a.write(w, 200, k, "密钥已更新")
	return nil
}
func (a *App) keyACL(w http.ResponseWriter, r *http.Request) error {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	k, err := a.keyByID(r)
	if err != nil {
		return err
	}
	if r.Method == "PUT" {
		var body struct {
			IDs []string `json:"storage_config_ids"`
		}
		if err = decode(w, r, &body); err != nil {
			return err
		}
		if len(body.IDs) > 100 {
			return badRequest("ACL 最多包含 100 个存储")
		}
		for _, id := range body.IDs {
			var s Storage
			if err = a.resource(r.Context(), "storage_configs", "id", id, &s); err != nil {
				return err
			}
		}
		k.StorageIDs = body.IDs
		if err = a.saveKey(r, k); err != nil {
			return err
		}
	}
	if k.StorageIDs == nil {
		k.StorageIDs = []string{}
	}
	a.write(w, 200, map[string]any{"storage_config_ids": k.StorageIDs}, "获取成功")
	return nil
}
