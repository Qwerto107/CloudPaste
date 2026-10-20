package server

import (
	"fmt"
)

// 次数均按一分钟统计；WebSocket 消息按成员，其余按真实来源 IP。
type RateLimitConfig struct {
	PasswordPerIP                   int
	TransferCreatePerIP             int
	TransferJoinPerIP               int
	TransferRelayPerIP              int
	TransferSocketPerIP             int
	TransferMessagesPerMember       int
	TransferToolPerIP               int
	TransferFolderMessagesPerMember int
	TransferFolderRelayPerIP        int
}

// 启动入口共用默认值；运行中的次数由后台设置快照提供。
func (c *Config) configureRateLimits() error {
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
		{"TRANSFER_FOLDER_WS_MESSAGES_PER_MINUTE", &c.RateLimits.TransferFolderMessagesPerMember, 12000},
		{"TRANSFER_FOLDER_RELAY_PER_MINUTE", &c.RateLimits.TransferFolderRelayPerIP, 12000},
	}
	for _, entry := range entries {
		if *entry.target == 0 {
			*entry.target = entry.fallback
		}
		if *entry.target < 1 || *entry.target > 1000000 {
			return fmt.Errorf("%s 必须为 1-1000000 的整数", entry.name)
		}
	}
	return nil
}
