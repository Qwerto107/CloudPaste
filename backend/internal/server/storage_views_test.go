package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStorageQueriesRequireFilePermission(t *testing.T) {
	f := newFixture(t)
	id, _ := f.local(t, t.TempDir(), "/disk")
	for _, permissions := range []int{0, TextShare, TextManage, FileShare, FileManage, WebDAVRead} {
		auth, _ := f.key(t, "unrelated", "/unrelated", permissions)
		f.request(t, "GET", "/api/storage", auth, nil, nil, 403)
		f.request(t, "GET", "/api/storage/"+id+"?reveal=plain", auth, nil, nil, 403)
	}
	for _, permissions := range []int{MountView, MountUpload} {
		auth, _ := f.key(t, "file-user", "/disk", permissions)
		f.request(t, "GET", "/api/storage", auth, nil, nil, 200)
		f.request(t, "GET", "/api/storage/"+id, auth, nil, nil, 200)
	}
}

// 返回所需的存储选择与能力字段，但不暴露路径、上游地址、账号或管理备注。
func TestUserStorageResponsesUseFieldAllowlist(t *testing.T) {
	f := newFixture(t)
	localID, _ := f.local(t, t.TempDir(), "/disk")
	f.request(t, "PUT", "/api/storage/"+localID, f.admin, map[string]any{"remark": "internal-local-note"}, nil, 200)
	s3 := dataMap(t, f.request(t, "POST", "/api/storage", f.admin, map[string]any{
		"name": "s3", "storage_type": "S3", "endpoint_url": "https://internal-s3.test",
		"bucket_name": "internal-bucket", "region": "internal-region", "remark": "internal-s3-note",
		"access_key_id": "internal-access", "secret_access_key": "internal-secret", "is_public": true,
	}, nil, 201))
	auth, _ := f.key(t, "reader", "/disk", MountView)
	allowed := map[string]bool{"id": true, "name": true, "storage_type": true, "is_public": true, "is_default": true, "status": true, "readonly": true, "capabilities": true, "upload_protocol": true}
	check := func(view map[string]any) {
		t.Helper()
		for field := range view {
			if !allowed[field] {
				t.Fatalf("普通用户返回了内部配置字段 %q", field)
			}
		}
		if view["id"] == nil || view["storage_type"] == nil || view["capabilities"] == nil || view["upload_protocol"] != "go-proxy" {
			t.Fatalf("缺少上传或浏览所需的字段: %v", view)
		}
	}
	for _, id := range []string{localID, s3["id"].(string)} {
		check(dataMap(t, f.request(t, "GET", "/api/storage/"+id+"?reveal=plain", auth, nil, nil, 200)))
	}
	list := f.request(t, "GET", "/api/storage", auth, nil, nil, 200)
	var result struct {
		Data struct {
			Items []map[string]any
		}
	}
	if err := json.Unmarshal(list.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data.Items) != 2 {
		t.Fatalf("普通存储列表返回 %d 项", len(result.Data.Items))
	}
	for _, view := range result.Data.Items {
		check(view)
	}
	if strings.Contains(list.Body.String(), "internal-") {
		t.Fatal("列表返回了内部配置值")
	}
	// 管理员仍可编辑完整配置，并通过已有 reveal 入口查看凭据。
	admin := dataMap(t, f.request(t, "GET", "/api/storage/"+s3["id"].(string)+"?reveal=plain", f.admin, nil, nil, 200))
	if admin["bucket_name"] != "internal-bucket" || admin["secret_access_key"] != "internal-secret" {
		t.Fatal("管理员完整配置或凭据查询被破坏")
	}
	local := dataMap(t, f.request(t, "GET", "/api/storage/"+localID, f.admin, nil, nil, 200))
	if local["root_path"] == nil || local["remark"] != "internal-local-note" {
		t.Fatal("管理员无法读取本地配置")
	}
}

func TestSanitizedStorageQueriesStillEnforceACLAndPublicFlag(t *testing.T) {
	f := newFixture(t)
	first, _ := f.local(t, t.TempDir(), "/first")
	second, _ := f.local(t, t.TempDir(), "/second")
	auth, keyID := f.key(t, "reader", "/", MountView)
	f.request(t, "PUT", "/api/admin/api-keys/"+keyID+"/storage-acl", f.admin, map[string]any{"storage_config_ids": []string{first}}, nil, 200)
	f.request(t, "GET", "/api/storage/"+first, auth, nil, nil, 200)
	f.request(t, "GET", "/api/storage/"+second, auth, nil, nil, 404)
	f.request(t, "PUT", "/api/storage/"+first, f.admin, map[string]any{"is_public": false}, nil, 200)
	f.request(t, "GET", "/api/storage/"+first, auth, nil, nil, 404)
	list := dataMap(t, f.request(t, "GET", "/api/storage", auth, nil, nil, 200))
	if list["total"] != float64(0) {
		t.Fatal("配置白名单绕过了存储 ACL 或公开标记")
	}
}
