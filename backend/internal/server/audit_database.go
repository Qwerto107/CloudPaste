package server

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"
)

// 日志使用独立 SQLite 文件和连接，日志查询、清理不会占用业务数据库连接或写锁。
func openAuditDatabase(dir string) (*sql.DB, error) {
	filename := filepath.Join(dir, "operation-logs.db")
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA busy_timeout=500", "PRAGMA cache_size=-1024",
		`CREATE TABLE IF NOT EXISTS operation_logs (id INTEGER PRIMARY KEY, created_at INTEGER NOT NULL, module TEXT NOT NULL, action TEXT NOT NULL, actor_type TEXT NOT NULL, actor_id TEXT NOT NULL, actor_name TEXT NOT NULL, ip TEXT NOT NULL, target TEXT NOT NULL, method TEXT NOT NULL, endpoint TEXT NOT NULL, status INTEGER NOT NULL, result TEXT NOT NULL, duration_ms INTEGER NOT NULL, details TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS operation_logs_time ON operation_logs(created_at DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS operation_logs_module_time ON operation_logs(module,created_at DESC,id DESC)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return db, nil
}
