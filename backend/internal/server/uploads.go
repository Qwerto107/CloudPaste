package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
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
		if unicode.IsControl(r) {
			return badRequest("文件名包含控制字符")
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

func (a *App) initUploads() error {
	a.uploadDir = filepath.Join(a.cfg.DataDir, "uploads")
	if err := os.MkdirAll(a.uploadDir, 0700); err != nil {
		return err
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	if err := a.cleanupUploads(context.Background()); err != nil {
		return err
	}
	sessions, _, err := a.resources(context.Background(), "uploads", "", 129, 0)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, raw := range sessions {
		var session uploadSession
		if err = json.Unmarshal(raw, &session); err != nil {
			return err
		}
		keep[session.ID] = true
		if session.Completing {
			session.Completing = false
			if err = a.saveResource(context.Background(), "uploads", session.ID, "", session.Owner, session); err != nil {
				return err
			}
		}
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
func (a *App) cleanupUploads(ctx context.Context) error {
	raws, _, err := a.resources(ctx, "uploads", "", 129, 0)
	if err != nil {
		return err
	}
	for _, raw := range raws {
		var s uploadSession
		if err = json.Unmarshal(raw, &s); err != nil {
			return err
		}
		if s.Expires <= time.Now().Unix() && !s.Completing {
			if err = a.removeUpload(ctx, s.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
func (a *App) removeUpload(ctx context.Context, id string) error {
	if !validUploadID(id) {
		return badRequest("上传编号无效")
	}
	root, err := os.OpenRoot(a.uploadDir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.Remove(id); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return a.deleteResource(ctx, "uploads", id)
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
	if err := a.cleanupUploads(ctx); err != nil {
		return err
	}
	used, err := a.stagedSize(ctx)
	if err != nil {
		return err
	}
	if size > a.cfg.MaxStagedBytes-used {
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
		select {
		case a.uploads <- struct{}{}:
			defer func() { <-a.uploads }()
		default:
			return &apiError{429, "UPLOAD_BUSY", "同时上传数量已达上限，请稍后重试"}
		}
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
	for _, method := range []string{"PUT", "POST"} {
		mux.HandleFunc(method+" /api/fs/upload", a.protected(MountUpload, false, a.uploadEndpoint(a.uploadFile)))
		mux.HandleFunc(method+" /api/share/upload", a.protected(MountUpload|FileShare, false, a.uploadEndpoint(a.uploadFile)))
	}
	a.registerMultipart(mux)
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
func (a *App) readUpload(w http.ResponseWriter, r *http.Request, f *os.File, share bool, maximum int64) (string, string, int64, map[string]string, error) {
	fields := map[string]string{}
	name, contentType := "", r.Header.Get("Content-Type")
	var size int64
	r.Body = http.MaxBytesReader(w, r.Body, maximum+(128<<10))
	if r.Method == "PUT" {
		prefix := "X-FS-"
		if share {
			prefix = "X-Share-"
		}
		var err error
		name, err = url.PathUnescape(r.Header.Get(prefix + "Filename"))
		if err != nil {
			return "", "", 0, nil, badRequest("文件名编码无效")
		}
		if err = validateUploadName(name); err != nil {
			return "", "", 0, nil, err
		}
		fields, err = decodeUploadOptions(r.Header.Get(prefix + "Options"))
		if err != nil {
			return "", "", 0, nil, err
		}
		if r.ContentLength > maximum {
			return "", "", 0, nil, uploadTooLarge()
		}
		size, err = uploadCopy(r.Context(), f, r.Body, maximum)
		if err != nil {
			return "", "", 0, nil, err
		}
		if r.ContentLength >= 0 && size != r.ContentLength {
			return "", "", 0, nil, badRequest("实际文件大小与声明不一致")
		}
	} else {
		media, params, err := mime.ParseMediaType(contentType)
		if err != nil || media != "multipart/form-data" || params["boundary"] == "" {
			return "", "", 0, nil, badRequest("需要 multipart/form-data 文件表单")
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		fieldBytes := int64(0)
		parts := 0
		for {
			part, err := reader.NextRawPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", "", 0, nil, badRequest("文件表单不完整")
			}
			parts++
			if parts > 33 {
				return "", "", 0, nil, badRequest("表单字段过多")
			}
			if part.FormName() == "file" {
				if name != "" {
					return "", "", 0, nil, badRequest("每个请求只能上传一个文件")
				}
				_, parameters, e := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
				if e != nil {
					return "", "", 0, nil, badRequest("文件表单无效")
				}
				name = parameters["filename"]
				if err = validateUploadName(name); err != nil {
					return "", "", 0, nil, err
				}
				contentType = part.Header.Get("Content-Type")
				size, err = uploadCopy(r.Context(), f, part, maximum)
				if err != nil {
					return "", "", 0, nil, err
				}
			} else {
				key := part.FormName()
				if key == "" || len(key) > 100 {
					return "", "", 0, nil, badRequest("表单字段无效")
				}
				if _, exists := fields[key]; exists {
					return "", "", 0, nil, badRequest("表单字段重复")
				}
				b, e := io.ReadAll(io.LimitReader(part, (64<<10)-fieldBytes+1))
				fieldBytes += int64(len(b))
				if e != nil || fieldBytes > 64<<10 {
					return "", "", 0, nil, badRequest("表单参数过长")
				}
				fields[key] = string(b)
			}
			_ = part.Close()
		}
		if name == "" {
			return "", "", 0, nil, badRequest("缺少 file 文件")
		}
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(path.Ext(name))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(contentType); err != nil {
		return "", "", 0, nil, badRequest("文件类型无效")
	}
	return name, contentType, size, fields, nil
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
	temporary := randomID()
	f, err := os.OpenFile(filepath.Join(a.uploadDir, temporary), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(filepath.Join(a.uploadDir, temporary))
	defer f.Close()
	share := r.URL.Path == "/api/share/upload"
	name, contentType, size, fields, err := a.readUpload(w, r, f, share, min(maximum, reserve))
	if err != nil {
		return err
	}
	if fields["overwrite"] == "true" || fields["overwrite"] == "1" {
		return badRequest("暂不支持覆盖已有文件")
	}
	p, err := a.authenticate(r)
	if err != nil {
		return err
	}
	if p == nil || !p.Has(MountUpload) {
		return forbidden()
	}
	r = r.WithContext(withPrincipal(r.Context(), p))
	var dest uploadDestination
	var record Share
	if share {
		if !p.Has(FileShare) {
			return forbidden()
		}
		record, err = a.uploadShareRecord(r, fields, name, size, contentType)
		if err != nil {
			return err
		}
		storedName := name
		if fields["original_filename"] != "1" && fields["original_filename"] != "true" {
			extension := path.Ext(name)
			if len(extension) > 32 {
				extension = ""
			}
			storedName = randomID() + extension
		}
		dest, _, _, err = a.shareDestination(r, fields["storage_config_id"], fields["path"], storedName)
	} else {
		dir := r.URL.Query().Get("path")
		if dir == "" {
			dir = fields["path"]
		}
		canonical, e := cleanPath(dir)
		if e != nil {
			return e
		}
		dest, _, _, err = a.mountedDestination(r, path.Join(canonical, name), true)
	}
	if err != nil {
		return err
	}
	d, key, err := a.recheckDestination(r, dest, MountUpload)
	if err != nil {
		return err
	}
	writer, ok := d.(uploadWriter)
	if !ok {
		return unsupported()
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err = writer.PutNew(r.Context(), key, f, size, contentType); err != nil {
		return err
	}
	if share {
		record.Path = dest.Path
		record.StorageID = dest.StorageID
		record.StorageKey = dest.StorageKey
		record.SourceVersion = dest.Version
		record.MetaVersion = dest.Proof
		if err = a.saveResource(r.Context(), "shares", record.ID, record.Slug, record.Owner, record); err != nil {
			return &apiError{409, "SHARE_SAVE_FAILED", "文件已保存，但分享记录未保存，请在挂载中重新创建分享"}
		}
		v := shareView(record)
		v["url"] = "/file/" + record.Slug
		a.write(w, 201, v, "文件已上传并创建分享")
	} else {
		a.write(w, 201, map[string]any{"path": dest.Path, "name": name, "size": size}, "文件上传成功")
	}
	return nil
}
func (a *App) uploadShareRecord(r *http.Request, fields map[string]string, name string, size int64, contentType string) (Share, error) {
	s := Share{ID: randomID(), Slug: fields["slug"], Filename: name, Size: size, MimeType: contentType, Remark: fields["remark"], Owner: principal(r).Owner(), CreatedAt: nowText(), UseProxy: 1}
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
		s.Password, err = passwordHash(fields["password"])
		if err != nil {
			return s, err
		}
	}
	var count int
	if err := a.db.QueryRowContext(r.Context(), "SELECT count(*) FROM shares WHERE slug=?", s.Slug).Scan(&count); err != nil {
		return s, err
	}
	if count != 0 {
		return s, badRequest("链接后缀已被占用")
	}
	return s, nil
}
