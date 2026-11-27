package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResourceTableIndexesCreated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, indexName := range []string{
		"idx_pastes_created",
		"idx_pastes_owner_created",
		"idx_shares_created",
		"idx_shares_owner_created",
	} {
		var found string
		err := f.app.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='index' AND name=?", indexName).Scan(&found)
		if err != nil || found != indexName {
			t.Fatalf("期望存在索引 %s，实际 err=%v found=%q", indexName, err, found)
		}
	}
}

func TestShareAutoCleanupDisabledByDefaultAndCleansExpired(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 创建本地存储与两个文件：一个过期超过 180 天，一个仅过期 10 天。
	storageDir := t.TempDir()
	oldFile := filepath.Join(storageDir, "old.txt")
	recentFile := filepath.Join(storageDir, "recent.txt")
	if err := os.WriteFile(oldFile, []byte("old-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recentFile, []byte("recent-content"), 0600); err != nil {
		t.Fatal(err)
	}
	storage := Storage{
		ID:        "st-cleanup",
		Name:      "本地测试存储",
		Type:      "LOCAL",
		Status:    "ENABLED",
		RootPath:  storageDir,
		CreatedAt: nowText(),
		UpdatedAt: nowText(),
	}
	if err := f.app.saveResource(ctx, "storage_configs", storage.ID, "", "admin", storage); err != nil {
		t.Fatal(err)
	}

	expiredOld := now.Add(-181 * 24 * time.Hour).Format(time.RFC3339)
	expiredRecent := now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	future := now.Add(24 * time.Hour).Format(time.RFC3339)

	// 插入文本分享：过期超过 180 天、仅过期 10 天、未过期、永久有效。
	for _, p := range []Paste{
		{ID: "p-old", Slug: "p-old", Content: "old", Owner: "admin", ExpiresAt: &expiredOld, CreatedAt: nowText(), UpdatedAt: nowText()},
		{ID: "p-recent", Slug: "p-recent", Content: "recent", Owner: "admin", ExpiresAt: &expiredRecent, CreatedAt: nowText(), UpdatedAt: nowText()},
		{ID: "p-active", Slug: "p-active", Content: "active", Owner: "admin", ExpiresAt: &future, CreatedAt: nowText(), UpdatedAt: nowText()},
		{ID: "p-forever", Slug: "p-forever", Content: "forever", Owner: "admin", ExpiresAt: nil, CreatedAt: nowText(), UpdatedAt: nowText()},
	} {
		if err := f.app.saveResource(ctx, "pastes", p.ID, p.Slug, p.Owner, p); err != nil {
			t.Fatal(err)
		}
	}

	// 插入文件分享：过期超过 180 天、仅过期 10 天。
	for _, s := range []Share{
		{ID: "s-old", Slug: "s-old", Filename: "old.txt", StorageID: storage.ID, StorageKey: "old.txt", Owner: "admin", ExpiresAt: &expiredOld, CreatedAt: nowText(), UpdatedAt: nowText()},
		{ID: "s-recent", Slug: "s-recent", Filename: "recent.txt", StorageID: storage.ID, StorageKey: "recent.txt", Owner: "admin", ExpiresAt: &expiredRecent, CreatedAt: nowText(), UpdatedAt: nowText()},
	} {
		if err := f.app.saveResource(ctx, "shares", s.ID, s.Slug, s.Owner, s); err != nil {
			t.Fatal(err)
		}
	}

	// 默认关闭时，不清理任何记录或文件。
	if f.app.cleaner.enabled.Load() {
		t.Fatal("过期分享自动清理应默认关闭")
	}
	if f.app.cleaner.retentionDays.Load() != 180 {
		t.Fatalf("默认保留天数应为 180，实际 %d", f.app.cleaner.retentionDays.Load())
	}
	pDel, sDel := f.app.cleaner.runOnce(now)
	if pDel != 0 || sDel != 0 {
		t.Fatalf("关闭状态下不应清理任何记录，实际 pastes=%d shares=%d", pDel, sDel)
	}

	// 开启自动清理（保持默认 180 天），仅清理过期超过 180 天的记录和物理文件。
	f.app.cleaner.retentionDays.Store(180)
	f.app.cleaner.enabled.Store(true)
	pDel, sDel = f.app.cleaner.runOnce(now)
	if pDel != 1 || sDel != 1 {
		t.Fatalf("应各清理 1 条超期记录，实际 pastes=%d shares=%d", pDel, sDel)
	}

	// 校验 p-old 与 s-old 已删，对应 old.txt 物理文件也已删除。
	var dummy Paste
	if err := f.app.resource(ctx, "pastes", "id", "p-old", &dummy); err == nil {
		t.Fatal("p-old 应已被清理")
	}
	for _, keep := range []string{"p-recent", "p-active", "p-forever"} {
		if err := f.app.resource(ctx, "pastes", "id", keep, &dummy); err != nil {
			t.Fatalf("文本 %s 不应被清理: %v", keep, err)
		}
	}
	var dummyShare Share
	if err := f.app.resource(ctx, "shares", "id", "s-old", &dummyShare); err == nil {
		t.Fatal("s-old 应已被清理")
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("过期文件 old.txt 应已被删除，实际 err=%v", err)
	}
	if err := f.app.resource(ctx, "shares", "id", "s-recent", &dummyShare); err != nil {
		t.Fatalf("s-recent 不应被清理: %v", err)
	}
	if _, err := os.Stat(recentFile); err != nil {
		t.Fatalf("recent.txt 不应被删除: %v", err)
	}
}
