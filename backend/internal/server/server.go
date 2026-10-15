package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type endpoint func(http.ResponseWriter, *http.Request) error
type contextKey struct{}
type apiError struct {
	Status        int
	Code, Message string
}

func (e *apiError) Error() string     { return e.Message }
func badRequest(message string) error { return &apiError{400, "BAD_REQUEST", message} }
func forbidden() error                { return &apiError{403, "FORBIDDEN", "无权访问此资源"} }
func notFound() error                 { return &apiError{404, "NOT_FOUND", "资源不存在"} }
func unsupported() error {
	return &apiError{501, "NOT_IMPLEMENTED", "Go 后端本阶段暂不支持此功能"}
}

type attemptWindow struct {
	Started time.Time
	Count   int
}

// 访客与管理入口分别按 IP 计数，不再设置跨来源的每分钟总额度。
type passwordAttemptBudget struct {
	byIP map[string]attemptWindow
}

// App 统一持有数据库和有界并发资源，不创建后台轮询池或无限缓存。
type App struct {
	cfg                   Config
	uploadLimit           atomic.Int64
	db                    *sql.DB
	key                   []byte
	logger                *slog.Logger
	downloads             chan struct{}
	downloadMu            sync.Mutex
	downloadsByIP         map[string]int
	requests              chan struct{}
	passwordChecks        chan struct{}
	passwordAttemptMu     sync.Mutex
	guestPasswordAttempts passwordAttemptBudget
	adminPasswordAttempts passwordAttemptBudget
	mutationMu            sync.Mutex
	clientsMu             sync.Mutex
	storageClients        map[string]cachedDriver
	uploads               chan struct{}
	uploadMu              sync.Mutex
	rawReserved           int64
	uploadDir             string
	transfers             *transferHub
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	if err := cfg.configureRateLimits(false); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxDownloads <= 0 {
		cfg.MaxDownloads = 48
	}
	if cfg.MaxUploads <= 0 {
		cfg.MaxUploads = 16
	}
	if cfg.MaxUploadBytes <= 0 {
		cfg.MaxUploadBytes = 10 << 30
	}
	cfg.MaxUploadBytes = min(cfg.MaxUploadBytes, 10<<30)
	if cfg.MaxStagedBytes <= 0 {
		cfg.MaxStagedBytes = 20 << 30
	}
	if cfg.TransferMaxFileBytes <= 0 {
		cfg.TransferMaxFileBytes = 10 << 30
	}
	db, err := openDatabase(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	a := &App{
		cfg: cfg, db: db, logger: logger,
		downloads:      make(chan struct{}, cfg.MaxDownloads),
		requests:       make(chan struct{}, 16),
		passwordChecks: make(chan struct{}, 2),
		storageClients: make(map[string]cachedDriver),
		uploads:        make(chan struct{}, cfg.MaxUploads),
	}
	a.key, err = loadKey(cfg)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = a.bootstrapAdmin(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = a.initUploads(); err != nil {
		_ = db.Close()
		return nil, err
	}
	// 恢复保存的较小上传上限，旧库中的异常值不覆盖环境配置。
	var limit string
	if err := db.QueryRow("SELECT value FROM settings WHERE key='max_upload_size'").Scan(&limit); err == nil {
		if value, e := strconv.ParseInt(limit, 10, 64); e == nil && value >= 1 && value <= cfg.MaxUploadBytes/(1<<20) {
			a.uploadLimit.Store(value << 20)
		}
	}
	a.transfers = newTransferHub(a)
	if err = a.restoreTransferSettings(); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}
func (a *App) Close() error {
	if a.transfers != nil {
		a.transfers.close()
	}
	a.clientsMu.Lock()
	for _, entry := range a.storageClients {
		_ = entry.Driver.Close()
	}
	a.storageClients = make(map[string]cachedDriver)
	a.clientsMu.Unlock()
	return a.db.Close()
}

func (a *App) write(w http.ResponseWriter, status int, data any, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	code := "OK"
	if status == 201 {
		code = "CREATED"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "code": code, "message": message, "data": data})
}
func (a *App) fail(w http.ResponseWriter, err error) {
	var api *apiError
	if !errors.As(err, &api) {
		if errors.Is(err, context.Canceled) {
			return
		}
		a.logger.Error("请求处理失败", "error", err)
		api = &apiError{500, "INTERNAL_ERROR", "服务器内部错误"}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(api.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "code": api.Code, "message": api.Message})
}
func (a *App) handle(fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			a.fail(w, err)
		}
	}
}
func (a *App) protected(permission int, adminOnly bool, fn endpoint) http.HandlerFunc {
	return a.handle(func(w http.ResponseWriter, r *http.Request) error {
		// 管理员专属接口不允许从普通用户路径进入。
		if adminOnly && !adminRequest(r) {
			return notFound()
		}
		p := principal(r)
		if p == nil {
			return &apiError{401, "UNAUTHORIZED", "需要认证访问"}
		}
		if (adminOnly && !p.Admin) || !p.Has(permission) {
			return forbidden()
		}
		return fn(w, r)
	})
}
func principal(r *http.Request) *Principal {
	p, _ := r.Context().Value(contextKey{}).(*Principal)
	return p
}
func decode(w http.ResponseWriter, r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dest); err != nil {
		return badRequest("请求 JSON 无效或超过 2 MiB")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return badRequest("请求只能包含一个 JSON 对象")
	}
	return nil
}
func paging(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if page, _ := strconv.Atoi(r.URL.Query().Get("page")); page > 1 {
		offset = (page - 1) * limit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func pagination(total, limit, offset int) map[string]any {
	return map[string]any{"total": total, "limit": limit, "offset": offset, "hasMore": offset < total && limit < total-offset}
}

func (a *App) Handler() http.Handler {
	mux := &apiMux{http.NewServeMux()}
	a.registerAuth(mux)
	a.registerPastes(mux)
	a.registerStorage(mux)
	a.registerFiles(mux)
	a.registerSystem(mux)
	a.registerUploads(mux)
	a.registerTransfers(mux)
	mux.HandleFunc("/", a.handle(func(w http.ResponseWriter, r *http.Request) error {
		return notFound()
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		// 文件内容也继承沙箱，防止 HTML/SVG 成为登录站点的脚本执行入口。
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data: https:; style-src 'unsafe-inline'")
		for _, origin := range a.cfg.AllowedOrigins {
			if origin == r.Header.Get("Origin") && origin != "" && origin != "*" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Content-Range, Range, X-Custom-Auth-Key, X-FS-Path-Token, X-FS-Path-Tokens, X-FS-Filename, X-FS-Options, X-Share-Filename, X-Share-Options, X-Share-Access, X-Guest-Token")
				w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, ETag, Accept-Ranges, Content-Disposition")
			}
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		// 在 ServeMux 的路径清理之前拒绝歧义路径，避免路由与授权解释不同。
		if _, err := cleanPath(r.URL.Path); err != nil {
			a.fail(w, err)
			return
		}
		adminScope := isAdminPath(r.URL.Path)
		// 固定管理前缀必须按字面传输，拒绝编码前缀导致代理与后端识别不一致。
		if adminScope && !isAdminPath(r.URL.EscapedPath()) {
			a.fail(w, badRequest("管理员接口前缀不允许编码"))
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), adminScopeKey{}, adminScope))
		// 长连接和实时文件使用自己的有界额度，不能长期占用普通 API 的 16 个槽。
		if a.transferStreamRequest(r) {
			mux.ServeHTTP(w, r)
			return
		}
		select {
		case a.requests <- struct{}{}:
			defer func() { <-a.requests }()
		default:
			a.fail(w, &apiError{503, "BUSY", "请求过多，请稍后重试"})
			return
		}
		defer func() {
			if value := recover(); value != nil {
				// 流式下载中断交给 net/http 关闭连接，不能继续写入错误 JSON。
				if value == http.ErrAbortHandler {
					panic(value)
				}
				a.logger.Error("请求发生异常", "path", r.URL.Path, "error", fmt.Sprint(value))
				a.fail(w, &apiError{500, "INTERNAL_ERROR", "服务器内部错误"})
			}
		}()
		// 已移除的 DAV 路径不进入认证流程。
		if isDAVPath(r.URL.Path) {
			a.fail(w, notFound())
			return
		}
		p, err := a.authenticate(r)
		// 浏览器原生媒体请求不能附带登录头，仅在管理员读取入口接受会话绑定的短期票据。
		if err == nil && p == nil && adminScope && (r.Method == "GET" || r.Method == "HEAD") && strings.HasPrefix(r.URL.Path, "/api/admin/s/") {
			p, err = a.adminDownloadPrincipal(r)
		}
		if err == nil && p == nil && !adminScope && (r.Method == "GET" || r.Method == "HEAD") && strings.HasPrefix(r.URL.Path, "/api/owner/s/") {
			p, err = a.ownerDownloadPrincipal(r)
		}
		if err != nil {
			a.fail(w, err)
			return
		}
		// 管理员凭据只能用于管理前缀，API Key 只能用于普通用户入口。
		if p != nil && p.Admin != adminScope {
			a.fail(w, forbidden())
			return
		}
		if adminScope && p == nil && r.URL.Path != adminAPIPrefix+"/login" {
			a.fail(w, &apiError{401, "UNAUTHORIZED", "管理员接口需要认证"})
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), contextKey{}, p))
		mux.ServeHTTP(w, r)
	})
}
