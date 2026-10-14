package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func storageItems(t *testing.T, f *fixture, auth string) []map[string]any {
	t.Helper()
	response := f.request(t, "GET", "/api/storage", auth, nil, nil, 200)
	var result struct {
		Data struct{ Items []map[string]any }
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data.Items
}

func TestStorageQueriesRequireFilePermission(t *testing.T) {
	f := newFixture(t)
	f.local(t, t.TempDir(), "/disk")
	for _, permissions := range []int{0, TextShare, TextManage, FileShare, FileManage, WebDAVRead} {
		auth, _ := f.key(t, "unrelated", "/unrelated", permissions)
		f.request(t, "GET", "/api/storage", auth, nil, nil, 403)
	}
	for _, permissions := range []int{MountView, MountUpload} {
		auth, _ := f.key(t, "file-user", "/disk", permissions)
		storageItems(t, f, auth)
	}
}

// 存储列表只暴露上传选择所需字段，后台列表仍返回可编辑配置。
func TestUserStorageResponsesUseFieldAllowlist(t *testing.T) {
	f := newFixture(t)
	f.local(t, t.TempDir(), "/disk")
	f.request(t, "POST", "/api/storage", f.admin, map[string]any{"name": "s3", "storage_type": "S3", "endpoint_url": "https://internal-s3.test", "bucket_name": "internal-bucket", "region": "internal-region", "remark": "internal-note", "access_key_id": "internal-access", "secret_access_key": "internal-secret", "is_public": true}, nil, 201)
	auth, _ := f.key(t, "reader", "/disk", MountView)
	allowed := map[string]bool{"id": true, "name": true, "storage_type": true, "is_public": true, "is_default": true, "status": true, "readonly": true, "capabilities": true, "upload_protocol": true}
	items := storageItems(t, f, auth)
	if len(items) != 2 {
		t.Fatalf("列表返回 %d 项", len(items))
	}
	for _, item := range items {
		for field := range item {
			if !allowed[field] {
				t.Fatalf("普通用户返回了内部字段 %q", field)
			}
		}
		if item["upload_protocol"] != "go-proxy" {
			t.Fatal("缺少上传能力")
		}
	}
	body, _ := json.Marshal(items)
	if strings.Contains(string(body), "internal-") {
		t.Fatal("列表泄露内部配置值")
	}
	for _, item := range storageItems(t, f, f.admin) {
		if item["secret_access_key"] != nil {
			t.Fatal("管理员列表意外返回凭据")
		}
		if item["storage_type"] == "S3" && item["bucket_name"] != "internal-bucket" {
			t.Fatal("管理员不能编辑完整配置")
		}
	}
}

func TestSanitizedStorageQueriesStillEnforceACLAndPublicFlag(t *testing.T) {
	f := newFixture(t)
	first, _ := f.local(t, t.TempDir(), "/first")
	f.local(t, t.TempDir(), "/second")
	auth, keyID := f.key(t, "reader", "/", MountView)
	f.request(t, "PUT", "/api/admin/api-keys/"+keyID, f.admin, map[string]any{"storage_config_ids": []string{first}}, nil, 200)
	items := storageItems(t, f, auth)
	if len(items) != 1 || items[0]["id"] != first {
		t.Fatal("列表未应用存储 ACL")
	}
	f.request(t, "PUT", "/api/storage/"+first, f.admin, map[string]any{"is_public": false}, nil, 200)
	if len(storageItems(t, f, auth)) != 0 {
		t.Fatal("列表绕过公开标记")
	}
}

// 删除存储必须撤销历史挂载和分享，同时保留磁盘上的文件。
func TestRemovingStorageRevokesLegacySourcesWithoutDeletingFiles(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "file.txt", "stored content")
	storage, _ := f.local(t, root, "/disk")
	share := f.legacyShare(t, f.admin, map[string]any{"path": "/disk/file.txt"})
	f.request(t, "DELETE", "/api/storage/"+storage, f.admin, nil, nil, 200)
	if mounts, err := f.app.allMounts(t.Context()); err != nil || len(mounts) != 0 {
		t.Fatalf("历史挂载没有撤销: %v %v", mounts, err)
	}
	f.request(t, "GET", "/api/share/get/"+share["slug"].(string), "", nil, nil, 404)
	if content, err := os.ReadFile(filepath.Join(root, "file.txt")); err != nil || string(content) != "stored content" {
		t.Fatalf("删除存储修改了原文件: %q %v", content, err)
	}
}
