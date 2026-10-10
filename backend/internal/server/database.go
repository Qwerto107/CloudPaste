package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var resourceTables = map[string]bool{"pastes": true, "shares": true, "storage_configs": true, "mounts": true, "fs_meta": true, "uploads": true}

func openDatabase(dir string) (*sql.DB, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	// 使用新文件，绝不隐式迁移或覆盖 Node 后端数据库。
	filename := filepath.Join(dir, "cloudpaste-go.db")
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return nil, err
	}
	// 单连接避免 SQLite 写锁争用，也限制每个连接的页缓存开销。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	statements := []string{
		"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON", "PRAGMA cache_size=-2048", "PRAGMA synchronous=NORMAL",
		`CREATE TABLE IF NOT EXISTS admins (id TEXT PRIMARY KEY, username TEXT UNIQUE NOT NULL, password TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, admin_id TEXT NOT NULL REFERENCES admins(id) ON DELETE CASCADE, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS api_keys (id TEXT PRIMARY KEY, key_hash TEXT UNIQUE NOT NULL, key_encrypted TEXT NOT NULL, data TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	}
	for table := range resourceTables {
		statements = append(statements, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, slug TEXT UNIQUE, owner TEXT NOT NULL, data TEXT NOT NULL, created_at TEXT NOT NULL)`, table))
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return db, nil
}

func (a *App) saveResource(ctx context.Context, table, id, slug, owner string, data any) error {
	if !resourceTables[table] {
		return errors.New("未知数据表")
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var nullableSlug any
	if slug != "" {
		nullableSlug = slug
	}
	_, err = a.db.ExecContext(ctx, "INSERT INTO "+table+" (id,slug,owner,data,created_at) VALUES (?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET slug=excluded.slug,owner=excluded.owner,data=excluded.data", id, nullableSlug, owner, string(b), nowText())
	return err
}

func (a *App) resource(ctx context.Context, table, field, value string, dest any) error {
	if !resourceTables[table] || (field != "id" && field != "slug") {
		return errors.New("未知查询")
	}
	var data string
	if err := a.db.QueryRowContext(ctx, "SELECT data FROM "+table+" WHERE "+field+"=?", value).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound()
		}
		return err
	}
	return json.Unmarshal([]byte(data), dest)
}

func (a *App) resources(ctx context.Context, table, owner string, limit, offset int) ([]json.RawMessage, int, error) {
	if !resourceTables[table] {
		return nil, 0, errors.New("未知数据表")
	}
	where, args := "", []any{}
	if owner != "" {
		where = " WHERE owner=?"
		args = append(args, owner)
	}
	var total int
	if err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	projection := "data"
	if table == "pastes" {
		// 管理列表只需摘要；完整文本通过详情接口读取，避免大量大文本同时进入内存。
		projection = `json_set(data,'$.content',substr(json_extract(data,'$.content'),1,200))`
	}
	rows, err := a.db.QueryContext(ctx, "SELECT "+projection+" FROM "+table+where+" ORDER BY created_at DESC,id LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, 0, err
		}
		items = append(items, json.RawMessage(data))
	}
	return items, total, rows.Err()
}

func (a *App) deleteResource(ctx context.Context, table, id string) error {
	if !resourceTables[table] {
		return errors.New("未知数据表")
	}
	_, err := a.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE id=?", id)
	return err
}

func nowText() string { return time.Now().UTC().Format(time.RFC3339Nano) }
