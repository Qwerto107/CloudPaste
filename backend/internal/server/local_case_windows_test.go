package server

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// 分享读取中的大小写、末尾点/空格和 8.3 别名都不能绕过真实名称校验。
func TestLocalWindowsAliasesCannotBypassPasswords(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "private-vault/example.txt", "directory-secret")
	writeFile(t, root, "public/protected-report.txt", "file-secret")
	driver := &localDriver{rootPath: root}
	aliases := []string{"PRIVATE-VAULT/example.txt", "private-vault./example.txt", "private-vault /example.txt", "public/PROTECTED-REPORT.TXT", "public/protected-report.txt.", "public/protected-report.txt "}
	if short := localShortName(t, filepath.Join(root, "private-vault")); short != "private-vault" {
		aliases = append(aliases, short+"/example.txt")
	}
	if short := localShortName(t, filepath.Join(root, "public", "protected-report.txt")); short != "protected-report.txt" {
		aliases = append(aliases, "public/"+short)
	}
	for _, key := range aliases {
		if _, err := driver.Stat(context.Background(), key); err == nil {
			t.Fatalf("元信息接受了别名: %s", key)
		}
		if stream, err := driver.Open(context.Background(), key, httptest.NewRequest("GET", "/", nil)); err == nil {
			stream.Body.Close()
			t.Fatalf("驱动读取接受了别名: %s", key)
		}
	}
	// basic_path 自身也不能用大小写别名建立新的授权根。
	scoped := &localDriver{rootPath: root, scope: "PRIVATE-VAULT"}
	if _, err := scoped.Stat(context.Background(), "example.txt"); err == nil {
		t.Fatal("授权根接受了目录别名")
	}
}

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
