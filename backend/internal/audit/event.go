package audit

import (
	"strings"
	"unicode"
)

// Event 操作日志事件只接收明确挑选的字段，绝不截取请求体、响应体或完整 URL。
type Event struct {
	ID         int64  `json:"id"`
	Time       int64  `json:"time"`
	Module     string `json:"module"`
	Action     string `json:"action"`
	ActorType  string `json:"actor_type"`
	ActorID    string `json:"actor_id"`
	ActorName  string `json:"actor_name"`
	IP         string `json:"ip"`
	Target     string `json:"target"`
	Method     string `json:"method"`
	Endpoint   string `json:"endpoint"`
	Status     int    `json:"status"`
	Result     string `json:"result"`
	DurationMS int64  `json:"duration_ms"`
	Details    string `json:"details"`
}

// ModuleBits 定义各审计模块的位图掩码，请求路径可无锁原子判断开关。
var ModuleBits = map[string]uint32{
	"auth":     1 << 0,
	"text":     1 << 1,
	"file":     1 << 2,
	"transfer": 1 << 3,
	"storage":  1 << 4,
	"keys":     1 << 5,
	"settings": 1 << 6,
}

// SanitizeText 替换控制字符并按字符数截断，防止日志注入或超长字段。
func SanitizeText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
