package server

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 原始提示只在解析请求时使用，不写入日志；归一化摘要也不能参与鉴权或覆盖 IP。
type transferDeviceInput struct {
	Platform        string                `json:"platform,omitempty"`
	PlatformVersion string                `json:"platform_version,omitempty"`
	Mobile          *bool                 `json:"mobile,omitempty"`
	Brands          []transferDeviceBrand `json:"brands,omitempty"`
	FullVersionList []transferDeviceBrand `json:"full_version_list,omitempty"`
	Model           string                `json:"model,omitempty"`
	FormFactors     []string              `json:"form_factors,omitempty"`
	TouchPoints     int                   `json:"touch_points,omitempty"`
}
type transferDeviceBrand struct {
	Brand   string `json:"brand"`
	Version string `json:"version"`
}
type transferDevice struct {
	Type           string `json:"type,omitempty"`
	Browser        string `json:"browser,omitempty"`
	BrowserVersion string `json:"browser_version,omitempty"`
	OS             string `json:"os,omitempty"`
	OSVersion      string `json:"os_version,omitempty"`
	Model          string `json:"model,omitempty"`
	Summary        string `json:"summary"`
}

func validDeviceText(value string, limit int) bool {
	return utf8.ValidString(value) && len(value) <= limit && !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
	})
}
func (d *transferDeviceInput) validate() error {
	if d == nil {
		return nil
	}
	// 字符串和数组均有界，拒绝控制符和双向覆盖符，防止日志注入及无界解析。
	if len(d.Brands) > 8 || len(d.FullVersionList) > 8 || len(d.FormFactors) > 4 || d.TouchPoints < 0 || d.TouchPoints > 100 {
		return badRequest("设备信息无效或过长")
	}
	for _, field := range []struct {
		value *string
		limit int
	}{
		{&d.Platform, 32}, {&d.PlatformVersion, 32}, {&d.Model, 96},
	} {
		if !validDeviceText(*field.value, field.limit) {
			return badRequest("设备信息无效或过长")
		}
		*field.value = strings.TrimSpace(*field.value)
	}
	for _, list := range [][]transferDeviceBrand{d.Brands, d.FullVersionList} {
		for _, brand := range list {
			if !validDeviceText(brand.Brand, 48) || !validDeviceText(brand.Version, 32) {
				return badRequest("设备信息无效或过长")
			}
		}
	}
	for _, factor := range d.FormFactors {
		if !validDeviceText(factor, 24) {
			return badRequest("设备信息无效或过长")
		}
	}
	return nil
}

var transferBrowserPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"Edge", regexp.MustCompile(`(?:Edg|EdgA|EdgiOS)/([\d.]+)`)},
	{"Opera", regexp.MustCompile(`OPR/([\d.]+)`)},
	{"Samsung Internet", regexp.MustCompile(`SamsungBrowser/([\d.]+)`)},
	{"Firefox", regexp.MustCompile(`(?:Firefox|FxiOS)/([\d.]+)`)},
	{"Chrome", regexp.MustCompile(`(?:Chrome|CriOS)/([\d.]+)`)},
	{"Safari", regexp.MustCompile(`Version/([\d.]+).*Safari`)},
}
var transferOSVersionPattern = regexp.MustCompile(`(?:CPU (?:iPhone )?OS |Android |Mac OS X )([\d._]+)`)

func parseTransferDevice(userAgent string, input *transferDeviceInput) *transferDevice {
	if input == nil {
		input = &transferDeviceInput{}
	}
	// 原始 UA 仅用于此处识别，不保存在房间、响应或操作日志中。
	ua := userAgent
	if len(ua) > 4096 {
		ua = ua[:4096]
	}
	d := &transferDevice{Type: "unknown", Model: input.Model}
	for _, browser := range transferBrowserPatterns {
		if match := browser.pattern.FindStringSubmatch(ua); match != nil {
			d.Browser, d.BrowserVersion = browser.name, match[1]
			break
		}
	}
	switch {
	case strings.Contains(ua, "iPad") || (strings.Contains(ua, "Macintosh") && input.TouchPoints > 1):
		d.Type, d.OS = "tablet", "iPadOS"
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPod"):
		d.Type, d.OS = "mobile", "iOS"
	case strings.Contains(ua, "Android"):
		d.Type, d.OS = "tablet", "Android"
		if strings.Contains(ua, "Mobile") {
			d.Type = "mobile"
		}
	case strings.Contains(ua, "Windows NT"):
		d.Type, d.OS = "desktop", "Windows"
	case strings.Contains(ua, "Macintosh"):
		d.Type, d.OS = "desktop", "macOS"
	case strings.Contains(ua, "CrOS"):
		d.Type, d.OS = "desktop", "Chrome OS"
	case strings.Contains(ua, "Linux"):
		d.Type, d.OS = "desktop", "Linux"
	}
	if match := transferOSVersionPattern.FindStringSubmatch(ua); match != nil && !(d.OS == "iPadOS" && strings.Contains(ua, "Macintosh")) {
		d.OSVersion = strings.ReplaceAll(match[1], "_", ".")
	}
	if input.Platform != "" && input.Platform != "Unknown" && d.OS != "iPadOS" {
		d.OS = input.Platform
	}
	if input.Mobile != nil && *input.Mobile {
		d.Type = "mobile"
	}
	for _, factor := range input.FormFactors {
		switch factor {
		case "Tablet":
			d.Type = "tablet"
		case "Desktop":
			d.Type = "desktop"
		case "Mobile":
			d.Type = "mobile"
		}
	}
	brands := input.FullVersionList
	if len(brands) == 0 {
		brands = input.Brands
	}
	// 过滤 GREASE 品牌，优先具体浏览器；仅有 Chromium 时保留 UA 中更具体的名称。
	brandFound := false
	for _, name := range []string{"Microsoft Edge", "Opera", "Google Chrome", "Chromium"} {
		for _, brand := range brands {
			if brand.Brand == name && (name != "Chromium" || d.Browser == "") {
				d.Browser, d.BrowserVersion, brandFound = name, brand.Version, true
				if name == "Microsoft Edge" {
					d.Browser = "Edge"
				}
				if name == "Google Chrome" {
					d.Browser = "Chrome"
				}
				break
			}
		}
		if brandFound {
			break
		}
	}
	if input.PlatformVersion != "" {
		d.OSVersion = input.PlatformVersion
		if d.OS == "Windows" {
			// Windows 提示是 API 合约版本，13 及以上对应 11，不能原样当成系统版本。
			major, _ := strconv.Atoi(strings.Split(input.PlatformVersion, ".")[0])
			d.OSVersion = ""
			if major >= 13 {
				d.OSVersion = "11"
			} else if major >= 1 && major <= 10 {
				d.OSVersion = "10"
			}
		}
	}
	// UA 内版本只包含 ASCII 数字和点，仍收紧长度以限制响应及日志大小。
	if len(d.BrowserVersion) > 32 {
		d.BrowserVersion = d.BrowserVersion[:32]
	}
	if len(d.OSVersion) > 32 {
		d.OSVersion = d.OSVersion[:32]
	}
	// 分离正则返回的子串，避免房间因保存版本号而间接持有完整 UA 的内存。
	d.BrowserVersion = strings.Clone(d.BrowserVersion)
	d.OSVersion = strings.Clone(d.OSVersion)
	if d.Type == "unknown" && d.OS == "" && d.Browser == "" && d.Model == "" {
		return nil
	}
	labels := []string{}
	if kind := map[string]string{"desktop": "电脑", "mobile": "手机", "tablet": "平板"}[d.Type]; kind != "" {
		labels = append(labels, kind)
	}
	if d.OS != "" {
		labels = append(labels, strings.TrimSpace(d.OS+" "+d.OSVersion))
	}
	if d.Browser != "" {
		labels = append(labels, strings.TrimSpace(d.Browser+" "+d.BrowserVersion))
	}
	if d.Model != "" {
		labels = append(labels, d.Model)
	}
	d.Summary = strings.Join(labels, " · ")
	return d
}
