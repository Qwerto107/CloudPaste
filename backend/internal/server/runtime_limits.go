package server

import "strconv"

// 保存配置时替换不可变快照，请求路径只做原子读取，不访问数据库。
type runtimeLimits struct {
	MaxDownloads, MaxUploads       int
	MaxUploadBytes, MaxStagedBytes int64
	RateLimits                     RateLimitConfig
}

var defaultRuntimeLimits = runtimeLimits{
	MaxDownloads: 48, MaxUploads: 16, MaxUploadBytes: 10 << 30, MaxStagedBytes: 20 << 30,
	RateLimits: RateLimitConfig{10, 30, 30, 180, 30, 240, 240, 12000, 12000},
}

func (a *App) runtimeLimits() *runtimeLimits {
	if limits := a.limits.Load(); limits != nil {
		return limits
	}
	return &defaultRuntimeLimits
}

func rateLimitTargets(limits *RateLimitConfig) map[string]*int {
	return map[string]*int{
		"password_attempts_per_minute":           &limits.PasswordPerIP,
		"transfer_create_per_minute":             &limits.TransferCreatePerIP,
		"transfer_join_per_minute":               &limits.TransferJoinPerIP,
		"transfer_relay_per_minute":              &limits.TransferRelayPerIP,
		"transfer_ws_connect_per_minute":         &limits.TransferSocketPerIP,
		"transfer_ws_messages_per_minute":        &limits.TransferMessagesPerMember,
		"transfer_tool_per_minute":               &limits.TransferToolPerIP,
		"transfer_folder_ws_messages_per_minute": &limits.TransferFolderMessagesPerIP,
		"transfer_folder_relay_per_minute":       &limits.TransferFolderRelayPerIP,
	}
}

func runtimeSettingRange(key string) (int64, int64, bool) {
	switch key {
	case "max_uploads", "max_downloads":
		return 1, 65535, true
	case "max_upload_size":
		return 1, 10240, true
	case "max_staged_size_mb":
		return 1, 102400, true
	}
	if _, ok := rateLimitTargets(&RateLimitConfig{})[key]; ok {
		return 1, 1000000, true
	}
	return 0, 0, false
}

func (a *App) updatedRuntimeLimits(values map[string]string) runtimeLimits {
	limits := *a.runtimeLimits()
	targets := rateLimitTargets(&limits.RateLimits)
	for key, raw := range values {
		minimum, maximum, ok := runtimeSettingRange(key)
		number, err := strconv.ParseInt(raw, 10, 64)
		if !ok || err != nil || number < minimum || number > maximum {
			continue
		}
		switch key {
		case "max_downloads":
			limits.MaxDownloads = int(number)
		case "max_uploads":
			limits.MaxUploads = int(number)
		case "max_upload_size":
			limits.MaxUploadBytes = number << 20
		case "max_staged_size_mb":
			limits.MaxStagedBytes = number << 20
		default:
			*targets[key] = int(number)
		}
	}
	return limits
}

func (a *App) restoreRuntimeLimits() error {
	rows, err := a.db.Query("SELECT key,value FROM settings")
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
	limits := a.updatedRuntimeLimits(values)
	// 忽略旧库中互相冲突的额度，避免恢复后所有上传都被拒绝。
	if limits.MaxStagedBytes < limits.MaxUploadBytes {
		limits.MaxStagedBytes = defaultRuntimeLimits.MaxStagedBytes
	}
	a.limits.Store(&limits)
	return nil
}

func (a *App) runtimeSettingValues() map[string]string {
	limits := a.runtimeLimits()
	values := map[string]string{
		"max_downloads":      strconv.Itoa(limits.MaxDownloads),
		"max_uploads":        strconv.Itoa(limits.MaxUploads),
		"max_upload_size":    strconv.FormatInt(limits.MaxUploadBytes>>20, 10),
		"max_staged_size_mb": strconv.FormatInt(limits.MaxStagedBytes>>20, 10),
	}
	copy := limits.RateLimits
	for key, target := range rateLimitTargets(&copy) {
		values[key] = strconv.Itoa(*target)
	}
	return values
}
