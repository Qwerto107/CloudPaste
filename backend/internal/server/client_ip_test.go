package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func testTrustedProxies(t *testing.T, value string) *trustedProxySet {
	t.Helper()
	proxies, err := parseTrustedProxies(value)
	if err != nil {
		t.Fatal(err)
	}
	return proxies
}

func TestTrustedProxyClientIP(t *testing.T) {
	for _, test := range []struct{ name, trusted, peer, forwarded, want string }{
		{"default ignores forwarding", "", "192.0.2.10:1234", "198.51.100.7", "192.0.2.10"},
		{"untrusted peer", "192.0.2.11", "192.0.2.10:1234", "198.51.100.7", "192.0.2.10"},
		{"trusted peer", "192.0.2.10", "192.0.2.10:1234", "198.51.100.7", "198.51.100.7"},
		{"trusted network", "192.0.2.0/24", "192.0.2.10:1234", "198.51.100.7", "198.51.100.7"},
		{"ignore forged leftmost", "192.0.2.10", "192.0.2.10:1234", "203.0.113.9, 198.51.100.7", "198.51.100.7"},
		{"trusted chain", "192.0.2.10,192.0.2.11", "192.0.2.10:1234", "198.51.100.7, 192.0.2.11", "198.51.100.7"},
		{"ipv6 canonical form", "2001:db8::10", "[2001:db8::10]:1234", "2001:db8:0:1:0:0:0:7", "2001:db8:0:1::7"},
		{"mapped ipv4 peer", "192.0.2.10", "[::ffff:192.0.2.10]:1234", "::ffff:198.51.100.7", "198.51.100.7"},
		{"mapped ipv4 network", "::ffff:192.0.2.0/120", "192.0.2.10:1234", "198.51.100.7", "198.51.100.7"},
		{"missing header", "192.0.2.10", "192.0.2.10:1234", "", "192.0.2.10"},
		{"invalid rightmost", "192.0.2.10", "192.0.2.10:1234", "198.51.100.7, invalid", "192.0.2.10"},
		{"unspecified address", "192.0.2.10", "192.0.2.10:1234", "::ffff:0.0.0.0", "192.0.2.10"},
		{"excessive chain", "192.0.2.10", "192.0.2.10:1234", strings.Repeat("192.0.2.10,", 32) + "198.51.100.7", "192.0.2.10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := &App{cfg: Config{TrustedProxies: testTrustedProxies(t, test.trusted)}}
			r := httptest.NewRequest("GET", "/api/health", nil)
			r.RemoteAddr = test.peer
			r.Header.Set("X-Forwarded-For", test.forwarded)
			if got := a.clientIP(r); got != test.want {
				t.Fatalf("clientIP = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTrustedProxyDNSLifecycle(t *testing.T) {
	proxies := testTrustedProxies(t, "frontend")
	a := &App{cfg: Config{TrustedProxies: proxies}}
	r := httptest.NewRequest("GET", "/api/health", nil)
	r.RemoteAddr = "192.0.2.10:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	address := ""
	calls := 0
	proxies.lookup = func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		calls++
		if network != "ip" || host != "frontend" {
			t.Fatalf("unexpected DNS target: %s %s", network, host)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > time.Second {
			t.Fatal("DNS lookup has no bounded deadline")
		}
		if address == "" {
			return nil, errors.New("frontend not ready")
		}
		return []netip.Addr{netip.MustParseAddr(address)}, nil
	}
	if got := a.clientIP(r); got != "192.0.2.10" {
		t.Fatalf("DNS failure trusted forwarding: %s", got)
	}
	// 前端健康检查通过后才启动；短暂失败缓存到期后应恢复真实来源。
	proxies.cache["frontend"] = proxyDNSCache{}
	address = "192.0.2.10"
	for i := 0; i < 3; i++ {
		if got := a.clientIP(r); got != "198.51.100.7" {
			t.Fatalf("frontend address was not trusted: %s", got)
		}
	}
	if calls != 2 || len(proxies.cache) != 1 {
		t.Fatalf("DNS cache was not reused: calls=%d entries=%d", calls, len(proxies.cache))
	}
	// 容器重建后的地址替换旧地址；解析失败也不能继续信任过期地址。
	proxies.cache["frontend"] = proxyDNSCache{}
	address = "192.0.2.11"
	if got := a.clientIP(r); got != "192.0.2.10" {
		t.Fatalf("old frontend address remained trusted: %s", got)
	}
	r.RemoteAddr = "192.0.2.11:1234"
	if got := a.clientIP(r); got != "198.51.100.7" {
		t.Fatalf("new frontend address was not trusted: %s", got)
	}
	entry := proxies.cache["frontend"]
	entry.expires = time.Now().Add(-time.Second)
	proxies.cache["frontend"] = entry
	address = ""
	if got := a.clientIP(r); got != "192.0.2.11" {
		t.Fatalf("failed DNS retained expired trust: %s", got)
	}
}

func TestTrustedProxyPasswordBudgetsRemainIndependent(t *testing.T) {
	f := newFixture(t)
	f.app.cfg.TrustedProxies = testTrustedProxies(t, "127.0.0.1")
	attacker := map[string]string{"X-Forwarded-For": "198.51.100.7"}
	for i := 0; i < 10; i++ {
		f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "missing-user", "password": "wrong"}, attacker, 401)
	}
	for i := 0; i < 60; i++ {
		f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "missing-user", "password": "wrong"}, attacker, 429)
	}
	// 单个来源超限不会占用其他真实来源的尝试次数。
	f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "admin", "password": testPassword}, map[string]string{"X-Forwarded-For": "203.0.113.8"}, 200)
	if got := f.app.adminPasswordAttempts.byIP["198.51.100.7"].Count; got != 10 {
		t.Fatalf("rejected attempts changed source budget: count=%d", got)
	}
	if got := f.app.adminPasswordAttempts.byIP["203.0.113.8"].Count; got != 1 {
		t.Fatalf("fresh source did not get its own budget: count=%d", got)
	}
}

func TestTrustedProxyConfiguration(t *testing.T) {
	for _, value := range []string{"0.0.0.0/0", "::/0", "::ffff:0.0.0.0/96", "*", "0.0.0.0", "::", "999.1.1.1", "192.0.2.1/99", "https://frontend", "frontend:8080", "frontend,", "-frontend", "front_end", strings.Repeat("frontend,", 32) + "frontend"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", value)
			if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXIES") {
				t.Fatalf("invalid proxy configuration accepted: %q, %v", value, err)
			}
		})
	}
	t.Setenv("TRUSTED_PROXIES", "")
	config, err := ConfigFromEnv()
	if err != nil || config.TrustedProxies != nil {
		t.Fatalf("default must not trust forwarding: %v", err)
	}
	t.Setenv("TRUSTED_PROXIES", "frontend,127.0.0.1,2001:db8::/64")
	if _, err := ConfigFromEnv(); err != nil {
		t.Fatalf("valid proxy configuration rejected: %v", err)
	}
}
