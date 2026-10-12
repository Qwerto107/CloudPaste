package server

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	TextShare   = 1
	FileShare   = 2
	TextManage  = 4
	FileManage  = 8
	MountView   = 256
	MountUpload = 512
	WebDAVRead  = 65536 // 仅保留旧数据库的位值，不再授予协议访问能力。
)

// Flag 兼容原前端使用的 0/1 和布尔值。
type Flag int

func (f *Flag) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "true", "1":
		*f = 1
	case "false", "0", "null":
		*f = 0
	default:
		return fmt.Errorf("标志必须为布尔值或 0/1")
	}
	return nil
}

type Principal struct {
	ID, Name, BasicPath, Role string
	Admin                     bool
	Permissions               int
	StorageIDs                []string
	CredentialHash            string
}

func (p *Principal) Has(permission int) bool {
	return p != nil && (p.Admin || p.Permissions&permission == permission)
}
func (p *Principal) Owner() string {
	if p.Admin {
		return p.ID
	}
	return "apikey:" + p.ID
}

type APIKey struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Key          string   `json:"key,omitempty"`
	KeyHash      string   `json:"-"`
	EncryptedKey string   `json:"-"`
	Permissions  int      `json:"permissions"`
	Role         string   `json:"role"`
	BasicPath    string   `json:"basic_path"`
	Enabled      Flag     `json:"is_enable"`
	ExpiresAt    *string  `json:"expires_at"`
	CreatedAt    string   `json:"created_at"`
	StorageIDs   []string `json:"storage_config_ids"`
}

type Paste struct {
	ID        string  `json:"id"`
	Slug      string  `json:"slug"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	Remark    string  `json:"remark"`
	Password  string  `json:"password,omitempty"`
	ExpiresAt *string `json:"expires_at"`
	MaxViews  int     `json:"max_views"`
	Views     int     `json:"views"`
	Public    Flag    `json:"is_public"`
	Owner     string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type Storage struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"storage_type"`
	Public    Flag   `json:"is_public"`
	Default   Flag   `json:"is_default"`
	Status    string `json:"status"`
	Remark    string `json:"remark"`
	RootPath  string `json:"root_path,omitempty"`
	Endpoint  string `json:"endpoint_url,omitempty"`
	Bucket    string `json:"bucket_name,omitempty"`
	Region    string `json:"region,omitempty"`
	AccessKey string `json:"access_key_id,omitempty"`
	SecretKey string `json:"secret_access_key,omitempty"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	PathStyle Flag   `json:"force_path_style"`
	ReadOnly  Flag   `json:"readonly"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Mount struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	StorageID    string   `json:"storage_config_id"`
	StorageType  string   `json:"storage_type"`
	Path         string   `json:"mount_path"`
	RootPath     string   `json:"root_path"`
	Active       Flag     `json:"is_active"`
	WebProxy     Flag     `json:"web_proxy"`
	EnableSign   Flag     `json:"enable_sign"`
	SortOrder    int      `json:"sort_order"`
	Remark       string   `json:"remark"`
	Owner        string   `json:"created_by"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	Capabilities []string `json:"capabilities"`
}

type FsMeta struct {
	ID              string   `json:"id"`
	Path            string   `json:"path"`
	Header          string   `json:"headerMarkdown"`
	Footer          string   `json:"footerMarkdown"`
	HidePatterns    []string `json:"hidePatterns"`
	Password        string   `json:"password,omitempty"`
	PasswordInherit bool     `json:"passwordInherit"`
	HeaderInherit   bool     `json:"headerInherit"`
	FooterInherit   bool     `json:"footerInherit"`
	HideInherit     bool     `json:"hideInherit"`
}

type Share struct {
	// 合集保存文件来源快照，沿用同一分享的密码、有效期和查看次数。
	Files         []Share `json:"files,omitempty"`
	StorageID     string  `json:"storage_config_id,omitempty"`
	StorageKey    string  `json:"storage_key,omitempty"`
	SourceVersion string  `json:"source_version"`
	MetaVersion   string  `json:"meta_version"`
	ID            string  `json:"id"`
	Slug          string  `json:"slug"`
	Path          string  `json:"file_path"`
	Filename      string  `json:"filename"`
	Size          int64   `json:"size"`
	MimeType      string  `json:"mimetype"`
	Password      string  `json:"password,omitempty"`
	Remark        string  `json:"remark"`
	Owner         string  `json:"created_by"`
	ExpiresAt     *string `json:"expires_at"`
	MaxViews      int     `json:"max_views"`
	Views         int     `json:"views"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	UseProxy      Flag    `json:"use_proxy"`
}

type FileInfo struct {
	IsMount     bool       `json:"isMount,omitempty"`
	Path        string     `json:"path"`
	Name        string     `json:"name"`
	Directory   bool       `json:"isDirectory"`
	Size        *int64     `json:"size"`
	Modified    *time.Time `json:"modified"`
	MimeType    string     `json:"mimetype"`
	MountID     string     `json:"mount_id"`
	StorageType string     `json:"storage_type"`
	Type        int        `json:"type"`
	TypeName    string     `json:"typeName"`
}

func toMap(value any) map[string]any {
	b, _ := json.Marshal(value)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}
