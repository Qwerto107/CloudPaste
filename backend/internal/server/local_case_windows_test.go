package server

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// 同一磁盘对象的大小写、末尾点/空格和 8.3 别名都不能绕过目录或单文件密码。
func TestLocalWindowsAliasesCannotBypassPasswords(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	writeFile(t, root, "private-vault/example.txt", "directory-secret")
	writeFile(t, root, "public/protected-report.txt", "file-secret")
	writeFile(t, root, "public/ordinary.txt", "public-content")
	f.local(t, root, "/disk")
	auth, _ := f.key(t, "reader-writer", "/disk", MountView|FileShare|MountUpload|WebDAVRead)
	protected := []string{"/disk/private-vault", "/disk/public/protected-report.txt"}
	for _, target := range protected {
		f.request(t, "POST", "/api/fs-meta/create", f.admin, map[string]any{"path": target, "password": "directory-password", "passwordInherit": true}, nil, 201)
	}
	for _, target := range []string{"/disk/private-vault/example.txt", "/disk/public/protected-report.txt"} {
		f.request(t, "GET", "/api/fs/content?path="+url.QueryEscape(target), auth, nil, nil, 403)
		verified := dataMap(t, f.request(t, "POST", "/api/fs/meta/password/verify", auth, map[string]any{"path": target, "password": "directory-password"}, nil, 200))
		f.request(t, "GET", "/api/fs/content?path="+url.QueryEscape(target), auth, nil, map[string]string{"X-FS-Path-Token": verified["token"].(string)}, 200)
	}
	f.request(t, "GET", "/api/fs/content?path=/disk/public/ordinary.txt", auth, nil, nil, 200)
	f.request(t, "GET", "/api/fs/list?path=/disk/public", auth, nil, nil, 200)

	directoryAliases := []string{"PRIVATE-VAULT", "private-vault.", "private-vault "}
	if short := localShortName(t, filepath.Join(root, "private-vault")); short != "private-vault" {
		directoryAliases = append(directoryAliases, short)
	}
	for _, alias := range directoryAliases {
		target := "/disk/" + alias
		f.request(t, "GET", "/api/fs/list?path="+url.QueryEscape(target), auth, nil, nil, 403)
		checkLocalAliasRejected(t, f, auth, target+"/example.txt")
		f.upload(t, "PUT", "/api/fs/upload?path="+url.QueryEscape(target), auth, strings.NewReader("bad"), uploadHeaders("new.txt", nil, false), 403)
	}
	if _, err := os.Stat(filepath.Join(root, "private-vault", "new.txt")); !os.IsNotExist(err) {
		t.Fatal("上传通过别名写入了受保护目录")
	}
	fileAliases := []string{"PROTECTED-REPORT.TXT", "protected-report.txt.", "protected-report.txt "}
	if short := localShortName(t, filepath.Join(root, "public", "protected-report.txt")); short != "protected-report.txt" {
		fileAliases = append(fileAliases, short)
	}
	for _, alias := range fileAliases {
		checkLocalAliasRejected(t, f, auth, "/disk/public/"+alias)
		// 直接读取驱动也必须拒绝别名，不能只依赖上层的 Stat 检查。
		driver := &localDriver{rootPath: root}
		stream, err := driver.Open(context.Background(), "public/"+alias, httptest.NewRequest("GET", "/", nil))
		if err == nil {
			_ = stream.Body.Close()
			t.Fatalf("驱动读取接受了文件别名: %s", alias)
		}
	}
	// basic_path 本身也不能用别名建立新的授权根。
	scoped, _ := f.key(t, "scoped-alias", "/disk/PRIVATE-VAULT", MountView)
	f.request(t, "GET", "/api/fs/content?path=/disk/PRIVATE-VAULT/example.txt", scoped, nil, nil, 403)
}

func checkLocalAliasRejected(t *testing.T, f *fixture, auth, target string) {
	t.Helper()
	for _, entry := range []string{"get", "content", "download", "file-link"} {
		f.request(t, "GET", "/api/fs/"+entry+"?path="+url.QueryEscape(target), auth, nil, nil, 403)
	}
	f.request(t, "GET", (&url.URL{Path: "/dav" + target}).String(), auth, nil, nil, 404)
	f.request(t, "POST", "/api/fs/create-share", auth, map[string]any{"path": target}, nil, 403)
}

// 8.3 功能可能被卷禁用；有短名称时必须覆盖真实生成的别名，不能假定固定后缀。
func localShortName(t *testing.T, filename string) string {
	t.Helper()
	long, err := windows.UTF16PtrFromString(filename)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	size, err := windows.GetShortPathName(long, &buffer[0], uint32(len(buffer)))
	if err != nil || size == 0 || size >= uint32(len(buffer)) {
		t.Fatalf("读取短名称失败: length=%d err=%v", size, err)
	}
	short := filepath.Base(windows.UTF16ToString(buffer[:size]))
	if short == filepath.Base(filename) {
		t.Logf("文件系统未生成 8.3 别名: %s", short)
	}
	return short
}

// 备用数据流和末尾空白也不能成为新上传文件的权限路径别名。
func TestLocalWindowsRejectsAmbiguousNewNames(t *testing.T) {
	for _, name := range []string{"report.txt.", "report.txt ", "report.txt:secret"} {
		if validLocalName(name) {
			t.Fatalf("接受了有歧义的 Windows 名称: %q", name)
		}
	}
}
