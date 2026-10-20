package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// 上传先落入私有临时区，验证完成后才发布；整个过程只使用固定大小的读写缓冲。
type uploadDestination struct {
	Path       string `json:"path"`
	StorageID  string `json:"storage_id"`
	StorageKey string `json:"storage_key"`
	Version    string `json:"version"`
	Proof      string `json:"proof"`
}

func uploadTooLarge() error {
	return &apiError{413, "UPLOAD_TOO_LARGE", "文件超过服务器上传大小限制"}
}
func uploadQuota() error {
	return &apiError{507, "UPLOAD_QUOTA", "上传临时区空间额度已用完，请完成或取消已有上传"}
}
func validateUploadName(name string) error {
	if name == "" || name == "." || name == ".." || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") || isUploadTemporary(name) {
		return badRequest("文件名无效")
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return badRequest("文件名包含控制或格式字符")
		}
	}
	// 同一文件名在 Docker 与 Windows 开发环境中具有一致的安全含义。
	if strings.Contains(name, ":") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return badRequest("文件名包含不支持的字符")
	}
	return nil
}
func (a *App) uploadProof(ctx context.Context, target string) (string, error) {
	if target == "" {
		return secretHash("unmounted"), nil
	}
	child, err := a.effectiveMeta(ctx, target)
	if err != nil {
		return "", err
	}
	parent, err := a.effectiveMeta(ctx, path.Dir(target))
	if err != nil {
		return "", err
	}
	return secretHash(child.Password + ":" + parent.Password), nil
}

func (a *App) mountedDestination(r *http.Request, target string, checkPassword bool) (uploadDestination, Driver, string, error) {
	m, s, d, key, err := a.resolveMount(r, target, MountUpload, checkPassword)
	if err != nil {
		return uploadDestination{}, nil, "", err
	}
	if s.ReadOnly == 1 {
		return uploadDestination{}, nil, "", forbidden()
	}
	if checkPassword {
		if err = a.checkPathPassword(r, path.Dir(target)); err != nil {
			return uploadDestination{}, nil, "", err
		}
	}
	proof, err := a.uploadProof(r.Context(), target)
	logical := strings.TrimPrefix(path.Join(m.RootPath, strings.TrimPrefix(target, m.Path)), "/")
	return uploadDestination{Path: target, StorageID: s.ID, StorageKey: logical, Version: sourceVersion(m, s), Proof: proof}, d, key, err
}

func (a *App) shareDestination(r *http.Request, id, dir, name string) (uploadDestination, Driver, string, error) {
	var s Storage
	if err := a.resource(r.Context(), "storage_configs", "id", id, &s); err != nil {
		return uploadDestination{}, nil, "", err
	}
	if s.Status != "ENABLED" || s.ReadOnly == 1 || !a.storageAllowed(principal(r), s) {
		return uploadDestination{}, nil, "", forbidden()
	}
	// 访客只能写入专属目录，不能指定挂载路径或覆盖已有用户的文件。
	if p := principal(r); p.Guest {
		if dir != "" && dir != "/" {
			return uploadDestination{}, nil, "", forbidden()
		}
		d, err := a.driver(s)
		key := path.Join("guest", p.ID, name)
		return uploadDestination{StorageID: id, StorageKey: key, Version: sourceVersion(Mount{}, s), Proof: secretHash("unmounted")}, d, key, err
	}
	canonical, err := cleanPath(dir)
	if err != nil {
		return uploadDestination{}, nil, "", err
	}
	mounts, err := a.allMounts(r.Context())
	if err != nil {
		return uploadDestination{}, nil, "", err
	}
	// 优先将明确的虚拟路径解析到挂载，所有权限检查沿用文件浏览的边界。
	for _, m := range mounts {
		if m.Active == 1 && within(m.Path, canonical) {
			dest, d, key, err := a.mountedDestination(r, path.Join(canonical, name), true)
			if err == nil && dest.StorageID != id {
				err = forbidden()
			}
			return dest, d, key, err
		}
	}
	logical := path.Join(canonical, name)
	for _, m := range mounts {
		if m.Active == 1 && m.StorageID == id && within(m.RootPath, canonical) {
			target := path.Join(m.Path, strings.TrimPrefix(logical, m.RootPath))
			return a.mountedDestination(r, target, true)
		}
	}
	// 未挂载存储只允许根作用域账号写入，禁止借助上传分享绕过 basic_path。
	p := principal(r)
	if !p.Admin && p.BasicPath != "/" {
		return uploadDestination{}, nil, "", forbidden()
	}
	d, err := a.driver(s)
	proof, _ := a.uploadProof(r.Context(), "")
	key := strings.TrimPrefix(logical, "/")
	return uploadDestination{StorageID: id, StorageKey: key, Version: sourceVersion(Mount{}, s), Proof: proof}, d, key, err
}

func (a *App) recheckDestination(r *http.Request, dest uploadDestination, permission int) (Driver, string, error) {
	if !principal(r).Has(permission) {
		return nil, "", forbidden()
	}
	var current uploadDestination
	var d Driver
	var key string
	var err error
	if dest.Path != "" {
		// 分享读取不要求上传权限；仍检查当前目录与存储访问权限。
		m, s, driver, k, e := a.resolveMount(r, dest.Path, permission, false)
		err = e
		d = driver
		key = k
		if err == nil {
			proof, e := a.uploadProof(r.Context(), dest.Path)
			err = e
			current = uploadDestination{StorageID: s.ID, StorageKey: strings.TrimPrefix(path.Join(m.RootPath, strings.TrimPrefix(dest.Path, m.Path)), "/"), Version: sourceVersion(m, s), Proof: proof}
			if permission == MountUpload && s.ReadOnly == 1 {
				err = forbidden()
			}
		}
	} else {
		p := principal(r)
		if !p.Admin && p.BasicPath != "/" {
			return nil, "", forbidden()
		}
		var s Storage
		err = a.resource(r.Context(), "storage_configs", "id", dest.StorageID, &s)
		if err == nil {
			if s.Status != "ENABLED" || !a.storageAllowed(p, s) || (permission == MountUpload && s.ReadOnly == 1) {
				return nil, "", forbidden()
			}
			d, err = a.driver(s)
			key = dest.StorageKey
			current = uploadDestination{StorageID: s.ID, StorageKey: key, Version: sourceVersion(Mount{}, s), Proof: secretHash("unmounted")}
		}
	}
	if err != nil {
		return nil, "", err
	}
	if current.StorageID != dest.StorageID || current.StorageKey != dest.StorageKey || current.Version != dest.Version || current.Proof != dest.Proof {
		return nil, "", &apiError{410, "UPLOAD_CHANGED", "存储配置或目录保护已变化，请重新上传"}
	}
	return d, key, nil
}
func (a *App) uploadedShareDriver(r *http.Request, s Share) (Driver, string, error) {
	dest := uploadDestination{Path: s.Path, StorageID: s.StorageID, StorageKey: s.StorageKey, Version: s.SourceVersion, Proof: s.MetaVersion}
	// 这里只复核上传分享的来源与当前权限，文件查询由锁外的 shareSource 完成。
	return a.recheckDestination(r, dest, FileShare)
}

// 本地临时文件记录原始存储位置，重启后无需遍历整个存储即可回收；旧分片仅按到期清理。
type pendingUpload struct {
	ID         string
	Expires    int64
	LocalRoot  string `json:"local_root,omitempty"`
	LocalScope string `json:"local_scope,omitempty"`
	LocalKey   string `json:"local_key,omitempty"`
}

func (a *App) initUploads() error {
	a.uploadDir = filepath.Join(a.cfg.DataDir, "uploads")
	if err := os.MkdirAll(a.uploadDir, 0700); err != nil {
		return err
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	if err := a.cleanupUploads(context.Background(), true); err != nil {
		return err
	}
	sessions, _, err := a.resources(context.Background(), "uploads", "", 129, 0)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, raw := range sessions {
		var session pendingUpload
		if err = json.Unmarshal(raw, &session); err != nil {
			return err
		}
		keep[session.ID] = true
	}
	root, err := os.OpenRoot(a.uploadDir)
	if err != nil {
		return err
	}
	defer root.Close()
	folder, err := root.Open(".")
	if err != nil {
		return err
	}
	defer folder.Close()
	for {
		entries, e := folder.ReadDir(200)
		for _, entry := range entries {
			if !entry.IsDir() && !keep[entry.Name()] {
				if err = root.Remove(entry.Name()); err != nil {
					return err
				}
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (a *App) cleanupUploads(ctx context.Context, startup bool) error {
	for {
		// 每批只读取待清理记录；活动上传不参与删除，避免高并发时扫描全部记录。
		rows, err := a.db.QueryContext(ctx, `SELECT data FROM uploads WHERE
			coalesce(json_extract(data,'$.Expires'),json_extract(data,'$.expires')) <= ?
			OR (? AND json_extract(data,'$.local_root') IS NOT NULL) LIMIT 128`, time.Now().Unix(), startup)
		if err != nil {
			return err
		}
		var pending []pendingUpload
		for rows.Next() {
			var raw string
			var upload pendingUpload
			if err = rows.Scan(&raw); err == nil {
				err = json.Unmarshal([]byte(raw), &upload)
			}
			if err != nil {
				_ = rows.Close()
				return err
			}
			pending = append(pending, upload)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil || len(pending) == 0 {
			return err
		}
		// 单连接数据库必须先关闭查询，再删除临时文件和记录。
		for _, upload := range pending {
			if err = a.removeUpload(ctx, upload); err != nil {
				return err
			}
		}
	}
}
func (a *App) removeUpload(ctx context.Context, upload pendingUpload) error {
	if !validUploadID(upload.ID) {
		return badRequest("上传编号无效")
	}
	if upload.LocalRoot != "" {
		driver := &localDriver{rootPath: upload.LocalRoot, scope: upload.LocalScope}
		root, _, err := driver.openParent(upload.LocalKey, false)
		if err != nil {
			var api *apiError
			if !errors.Is(err, os.ErrNotExist) && !(errors.As(err, &api) && api.Status == 404) {
				return err
			}
		}
		if root != nil {
			err = root.Remove(uploadTempPrefix + upload.ID)
			_ = root.Close()
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return a.deleteResource(ctx, "uploads", upload.ID)
	}
	root, err := os.OpenRoot(a.uploadDir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.Remove(upload.ID); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return a.deleteResource(ctx, "uploads", upload.ID)
}
func validUploadID(id string) bool { return len(id) == 32 && validSlug.MatchString(id) }
func (a *App) stagedSize(ctx context.Context) (int64, error) {
	var total int64
	err := a.db.QueryRowContext(ctx, "SELECT coalesce(sum(json_extract(data,'$.size')),0) FROM uploads").Scan(&total)
	return total + a.rawReserved, err
}
func (a *App) reserveUpload(ctx context.Context, size int64) error {
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	if err := a.cleanupUploads(ctx, false); err != nil {
		return err
	}
	used, err := a.stagedSize(ctx)
	if err != nil {
		return err
	}
	if size > a.runtimeLimits().MaxStagedBytes-used {
		return uploadQuota()
	}
	a.rawReserved += size
	return nil
}
func (a *App) releaseUpload(size int64) {
	a.uploadMu.Lock()
	a.rawReserved -= size
	a.uploadMu.Unlock()
}

type uploadBodyReader struct {
	reader     io.Reader
	controller *http.ResponseController
	ctx        context.Context
}

func (r *uploadBodyReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.controller != nil {
		// 测试或不支持读期限的包装器只探测一次，避免每块数据反复分配错误对象。
		if err := r.controller.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
			r.controller = nil
		}
	}
	return r.reader.Read(b)
}
func (a *App) uploadEndpoint(fn endpoint) endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		a.uploadMu.Lock()
		if a.uploadCount >= a.runtimeLimits().MaxUploads {
			a.uploadMu.Unlock()
			return &apiError{429, "UPLOAD_BUSY", "同时上传数量已达上限，请稍后重试"}
		}
		a.uploadCount++
		a.uploadMu.Unlock()
		// 调低限制只影响新请求，已占用额度正常归还，不中断当前上传。
		defer func() {
			a.uploadMu.Lock()
			a.uploadCount--
			a.uploadMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(r.Context(), time.Hour)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = &uploadReadCloser{uploadBodyReader: uploadBodyReader{reader: r.Body, controller: http.NewResponseController(w), ctx: ctx}, closer: r.Body}
		return fn(w, r)
	}
}

type uploadReadCloser struct {
	uploadBodyReader
	closer io.Closer
}

func (r *uploadReadCloser) Close() error { return r.closer.Close() }

func (a *App) registerUploads(mux *apiMux) {
	// 当前上传页面只发送原始文件；分享与文件发布共用有界上传额度。
	mux.HandleFunc("PUT /api/share/upload", a.guestOrProtected(guestUpload, MountUpload|FileShare, a.uploadEndpoint(a.uploadFile)))
}
func decodeUploadOptions(header string) (map[string]string, error) {
	fields := map[string]string{}
	if header == "" {
		return fields, nil
	}
	if len(header) > 8192 {
		return nil, badRequest("上传参数过长")
	}
	b, err := base64.StdEncoding.Strict().DecodeString(header)
	if err != nil {
		return nil, badRequest("上传参数编码无效")
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(b, &values) != nil || len(values) > 32 {
		return nil, badRequest("上传参数无效")
	}
	for k, v := range values {
		var s string
		if json.Unmarshal(v, &s) == nil {
			fields[k] = s
		} else {
			fields[k] = string(v)
		}
	}
	return fields, nil
}
func uploadCopy(ctx context.Context, to io.Writer, from io.Reader, limit int64) (int64, error) {
	n, err := copyUpload(ctx, to, io.LimitReader(from, limit+1))
	var maximum *http.MaxBytesError
	if n > limit || errors.As(err, &maximum) {
		return n, uploadTooLarge()
	}
	if err != nil {
		return n, badRequest("上传内容不完整或读取超时")
	}
	return n, nil
}
func uploadMetadata(r *http.Request) (string, string, map[string]string, error) {
	name, err := url.PathUnescape(r.Header.Get("X-Share-Filename"))
	if err != nil {
		return "", "", nil, badRequest("文件名编码无效")
	}
	if err = validateUploadName(name); err != nil {
		return "", "", nil, err
	}
	fields, err := decodeUploadOptions(r.Header.Get("X-Share-Options"))
	if err != nil {
		return "", "", nil, err
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(path.Ext(name))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(contentType); err != nil {
		return "", "", nil, badRequest("文件类型无效")
	}
	return name, contentType, fields, nil
}

func (a *App) uploadFile(w http.ResponseWriter, r *http.Request) error {
	// 未知长度也按单文件上限预留额度，避免并发流式上传耗尽临时磁盘。
	// 单次上传固定上限，中途提高设置不会突破已预留额度。
	maximum := a.maxUploadBytes()
	reserve := maximum
	if r.Method == "PUT" && r.ContentLength >= 0 {
		reserve = r.ContentLength
	}
	if reserve > maximum {
		return uploadTooLarge()
	}
	if err := a.reserveUpload(r.Context(), reserve); err != nil {
		return err
	}
	defer a.releaseUpload(reserve)
	name, contentType, fields, err := uploadMetadata(r)
	if err != nil {
		return err
	}
	if fields["overwrite"] == "true" || fields["overwrite"] == "1" {
		return badRequest("暂不支持覆盖已有文件")
	}
	storedName := name
	if fields["original_filename"] != "1" && fields["original_filename"] != "true" {
		extension := path.Ext(name)
		if len(extension) > 32 {
			extension = ""
		}
		storedName = randomID() + extension
	}
	dest, d, key, err := a.shareDestination(r, fields["storage_config_id"], fields["path"], storedName)
	if err != nil {
		return err
	}
	var local *localUpload
	var file *os.File
	if driver, ok := d.(*localDriver); ok {
		// 先登记再创建文件，进程被终止时也能在下次启动准确清理，不扫描存储目录。
		pending := pendingUpload{ID: randomID(), Expires: time.Now().Add(2 * time.Hour).Unix(), LocalRoot: driver.rootPath, LocalScope: driver.scope, LocalKey: key}
		if err = a.saveResource(r.Context(), "uploads", pending.ID, "", "", pending); err != nil {
			return err
		}
		defer func() {
			// 客户端取消后仍清理，清理失败则保留记录供下次启动或到期重试。
			if local != nil {
				if cleanupErr := local.close(); cleanupErr != nil {
					a.logger.Error("清理本地上传临时文件失败", "error", cleanupErr)
					return
				}
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			if cleanupErr := a.deleteResource(ctx, "uploads", pending.ID); cleanupErr != nil {
				a.logger.Error("清理上传记录失败", "error", cleanupErr)
			}
		}()
		local, err = driver.beginUpload(key, pending.ID)
		if err != nil {
			return err
		}
		file = local.file
	} else {
		// S3 继续使用私有暂存和顺序分片，避免在内存中保存完整文件。
		temporary := filepath.Join(a.uploadDir, randomID())
		file, err = os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		defer os.Remove(temporary)
		defer file.Close()
	}
	r.Body = http.MaxBytesReader(w, r.Body, min(maximum, reserve))
	size, err := uploadCopy(r.Context(), file, r.Body, min(maximum, reserve))
	if err != nil {
		return err
	}
	if r.ContentLength >= 0 && size != r.ContentLength {
		return badRequest("实际文件大小与声明不一致")
	}
	// 上传期间可能撤销账号、访客开关或修改存储，发布前必须再次检查当前权限。
	p, err := a.creationPrincipal(r, guestUpload, MountUpload|FileShare)
	if err != nil {
		return err
	}
	if p == nil || !p.Has(MountUpload|FileShare) {
		return forbidden()
	}
	r = r.WithContext(withPrincipal(r.Context(), p))
	record, err := a.uploadShareRecord(r, fields, name, size, contentType)
	if err != nil {
		return err
	}
	d, key, err = a.recheckDestination(r, dest, MountUpload)
	if err != nil {
		return err
	}
	if local != nil {
		driver, ok := d.(*localDriver)
		if !ok {
			return forbidden()
		}
		err = local.publish(r.Context(), driver, key, size)
	} else {
		writer, ok := d.(uploadWriter)
		if !ok {
			return unsupported()
		}
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		err = writer.PutNew(r.Context(), key, file, size, contentType)
	}
	if err != nil {
		return err
	}
	record.Path = dest.Path
	record.StorageID = dest.StorageID
	record.StorageKey = dest.StorageKey
	record.SourceVersion = dest.Version
	record.MetaVersion = dest.Proof
	// 文件传输留在锁外，仅后缀回收和记录写入需要串行提交。
	a.mutationMu.Lock()
	err = a.saveShareResource(r.Context(), "shares", record.ID, record.Slug, record.Owner, record)
	a.mutationMu.Unlock()
	if err != nil {
		return &apiError{409, "SHARE_SAVE_FAILED", "文件已保存，但分享记录未保存，请重新上传"}
	}
	v, err := a.managedFileView(record)
	if err != nil {
		return err
	}
	v["url"] = "/file/" + record.Slug
	setAuditTarget(r, record.ID)
	a.write(w, 201, v, "文件已上传并创建分享")
	return nil
}
func (a *App) uploadShareRecord(r *http.Request, fields map[string]string, name string, size int64, contentType string) (Share, error) {
	s := Share{ID: randomID(), Slug: fields["slug"], Filename: name, Size: size, MimeType: contentType, Remark: fields["remark"], Owner: principal(r).Owner(), CreatedAt: nowText()}
	s.UpdatedAt = s.CreatedAt
	if s.Slug == "" {
		s.Slug = randomID()
	}
	if !validSlug.MatchString(s.Slug) || len(s.Remark) > 4000 {
		return s, badRequest("链接后缀或备注无效")
	}
	if raw := fields["max_views"]; raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return s, badRequest("最大查看次数无效")
		}
		s.MaxViews = v
	}
	if fields["expires_in"] == "" {
		expiry, err := a.defaultShareExpiry(r)
		if err != nil {
			return s, err
		}
		s.ExpiresAt = expiry
	}
	if raw := fields["expires_in"]; raw != "" {
		hours, err := strconv.ParseFloat(raw, 64)
		if err != nil || hours < 0 || hours > 876000 || strconv.FormatFloat(hours, 'g', -1, 64) == "NaN" {
			return s, badRequest("过期小时数无效")
		}
		if hours > 0 {
			v := time.Now().Add(time.Duration(hours * float64(time.Hour))).UTC().Format(time.RFC3339)
			s.ExpiresAt = &v
		}
	}
	if err := validateExpiry(s.ExpiresAt, s.MaxViews); err != nil {
		return s, err
	}
	if fields["password"] != "" {
		var err error
		s.Password, s.EncryptedPassword, err = a.protectSharePassword(fields["password"])
		if err != nil {
			return s, err
		}
	}
	// 预检查只判断可用性；实际回收在文件或合集成功提交的事务中执行。
	if _, err := checkShareSlug(r.Context(), a.db, "shares", s.Slug, s.ID); err != nil {
		return s, err
	}
	return s, nil
}
