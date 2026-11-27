package storage

import (
	"context"
	"io"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

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

type Listing struct {
	Items      []FileInfo `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
	HasMore    bool       `json:"hasMore"`
}

type Stream struct {
	Body   io.ReadCloser
	Status int
	Header http.Header
}

type Driver interface {
	List(context.Context, string, string, int) (Listing, error)
	Stat(context.Context, string) (FileInfo, error)
	Open(context.Context, string, *http.Request) (Stream, error)
	Close() error
}

type Deleter interface {
	Delete(context.Context, string) error
}

// Within 判断 target 是否位于 base 目录路径之下。
func Within(base, target string) bool {
	return base == "/" || target == base || strings.HasPrefix(target, base+"/")
}

// Classify 根据文件名与 MIME 类型填充展示分类信息。
func Classify(info *FileInfo) {
	if info.Directory {
		info.Type = 1
		info.TypeName = "folder"
		info.MimeType = "application/x-directory"
		info.Size = nil
		return
	}
	if info.MimeType == "" {
		info.MimeType = mime.TypeByExtension(strings.ToLower(path.Ext(info.Name)))
	}
	if info.MimeType == "" {
		info.MimeType = "application/octet-stream"
	}
	info.TypeName = "unknown"
	switch {
	case strings.HasPrefix(info.MimeType, "image/"):
		info.Type = 5
		info.TypeName = "image"
	case strings.HasPrefix(info.MimeType, "video/"):
		info.Type = 2
		info.TypeName = "video"
	case strings.HasPrefix(info.MimeType, "audio/"):
		info.Type = 3
		info.TypeName = "audio"
	case info.MimeType == "application/pdf":
		info.Type = 7
		info.TypeName = "document"
	case strings.HasPrefix(info.MimeType, "text/") || slices.Contains([]string{".json", ".yaml", ".yml", ".toml", ".md", ".js", ".go", ".rs", ".xml"}, strings.ToLower(path.Ext(info.Name))):
		info.Type = 4
		info.TypeName = "text"
	case slices.Contains([]string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx"}, strings.ToLower(path.Ext(info.Name))):
		info.Type = 6
		info.TypeName = "office"
	}
}
