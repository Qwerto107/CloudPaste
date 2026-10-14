package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestVisitorPasswordAttemptsDoNotBlockAdminLogin(t *testing.T) {
	f := newFixture(t)
	f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "protected-text", "content": "secret", "password": "share-password"}, nil, 201)
	// 访客与管理员由同一个代理转发，仍应有各自的密码尝试限额。
	for i := 0; i < 10; i++ {
		f.request(t, "POST", "/api/paste/protected-text", "", map[string]any{"password": "wrong"}, nil, 403)
	}
	f.request(t, "POST", "/api/paste/protected-text", "", map[string]any{"password": "share-password"}, nil, 429)
	f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "admin", "password": testPassword}, nil, 200)
}

func TestAdminLoginAttemptsDoNotBlockVisitorPassword(t *testing.T) {
	f := newFixture(t)
	f.request(t, "POST", "/api/paste", f.admin, map[string]any{"slug": "protected-text", "content": "secret", "password": "share-password"}, nil, 201)
	// 初始化登录已消耗一次，另外九次错误登录应触发管理侧限流。
	for i := 0; i < 9; i++ {
		f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "admin", "password": "wrong"}, nil, 401)
	}
	f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "admin", "password": testPassword}, nil, 429)
	f.request(t, "POST", "/api/paste/protected-text", "", map[string]any{"password": "share-password"}, nil, 200)
}

func TestPasswordBudgetsAreIndependentPerIP(t *testing.T) {
	f := newFixture(t)
	// 多个 IP 各自使用限额，不再受原先的全站 60 次限制。
	for i := 0; i < 120; i++ {
		r := httptest.NewRequest("POST", "/api/paste/example", nil)
		r.RemoteAddr = fmt.Sprintf("198.51.100.%d:1234", i/10+1)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i))
		if !f.app.allowPasswordAttempt(r) {
			t.Fatalf("visitor request %d rejected too early", i)
		}
	}
	r := httptest.NewRequest("POST", "/api/paste/example", nil)
	r.RemoteAddr = "198.51.100.200:1234"
	if !f.app.allowPasswordAttempt(r) {
		t.Fatal("other IPs exhausted a fresh IP allowance")
	}
	admin := httptest.NewRequest("POST", "/api/admin/login", nil)
	admin.RemoteAddr = r.RemoteAddr
	admin = admin.WithContext(context.WithValue(admin.Context(), adminScopeKey{}, true))
	if !f.app.allowPasswordAttempt(admin) {
		t.Fatal("visitor attempts exhausted admin allowance")
	}
	r.RemoteAddr = "198.51.100.1:1234"
	if f.app.allowPasswordAttempt(r) {
		t.Fatal("per-IP password limit not enforced")
	}
}

func TestUserDAVDoesNotVerifyAdminPasswordOrConsumeAdminBudget(t *testing.T) {
	f := newFixture(t)
	for _, password := range []string{"wrong-password", testPassword} {
		basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:"+password))
		for i := 0; i < 12; i++ {
			f.request(t, "GET", "/dav/file.txt", basic, nil, nil, 404)
		}
	}
	f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "admin", "password": testPassword}, nil, 200)
}
