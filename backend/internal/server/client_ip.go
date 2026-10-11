package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type proxyDNSCache struct {
	addresses []netip.Addr
	expires   time.Time
}

// 主机名只能来自管理员配置的受信基础设施，例如 Docker 内部 DNS 的 frontend。
// 缓存仅为这些固定名字分配空间，客户端请求不能添加 DNS 查询目标。
type trustedProxySet struct {
	prefixes []netip.Prefix
	hosts    []string
	mu       sync.Mutex
	cache    map[string]proxyDNSCache
	lookup   func(context.Context, string, string) ([]netip.Addr, error)
}

func parseTrustedProxies(value string) (*trustedProxySet, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	entries := strings.Split(value, ",")
	if len(entries) > 32 {
		return nil, errors.New("TRUSTED_PROXIES 最多包含 32 个代理地址、网段或主机名")
	}
	proxies := &trustedProxySet{cache: make(map[string]proxyDNSCache), lookup: net.DefaultResolver.LookupNetIP}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if address, err := netip.ParseAddr(entry); err == nil {
			address = address.Unmap()
			if address.IsUnspecified() || address.Zone() != "" {
				return nil, errors.New("TRUSTED_PROXIES 必须填写明确的代理地址")
			}
			proxies.prefixes = append(proxies.prefixes, netip.PrefixFrom(address, address.BitLen()))
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			if prefix.Addr().Is4In6() {
				if prefix.Bits() <= 96 {
					return nil, errors.New("TRUSTED_PROXIES 不允许信任所有客户端")
				}
				prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
			}
			if prefix.Bits() == 0 {
				return nil, errors.New("TRUSTED_PROXIES 不允许信任所有客户端")
			}
			proxies.prefixes = append(proxies.prefixes, prefix.Masked())
			continue
		}
		if !validProxyHostname(entry) {
			return nil, errors.New("TRUSTED_PROXIES 包含无效的代理地址、网段或主机名")
		}
		proxies.hosts = append(proxies.hosts, strings.ToLower(entry))
	}
	return proxies, nil
}

func validProxyHostname(value string) bool {
	if len(value) == 0 || len(value) > 253 {
		return false
	}
	hasLetter := false
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			letter := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
			hasLetter = hasLetter || letter
			if !letter && !(ch >= '0' && ch <= '9') && ch != '-' {
				return false
			}
		}
	}
	// 纯数字形式不能作为无效 IP 的主机名替代品。
	return hasLetter
}

func (p *trustedProxySet) contains(ctx context.Context, address netip.Addr) bool {
	if p == nil {
		return false
	}
	for _, prefix := range p.prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	for _, host := range p.hosts {
		for _, resolved := range p.resolve(ctx, host) {
			if resolved.Unmap() == address {
				return true
			}
		}
	}
	return false
}

func (p *trustedProxySet) resolve(ctx context.Context, host string) []netip.Addr {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.cache[host]
	if time.Now().Before(entry.expires) {
		return entry.addresses
	}
	// 不在启动时解析，允许依赖后端健康检查的前端稍后启动；失败不沿用旧信任。
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	addresses, err := p.lookup(ctx, "ip", host)
	ttl := 30 * time.Second
	if err != nil {
		addresses = nil
		ttl = time.Second
	}
	p.cache[host] = proxyDNSCache{addresses: addresses, expires: time.Now().Add(ttl)}
	return addresses
}

func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap().WithZone("")
	forwarded := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if forwarded == "" || len(forwarded) > 8192 {
		return peer.String()
	}
	chain := strings.Split(forwarded, ",")
	if len(chain) > 32 {
		return peer.String()
	}
	// 同一次解析最多等待一秒；从可信链右端逐跳检查，忽略攻击者可伪造的左端。
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	current := peer
	for i := len(chain) - 1; i >= 0; i-- {
		if !a.cfg.TrustedProxies.contains(ctx, current) {
			return current.String()
		}
		address, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil || address.Zone() != "" || address.Unmap().IsUnspecified() {
			return peer.String()
		}
		current = address.Unmap()
	}
	return current.String()
}
