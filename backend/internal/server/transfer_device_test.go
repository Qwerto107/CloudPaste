package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTransferDeviceValidation(t *testing.T) {
	for _, device := range []*transferDeviceInput{
		nil, {}, {Platform: "Windows", PlatformVersion: "15.0.0"}, {Model: strings.Repeat("机", 32)},
	} {
		if err := device.validate(); err != nil {
			t.Fatal("拒绝了有效或缺失的原始提示", err)
		}
	}
	for _, device := range []*transferDeviceInput{
		{Platform: strings.Repeat("x", 33)}, {Model: strings.Repeat("机", 33)},
		{Platform: "Linux\nforged-log"}, {Model: "设备\u202eexe"}, {PlatformVersion: "1\x00"},
		{TouchPoints: -1}, {Brands: make([]transferDeviceBrand, 9)},
	} {
		if err := device.validate(); err == nil {
			t.Fatal("接受了无效或无界的原始提示", device)
		}
	}
}

// 识别和摘要由后端完成，原始 UA 不进入响应、房间或日志对象。
func TestTransferDeviceParsedOnServer(t *testing.T) {
	device := parseTransferDevice("private-UA (Windows NT 10.0) Chrome/140.0.0.0", &transferDeviceInput{
		Platform: "Windows", PlatformVersion: "15.0.0", FormFactors: []string{"Desktop"},
		FullVersionList: []transferDeviceBrand{{Brand: "Not;A Brand", Version: "99"}, {Brand: "Google Chrome", Version: "140.0.1.2"}},
	})
	if device == nil || device.Summary != "电脑 · Windows 11 · Chrome 140.0.1.2" {
		t.Fatal("后端摘要不正确", device)
	}
	raw, _ := json.Marshal(device)
	if strings.Contains(string(raw), "private-UA") {
		t.Fatal("响应泄露原始 UA")
	}
	windows := parseTransferDevice("(Windows NT 10.0) Firefox/140.0", nil)
	if windows.OSVersion != "" {
		t.Fatal("仅凭 UA 猜测了 Windows 版本")
	}
	ipad := parseTransferDevice("(Macintosh; Intel Mac OS X 10_15_7) Version/18.0 Safari", &transferDeviceInput{TouchPoints: 5})
	if ipad.Type != "tablet" || ipad.OS != "iPadOS" || ipad.OSVersion != "" {
		t.Fatal("桌面模式 iPad 识别错误", ipad)
	}
}

// 前端只能上传原始提示，不能直接指定已识别结果、来源 IP 或管理员权限。
func TestTransferDeviceRejectsExtraFields(t *testing.T) {
	f := newFixture(t)
	for _, device := range []map[string]any{
		{"platform": "Windows", "ip": "203.0.113.1"}, {"summary": "管理员可信设备"},
		{"browser": "Chrome"}, {"admin": true}, {"model": strings.Repeat("x", 97)},
	} {
		f.request(t, "POST", "/api/transfer/create", f.admin, map[string]any{"name": "设备", "device": device}, nil, 400)
		f.request(t, "POST", "/api/transfer/join", "", map[string]any{"name": "设备", "code": "A1234", "device": device}, nil, 400)
	}
}
