package server

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config 只保留 Docker 和普通服务器需要的配置。
type Config struct {
	ListenAddr, DataDir, AdminUsername, AdminPassword, EncryptionSecret string
	AllowedOrigins                                                      []string
	MaxDownloads                                                        int
	MaxUploads                                                          int
	MaxUploadBytes, MaxStagedBytes                                      int64
	TrustedProxies                                                      *trustedProxySet
	TransferDisabled                                                    bool
	TransferMaxFileBytes, TransferBytesPerSecond                        int64
	RateLimits                                                          RateLimitConfig
}

func ConfigFromEnv() (Config, error) {
	c := Config{ListenAddr: ":8787", DataDir: "./data", AdminUsername: "admin", MaxDownloads: 48, MaxUploads: 16, MaxUploadBytes: 10 << 30, MaxStagedBytes: 20 << 30, TransferMaxFileBytes: 10 << 30, TransferBytesPerSecond: 64 << 20}
	if err := c.configureRateLimits(); err != nil {
		return c, err
	}
	if v := os.Getenv("PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return c, errors.New("PORT 必须为 1-65535")
		}
		c.ListenAddr = ":" + v
	}
	if v := os.Getenv("DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("ADMIN_USERNAME"); v != "" {
		c.AdminUsername = v
	}
	c.AdminPassword, c.EncryptionSecret = os.Getenv("ADMIN_PASSWORD"), os.Getenv("ENCRYPTION_SECRET")
	for _, origin := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, origin)
		}
	}
	var err error
	c.TrustedProxies, err = parseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return c, err
	}
	c.DataDir, err = filepath.Abs(c.DataDir)
	return c, err
}
