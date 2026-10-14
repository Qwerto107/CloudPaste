package server

import (
	"fmt"
	"os"
	"strconv"
)

// 次数均按一分钟统计；WebSocket 消息按成员，其余按真实来源 IP。
type RateLimitConfig struct {
	PasswordPerIP             int
	TransferCreatePerIP       int
	TransferJoinPerIP         int
	TransferRelayPerIP        int
	TransferSocketPerIP       int
	TransferMessagesPerMember int
	TransferToolPerIP         int
}

// 环境配置与直接创建 App 共用默认值，避免测试或其他启动入口遗漏限频保护。
func (c *Config) configureRateLimits(readEnv bool) error {
	entries := []struct {
		name     string
		target   *int
		fallback int
	}{
		{"PASSWORD_ATTEMPTS_PER_MINUTE", &c.RateLimits.PasswordPerIP, 10},
		{"TRANSFER_CREATE_PER_MINUTE", &c.RateLimits.TransferCreatePerIP, 30},
		{"TRANSFER_JOIN_PER_MINUTE", &c.RateLimits.TransferJoinPerIP, 30},
		{"TRANSFER_RELAY_PER_MINUTE", &c.RateLimits.TransferRelayPerIP, 180},
		{"TRANSFER_WS_CONNECT_PER_MINUTE", &c.RateLimits.TransferSocketPerIP, 30},
		{"TRANSFER_WS_MESSAGES_PER_MINUTE", &c.RateLimits.TransferMessagesPerMember, 240},
		{"TRANSFER_TOOL_PER_MINUTE", &c.RateLimits.TransferToolPerIP, 240},
	}
	for _, entry := range entries {
		if readEnv {
			if raw := os.Getenv(entry.name); raw != "" {
				value, err := strconv.Atoi(raw)
				if err != nil || value < 1 || value > 1000000 {
					return fmt.Errorf("%s 必须为 1-1000000 的整数", entry.name)
				}
				*entry.target = value
			}
		}
		if *entry.target == 0 {
			*entry.target = entry.fallback
		}
		if *entry.target < 1 || *entry.target > 1000000 {
			return fmt.Errorf("%s 必须为 1-1000000 的整数", entry.name)
		}
	}
	return nil
}
