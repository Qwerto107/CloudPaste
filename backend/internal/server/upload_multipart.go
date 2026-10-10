package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

type uploadPart struct {
	PartNumber int
	ETag       string
	Size       int64
}
type uploadSession struct {
	ID          string             `json:"id"`
	Owner       string             `json:"owner"`
	Credential  string             `json:"credential"`
	Destination uploadDestination  `json:"destination"`
	Size        int64              `json:"size"`
	PartSize    int64              `json:"part_size"`
	FileName    string             `json:"file_name"`
	ContentType string             `json:"content_type"`
	Expires     int64              `json:"expires"`
	Parts       map[int]uploadPart `json:"parts"`
	Completing  bool               `json:"completing"`
}

func (s uploadSession) partCount() int {
	if s.Size == 0 {
		return 1
	}
	return int((s.Size + s.PartSize - 1) / s.PartSize)
}
func (s uploadSession) partBytes(number int) int64 {
	return min(s.PartSize, s.Size-int64(number-1)*s.PartSize)
}
func multipartPolicy() map[string]any {
	return map[string]any{"signingMode": "on_demand", "maxPartsPerRequest": 32, "partsLedgerPolicy": "server_can_list", "urlTtlSeconds": 600}
}
func sessionView(s uploadSession) map[string]any {
	return map[string]any{"uploadId": s.ID, "key": strings.TrimPrefix(s.Destination.Path, "/"), "fileName": s.FileName, "fileSize": s.Size, "size": s.Size, "partSize": s.PartSize, "totalParts": s.partCount(), "strategy": "per_part_url", "presignedUrls": []any{}, "policy": multipartPolicy(), "initiated": time.Unix(s.Expires, 0).Add(-24 * time.Hour).UTC().Format(time.RFC3339)}
}
func (a *App) registerMultipart(mux *apiMux) {
	mux.HandleFunc("POST /api/fs/multipart/init", a.protected(MountUpload, false, a.initMultipart))
	mux.HandleFunc("POST /api/fs/multipart/sign-parts", a.protected(MountUpload, false, a.signMultipart))
	mux.HandleFunc("POST /api/fs/multipart/list-parts", a.protected(MountUpload, false, a.listMultipartParts))
	mux.HandleFunc("POST /api/fs/multipart/list-uploads", a.protected(MountUpload, false, a.listMultipartUploads))
	mux.HandleFunc("POST /api/fs/multipart/abort", a.protected(MountUpload, false, a.abortMultipart))
	mux.HandleFunc("POST /api/fs/multipart/complete", a.protected(MountUpload, false, a.uploadEndpoint(a.completeMultipart)))
	mux.HandleFunc("PUT /api/fs/multipart/part/{id}/{number}", a.handle(a.uploadEndpoint(a.uploadMultipartPart)))
}
func (a *App) initMultipart(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Path, FileName, ContentType string
		FileSize, PartSize          int64
		PartCount                   int
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if err := validateUploadName(body.FileName); err != nil {
		return err
	}
	if body.FileSize < 0 {
		return badRequest("文件大小无效")
	}
	if body.FileSize > a.maxUploadBytes() {
		return uploadTooLarge()
	}
	dir, err := cleanPath(body.Path)
	if err != nil {
		return err
	}
	dest, _, _, err := a.mountedDestination(r, path.Join(dir, body.FileName), true)
	if err != nil {
		return err
	}
	if body.ContentType == "" {
		body.ContentType = "application/octet-stream"
	}
	if _, _, err = mime.ParseMediaType(body.ContentType); err != nil {
		return badRequest("文件类型无效")
	}
	partSize := body.PartSize
	if partSize == 0 {
		partSize = 5 << 20
	}
	if partSize < 5<<20 || partSize > 16<<20 {
		return badRequest("分片大小应为 5-16 MiB")
	}
	s := uploadSession{ID: randomID(), Owner: principal(r).Owner(), Credential: principal(r).CredentialHash, Destination: dest, Size: body.FileSize, PartSize: partSize, FileName: body.FileName, ContentType: body.ContentType, Expires: time.Now().Add(24 * time.Hour).Unix(), Parts: map[int]uploadPart{}}
	if body.PartCount > 0 && body.PartCount != s.partCount() {
		return badRequest("分片数量与文件大小不一致")
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	if err = a.cleanupUploads(r.Context()); err != nil {
		return err
	}
	used, err := a.stagedSize(r.Context())
	if err != nil {
		return err
	}
	if s.Size > a.cfg.MaxStagedBytes-used {
		return uploadQuota()
	}
	var count int
	if err = a.db.QueryRowContext(r.Context(), "SELECT count(*) FROM uploads").Scan(&count); err != nil {
		return err
	}
	if count >= 128 {
		return uploadQuota()
	}
	f, err := os.OpenFile(filepath.Join(a.uploadDir, s.ID), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = f.Truncate(s.Size)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(filepath.Join(a.uploadDir, s.ID))
		return err
	}
	if err = a.saveResource(r.Context(), "uploads", s.ID, "", s.Owner, s); err != nil {
		_ = os.Remove(filepath.Join(a.uploadDir, s.ID))
		return err
	}
	a.write(w, 200, sessionView(s), "分片上传已初始化")
	return nil
}
func (a *App) ownedUpload(r *http.Request, id string, checkDestination bool) (uploadSession, error) {
	var s uploadSession
	if !validUploadID(id) {
		return s, badRequest("上传编号无效")
	}
	if err := a.resource(r.Context(), "uploads", "id", id, &s); err != nil {
		return s, err
	}
	p := principal(r)
	if p == nil || s.Owner != p.Owner() || s.Credential != p.CredentialHash {
		return s, forbidden()
	}
	if s.Expires <= time.Now().Unix() {
		return s, &apiError{410, "UPLOAD_EXPIRED", "上传会话已过期"}
	}
	if s.Completing {
		return s, &apiError{409, "UPLOAD_COMPLETING", "上传正在完成，请稍后重试"}
	}
	if checkDestination {
		_, _, err := a.recheckDestination(r, s.Destination, MountUpload)
		if err != nil {
			return s, err
		}
	}
	return s, nil
}

type multipartRequest struct {
	Path, UploadID string
	PartNumbers    []int
	Parts          []uploadPart
	FileName       string
	FileSize       int64
}

func (a *App) signMultipart(w http.ResponseWriter, r *http.Request) error {
	var b multipartRequest
	if err := decode(w, r, &b); err != nil {
		return err
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	s, err := a.ownedUpload(r, b.UploadID, true)
	if err != nil {
		return err
	}
	if len(b.PartNumbers) == 0 || len(b.PartNumbers) > 32 {
		return badRequest("一次签名 1-32 个分片")
	}
	urls := []map[string]any{}
	seen := map[int]bool{}
	for _, n := range b.PartNumbers {
		if n < 1 || n > s.partCount() || seen[n] {
			return badRequest("分片编号无效或重复")
		}
		seen[n] = true
		p := principal(r)
		token := a.ticket(Ticket{Purpose: "upload-part", Path: s.Destination.Path, Subject: p.ID, Version: p.Owner(), Credential: p.CredentialHash, MetaVersion: s.Destination.Proof, SourceVersion: s.Destination.Version, UploadID: s.ID, PartNumber: n, Expires: time.Now().Add(10 * time.Minute).Unix()})
		urls = append(urls, map[string]any{"partNumber": n, "url": scopedAPIPath(r, "/api/fs/multipart/part/") + s.ID + "/" + strconv.Itoa(n) + "?sign=" + url.QueryEscape(token)})
	}
	a.write(w, 200, map[string]any{"presignedUrls": urls, "policy": multipartPolicy()}, "分片地址已签发")
	return nil
}
func (a *App) listMultipartParts(w http.ResponseWriter, r *http.Request) error {
	var b multipartRequest
	if err := decode(w, r, &b); err != nil {
		return err
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	s, err := a.ownedUpload(r, b.UploadID, true)
	if err != nil {
		return err
	}
	parts := []uploadPart{}
	for _, part := range s.Parts {
		parts = append(parts, part)
	}
	slices.SortFunc(parts, func(a, b uploadPart) int { return a.PartNumber - b.PartNumber })
	a.write(w, 200, map[string]any{"parts": parts, "policy": multipartPolicy()}, "获取分片成功")
	return nil
}
func (a *App) listMultipartUploads(w http.ResponseWriter, r *http.Request) error {
	var b multipartRequest
	if err := decode(w, r, &b); err != nil {
		return err
	}
	dir, err := cleanPath(b.Path)
	if err != nil {
		return err
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	if err = a.cleanupUploads(r.Context()); err != nil {
		return err
	}
	raws, _, err := a.resources(r.Context(), "uploads", principal(r).Owner(), 128, 0)
	if err != nil {
		return err
	}
	items := []map[string]any{}
	for _, raw := range raws {
		var s uploadSession
		if err = json.Unmarshal(raw, &s); err != nil {
			return err
		}
		if s.Credential == principal(r).CredentialHash && !s.Completing && (b.Path == "" || path.Dir(s.Destination.Path) == dir) {
			if _, _, e := a.recheckDestination(r, s.Destination, MountUpload); e == nil {
				items = append(items, sessionView(s))
			}
		}
	}
	a.write(w, 200, map[string]any{"uploads": items}, "获取上传会话成功")
	return nil
}
func (a *App) abortMultipart(w http.ResponseWriter, r *http.Request) error {
	var b multipartRequest
	if err := decode(w, r, &b); err != nil {
		return err
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	s, err := a.ownedUpload(r, b.UploadID, false)
	if err != nil {
		return err
	}
	if err = a.removeUpload(r.Context(), s.ID); err != nil {
		return err
	}
	a.write(w, 200, nil, "分片上传已取消")
	return nil
}
func (a *App) uploadMultipartPart(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || !validUploadID(id) {
		return badRequest("分片编号无效")
	}
	a.uploadMu.Lock()
	var s uploadSession
	err = a.resource(r.Context(), "uploads", "id", id, &s)
	a.uploadMu.Unlock()
	if err != nil {
		return err
	}
	token := r.URL.Query().Get("sign")
	ticket, err := a.parseTicket(token, "upload-part", s.Destination.Path)
	if err != nil {
		return err
	}
	if ticket.UploadID != id || ticket.PartNumber != n || ticket.SourceVersion != s.Destination.Version || ticket.MetaVersion != s.Destination.Proof {
		return forbidden()
	}
	actor, err := a.ticketPrincipal(r, token, "upload-part", s.Destination.Path)
	if err != nil {
		return err
	}
	r = r.WithContext(withPrincipal(r.Context(), actor))
	a.uploadMu.Lock()
	s, err = a.ownedUpload(r, id, true)
	a.uploadMu.Unlock()
	if err != nil {
		return err
	}
	if n < 1 || n > s.partCount() {
		return badRequest("分片编号无效")
	}
	expected := s.partBytes(n)
	if r.ContentLength >= 0 && r.ContentLength != expected {
		return badRequest("分片大小不匹配")
	}
	temporary := randomID()
	f, err := os.OpenFile(filepath.Join(a.uploadDir, temporary), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(filepath.Join(a.uploadDir, temporary))
	defer f.Close()
	r.Body = http.MaxBytesReader(w, r.Body, expected+1)
	hash := sha256.New()
	size, err := uploadCopy(r.Context(), io.MultiWriter(f, hash), r.Body, expected)
	if err != nil {
		return err
	}
	if size != expected {
		return badRequest("分片实际大小不匹配")
	}
	etag := hex.EncodeToString(hash.Sum(nil))
	// 接收期间凭证可能被停用或退出，提交分片前再次读取当前认证状态。
	actor, err = a.ticketPrincipal(r, token, "upload-part", s.Destination.Path)
	if err != nil {
		return err
	}
	r = r.WithContext(withPrincipal(r.Context(), actor))
	// 先完整接收单个分片，再串行提交；重试失败不能沿用旧 ETag 完成损坏的文件。
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	s, err = a.ownedUpload(r, id, true)
	if err != nil {
		return err
	}
	delete(s.Parts, n)
	if err = a.saveResource(r.Context(), "uploads", id, "", s.Owner, s); err != nil {
		return err
	}
	backing, err := os.OpenFile(filepath.Join(a.uploadDir, id), os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer backing.Close()
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err = copyUpload(r.Context(), io.NewOffsetWriter(backing, int64(n-1)*s.PartSize), f); err != nil {
		return err
	}
	if err = backing.Sync(); err != nil {
		return err
	}
	s.Parts[n] = uploadPart{PartNumber: n, ETag: etag, Size: size}
	if err = a.saveResource(r.Context(), "uploads", id, "", s.Owner, s); err != nil {
		return err
	}
	w.Header().Set("ETag", etag)
	w.WriteHeader(200)
	return nil
}
func (a *App) completeMultipart(w http.ResponseWriter, r *http.Request) error {
	var b multipartRequest
	if err := decode(w, r, &b); err != nil {
		return err
	}
	// 元数据接收期间可能撤销凭证，完成上传前重新认证。
	actor, err := a.authenticate(r)
	if err != nil {
		return err
	}
	if actor == nil || !actor.Has(MountUpload) {
		return forbidden()
	}
	r = r.WithContext(withPrincipal(r.Context(), actor))
	a.uploadMu.Lock()
	s, err := a.ownedUpload(r, b.UploadID, true)
	if err != nil {
		a.uploadMu.Unlock()
		return err
	}
	if len(b.Parts) != s.partCount() || len(s.Parts) != s.partCount() {
		a.uploadMu.Unlock()
		return badRequest("分片尚未全部上传")
	}
	seen := map[int]bool{}
	for _, p := range b.Parts {
		stored, ok := s.Parts[p.PartNumber]
		if !ok || seen[p.PartNumber] || p.ETag != stored.ETag || stored.Size != s.partBytes(p.PartNumber) {
			a.uploadMu.Unlock()
			return badRequest("分片清单与服务器校验记录不一致")
		}
		seen[p.PartNumber] = true
	}
	s.Completing = true
	err = a.saveResource(r.Context(), "uploads", s.ID, "", s.Owner, s)
	a.uploadMu.Unlock()
	if err != nil {
		return err
	}
	completed := false
	defer func() {
		if !completed {
			a.uploadMu.Lock()
			defer a.uploadMu.Unlock()
			s.Completing = false
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			_ = a.saveResource(cleanup, "uploads", s.ID, "", s.Owner, s)
		}
	}()
	d, key, err := a.recheckDestination(r, s.Destination, MountUpload)
	if err != nil {
		return err
	}
	writer, ok := d.(uploadWriter)
	if !ok {
		return unsupported()
	}
	f, err := os.Open(filepath.Join(a.uploadDir, s.ID))
	if err != nil {
		return err
	}
	err = writer.PutNew(r.Context(), key, f, s.Size, s.ContentType)
	_ = f.Close()
	if err != nil {
		return err
	}
	a.uploadMu.Lock()
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	err = a.removeUpload(cleanup, s.ID)
	cancel()
	a.uploadMu.Unlock()
	if err != nil {
		return err
	}
	completed = true
	a.write(w, 200, map[string]any{"path": s.Destination.Path, "name": s.FileName, "size": s.Size}, "分片上传完成")
	return nil
}
