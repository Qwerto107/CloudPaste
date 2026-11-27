package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultShareExpiredRetentionDays = 180
	shareCleanupInterval             = time.Hour
	shareCleanupBatchLimit           = 200
)

// shareCleaner 后台异步回收超过保留期的过期分享记录及对应存储原件；默认关闭。
type shareCleaner struct {
	app           *App
	enabled       atomic.Bool
	retentionDays atomic.Int64
	runMu         sync.Mutex
	stop          chan struct{}
	done          chan struct{}
	trigger       chan struct{}
	once          sync.Once
}

func (a *App) startShareCleanup() error {
	c := &shareCleaner{
		app:     a,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		trigger: make(chan struct{}, 1),
	}
	c.retentionDays.Store(defaultShareExpiredRetentionDays)

	rows, err := a.db.Query("SELECT key,value FROM settings WHERE key IN ('share_auto_cleanup_enabled','share_expired_retention_days')")
	if err != nil {
		return err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.applySettings(values)
	a.cleaner = c
	go c.loop()
	return nil
}

func (c *shareCleaner) applySettings(values map[string]string) {
	if c == nil {
		return
	}
	updated := false
	if raw, ok := values["share_auto_cleanup_enabled"]; ok {
		c.enabled.Store(raw == "true")
		updated = true
	}
	if raw, ok := values["share_expired_retention_days"]; ok {
		if days, err := strconv.ParseInt(raw, 10, 64); err == nil && days >= 0 && days <= 3650 {
			c.retentionDays.Store(days)
			updated = true
		}
	}
	if updated && c.enabled.Load() {
		select {
		case c.trigger <- struct{}{}:
		default:
		}
	}
}

func (c *shareCleaner) close() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		close(c.stop)
		<-c.done
	})
}

func (c *shareCleaner) loop() {
	defer close(c.done)
	ticker := time.NewTicker(shareCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.runOnce(time.Now().UTC())
		case <-c.trigger:
			c.runOnce(time.Now().UTC())
		}
	}
}

func (c *shareCleaner) runOnce(now time.Time) (pastesDeleted int, sharesDeleted int) {
	if c == nil || !c.enabled.Load() {
		return 0, 0
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if !c.enabled.Load() {
		return 0, 0
	}
	days := c.retentionDays.Load()
	if days < 0 {
		days = 0
	}
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pastesDeleted = c.cleanupPastes(ctx, cutoff)
	sharesDeleted = c.cleanupShares(ctx, cutoff)
	if pastesDeleted > 0 || sharesDeleted > 0 {
		c.app.logger.Info("过期分享自动清理完成", "pastes", pastesDeleted, "shares", sharesDeleted, "retention_days", days)
	}
	return pastesDeleted, sharesDeleted
}

func expiredBefore(expires *string, cutoff time.Time) bool {
	if expires == nil || *expires == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, *expires)
	return err == nil && !t.After(cutoff)
}

func (c *shareCleaner) cleanupPastes(ctx context.Context, cutoff time.Time) int {
	rows, err := c.app.db.QueryContext(ctx, "SELECT data FROM pastes ORDER BY created_at ASC, id ASC LIMIT ?", shareCleanupBatchLimit)
	if err != nil {
		c.app.logger.Warn("扫描过期文本分享失败", "error", err)
		return 0
	}
	var candidates []Paste
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var p Paste
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			continue
		}
		if expiredBefore(p.ExpiresAt, cutoff) {
			candidates = append(candidates, p)
		}
	}
	_ = rows.Close()

	deleted := 0
	for _, p := range candidates {
		select {
		case <-c.stop:
			return deleted
		default:
		}
		c.app.mutationMu.Lock()
		var current Paste
		err := c.app.resource(ctx, "pastes", "id", p.ID, &current)
		if err == nil && pasteVersion(current) == pasteVersion(p) && expiredBefore(current.ExpiresAt, cutoff) {
			if delErr := c.app.deleteResource(ctx, "pastes", p.ID); delErr == nil {
				deleted++
			}
		}
		c.app.mutationMu.Unlock()
	}
	return deleted
}

func (c *shareCleaner) cleanupShares(ctx context.Context, cutoff time.Time) int {
	rows, err := c.app.db.QueryContext(ctx, "SELECT data FROM shares ORDER BY created_at ASC, id ASC LIMIT ?", shareCleanupBatchLimit)
	if err != nil {
		c.app.logger.Warn("扫描过期文件分享失败", "error", err)
		return 0
	}
	var candidates []Share
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var s Share
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			continue
		}
		if expiredBefore(s.ExpiresAt, cutoff) {
			candidates = append(candidates, s)
		}
	}
	_ = rows.Close()

	deleted := 0
	for _, s := range candidates {
		select {
		case <-c.stop:
			return deleted
		default:
		}
		if c.cleanupOneShare(ctx, s, cutoff) {
			deleted++
		}
	}
	return deleted
}

func (c *shareCleaner) cleanupOneShare(ctx context.Context, s Share, cutoff time.Time) bool {
	files := s.Files
	if len(files) == 0 {
		files = []Share{s}
	}
	var targets []shareFileDeletion
	for _, file := range files {
		if file.StorageID == "" || file.StorageKey == "" {
			continue
		}
		var storage Storage
		err := c.app.resource(ctx, "storage_configs", "id", file.StorageID, &storage)
		if err != nil {
			var api *apiError
			if errors.As(err, &api) && api.Status == 404 {
				// 存储配置已不存在，允许继续清理孤立的过期分享记录。
				continue
			}
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return false
		}
		if storage.ReadOnly == 1 || storage.Status != "ENABLED" {
			continue
		}
		driver, err := c.app.driver(storage)
		if err != nil {
			c.app.logger.Warn("初始化过期文件存储驱动失败", "share_id", s.ID, "storage_id", storage.ID, "error", err)
			return false
		}
		deleter, ok := driver.(fileDeleter)
		if !ok {
			continue
		}
		targets = append(targets, shareFileDeletion{driver: deleter, key: file.StorageKey})
	}

	// 存储删除在锁外执行；物理文件删除失败时保留数据库记录，留待下次重试。
	for _, target := range targets {
		if err := target.driver.Delete(ctx, target.key); err != nil {
			c.app.logger.Warn("清理过期分享原文件失败", "share_id", s.ID, "key", target.key, "error", err)
			return false
		}
	}

	c.app.mutationMu.Lock()
	defer c.app.mutationMu.Unlock()
	var current Share
	if err := c.app.resource(ctx, "shares", "id", s.ID, &current); err != nil {
		return false
	}
	if shareVersion(current) != shareVersion(s) || !expiredBefore(current.ExpiresAt, cutoff) {
		return false
	}
	return c.app.deleteResource(ctx, "shares", s.ID) == nil
}
