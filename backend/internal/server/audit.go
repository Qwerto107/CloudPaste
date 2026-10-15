package server

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// 操作日志只接收明确挑选的字段，绝不截取请求体、响应体或完整 URL。
type auditEvent struct {
	ID         int64  `json:"id"`
	Time       int64  `json:"time"`
	Module     string `json:"module"`
	Action     string `json:"action"`
	ActorType  string `json:"actor_type"`
	ActorID    string `json:"actor_id"`
	ActorName  string `json:"actor_name"`
	IP         string `json:"ip"`
	Target     string `json:"target"`
	Method     string `json:"method"`
	Endpoint   string `json:"endpoint"`
	Status     int    `json:"status"`
	Result     string `json:"result"`
	DurationMS int64  `json:"duration_ms"`
	Details    string `json:"details"`
}

type auditContextKey struct{}

func (a *App) transferAuditIdentity(r *http.Request) auditEvent {
	kind, id, name := auditActor(principal(r))
	return auditEvent{Module: "transfer", ActorType: kind, ActorID: id, ActorName: name, IP: a.auditClientIP(r), Method: "WS", Endpoint: "/api/transfer/socket"}
}
func (h *transferHub) auditTransfer(member *transferMember, action, target, result string, started time.Time, details any) {
	// 日志默认关闭，先判断开关，避免为每条互传文字做无用的序列化。
	if member == nil || !h.app.audit.allows("transfer") {
		return
	}
	event := member.audit
	event.Action, event.Target, event.Result = action, target, result
	event.DurationMS = time.Since(started).Milliseconds()
	if details != nil {
		data, _ := json.Marshal(details)
		event.Details = string(data)
	}
	h.app.recordAudit(event)
}

func auditSettingValue(key, value string) string {
	// 布尔值、数值和站点标题可对比；自定义正文、HTML、链接等不写入日志。
	for _, definition := range defaultSettings {
		if definition.Key == key && (definition.Type == "bool" || definition.Type == "number" || key == "site_title") {
			return auditText(value, 100)
		}
	}
	return "[内容已隐藏]"
}

type auditLogger struct {
	app                 *App
	db                  *sql.DB
	queue               chan auditEvent
	stop, done, cleanup chan struct{}
	days                atomic.Int64
	enabled             atomic.Bool
	disabledModules     atomic.Uint32
	dropped             atomic.Uint64
	once                sync.Once
}

// 模块位图只在保存配置时更新，请求路径仅做原子读取，不查询数据库或加锁。
var auditModuleBits = map[string]uint32{
	"auth": 1 << 0, "text": 1 << 1, "file": 1 << 2, "transfer": 1 << 3,
	"storage": 1 << 4, "keys": 1 << 5, "settings": 1 << 6,
}

func (l *auditLogger) allows(module string) bool {
	return l != nil && l.enabled.Load() && l.disabledModules.Load()&auditModuleBits[module] == 0
}

func (l *auditLogger) applySettings(values map[string]string) {
	mask := l.disabledModules.Load()
	for module, bit := range auditModuleBits {
		if value, ok := values["audit_"+module+"_enabled"]; ok {
			if value == "true" {
				mask &^= bit
			} else {
				mask |= bit
			}
		}
	}
	l.disabledModules.Store(mask)
	if value, ok := values["audit_enabled"]; ok {
		l.enabled.Store(value == "true")
	}
}

func auditText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func auditActor(p *Principal) (string, string, string) {
	if p == nil || p.Guest {
		return "guest", "", "访客"
	}
	if p.Admin {
		return "admin", p.ID, auditText(p.Name, 100)
	}
	return "apikey", p.ID, auditText(p.Name, 100)
}

func setAuditActor(r *http.Request, p *Principal) {
	if event, ok := r.Context().Value(auditContextKey{}).(*auditEvent); ok {
		event.ActorType, event.ActorID, event.ActorName = auditActor(p)
	}
}
func setAuditTarget(r *http.Request, target string) {
	if event, ok := r.Context().Value(auditContextKey{}).(*auditEvent); ok {
		event.Target = auditText(target, 1000)
	}
}
func setAuditDetails(r *http.Request, details any) {
	if event, ok := r.Context().Value(auditContextKey{}).(*auditEvent); ok {
		data, err := json.Marshal(details)
		if err == nil && len(data) <= 8192 {
			event.Details = string(data)
		}
	}
}

func (a *App) startAudit() error {
	days := int64(30)
	var raw string
	err := a.db.QueryRow("SELECT value FROM settings WHERE key='audit_retention_days'").Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed >= 1 && parsed <= 3650 {
		days = parsed
	}
	l := &auditLogger{app: a, queue: make(chan auditEvent, 1024), stop: make(chan struct{}), done: make(chan struct{}), cleanup: make(chan struct{}, 1)}
	l.db, err = openAuditDatabase(a.cfg.DataDir)
	if err != nil {
		return err
	}
	l.days.Store(days)
	var enabled string
	err = a.db.QueryRow("SELECT value FROM settings WHERE key='audit_enabled'").Scan(&enabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		_ = l.db.Close()
		return err
	}
	l.enabled.Store(enabled == "true")
	rows, err := a.db.Query("SELECT key,value FROM settings WHERE key LIKE 'audit_%'")
	if err != nil {
		_ = l.db.Close()
		return err
	}
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			break
		}
		values[key] = value
	}
	readErr := rows.Err()
	_ = rows.Close()
	if err != nil || readErr != nil {
		_ = l.db.Close()
		return errors.Join(err, readErr)
	}
	// 未配置的模块默认开启，保持升级前的记录范围；总开关仍默认关闭。
	l.applySettings(values)
	a.audit = l
	go l.run()
	return nil
}
func (l *auditLogger) close() {
	l.once.Do(func() { close(l.stop); <-l.done; _ = l.db.Close() })
}
func (a *App) recordAudit(event auditEvent) {
	if !a.audit.allows(event.Module) {
		return
	}
	a.audit.enqueue(event)
}
func (l *auditLogger) enqueue(event auditEvent) {
	if event.Time == 0 {
		event.Time = time.Now().UnixMilli()
	}
	event.Target = auditText(event.Target, 1000)
	// 满队列不等待、不新建 goroutine，避免日志拖慢传输或耗尽内存。
	select {
	case l.queue <- event:
	default:
		l.dropped.Add(1)
	}
}
func (l *auditLogger) run() {
	defer close(l.done)
	flush := time.NewTicker(250 * time.Millisecond)
	cleanup := time.NewTicker(time.Second)
	defer flush.Stop()
	defer cleanup.Stop()
	batch := make([]auditEvent, 0, 64)
	coalescer := newAuditCoalescer()
	nextCleanup, nextWarning := time.Time{}, time.Now()
	write := func() {
		if len(batch) > 0 {
			l.writeBatch(batch)
			batch = batch[:0]
		}
	}
	appendEvents := func(events []auditEvent) {
		for _, event := range events {
			batch = append(batch, event)
			if len(batch) == 64 {
				write()
			}
		}
	}
	for {
		select {
		case event := <-l.queue:
			appendEvents(coalescer.add(event, time.Now()))
		case <-flush.C:
			write()
		case <-l.cleanup:
			nextCleanup = time.Time{}
		case now := <-cleanup.C:
			appendEvents(coalescer.expire(now, false))
			if !now.Before(nextCleanup) {
				// 每轮最多删除 1000 条，清理与业务写入交错进行。
				if l.prune() < 1000 {
					nextCleanup = now.Add(time.Hour)
				}
			}
			if !now.Before(nextWarning) {
				if count := l.dropped.Swap(0); count > 0 {
					l.app.logger.Warn("操作日志未能保存", "count", count)
				}
				nextWarning = now.Add(time.Minute)
			}
		case <-l.stop:
			// 正常退出时排空已接收的记录，数据库在此后才关闭。
			for {
				select {
				case event := <-l.queue:
					appendEvents(coalescer.add(event, time.Now()))
				default:
					appendEvents(coalescer.expire(time.Now(), true))
					write()
					return
				}
			}
		}
	}
}
func (l *auditLogger) writeBatch(batch []auditEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := func() error {
		tx, err := l.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO operation_logs(created_at,module,action,actor_type,actor_id,actor_name,ip,target,method,endpoint,status,result,duration_ms,details) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, e := range batch {
			if _, err = stmt.ExecContext(ctx, e.Time, e.Module, e.Action, e.ActorType, e.ActorID, e.ActorName, e.IP, e.Target, e.Method, e.Endpoint, e.Status, e.Result, e.DurationMS, e.Details); err != nil {
				return err
			}
		}
		return tx.Commit()
	}()
	if err != nil {
		l.dropped.Add(uint64(len(batch)))
	}
}
func (l *auditLogger) prune() int64 {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := l.db.ExecContext(ctx, "DELETE FROM operation_logs WHERE id IN (SELECT id FROM operation_logs WHERE created_at < ? ORDER BY created_at LIMIT 1000)", time.Now().Add(-time.Duration(l.days.Load())*24*time.Hour).UnixMilli())
	if err != nil {
		l.app.logger.Warn("操作日志清理失败")
		return 0
	}
	count, _ := result.RowsAffected()
	return count
}

// 使用路由模板记录接口；不保存查询参数、配对码或下载工具请求票据。
func auditRoute(r *http.Request) (auditEvent, bool) {
	e := auditEvent{Method: r.Method, ActorType: "guest", ActorName: "访客"}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return e, false
	}
	if r.Method == "OPTIONS" || r.Method == "HEAD" {
		return e, false
	}
	p := strings.TrimPrefix(r.URL.Path, "/api/")
	prefix := "/api/"
	if strings.HasPrefix(p, "admin/") {
		p = strings.TrimPrefix(p, "admin/")
		prefix += "admin/"
	}
	parts := strings.Split(p, "/")
	route := parts[0]
	if len(parts) > 1 {
		e.Target = auditText(parts[1], 100)
	}
	method := r.Method
	switch route {
	case "login", "logout", "change-password":
		if method != "POST" {
			return e, false
		}
		e.Module = "auth"
		e.Action = map[string]string{"login": "登录", "logout": "退出", "change-password": "修改账号"}[route]
	case "test":
		if p != "test/api-key" {
			return e, false
		}
		e.Module, e.Action, route, e.Target = "auth", "验证 API Key", "test/api-key", ""
	case "paste":
		e.Module = "text"
		if len(parts) == 1 && method == "POST" {
			e.Action = "创建文本"
		} else if len(parts) == 2 {
			route += "/{slug}"
			if method == "POST" {
				e.Action = "验证密码"
			} else {
				e.Action = "查看文本"
			}
		}
	case "pastes":
		e.Module = "text"
		if method == "PUT" {
			e.Action = "修改文本"
			route += "/{slug}"
		} else if method == "DELETE" {
			e.Action = "删除文本"
			route += "/batch-delete"
			e.Target = ""
		} else {
			return e, false
		}
	case "share":
		e.Module = "file"
		if len(parts) < 2 {
			return e, false
		}
		switch parts[1] {
		case "upload":
			e.Action = "上传文件"
			route += "/upload"
			e.Target = ""
		case "collection":
			e.Action = "创建合集"
			route += "/collection"
			e.Target = ""
		case "get", "verify":
			if len(parts) != 3 {
				return e, false
			}
			e.Action = "查看文件"
			if parts[1] == "verify" {
				e.Action = "验证密码"
			}
			route += "/" + parts[1] + "/{slug}"
			e.Target = auditText(parts[2], 100)
		}
	case "s":
		e.Module, e.Action = "file", "读取文件"
		if r.URL.Query().Get("download") == "true" || r.URL.Query().Get("archive") == "true" {
			e.Action = "下载文件"
		}
		// 媒体/工具的 Range 分段不逐条落库，避免高频读取产生写放大。
		if r.Header.Get("Range") != "" {
			return e, false
		}
		route += "/{slug}"
	case "owner":
		// 只展开实际的创建者读取入口，避免异常路径递归或记录无关地址。
		if len(parts) < 3 || (parts[1] != "paste" && parts[1] != "share" && parts[1] != "s") {
			return e, false
		}
		copyRequest := r.Clone(r.Context())
		copyRequest.URL.Path = "/api/" + strings.TrimPrefix(p, "owner/")
		event, ok := auditRoute(copyRequest)
		event.Endpoint = "/api/owner/" + strings.TrimPrefix(event.Endpoint, "/api/")
		return event, ok
	case "files":
		e.Module = "file"
		if method == "PUT" {
			e.Action = "修改文件分享"
			route += "/{id}"
		} else if method == "DELETE" {
			e.Action = "删除文件分享"
			route += "/batch-delete"
			e.Target = ""
		} else {
			return e, false
		}
	case "storage", "api-keys":
		e.Module = "storage"
		if route == "api-keys" {
			e.Module = "keys"
		}
		if len(parts) > 1 {
			route += "/{id}"
		}
		e.Action = map[string]string{"POST": "新增", "PUT": "修改", "DELETE": "删除"}[method]
		if len(parts) == 3 {
			switch parts[2] {
			case "test":
				e.Action = "测试存储"
			case "set-default":
				e.Action = "设为默认存储"
			case "reveal":
				e.Action = "查看密钥"
			default:
				return e, false
			}
			route += "/" + parts[2]
		}
	case "settings":
		if method != "PUT" {
			return e, false
		}
		e.Module, e.Action, route, e.Target = "settings", "修改配置", "settings/group/{group}", r.PathValue("group")
		if len(parts) == 3 {
			e.Target = auditText(parts[2], 10)
		}
	case "transfer":
		e.Module = "transfer"
		if p == "transfer/create" {
			e.Action = "创建互传"
		} else if p == "transfer/join" {
			// HTTP 请求只预留加入席位，须由创建者确认后才算真正加入。
			e.Action = "申请加入互传"
		} else {
			return e, false
		}
		route = p
		e.Target = ""
	default:
		return e, false
	}
	e.Endpoint = prefix + route
	return e, e.Action != ""
}

type auditWriter struct {
	http.ResponseWriter
	status int
	failed bool
}

func (w *auditWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *auditWriter) WriteHeader(status int) {
	if w.status == 0 && (status >= 200 || status == 101) {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *auditWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, err := w.ResponseWriter.Write(data)
	if err != nil {
		w.failed = true
	}
	return n, err
}
func (w *auditWriter) Flush() {
	if w.status == 0 {
		w.status = 200
	}
	if http.NewResponseController(w.ResponseWriter).Flush() != nil {
		w.failed = true
	}
}
func (w *auditWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (a *App) auditHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 默认关闭时直接进入业务处理，不做路由分类或响应包装。
		if a.audit == nil || !a.audit.enabled.Load() {
			next.ServeHTTP(w, r)
			return
		}
		event, record := auditRoute(r)
		if !record || !a.audit.allows(event.Module) {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Custom-Auth-Key") != "" {
			event.ActorType, event.ActorName = "unknown", "未认证"
		}
		r = r.WithContext(context.WithValue(r.Context(), auditContextKey{}, &event))
		writer := &auditWriter{ResponseWriter: w}
		defer func() {
			panicked := recover()
			event.IP = a.auditClientIP(r)
			event.Status = writer.status
			if event.Status == 0 {
				event.Status = 200
			}
			event.Result = "success"
			if event.Status >= 400 || writer.failed || r.Context().Err() != nil || panicked != nil {
				event.Result = "failure"
			}
			event.DurationMS = time.Since(started).Milliseconds()
			// 请求开始时已启用日志，即使本次请求关闭开关，也保留最后一条设置记录。
			a.audit.enqueue(event)
			if panicked != nil {
				panic(panicked)
			}
		}()
		next.ServeHTTP(writer, r)
	})
}

func (a *App) listAudit(w http.ResponseWriter, r *http.Request) error {
	// 参数值全部绑定，列名只能从服务端白名单选择；查询有时间上限和分页上限。
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	where := "created_at >= ?"
	args := []any{time.Now().Add(-time.Duration(a.audit.days.Load()) * 24 * time.Hour).UnixMilli()}
	q := r.URL.Query()
	for _, key := range []string{"module", "actor_type", "result"} {
		if value := q.Get(key); value != "" {
			if len(value) > 32 {
				return badRequest("筛选值过长")
			}
			where += " AND " + key + "=?"
			args = append(args, value)
		}
	}
	if value := q.Get("actor"); value != "" {
		if len(value) > 200 {
			return badRequest("操作人筛选过长")
		}
		where += " AND (actor_id=? OR instr(actor_name,?)>0)"
		args = append(args, value, value)
	}
	for _, key := range []string{"from", "to"} {
		if raw := q.Get(key); raw != "" {
			date, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return badRequest("时间必须为 RFC3339 格式")
			}
			op := ">="
			if key == "to" {
				op = "<="
			}
			where += " AND created_at " + op + " ?"
			args = append(args, date.UnixMilli())
		}
	}
	limit, offset := paging(r)
	rows, err := a.audit.db.QueryContext(ctx, "SELECT id,created_at,module,action,actor_type,actor_id,actor_name,ip,target,method,endpoint,status,result,duration_ms,details FROM operation_logs WHERE "+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", append(args, limit+1, offset)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []auditEvent{}
	for rows.Next() {
		var e auditEvent
		if err = rows.Scan(&e.ID, &e.Time, &e.Module, &e.Action, &e.ActorType, &e.ActorID, &e.ActorName, &e.IP, &e.Target, &e.Method, &e.Endpoint, &e.Status, &e.Result, &e.DurationMS, &e.Details); err != nil {
			return err
		}
		items = append(items, e)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	// 旧版文件读取票据写入的管理员姓名为空；只在展示时按 ID 补全，不改写历史日志。
	adminNames := map[string]string{}
	for i := range items {
		event := &items[i]
		if event.ActorType != "admin" || event.ActorName != "" || event.ActorID == "" {
			continue
		}
		name, known := adminNames[event.ActorID]
		if !known {
			_ = a.db.QueryRowContext(ctx, "SELECT username FROM admins WHERE id=?", event.ActorID).Scan(&name)
			adminNames[event.ActorID] = name
		}
		if name != "" {
			event.ActorName = auditText(name, 100)
		}
	}
	a.write(w, 200, map[string]any{"items": items, "has_more": hasMore, "retention_days": a.audit.days.Load(), "enabled": a.audit.enabled.Load()}, "获取成功")
	return nil
}

type auditDeleteRequest struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Module string `json:"module"`
}

func (a *App) deleteAudit(w http.ResponseWriter, r *http.Request) error {
	var body auditDeleteRequest
	if err := decode(w, r, &body); err != nil {
		return err
	}
	from, fromError := time.Parse(time.RFC3339, body.From)
	to, toError := time.Parse(time.RFC3339, body.To)
	if fromError != nil || toError != nil || !from.Before(to) {
		return badRequest("必须提供有效的开始和结束时间，且开始时间早于结束时间")
	}
	if body.Module != "" {
		valid := false
		for _, module := range []string{"auth", "text", "file", "transfer", "storage", "keys", "settings"} {
			if body.Module == module {
				valid = true
				break
			}
		}
		if !valid {
			return badRequest("日志模块无效")
		}
	}

	// 每次只清理一批，由管理页面按需继续调用，避免大范围删除长期占用日志库。
	where := "created_at >= ? AND created_at <= ?"
	args := []any{from.UnixMilli(), to.UnixMilli()}
	if body.Module != "" {
		where += " AND module = ?"
		args = append(args, body.Module)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, err := a.audit.db.ExecContext(ctx, "DELETE FROM operation_logs WHERE id IN (SELECT id FROM operation_logs WHERE "+where+" ORDER BY created_at,id LIMIT 1000)", args...)
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	hasMore := false
	if deleted == 1000 {
		err = a.audit.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM operation_logs WHERE "+where+")", args...).Scan(&hasMore)
		if err != nil {
			return err
		}
	}
	a.write(w, 200, map[string]any{"deleted": deleted, "has_more": hasMore}, "日志已清理")
	return nil
}
