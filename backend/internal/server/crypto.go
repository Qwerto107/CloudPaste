package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func randomID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func secretHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func passwordHash(value string) (string, error) {
	if len(value) == 0 || len(value) > 72 {
		return "", badRequest("密码必须为 1-72 字节")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	return string(b), err
}
func passwordMatches(hash, value string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(value)) == nil
}

func loadKey(cfg Config) ([]byte, error) {
	if cfg.EncryptionSecret != "" {
		key, err := base64.StdEncoding.DecodeString(cfg.EncryptionSecret)
		if err != nil || len(key) != 32 {
			return nil, errors.New("ENCRYPTION_SECRET 必须为 32 字节随机值的 Base64 编码")
		}
		return key, nil
	}
	filename := filepath.Join(cfg.DataDir, "encryption.key")
	key, err := os.ReadFile(filename)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("加密密钥文件损坏")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	// 独占创建，防止并发启动时覆盖已有加密密钥。
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err = f.Write(key); err != nil {
		return nil, err
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	return key, nil
}

func (a *App) encrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), []byte("cloudpaste-go:config:v1"))
	return "gcm:v1:" + base64.RawURLEncoding.EncodeToString(sealed), nil
}
func (a *App) decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "gcm:v1:") {
		return "", errors.New("拒绝未加密的存储凭证")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "gcm:v1:"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("加密数据无效")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte("cloudpaste-go:config:v1"))
	return string(plain), err
}

type Ticket struct {
	Purpose string `json:"purpose"`
	Path    string `json:"path"`
	Subject string `json:"subject"`
	Version string `json:"version,omitempty"`
	// 管理员绑定会话、创建者绑定 API Key 摘要，复核时不需要在链接中暴露登录凭据。
	Session string `json:"session,omitempty"`
	Expires int64  `json:"exp"`
}

func (a *App) ticket(t Ticket) string {
	b, _ := json.Marshal(t)
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write([]byte("ticket:v1:" + payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (a *App) verifyTicket(token, purpose, path, subject, version string) bool {
	t, err := a.parseTicket(token, purpose, path)
	return err == nil && t.Subject == subject && t.Version == version
}

func (a *App) parseTicket(token, purpose, target string) (Ticket, error) {
	var t Ticket
	if len(token) > 8192 {
		return t, forbidden()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return t, forbidden()
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return t, forbidden()
	}
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write([]byte("ticket:v1:" + parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return t, forbidden()
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return t, forbidden()
	}
	if json.Unmarshal(payload, &t) != nil || t.Purpose != purpose || t.Path != target || t.Expires <= time.Now().Unix() {
		return t, forbidden()
	}
	return t, nil
}
