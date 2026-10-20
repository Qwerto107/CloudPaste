package server

import (
	"strconv"
	"time"
)

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
	rows, err := a.db.Query("SELECT key,value FROM settings WHERE key IN ('transfer_rate_mb','transfer_max_relays','transfer_max_file_mb','transfer_folder_concurrency')")
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
		"transfer_rate_mb":            strconv.FormatInt(h.bytesPerSecond>>20, 10),
		"transfer_max_relays":         strconv.Itoa(h.maxRelays),
		"transfer_folder_concurrency": strconv.Itoa(h.folderConcurrency),
		"transfer_max_file_mb":        strconv.FormatInt(h.maxFile>>20, 10),
	}
}
