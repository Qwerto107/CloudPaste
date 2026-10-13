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
	TransferSTUN                                                        string
}

func ConfigFromEnv() (Config, error) {
	c := Config{ListenAddr: ":8787", DataDir: "./data", AdminUsername: "admin", MaxDownloads: 8}
	c.TransferDisabled = os.Getenv("TRANSFER_ENABLED") == "false"
	c.TransferMaxFileBytes = 10 << 30
	c.TransferBytesPerSecond = 64 << 20
	for name, target := range map[string]*int64{"TRANSFER_MAX_FILE_MB": &c.TransferMaxFileBytes, "TRANSFER_RATE_MB": &c.TransferBytesPerSecond} {
		if value := os.Getenv(name); value != "" {
			n, err := strconv.ParseInt(value, 10, 64)
			minimum := int64(1)
			if name == "TRANSFER_RATE_MB" {
				minimum = 0
			}
			if err != nil || n < minimum || n > 10240 {
				return c, errors.New(name + " 超出允许范围（速率可为 0，最大 10240）")
			}
			*target = n << 20
		}
	}
	// 不默认向第三方发送网络地址；跨网络直连可由部署者指定 STUN。
	c.TransferSTUN = strings.TrimSpace(os.Getenv("TRANSFER_STUN"))
	if c.TransferSTUN != "" && (!strings.HasPrefix(c.TransferSTUN, "stun:") || len(c.TransferSTUN) > 256 || strings.ContainsAny(c.TransferSTUN, "\r\n\t /")) {
		return c, errors.New("TRANSFER_STUN 必须为 stun:主机:端口")
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
	c.MaxUploads = 2
	c.MaxUploadBytes = 100 << 20
	c.MaxStagedBytes = 1 << 30
	if value := os.Getenv("MAX_UPLOAD_SIZE_MB"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 || n > 10240 {
			return c, errors.New("MAX_UPLOAD_SIZE_MB 必须为 1-10240")
		}
		c.MaxUploadBytes = n << 20
	}
	if value := os.Getenv("MAX_STAGED_SIZE_MB"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 || n > 102400 {
			return c, errors.New("MAX_STAGED_SIZE_MB 必须为 1-102400")
		}
		c.MaxStagedBytes = n << 20
	}
	if value := os.Getenv("MAX_UPLOADS"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 8 {
			return c, errors.New("MAX_UPLOADS 必须为 1-8")
		}
		c.MaxUploads = n
	}
	if c.MaxStagedBytes < c.MaxUploadBytes {
		return c, errors.New("临时区配额不能小于单文件限制")
	}
	if v := os.Getenv("MAX_DOWNLOADS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 128 {
			return c, errors.New("MAX_DOWNLOADS 必须为 1-128")
		}
		c.MaxDownloads = n
	}
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
