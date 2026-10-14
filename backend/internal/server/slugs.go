package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type slugQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// 创建和改名共用同一规则；隐藏或有密码的有效分享仍占用后缀。
// 返回占用后缀的失效记录 ID，查询本身不修改任何数据。
func checkShareSlug(ctx context.Context, query slugQuerier, table, slug, id string) (string, error) {
	if table != "pastes" && table != "shares" {
		return "", errors.New("未知分享表")
	}
	var occupiedID, raw string
	err := query.QueryRowContext(ctx, "SELECT id,data FROM "+table+" WHERE slug=? AND id<>?", slug, id).Scan(&occupiedID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var share struct {
		ExpiresAt *string `json:"expires_at"`
		MaxViews  int     `json:"max_views"`
		Views     int     `json:"views"`
	}
	if err := json.Unmarshal([]byte(raw), &share); err != nil {
		return "", err
	}
	if !expired(share.ExpiresAt) && (share.MaxViews <= 0 || share.Views < share.MaxViews) {
		return "", badRequest("链接后缀已被占用")
	}
	return occupiedID, nil
}

// 回收与新记录写入必须处于同一事务，失败时原后缀仍属于旧记录。
func reclaimShareSlug(ctx context.Context, tx *sql.Tx, table, slug, id string) error {
	occupiedID, err := checkShareSlug(ctx, tx, table, slug, id)
	if err != nil || occupiedID == "" {
		return err
	}
	// 保留失效记录和文件，只换随机后缀；同步索引列和 JSON，避免历史记录复用旧地址。
	replacement := randomID()
	_, err = tx.ExecContext(ctx, "UPDATE "+table+" SET slug=?,data=json_set(data,'$.slug',?) WHERE id=?", replacement, replacement, occupiedID)
	return err
}

// 调用方持有 mutationMu；查看次数更新仍使用 saveResource，避免每次访问额外开启事务。
func (a *App) saveShareResource(ctx context.Context, table, id, slug, owner string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := reclaimShareSlug(ctx, tx, table, slug, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+" (id,slug,owner,data,created_at) VALUES (?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET slug=excluded.slug,owner=excluded.owner,data=excluded.data", id, slug, owner, string(b), nowText()); err != nil {
		return err
	}
	return tx.Commit()
}
