package transfer

import (
	"net"
	"strconv"
	"strings"
)

const DefaultSTUN = "stun:stun.miwifi.com:3478\n" +
	"stun:stun.hitv.com:3478\n" +
	"stun:stun.cloudflare.com:3478\n" +
	"stun:stun.l.google.com:19302\n" +
	"stun:stun.nextcloud.com:3478"

// NormalizeSTUN 规范化 STUN 列表并限制数量，避免浏览器同时向过多服务器探测。
func NormalizeSTUN(value string) (string, bool) {
	if len(value) > 1284 {
		return "", false
	}
	servers := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !ValidSTUN(line) {
			return "", false
		}
		key := strings.ToLower(line)
		if seen[key] {
			continue
		}
		seen[key] = true
		servers = append(servers, line)
		if len(servers) > 5 {
			return "", false
		}
	}
	return strings.Join(servers, "\n"), true
}

func ValidSTUN(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 256 || !strings.HasPrefix(value, "stun:") || strings.ContainsAny(value, " /\\\r\n\t@?#") {
		return false
	}
	host, port, err := net.SplitHostPort(strings.TrimPrefix(value, "stun:"))
	number, portErr := strconv.ParseUint(port, 10, 16)
	if err != nil || portErr != nil || number == 0 || host == "" {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	return true
}
