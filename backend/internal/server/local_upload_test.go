package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// 比较文件身份而非耗时，直接证明最终文件由接收时的临时文件发布，没有完整复制。
func TestLocalUploadPublishesReceivedFile(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	storage, _ := f.local(t, root, "/disk")
	var received os.FileInfo
	body := &callbackReader{Reader: strings.NewReader("received contents"), callback: func() {
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 1 || !isUploadTemporary(entries[0].Name()) {
			t.Fatalf("未直接写入存储临时文件: %v %v", entries, err)
		}
		received, err = os.Stat(filepath.Join(root, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		// Windows 按路径延迟查询文件身份，需要在临时名称删除前固定身份。
		if !os.SameFile(received, received) {
			t.Fatal("无法确认临时文件身份")
		}
		if entries, err = os.ReadDir(f.app.uploadDir); err != nil || len(entries) != 0 {
			t.Fatalf("本地上传仍使用重复暂存: %v %v", entries, err)
		}
		driver := &localDriver{rootPath: root}
		listing, err := driver.List(context.Background(), "", "", 20)
		if err != nil || len(listing.Items) != 0 {
			t.Fatalf("临时文件出现在目录列表中: %+v %v", listing, err)
		}
		if _, err = cleanPath("/disk/" + received.Name()); err == nil {
			t.Fatal("临时文件可通过业务路径访问")
		}
	}}
	fields := map[string]any{"storage_config_id": storage, "path": "/disk", "original_filename": true}
	f.upload(t, "PUT", "/api/share/upload", f.admin, body, uploadHeaders("final.txt", fields, true), 201)
	final, err := os.Stat(filepath.Join(root, "final.txt"))
	if err != nil || received == nil || !os.SameFile(received, final) {
		t.Fatalf("发布时重新复制了文件: %v", err)
	}
	if contents, err := os.ReadFile(filepath.Join(root, "final.txt")); err != nil || string(contents) != "received contents" {
		t.Fatal("发布内容不一致", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "final.txt" {
		t.Fatal("临时文件未清理", entries, err)
	}
	var pending int
	if err = f.app.db.QueryRow("SELECT count(*) FROM uploads").Scan(&pending); err != nil || pending != 0 || f.app.rawReserved != 0 {
		t.Fatal("上传记录或额度未释放", pending, err)
	}
}

// 文件接收过程中存储变为只读时，不能发布或遗留临时文件。
func TestLocalUploadRechecksStorageBeforePublish(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	storage, _ := f.local(t, root, "/disk")
	body := &callbackReader{Reader: strings.NewReader("contents"), callback: func() {
		f.request(t, "PUT", "/api/storage/"+storage, f.admin, map[string]any{"readonly": true}, nil, 200)
	}}
	fields := map[string]any{"storage_config_id": storage, "path": "/disk", "original_filename": true}
	f.upload(t, "PUT", "/api/share/upload", f.admin, body, uploadHeaders("blocked.txt", fields, true), 403)
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("配置变更后遗留文件", entries, err)
	}
}

func TestLocalUploadCleansIncompleteBody(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	storage, _ := f.local(t, root, "/disk")
	fields := map[string]any{"storage_config_id": storage, "path": "/disk", "original_filename": true}
	body := io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(io.ErrUnexpectedEOF))
	f.upload(t, "PUT", "/api/share/upload", f.admin, body, uploadHeaders("partial.txt", fields, true), 400)
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 || f.app.rawReserved != 0 {
		t.Fatal("接收失败后遗留文件或额度", entries, err)
	}
	var pending int
	if err = f.app.db.QueryRow("SELECT count(*) FROM uploads").Scan(&pending); err != nil || pending != 0 {
		t.Fatal("接收失败后遗留清理记录", pending, err)
	}
}

// 目录替换后保留旧句柄只能用于清理，不能继续向旧目录发布。
func TestLocalUploadRejectsReplacedDirectory(t *testing.T) {
	root := t.TempDir()
	driver := &localDriver{rootPath: root}
	upload, err := driver.beginUpload("target/file.txt", randomID())
	if err != nil {
		t.Fatal(err)
	}
	defer upload.close()
	if err = os.Rename(filepath.Join(root, "target"), filepath.Join(root, "moved")); err != nil {
		t.Skipf("当前系统不允许移动已打开的目录: %v", err)
	}
	if err = os.Mkdir(filepath.Join(root, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	var api *apiError
	if err = upload.publish(context.Background(), driver, "target/file.txt", 0); !isAPIStatus(err, 403, &api) {
		t.Fatal("目录替换后仍可发布", err)
	}
}

// 模拟进程退出遗留的文件：平时不清理活动任务，启动时仅回收登记的临时名称。
func TestLocalUploadRecoveryKeepsPublishedFiles(t *testing.T) {
	f := newFixture(t)
	root := t.TempDir()
	driver := &localDriver{rootPath: root}
	pending := pendingUpload{ID: randomID(), Expires: time.Now().Add(2 * time.Hour).Unix(), LocalRoot: root, LocalKey: "published.txt"}
	if err := f.app.saveResource(context.Background(), "uploads", pending.ID, "", "", pending); err != nil {
		t.Fatal(err)
	}
	upload, err := driver.beginUpload(pending.LocalKey, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = upload.file.WriteString("saved"); err != nil {
		t.Fatal(err)
	}
	if err = upload.publish(context.Background(), driver, pending.LocalKey, 5); err != nil {
		t.Fatal(err)
	}
	// 保留临时硬链接和登记记录，模拟发布后尚未来得及执行清理的进程中断。
	_ = upload.root.Close()
	temporary := filepath.Join(root, uploadTempPrefix+pending.ID)
	if err = f.app.cleanupUploads(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(temporary); err != nil {
		t.Fatal("活动记录被提前清理", err)
	}
	if err = f.app.initUploads(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatal("重启未清理临时文件", err)
	}
	if contents, err := os.ReadFile(filepath.Join(root, "published.txt")); err != nil || string(contents) != "saved" {
		t.Fatal("清理影响已发布文件", err)
	}
}
