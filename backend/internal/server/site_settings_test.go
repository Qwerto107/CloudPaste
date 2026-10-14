package server

import (
	"encoding/json"
	"testing"
)

// 设置经过真实读写入口，并在重开数据库后检查，避免只更新前端开关。
func TestAnnouncementAndGitHubSettings(t *testing.T) {
	f := newFixture(t)
	read := func() map[string]string {
		response := f.request(t, "GET", "/api/public/settings?group=4", "", nil, nil, 200)
		var payload struct{ Data []setting }
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		values := map[string]string{}
		for _, item := range payload.Data {
			values[item.Key] = item.Value
		}
		return values
	}
	defaults := read()
	if defaults["site_announcement_default_open"] != "false" || defaults["site_github_enabled"] != "true" || defaults["site_github_url"] != "https://github.com/Qwerto107/CloudPaste" {
		t.Fatalf("incorrect defaults: %v", defaults)
	}
	for _, invalid := range []string{"javascript:alert(1)", "data:text/html,test", "//github.com/example/repo", "https://user:password@github.com/repo", "https://github.com/\ntest"} {
		f.request(t, "PUT", "/api/admin/settings/group/4", f.admin, map[string]any{"site_github_url": invalid}, nil, 400)
	}
	f.request(t, "PUT", "/api/admin/settings/group/4", f.admin, map[string]any{"site_announcement_default_open": "yes"}, nil, 400)
	f.request(t, "PUT", "/api/admin/settings/group/1", f.admin, map[string]any{"site_announcement_default_open": true}, nil, 400)
	f.request(t, "PUT", "/api/admin/settings/group/4", f.admin, map[string]any{
		"site_announcement_enabled": true, "site_announcement_default_open": true,
		"site_announcement_content": "公告内容", "site_github_enabled": false,
		"site_github_url": "https://github.com/Qwerto107/CloudPaste/issues",
	}, nil, 200)
	values := read()
	if values["site_announcement_default_open"] != "true" || values["site_announcement_content"] != "公告内容" || values["site_github_enabled"] != "false" {
		t.Fatalf("settings were not published: %v", values)
	}
	second, err := New(f.app.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var stored string
	if err := second.db.QueryRow("SELECT value FROM settings WHERE key=?", "site_announcement_default_open").Scan(&stored); err != nil || stored != "true" {
		t.Fatalf("setting not persisted: %s %v", stored, err)
	}
	f.request(t, "PUT", "/api/admin/settings/group/4", f.admin, map[string]any{"site_github_url": ""}, nil, 200)
}
