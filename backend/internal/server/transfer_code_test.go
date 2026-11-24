package server

import (
	"regexp"
	"strings"
	"testing"
)

// 创建接口返回四位数字；加入同时支持显示空格、横杠和粘贴空白。
func TestTransferCodeFormatAndParsing(t *testing.T) {
	for _, variant := range []string{"compact", "display", "clipboard"} {
		t.Run(variant, func(t *testing.T) {
			f := newFixture(t)
			created := dataMap(t, f.request(t, "POST", "/api/transfer/create", f.admin, map[string]any{"name": "owner"}, nil, 201))
			code := created["code"].(string)
			if !regexp.MustCompile(`^[0-9]{4}$`).MatchString(code) {
				t.Fatalf("配对码格式错误: %s", code)
			}
			f.app.transfers.mu.Lock()
			for _, room := range f.app.transfers.rooms {
				room.members[0].connected = true
			}
			f.app.transfers.mu.Unlock()
			for _, invalid := range []string{"", "12345678", "A1234", "123", "12345", "12a4"} {
				f.request(t, "POST", "/api/transfer/join", "", map[string]any{"code": invalid, "name": "guest"}, nil, 404)
			}
			if variant != "compact" {
				code = code[:2] + "-" + code[2:]
			}
			if variant == "clipboard" {
				code = " \n" + strings.ReplaceAll(code, "-", " ") + " \t"
			}
			f.request(t, "POST", "/api/transfer/join", "", map[string]any{"code": code, "name": "guest"}, nil, 200)
			// 同一配对码只允许预留一位加入者，不能因显示格式不同而重复占位。
			f.request(t, "POST", "/api/transfer/join", "", map[string]any{"code": created["code"], "name": "another"}, nil, 404)
		})
	}
}
