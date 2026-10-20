package server

import (
	"net"
	"strconv"
	"strings"
	"time"
)

func validTransferSTUN(value string) bool {
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

// 只暴露业务保护值；背压缓冲、票据强度和消息边界仍由协议固定保证。
func (a *App) transferSettingRange(key string) (int64, int64) {
	switch key {
	case "transfer_rate_mb":
		return 0, 10240
	case "transfer_max_relays":
		return 0, 1<<(strconv.IntSize-1) - 1
	case "transfer_folder_concurrency":
		return 1, transferFolderMaxConcurrency
	case "transfer_max_file_mb":
		return 1, a.cfg.TransferMaxFileBytes >> 20
	}
	return 0, 0
}

func (a *App) restoreTransferSettings() error {
	rows, err := a.db.Query("SELECT key,value FROM settings WHERE key IN ('transfer_rate_mb','transfer_max_relays','transfer_max_file_mb','transfer_folder_concurrency','transfer_stun')")
	if err != nil {
		return err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return err
	}
	a.applyTransferSettings(values)
	return nil
}

func (a *App) applyTransferSettings(values map[string]string) {
	h := a.transfers
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, raw := range values {
		if key == "transfer_stun" {
			// 仅在新配对消息中下发，保存设置不触发已有连接重新协商。
			if validTransferSTUN(raw) {
				h.stun = raw
			}
			continue
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		minimum, maximum := a.transferSettingRange(key)
		if err != nil || n < minimum || n > maximum {
			continue
		}
		switch key {
		case "transfer_rate_mb":
			h.bytesPerSecond = n << 20
			h.nextByte = time.Time{}
		case "transfer_max_relays":
			h.maxRelays = int(n)
		case "transfer_folder_concurrency":
			h.folderConcurrency = int(n)
		case "transfer_max_file_mb":
			h.maxFile = n << 20
		}
	}
	h.dispatchFolderQueuesLocked()
}

func (a *App) transferSettingValues() map[string]string {
	h := a.transfers
	h.mu.Lock()
	defer h.mu.Unlock()
	return map[string]string{
		"transfer_stun":               h.stun,
		"transfer_rate_mb":            strconv.FormatInt(h.bytesPerSecond>>20, 10),
		"transfer_max_relays":         strconv.Itoa(h.maxRelays),
		"transfer_folder_concurrency": strconv.Itoa(h.folderConcurrency),
		"transfer_max_file_mb":        strconv.FormatInt(h.maxFile>>20, 10),
	}
}
