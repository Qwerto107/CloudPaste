package server

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// 上传仍逐个传输；全部完成后在一次事务中归入合集，旧的单文件链接同时失效。
func (a *App) createCollection(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		IDs       []string    `json:"ids"`
		Slug      string      `json:"slug"`
		Name      string      `json:"name"`
		Password  string      `json:"password"`
		Remark    string      `json:"remark"`
		ExpiresIn json.Number `json:"expires_in"`
		MaxViews  int         `json:"max_views"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if len(body.IDs) == 0 || len(body.IDs) > 30 {
		return badRequest("一个合集包含 1-30 个文件")
	}
	if body.Name == "" {
		body.Name = "文件合集"
	}
	if err := validateUploadName(body.Name); err != nil {
		return err
	}
	collection, err := a.uploadShareRecord(r, map[string]string{
		"slug": body.Slug, "password": body.Password, "remark": body.Remark,
		"expires_in": body.ExpiresIn.String(), "max_views": strconv.Itoa(body.MaxViews),
	}, body.Name, 0, "application/zip")
	if err != nil {
		return err
	}
	err = func() error {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
		actor, err := a.authenticate(r)
		if err != nil {
			return err
		}
		if !actor.Has(FileShare) || actor.Owner() != collection.Owner {
			return forbidden()
		}
		currentRequest := r.WithContext(withPrincipal(r.Context(), actor))
		seen := map[string]bool{}
		for _, id := range body.IDs {
			if seen[id] {
				return badRequest("合集不能包含重复文件")
			}
			seen[id] = true
			var file Share
			if err := a.resource(r.Context(), "shares", "id", id, &file); err != nil {
				return err
			}
			// 只能合并本人上传的文件，不能嵌套合集或借用其他创建者的权限。
			if file.Owner != actor.Owner() || file.StorageID == "" || len(file.Files) != 0 {
				return forbidden()
			}
			// 合集有效期从生成时开始，不能因首个文件上传较早而阻止长批次完成。
			if _, _, err := a.shareDriver(currentRequest, file); err != nil {
				return err
			}
			collection.Files = append(collection.Files, file)
			collection.Size += file.Size
		}
		data, err := json.Marshal(collection)
		if err != nil {
			return err
		}
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := reclaimShareSlug(r.Context(), tx, "shares", collection.Slug, collection.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), "INSERT INTO shares(id,slug,owner,data,created_at) VALUES(?,?,?,?,?)", collection.ID, collection.Slug, collection.Owner, string(data), collection.CreatedAt); err != nil {
			return err
		}
		for _, id := range body.IDs {
			if _, err := tx.ExecContext(r.Context(), "DELETE FROM shares WHERE id=? AND owner=?", id, collection.Owner); err != nil {
				return err
			}
		}
		return tx.Commit()
	}()
	if err != nil {
		return err
	}
	a.write(w, 201, shareView(collection), "文件合集已创建")
	return nil
}

// 合集每次访问都复核全部来源，撤销密钥、修改存储或目录密码同样使合集失效。
func (a *App) validateShareSources(r *http.Request, s Share) error {
	if len(s.Files) == 0 {
		_, _, err := a.shareDriver(r, s)
		return err
	}
	for _, file := range s.Files {
		if file.Owner != s.Owner || len(file.Files) != 0 {
			return forbidden()
		}
		if _, _, err := a.shareDriver(r, file); err != nil {
			return err
		}
	}
	return nil
}

// 多次文件读取共享合集票据；没有票据的直接下载只计一次查看。
func (a *App) commitCollectionAccess(r *http.Request, s Share) error {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	current, err := a.revalidateShare(r, s)
	if err != nil {
		return err
	}
	consume, err := a.shareContentAccess(r, current)
	if err != nil {
		return err
	}
	if consume {
		current.Views++
		return a.saveResource(r.Context(), "shares", current.ID, current.Slug, current.Owner, current)
	}
	return nil
}

func (a *App) collectionContent(w http.ResponseWriter, r *http.Request, s Share) error {
	if r.URL.Query().Get("archive") == "true" {
		return a.collectionArchive(w, r, s)
	}
	for _, file := range s.Files {
		if file.ID != r.URL.Query().Get("file") {
			continue
		}
		info, driver, key, err := a.shareSource(r, file)
		if err != nil {
			return err
		}
		if err := a.commitCollectionAccess(r, s); err != nil {
			return err
		}
		return a.deliver(w, r, driver, key, info, r.URL.Query().Get("download") == "true")
	}
	return notFound()
}

func (a *App) collectionArchive(w http.ResponseWriter, r *http.Request, s Share) error {
	// 整个打包占一个下载槽；逐个读取，始终只持有一个文件流和固定缓冲区。
	release, err := a.beginDownload(r)
	if err != nil {
		return err
	}
	defer release()
	type source struct {
		info   FileInfo
		driver Driver
		key    string
	}
	sources := make([]source, 0, len(s.Files))
	for _, file := range s.Files {
		if err := validateUploadName(file.Filename); err != nil {
			return err
		}
		info, driver, key, err := a.shareSource(r, file)
		if err != nil {
			return err
		}
		if info.Directory {
			return badRequest("合集只能打包普通文件")
		}
		sources = append(sources, source{info, driver, key})
	}
	if err := a.commitCollectionAccess(r, s); err != nil {
		return err
	}
	// ZIP 需要完整文件，不能把下载 ZIP 的 Range 或条件请求转发给源文件。
	sourceRequest := r.Clone(r.Context())
	sourceRequest.Header = make(http.Header)
	var first Stream
	if r.Method != "HEAD" {
		first, err = sources[0].driver.Open(r.Context(), sources[0].key, sourceRequest)
		if err != nil {
			return err
		}
		if first.Body == nil || first.Status != http.StatusOK {
			if first.Body != nil {
				_ = first.Body.Close()
			}
			return badRequest("文件暂时无法读取")
		}
		defer first.Body.Close()
	}
	response, err := newDownloadResponse(w, downloadWriteTimeout)
	if err != nil {
		return err
	}
	defer response.finish()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": s.Filename + ".zip"}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)
	if r.Method == "HEAD" {
		return nil
	}
	archive := zip.NewWriter(response)
	buffer := make([]byte, 32<<10)
	usedNames := map[string]bool{}
	for index, source := range sources {
		stream := first
		if index > 0 {
			stream, err = source.driver.Open(r.Context(), source.key, sourceRequest)
			if err != nil {
				a.abortCollectionArchive(r, err)
			}
		}
		name := source.info.Name
		extension := path.Ext(name)
		for suffix := 2; usedNames[strings.ToLower(name)]; suffix++ {
			name = strings.TrimSuffix(source.info.Name, extension) + fmt.Sprintf(" (%d)", suffix) + extension
		}
		usedNames[strings.ToLower(name)] = true
		err = func() error {
			if stream.Body != nil {
				defer stream.Body.Close()
			}
			if stream.Body == nil || stream.Status != http.StatusOK {
				return badRequest("文件暂时无法读取")
			}
			// Store 只打包不压缩，避免视频、图片等已压缩文件反复消耗 CPU。
			header := &zip.FileHeader{Name: name, Method: zip.Store}
			header.SetMode(0600)
			if source.info.Modified != nil {
				header.SetModTime(*source.info.Modified)
			}
			entry, err := archive.CreateHeader(header)
			if err != nil {
				return err
			}
			_, err = io.CopyBuffer(entry, stream.Body, buffer)
			return err
		}()
		if err != nil {
			a.abortCollectionArchive(r, err)
		}
	}
	if err := archive.Close(); err != nil {
		a.abortCollectionArchive(r, err)
	}
	return nil
}

func (a *App) abortCollectionArchive(r *http.Request, err error) {
	// 传输已开始后直接中断连接，不能往 ZIP 中追加 JSON 或把不完整文件报成成功。
	if r.Context().Err() == nil {
		a.logger.Warn("文件合集打包中断", "error", err)
	}
	panic(http.ErrAbortHandler)
}
