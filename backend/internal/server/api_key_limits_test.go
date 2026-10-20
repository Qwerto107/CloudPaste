package server

import (
	"encoding/base64"
	"errors"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestAPIKeyFailureBudgetSharedAcrossFormats(t *testing.T) {
	f := newFixture(t)
	f.app.cfg.RateLimits.PasswordPerIP = 3
	key, _ := f.key(t, "valid", "/", 0)
	request := func(ip string, headers map[string]string, want int) {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/test/api-key", nil)
		r.RemoteAddr = net.JoinHostPort(ip, "1234")
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		requireRateStatus(t, w, want)
	}
	ip := "198.51.100.1"
	request(ip, map[string]string{"Authorization": "ApiKey wrong"}, 401)
	request(ip, map[string]string{"Authorization": "ApiKeyBase64 " + base64.StdEncoding.EncodeToString([]byte("wrong"))}, 401)
	request(ip, map[string]string{"X-Custom-Auth-Key": "wrong"}, 401)
	request(ip, map[string]string{"Authorization": key, "X-Forwarded-For": "203.0.113.5"}, 429)
	request("198.51.100.2", map[string]string{"Authorization": key}, 200)
	// 猜测密钥不能耗尽同一来源的管理员登录和分享密码额度。
	requireRateStatus(t, rateLimitRequest(t, f, "POST", "/api/admin/login", ip, "", map[string]any{"username": "admin", "password": testPassword}), 200)
	r := httptest.NewRequest("POST", "/api/paste/example", nil)
	r.RemoteAddr = net.JoinHostPort(ip, "1234")
	if !f.app.allowPasswordAttempt(r) {
		t.Fatal("API Key failures consumed guest password budget")
	}
	f.app.passwordAttemptMu.Lock()
	f.app.apiKeyFailures.byKey[ip].Started = time.Now().Add(-time.Minute)
	f.app.passwordAttemptMu.Unlock()
	request(ip, map[string]string{"Authorization": key}, 200)
}

func TestAPIKeySuccessAndPermissionDeniedDoNotConsumeBudget(t *testing.T) {
	f := newFixture(t)
	f.app.cfg.RateLimits.PasswordPerIP = 2
	f.request(t, "POST", "/api/admin/api-keys", f.admin, map[string]any{"name": "short", "custom_key": "abc123", "permissions": 0}, nil, 201)
	for i := 0; i < 5; i++ {
		requireRateStatus(t, rateLimitRequest(t, f, "GET", "/api/test/api-key", "198.51.100.1", "ApiKey abc123", nil), 200)
		requireRateStatus(t, rateLimitRequest(t, f, "GET", "/api/pastes", "198.51.100.1", "ApiKey abc123", nil), 403)
	}
	f.app.passwordAttemptMu.Lock()
	defer f.app.passwordAttemptMu.Unlock()
	if f.app.apiKeyFailures != nil {
		t.Fatal("valid keys created failure counters")
	}
}

func TestAPIKeyMalformedAndDisabledKeysShareIPv6PrefixBudget(t *testing.T) {
	f := newFixture(t)
	f.app.cfg.RateLimits.PasswordPerIP = 3
	f.request(t, "POST", "/api/admin/api-keys", f.admin, map[string]any{"name": "disabled", "custom_key": "disabled-key", "is_enable": 0}, nil, 201)
	for _, step := range []struct {
		ip, auth string
		status   int
	}{
		{"2001:db8:1:2::1", "ApiKey", 401},
		{"2001:db8:1:2::2", "ApiKeyBase64 %%%", 401},
		{"2001:db8:1:2::3", "ApiKey disabled-key", 401},
		{"2001:db8:1:2::4", "ApiKey another", 429},
		{"2001:db8:1:3::1", "ApiKey another", 401},
	} {
		requireRateStatus(t, rateLimitRequest(t, f, "GET", "/api/test/api-key", step.ip, step.auth, nil), step.status)
	}
}

func TestAPIKeyConcurrentFailureLimit(t *testing.T) {
	f := newFixture(t)
	f.app.cfg.RateLimits.PasswordPerIP = 10
	const requests = 32
	statuses := make(chan int, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", "/api/test/api-key", nil)
			r.RemoteAddr = "198.51.100.1:1234"
			r.Header.Set("Authorization", "ApiKey wrong")
			_, err := f.app.authenticate(r)
			var failure *apiError
			if !errors.As(err, &failure) {
				statuses <- 0
				return
			}
			statuses <- failure.Status
		}()
	}
	wg.Wait()
	close(statuses)
	counts := make(map[int]int)
	for status := range statuses {
		counts[status]++
	}
	if counts[401] != 10 || counts[429] != requests-10 {
		t.Fatal("concurrent failures exceeded configured budget", counts)
	}
}
