package server

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	ip2location "github.com/ip2location/ip2location-go/v9"
)

// 只返回国家和省级归属地，不暴露数据库中的城市、坐标或本地文件路径。
type ipLocation struct {
	CountryCode string              `json:"country_code,omitempty"`
	Country     string              `json:"country,omitempty"`
	Region      string              `json:"region,omitempty"`
	Summary     string              `json:"summary"`
	Source      string              `json:"source,omitempty"`
	database    *ipLocationDatabase // 仅用于切换库时识别过期查询，不参与 JSON 或日志。
}

type ipLocationDatabase struct {
	db       *ip2location.DB
	filename string
	date     string
	size     int64
}

func openIPLocation(path string) (*ipLocationDatabase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 IP2Location 数据库失败: %w", err)
	}
	info, err := f.Stat()
	var header [64]byte
	if err != nil || !info.Mode().IsRegular() || info.Size() < 64 || info.Size() > 512<<20 {
		_ = f.Close()
		return nil, errors.New("IP2Location 数据库必须为不超过 512 MiB 的普通 BIN 文件")
	}
	// 提前校验格式，避免 ZIP、CSV 或其他 DB 类型进入 SDK 的字段偏移计算。
	if _, err = f.ReadAt(header[:], 0); err != nil || header[0] != 3 || header[1] != 4 || binary.LittleEndian.Uint32(header[13:17]) != 0 {
		_ = f.Close()
		return nil, errors.New("请选择解压后的 IP2Location LITE DB3 IPv4 BIN 文件")
	}
	// 数据行及索引必须位于文件内部，拒绝截断文件和越界的元数据。
	rows := uint64(binary.LittleEndian.Uint32(header[5:9]))
	base := uint64(binary.LittleEndian.Uint32(header[9:13]))
	index := uint64(binary.LittleEndian.Uint32(header[21:25]))
	if rows == 0 || base < 65 || base-1+(rows+1)*16 > uint64(info.Size()) || (index != 0 && index-1+(1<<16)*8 > uint64(info.Size())) {
		_ = f.Close()
		return nil, errors.New("IP2Location 数据库内容不完整")
	}
	// 官方 SDK 使用 ReadAt 按需读取，多个连接可共享只读句柄，不缓存整个数据库。
	db, err := ip2location.OpenDBWithReader(f)
	if err != nil {
		return nil, fmt.Errorf("读取 IP2Location 数据库失败: %w", err)
	}
	return &ipLocationDatabase{db: db, date: fmt.Sprintf("%04d-%02d-%02d", 2000+int(header[2]), header[3], header[4]), size: info.Size()}, nil
}

func (a *App) locateIP(raw string) (location *ipLocation) {
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return
	}
	ip = ip.Unmap()
	// 未手动加载数据库或来源为 IPv6 时不提供归属地字段，也不调用 SDK。
	if !ip.Is4() {
		return nil
	}
	a.ipLocationMu.RLock()
	defer a.ipLocationMu.RUnlock()
	if a.ipLocationDB == nil {
		return nil
	}
	database := a.ipLocationDB
	location = &ipLocation{Summary: "未知", database: database}
	if ip.IsLoopback() {
		location.Summary = "本机"
		return
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		location.Summary = "局域网"
		return
	}
	if !ip.IsGlobalUnicast() {
		return
	}
	// 损坏的数据文件不得导致连接处理器崩溃；错误结果统一降级为未知。
	defer func() {
		if recover() != nil {
			location = &ipLocation{Summary: "未知", database: database}
		}
	}()
	record, err := database.db.Get_all(ip.String())
	if err != nil || len(record.Country_short) != 2 {
		return
	}
	code := record.Country_short
	if code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return
	}
	country := locationText(record.Country_long)
	if country == "" {
		return
	}
	region := locationText(record.Region)
	if code == "CN" {
		country = "中国"
		if translated := chinaRegions[strings.ToLower(region)]; translated != "" {
			region = translated
		}
	}
	location = &ipLocation{CountryCode: code, Country: country, Region: region, Summary: country, Source: "ip2location", database: database}
	if region != "" && region != country {
		location.Summary += " · " + region
	}
	return
}

func locationText(value string) string {
	value = strings.TrimSpace(value)
	if value == "-" || len(value) > 128 || !utf8.ValidString(value) || strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
	}) {
		return ""
	}
	return value
}

// DB3 使用英文省名，仅对中国省级行政区做固定映射，不推测缺失的省份。
var chinaRegions = map[string]string{
	"anhui": "安徽", "beijing": "北京", "chongqing": "重庆", "fujian": "福建",
	"gansu": "甘肃", "guangdong": "广东", "guangxi": "广西", "guizhou": "贵州",
	"hainan": "海南", "hebei": "河北", "heilongjiang": "黑龙江", "henan": "河南",
	"hubei": "湖北", "hunan": "湖南", "jiangsu": "江苏", "jiangxi": "江西",
	"jilin": "吉林", "liaoning": "辽宁", "nei mongol": "内蒙古", "inner mongolia": "内蒙古",
	"ningxia": "宁夏", "qinghai": "青海", "shaanxi": "陕西", "shandong": "山东",
	"shanghai": "上海", "shanxi": "山西", "sichuan": "四川", "tianjin": "天津",
	"xinjiang": "新疆", "xizang": "西藏", "tibet": "西藏", "yunnan": "云南", "zhejiang": "浙江",
	"hong kong": "香港", "macao": "澳门", "macau": "澳门", "taiwan": "台湾",
}
