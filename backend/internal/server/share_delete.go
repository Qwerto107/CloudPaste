package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fileDeleter interface {
	Delete(context.Context, string) error
}

type shareFileDeletion struct {
	driver fileDeleter
	key    string
}

// 只删除已验证的普通文件，不递归删除目录、不跟随符号链接。
func (d *localDriver) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, name, err := d.openParent(key, false)
	var api *apiError
	if errors.Is(err, os.ErrNotExist) || (errors.As(err, &api) && api.Status == 404) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return localError(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return forbidden()
	}
	if err := localCheckName(root, name, info); err != nil {
		return err
	}
	err = root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return localErrorOrNil(err)
}

func (d *s3Driver) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(d.bucket), Key: aws.String(key)})
	// 缺失文件视为已删除，合集部分失败后可安全重试。
	var api *apiError
	if errors.As(s3Error(err), &api) && api.Status == 404 {
		return nil
	}
	return s3Error(err)
}

// 删除前一次性检查全部来源，避免合集中的只读或越权项导致前面文件先被删除。
func (a *App) shareDeletionTargets(r *http.Request, share Share) ([]shareFileDeletion, error) {
	files := share.Files
	if len(files) == 0 {
		files = []Share{share}
	}
	targets := make([]shareFileDeletion, 0, len(files))
	for _, file := range files {
		if file.Owner != share.Owner || len(file.Files) != 0 {
			return nil, forbidden()
		}
		var storage Storage
		var driver Driver
		var key string
		var err error
		if file.StorageID != "" {
			if err = a.resource(r.Context(), "storage_configs", "id", file.StorageID, &storage); err != nil {
				return nil, err
			}
			dest := uploadDestination{Path: file.Path, StorageID: file.StorageID, StorageKey: file.StorageKey, Version: file.SourceVersion, Proof: file.MetaVersion}
			driver, key, err = a.recheckDestination(r, dest, FileManage)
		} else {
			var mount Mount
			mount, storage, driver, key, err = a.resolveMount(r, file.Path, FileManage, false)
			if err == nil {
				meta, metaErr := a.effectiveMeta(r.Context(), file.Path)
				err = metaErr
				if err == nil && (file.SourceVersion != sourceVersion(mount, storage) || file.MetaVersion != secretHash(meta.Password)) {
					err = &apiError{409, "SOURCE_CHANGED", "文件来源已变化，请选择仅删除记录"}
				}
			}
		}
		if err != nil {
			return nil, err
		}
		if storage.ReadOnly == 1 {
			return nil, forbidden()
		}
		deleter, ok := driver.(fileDeleter)
		if !ok {
			return nil, unsupported()
		}
		targets = append(targets, shareFileDeletion{driver: deleter, key: key})
	}
	return targets, nil
}

func (a *App) deleteShare(r *http.Request, id string, deleteFiles bool) (bool, error) {
	r.SetPathValue("id", id)
	share, targets, err := func() (Share, []shareFileDeletion, error) {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
		actor, err := a.authenticate(r)
		if err != nil {
			return Share{}, nil, err
		}
		if !actor.Has(FileManage) {
			return Share{}, nil, forbidden()
		}
		r = r.WithContext(withPrincipal(r.Context(), actor))
		share, err := a.ownedShare(r)
		if err != nil || !deleteFiles {
			return share, nil, err
		}
		targets, err := a.shareDeletionTargets(r, share)
		return share, targets, err
	}()
	var api *apiError
	if errors.As(err, &api) && api.Status == 404 && share.ID == "" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// 存储操作放在锁外，远程删除失败时保留分享记录，允许重试或仅删除记录。
	for _, target := range targets {
		if err := target.driver.Delete(r.Context(), target.key); err != nil {
			return false, err
		}
	}
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	// 存储操作期间会话或权限可能被撤销，提交记录删除前再次认证。
	actor, err := a.authenticate(r)
	if err != nil {
		return false, err
	}
	if !actor.Has(FileManage) {
		return false, forbidden()
	}
	r = r.WithContext(withPrincipal(r.Context(), actor))
	current, err := a.ownedShare(r)
	if err != nil {
		return false, err
	}
	if shareVersion(current) != shareVersion(share) {
		return false, &apiError{409, "SHARE_CHANGED", "删除期间分享已变化，记录仍保留，请刷新后重试"}
	}
	if err := a.deleteResource(r.Context(), "shares", id); err != nil {
		return false, err
	}
	return true, nil
}
