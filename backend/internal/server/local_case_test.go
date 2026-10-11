package server

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 大小写敏感文件系统上的不同名称必须仍能分别读取，不能全局转为小写。
func TestLocalDistinctCaseSensitiveNamesRemainAccessible(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Report.txt", "upper")
	if _, err := os.Stat(filepath.Join(root, "report.txt")); err == nil {
		t.Skip("当前文件系统不区分大小写")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	writeFile(t, root, "report.txt", "lower")
	driver := &localDriver{rootPath: root}
	for name, expected := range map[string]string{"Report.txt": "upper", "report.txt": "lower"} {
		stream, err := driver.Open(context.Background(), name, httptest.NewRequest("GET", "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(stream.Body)
		_ = stream.Body.Close()
		if err != nil || string(body) != expected {
			t.Fatalf("%s: body=%q err=%v", name, body, err)
		}
	}
}
