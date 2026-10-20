package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/coder/websocket"
)

const (
	transferRooms       = 16
	transferConnections = 40
	transferFiles       = 32
	// 覆盖一批文件夹任务的通知与状态消息，并保留控制消息空间。
	transferQueue    = 3*transferFolderMaxConcurrency + transferFiles
	transferFrame    = 16 << 10
	transferLifetime = 2 * time.Hour
	transferIdle     = time.Minute
)

// 房间、成员和文件状态只在 mu 下修改；网络读写从不持有这把锁。
// 不保存消息历史或文件内容，固定队列满时主动断开慢客户端。
type transferHub struct {
	mu                                    sync.Mutex
	app                                   *App
	ctx                                   context.Context
	cancel                                context.CancelFunc
	wg                                    sync.WaitGroup
	rooms                                 map[string]*transferRoom
	limits, probes                        *rateLimitTable
	connections                           map[string]int
	connectionCount                       int
	relays                                int
	maxRelays                             int
	folderConcurrency, folderReservations int
	nextByte                              time.Time
	maxFile, bytesPerSecond               int64
	stun                                  string
	enabled                               atomic.Bool
}

type transferRoom struct {
	id, code          string
	owner             Principal
	ip                string
	created, expires  time.Time
	members           [2]*transferMember
	paired, relayOnly bool
	files             map[string]*transferFile
	folders           map[string]*transferFolder
	offeredBytes      int64
	maxFile           int64
	ctx               context.Context
	cancel            context.CancelFunc
}

type transferMember struct {
	audit       auditEvent // 仅保存操作人摘要，不保留其登录凭据。
	room        *transferRoom
	index       int
	name, token string
	ip          string // WebSocket 建立时由可信代理规则解析，不能由设备名称或客户端消息指定。
	device      *transferDevice
	location    *ipLocation
	created     time.Time
	connected   bool
	out         chan any
}

type transferMessage struct {
	Type        string               `json:"type"`
	Token       string               `json:"token,omitempty"`
	ID          string               `json:"id,omitempty"`
	ClientID    string               `json:"client_id,omitempty"`
	Name        string               `json:"name,omitempty"`
	Text        string               `json:"text,omitempty"`
	Mode        string               `json:"mode,omitempty"`
	Size        int64                `json:"size,omitempty"`
	Bytes       int64                `json:"bytes,omitempty"`
	Data        json.RawMessage      `json:"data,omitempty"`
	ImageType   string               `json:"image_type,omitempty"`
	Connection  *transferConnection  `json:"connection,omitempty"`
	RequestID   string               `json:"request_id,omitempty"`
	Folder      *transferFolderEntry `json:"folder,omitempty"`
	Count       int                  `json:"count,omitempty"`
	Concurrency int                  `json:"concurrency,omitempty"`
	Progress    []transferProgress   `json:"progress,omitempty"`
}

// 直连地址由接收端浏览器上报，仅作为诊断信息，不参与鉴权或传输判断。
type transferConnection struct {
	Local  transferEndpoint `json:"local"`
	Remote transferEndpoint `json:"remote"`
}

type transferEndpoint struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

func transferSecret() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func newTransferHub(a *App) *transferHub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &transferHub{app: a, ctx: ctx, cancel: cancel, rooms: map[string]*transferRoom{}, limits: newRateLimitTable(1024), probes: newRateLimitTable(256), connections: map[string]int{}, maxFile: a.cfg.TransferMaxFileBytes, bytesPerSecond: a.cfg.TransferBytesPerSecond}
	if h.maxFile <= 0 {
		h.maxFile = 10 << 30
	}
	h.maxRelays = 64
	h.folderConcurrency = transferFolderDefaultConcurrency
	h.enabled.Store(!a.cfg.TransferDisabled)
	h.wg.Add(1)
	go h.sweep()
	return h
}

func (h *transferHub) close() {
	h.mu.Lock()
	h.cancel()
	for _, room := range h.rooms {
		h.removeLocked(room)
	}
	h.mu.Unlock()
	h.wg.Wait()
}

func (h *transferHub) removeLocked(room *transferRoom) {
	if h.rooms[room.id] != room {
		return
	}
	delete(h.rooms, room.id)
	h.auditTransfer(room.members[0], "连接结束", room.id, "success", room.created, nil)
	room.cancel()
	// 断开时也释放尚未发出文件的预留额度。
	for _, folder := range room.folders {
		h.finishFolderLocked(room, folder, "cancelled")
	}
	for _, file := range room.files {
		h.finishLocked(file, "cancelled")
	}
	h.dispatchFolderQueuesLocked()
}

func (h *transferHub) sweep() {
	defer h.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	checks := 0
	for {
		select {
		case <-h.ctx.Done():
			return
		case now := <-ticker.C:
			h.mu.Lock()
			var owners []*transferRoom
			for _, room := range h.rooms {
				if room.ctx.Err() != nil || now.After(room.expires) || (!room.paired && now.Sub(room.created) > 5*time.Minute) || (!room.members[0].connected && now.Sub(room.created) > 15*time.Second) {
					h.removeLocked(room)
					continue
				}
				if guest := room.members[1]; guest != nil && !room.paired && now.Sub(guest.created) > time.Minute {
					h.removeLocked(room)
					continue
				}
				h.expireFoldersLocked(room, now)
				for _, file := range room.files {
					if file.state == "offered" && now.Sub(file.lastActivity()) > 5*time.Minute {
						h.finishLocked(file, "cancelled")
					}
					idle := transferIdle
					if file.tool != nil {
						// 下载工具可能等待用户确认或暂停；每个实际请求仍有一分钟停滞保护。
						idle = 10 * time.Minute
					}
					if file.state == "active" && (now.Sub(file.lastActivity()) > idle || (file.tool != nil && !now.Before(file.tool.expires))) {
						h.finishLocked(file, "failed")
					}
				}
				if checks%6 == 0 {
					owners = append(owners, room)
				}
			}
			h.limits.prune(now)
			h.probes.prune(now)
			h.mu.Unlock()
			// 创建者退出、密钥被禁用或权限撤销后，最迟下一轮撤销房间。
			for _, room := range owners {
				if !h.ownerValid(room.owner) {
					h.mu.Lock()
					h.removeLocked(room)
					h.mu.Unlock()
				}
			}
			checks++
		}
	}
}

func (h *transferHub) ownerValid(p Principal) bool {
	if p.Guest {
		return true // 开关仅控制新连接，已创建的连接沿用原有生命周期。
	}
	ctx, cancel := context.WithTimeout(h.ctx, 2*time.Second)
	defer cancel()
	if p.Admin {
		var count int
		err := h.app.db.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE admin_id=? AND token_hash=? AND expires_at>?", p.ID, p.CredentialHash, time.Now().Unix()).Scan(&count)
		return err == nil && count == 1
	}
	var raw, hash string
	if err := h.app.db.QueryRowContext(ctx, "SELECT data,key_hash FROM api_keys WHERE id=?", p.ID).Scan(&raw, &hash); err != nil {
		return false
	}
	var key APIKey
	return json.Unmarshal([]byte(raw), &key) == nil && hash == p.CredentialHash && key.Enabled == 1 && !expired(key.ExpiresAt) && key.Permissions&(TextShare|FileShare) == TextShare|FileShare
}

func (h *transferHub) allowLocked(key string, maxCount int) bool {
	return h.limits.allow(key, maxCount, time.Now())
}

func (h *transferHub) allowIPLocked(scope, ip string, maxCount int) bool {
	return h.allowLocked(scope+":"+rateLimitIP(ip), maxCount)
}

func transferUnavailable() error {
	return &apiError{404, "TRANSFER_UNAVAILABLE", "连接已失效或不可用"}
}
func transferBusy() error {
	return &apiError{429, "TRANSFER_BUSY", "互传请求过多，请稍后重试"}
}

func transferDecode(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		return badRequest("连接信息无效")
	}
	return nil
}

func transferName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 || len(s) > 180 || strings.ContainsAny(s, "/\\") || strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || r == '\u202e' || r == '\u202d' }) {
		return ""
	}
	return s
}

func (a *App) registerTransfers(mux *apiMux) {
	mux.HandleFunc("POST /api/transfer/create", a.guestOrProtected(guestCreateTransfer, TextShare|FileShare, a.createTransfer))
	// 加入入口可认证登录身份；后续互传仍只使用短期成员凭证。
	mux.HandleFunc("POST /api/transfer/join", a.handle(a.joinTransfer))
	mux.ServeMux.HandleFunc("GET /api/transfer/socket", a.transferSocket)
	mux.ServeMux.HandleFunc("PUT /api/transfer/upload/{id}", a.handle(a.transferUpload))
	mux.ServeMux.HandleFunc("GET /api/transfer/download/{id}", a.handle(a.transferDownload))
	mux.ServeMux.HandleFunc("GET /api/transfer/download/{id}/tool", a.handle(a.transferToolDownload))
	mux.ServeMux.HandleFunc("PUT /api/transfer/upload/{id}/{request}", a.handle(a.transferToolUpload))
}

func (a *App) transferStreamRequest(r *http.Request) bool {
	return (r.Method == "GET" && (r.URL.Path == "/api/transfer/socket" || strings.HasPrefix(r.URL.Path, "/api/transfer/download/"))) || (r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/transfer/upload/"))
}

func (a *App) transferOrigin(r *http.Request, required bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return !required
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Host == r.Host {
		return true
	}
	for _, allowed := range a.cfg.AllowedOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}

func (a *App) createTransfer(w http.ResponseWriter, r *http.Request) error {
	if !a.transfers.enabled.Load() {
		return transferUnavailable()
	}
	if !a.transferOrigin(r, false) {
		return forbidden()
	}
	var body struct {
		Name      string               `json:"name"`
		RelayOnly bool                 `json:"relay_only"`
		Device    *transferDeviceInput `json:"device"`
	}
	if err := transferDecode(w, r, &body); err != nil {
		return err
	}
	name := transferName(body.Name)
	if err := body.Device.validate(); err != nil {
		return err
	}
	device := parseTransferDevice(r.UserAgent(), body.Device)
	if name == "" {
		return badRequest("请输入有效的设备名称")
	}
	h := a.transfers
	ip := a.clientIP(r)
	h.mu.Lock()
	if h.ctx.Err() != nil || !h.allowIPLocked("create", ip, a.runtimeLimits().RateLimits.TransferCreatePerIP) || len(h.rooms) >= transferRooms {
		h.mu.Unlock()
		return transferBusy()
	}
	p := principal(r)
	count := 0
	for _, room := range h.rooms {
		if room.owner.Owner() == p.Owner() || room.ip == ip {
			count++
		}
	}
	if count >= 2 {
		h.mu.Unlock()
		return transferBusy()
	}
	var code string
	for code == "" {
		// 均匀生成一个字母和四位数字，保留前导零；仍检查在线房间是否重名。
		number, err := rand.Int(rand.Reader, big.NewInt(26*10000))
		if err != nil {
			h.mu.Unlock()
			return err
		}
		value := number.Int64()
		code = fmt.Sprintf("%c%04d", 'A'+value/10000, value%10000)
		for _, room := range h.rooms {
			if room.code == code {
				code = ""
				break
			}
		}
	}
	ctx, cancel := context.WithDeadline(h.ctx, time.Now().Add(transferLifetime))
	room := &transferRoom{id: randomID(), code: code, owner: *p, ip: ip, created: time.Now(), expires: time.Now().Add(transferLifetime), files: map[string]*transferFile{}, ctx: ctx, cancel: cancel, relayOnly: body.RelayOnly, maxFile: h.maxFile}
	member := &transferMember{room: room, index: 0, name: name, device: device, created: time.Now(), out: make(chan any, transferQueue)}
	member.audit = a.transferAuditIdentity(r)
	// 返回给浏览器的凭证只保留在页面内存，服务端只保存其摘要。
	secret := transferSecret()
	member.token = secretHash(secret)
	room.members[0] = member
	h.rooms[room.id] = room
	h.mu.Unlock()
	setAuditTarget(r, room.id)
	a.setTransferAuditDetails(r, device, ip, nil)
	a.write(w, 201, map[string]any{"token": secret, "code": code, "member": 0, "expires_at": room.expires, "max_file_size": room.maxFile}, "连接已创建")
	return nil
}

func (a *App) joinTransfer(w http.ResponseWriter, r *http.Request) error {
	if principal(r) == nil {
		allowed, err := a.guestAllowed(r, guestJoinTransfer)
		if err != nil {
			return err
		}
		if !allowed {
			return &apiError{403, "FORBIDDEN", "访客无加入互传权限，请登录后重试"}
		}
	}
	if !a.transfers.enabled.Load() {
		return transferUnavailable()
	}
	if !a.transferOrigin(r, false) {
		return forbidden()
	}
	var body struct {
		Code      string               `json:"code"`
		Name      string               `json:"name"`
		RelayOnly bool                 `json:"relay_only"`
		Device    *transferDeviceInput `json:"device"`
	}
	if err := transferDecode(w, r, &body); err != nil {
		return err
	}
	if err := body.Device.validate(); err != nil {
		return err
	}
	device := parseTransferDevice(r.UserAgent(), body.Device)
	h := a.transfers
	ip := a.clientIP(r)
	h.mu.Lock()
	if !h.allowIPLocked("join", ip, a.runtimeLimits().RateLimits.TransferJoinPerIP) {
		h.mu.Unlock()
		return transferBusy()
	}
	// 显示用横杠、复制时的紧凑形式及粘贴空白都解析为同一个配对码。
	code := strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, body.Code))
	for _, room := range h.rooms {
		if code != room.code || room.paired || room.members[1] != nil || !room.members[0].connected || time.Since(room.created) > 5*time.Minute || room.ctx.Err() != nil {
			continue
		}
		name := transferName(body.Name)
		if name == "" {
			h.mu.Unlock()
			return badRequest("请输入有效的设备名称")
		}
		secret := transferSecret()
		room.members[1] = &transferMember{room: room, index: 1, name: name, device: device, token: secretHash(secret), created: time.Now(), out: make(chan any, transferQueue)}
		room.members[1].audit = a.transferAuditIdentity(r)
		room.relayOnly = room.relayOnly || body.RelayOnly
		details := h.joinAuditDetailsLocked(room, time.Time{})
		h.mu.Unlock()
		setAuditTarget(r, room.id)
		a.setTransferAuditDetails(r, device, ip, details)
		a.write(w, 200, map[string]any{"token": secret, "member": 1, "expires_at": room.expires, "max_file_size": room.maxFile}, "等待对方确认")
		return nil
	}
	h.mu.Unlock()
	return transferUnavailable()
}

func (h *transferHub) sendLocked(member *transferMember, value any) {
	if member == nil {
		return
	}
	select {
	case member.out <- value:
	default:
		member.room.cancel()
	}
}
func (h *transferHub) broadcastLocked(room *transferRoom, value any) {
	for _, m := range room.members {
		h.sendLocked(m, value)
	}
}

func (a *App) transferSocket(w http.ResponseWriter, r *http.Request) {
	if !a.transferOrigin(r, true) || r.Header.Get("Authorization") != "" {
		a.fail(w, forbidden())
		return
	}
	h := a.transfers
	ip := a.clientIP(r)
	h.mu.Lock()
	if h.ctx.Err() != nil || h.connectionCount >= transferConnections || h.connections[ip] >= 8 || !h.allowIPLocked("socket", ip, a.runtimeLimits().RateLimits.TransferSocketPerIP) {
		h.mu.Unlock()
		a.fail(w, transferBusy())
		return
	}
	h.connectionCount++
	h.connections[ip]++
	h.wg.Add(1)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.connectionCount--
		h.connections[ip]--
		if h.connections[ip] == 0 {
			delete(h.connections, ip)
		}
		h.mu.Unlock()
		h.wg.Done()
	}()
	// Origin 已完整匹配，不能用通配规则代替；不启用压缩以限制解压资源。
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(transferFrame)
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	typ, raw, err := c.Read(ctx)
	cancel()
	var auth transferMessage
	if err != nil || typ != websocket.MessageText || json.Unmarshal(raw, &auth) != nil || auth.Type != "auth" || len(auth.Token) != 43 {
		return
	}
	h.mu.Lock()
	var member *transferMember
	for _, room := range h.rooms {
		if room.ctx.Err() != nil {
			continue
		}
		for _, m := range room.members {
			if m != nil && m.token == secretHash(auth.Token) && !m.connected {
				member = m
				break
			}
		}
	}
	if member == nil {
		h.mu.Unlock()
		return
	}
	// 只查询已认证成员；文件读取放在房间锁外，避免阻塞其他设备的消息转发。
	h.mu.Unlock()
	location := a.locateIP(ip)
	h.mu.Lock()
	if member.connected || member.room.ctx.Err() != nil || h.rooms[member.room.id] != member.room {
		h.mu.Unlock()
		return
	}
	member.connected = true
	member.ip = ip
	// 数据库可能在锁外查询期间被卸载或更换，不能发布旧库返回的结果。
	a.ipLocationMu.RLock()
	if location != nil && location.database != a.ipLocationDB {
		location = nil
	}
	member.location = location
	a.ipLocationMu.RUnlock()
	member.audit.IP = ip
	h.sendLocked(member, map[string]any{"type": "ready", "member": member.index})
	if member.index == 1 {
		h.sendLocked(member.room.members[0], map[string]any{"type": "joining", "name": member.name, "ip": member.ip, "device": member.device, "location": member.location})
	}
	h.mu.Unlock()
	defer func() { h.mu.Lock(); h.removeLocked(member.room); h.mu.Unlock() }()
	// 读循环处理 pong；写循环负责有界队列和心跳，两者随房间一起取消。
	done := make(chan struct{})
	go func() { defer close(done); h.writeSocket(c, member) }()
	defer func() { member.room.cancel(); <-done }()
	for {
		typ, raw, err = c.Read(member.room.ctx)
		if err != nil {
			return
		}
		var message transferMessage
		if typ != websocket.MessageText || json.Unmarshal(raw, &message) != nil {
			return
		}
		h.mu.Lock()
		key, limit := "message:"+member.token, a.runtimeLimits().RateLimits.TransferMessagesPerMember
		if h.isFolderMessageLocked(member, message) {
			key, limit = "folder-message:"+member.token, a.runtimeLimits().RateLimits.TransferFolderMessagesPerMember
		}
		if !h.allowLocked(key, limit) {
			h.mu.Unlock()
			return
		}
		err = h.messageLocked(member, message)
		if err != nil {
			h.sendLocked(member, map[string]any{"type": "error", "message": err.Error(), "id": message.ID, "client_id": message.ClientID, "action": message.Type})
		}
		h.mu.Unlock()
	}
}

func (h *transferHub) writeSocket(c *websocket.Conn, m *transferMember) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	defer m.room.cancel()
	for {
		select {
		case <-m.room.ctx.Done():
			return
		case value := <-m.out:
			raw, err := json.Marshal(value)
			if err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(m.room.ctx, 10*time.Second)
			err = c.Write(ctx, websocket.MessageText, raw)
			cancel()
			if err != nil {
				return
			}
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(m.room.ctx, 8*time.Second)
			err := c.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (h *transferHub) messageLocked(m *transferMember, msg transferMessage) error {
	r := m.room
	if r.ctx.Err() != nil {
		return transferUnavailable()
	}
	if msg.Type == "leave" || msg.Type == "reject" {
		h.removeLocked(r)
		return nil
	}
	if msg.Type == "confirm" {
		if m.index != 0 || r.paired || r.members[1] == nil || !r.members[1].connected {
			return forbidden()
		}
		r.paired = true
		r.code = ""
		// 仅在创建者确认、双方配对成功后记录加入；申请本身由 HTTP 日志单独记录。
		h.auditTransfer(r.members[1], "加入互传", r.id, "success", r.members[1].created, h.joinAuditDetailsLocked(r, time.Now()))
		// 来源地址只向本房间设备披露，创建者地址须在授权配对后才发送给加入者。
		h.broadcastLocked(r, map[string]any{
			"type": "paired", "names": []string{r.members[0].name, r.members[1].name},
			"ips":        []string{r.members[0].ip, r.members[1].ip},
			"devices":    []*transferDevice{r.members[0].device, r.members[1].device},
			"locations":  []*ipLocation{r.members[0].location, r.members[1].location},
			"relay_only": r.relayOnly, "stun": h.stun, "audit_enabled": h.app.audit.allows("transfer"),
		})
		return nil
	}
	if !r.paired {
		return forbidden()
	}
	switch msg.Type {
	case "folder-offer", "accept-folder", "cancel-folder", "folder-progress", "folder-concurrency", "folder-ready":
		return h.folderMessageLocked(m, msg)
	case "text":
		if len(msg.Text) == 0 || len(msg.Text) > 4096 {
			return badRequest("文字长度应为 1-4096 字节")
		}
		// 服务端统一记录发送时间，双方看到同一个时间值，不接受客户端伪造时间。
		sent := time.Now()
		h.broadcastLocked(r, map[string]any{"type": "text", "text": msg.Text, "from": m.index, "sent_at": sent.UTC().Format(time.RFC3339Nano)})
		// 用户选择保存互传文字正文；仅在日志开启时进入有界异步队列。
		h.auditTransfer(m, "发送文本", r.id, "success", sent, map[string]any{"text": msg.Text, "bytes": len(msg.Text)})
	case "signal":
		if r.relayOnly || len(msg.Data) > 12000 || !json.Valid(msg.Data) {
			return badRequest("连接协商信息无效")
		}
		h.sendLocked(r.members[1-m.index], map[string]any{"type": "signal", "data": msg.Data})
	case "offer":
		// 图片仍使用有界中继管道，仅通知前端可以自动接收预览；不接收 SVG 等活动内容。
		if msg.ImageType != "" && ((msg.ImageType != "image/png" && msg.ImageType != "image/jpeg") || msg.Size <= 0 || msg.Size > 4<<20) {
			return badRequest("聊天图片仅支持不超过 4 MiB 的 PNG / JPEG")
		}
		var folder *transferFolder
		if msg.Folder != nil {
			folder = r.folders[msg.Folder.ID]
			if folder == nil || folder.sender != m.index || folder.state != "active" || folder.pending <= 0 || len(folder.current) >= transferFolderMaxConcurrency || folder.offered >= folder.count || msg.ImageType != "" || !validTransferPath(msg.Folder.Path, msg.Folder.Directory) || (msg.Folder.Directory && msg.Size != 0) || msg.Size > folder.size-folder.offeredBytes {
				return badRequest("文件夹条目或队列状态无效")
			}
			base := msg.Folder.Path
			if index := strings.LastIndex(base, "/"); index >= 0 {
				base = base[index+1:]
			}
			if base == "" {
				base = folder.name
			}
			if msg.Name != base {
				return badRequest("文件夹条目名称与路径不一致")
			}
		}
		if transferName(msg.Name) == "" || len(msg.ClientID) > 64 || msg.ClientID == "" || msg.Size < 0 || msg.Size > r.maxFile || len(r.files) >= transferFiles+transferFolderMaxConcurrency || (folder == nil && (ordinaryTransferTasks(r)+len(r.folders) >= transferFiles || r.offeredBytes+msg.Size > r.maxFile*4)) {
			return badRequest("文件名称、大小或本次连接的文件额度不符合限制")
		}
		f := &transferFile{id: randomID(), clientID: msg.ClientID, name: transferName(msg.Name), imageType: msg.ImageType, size: msg.Size, sender: m.index, room: r, state: "offered", created: time.Now()}
		f.folder, f.folderEntry = folder, msg.Folder
		r.files[f.id] = f
		if folder != nil {
			folder.current[f.id] = true
			folder.pending--
			f.reservedRelay = folder.mode == "relay"
			folder.offered++
			folder.offeredBytes += f.size
		} else {
			r.offeredBytes += f.size
		}
		h.broadcastLocked(r, map[string]any{"type": "file", "id": f.id, "client_id": f.clientID, "name": f.name, "size": f.size, "from": f.sender, "state": "offered", "image_type": msg.ImageType, "sent_at": f.created.UTC().Format(time.RFC3339Nano), "folder": msg.Folder})
	case "accept":
		f := r.files[msg.ID]
		if f == nil || f.sender == m.index || f.state != "offered" {
			return forbidden()
		}
		for _, folder := range r.folders {
			if folder.state == "active" && f.folder != folder {
				return badRequest("请先完成当前文件夹传输")
			}
		}
		if f.folderEntry != nil && f.folderEntry.Directory {
			if msg.Mode != "directory" {
				return badRequest("目录条目不传输文件内容")
			}
			h.finishLocked(f, "complete")
			return nil
		}
		if f.folder != nil && msg.Mode != f.folder.mode {
			return badRequest("文件夹传输方式不一致")
		}
		for _, other := range r.files {
			if other.state == "active" && (f.folder == nil || other.folder != f.folder) {
				return badRequest("请先完成当前文件传输")
			}
		}
		if msg.Mode != "relay" && msg.Mode != "tool" && (msg.Mode != "p2p" || r.relayOnly) {
			return badRequest("传输方式无效")
		}
		if msg.Mode == "relay" && !f.reservedRelay && h.maxRelays > 0 && h.relays+h.folderReservations >= h.maxRelays {
			return transferBusy()
		}
		f.mode = msg.Mode
		f.state = "active"
		f.progress = time.Now()
		f.ctx, f.cancel = context.WithCancel(r.ctx)
		if f.mode == "relay" {
			if f.reservedRelay {
				h.folderReservations--
				f.reservedRelay = false
			}
			h.relays++
			f.relaySlot = true
			f.reader, f.writer = io.Pipe()
			context.AfterFunc(f.ctx, func() { f.reader.CloseWithError(context.Canceled); f.writer.CloseWithError(context.Canceled) })
			secret := transferSecret()
			f.downloadToken = secretHash(secret)
			h.sendLocked(m, map[string]any{"type": "download", "id": f.id, "url": "/api/transfer/download/" + f.id + "?ticket=" + secret})
		} else if f.mode == "tool" {
			// 只在接收方明确选择下载工具后开放可重试票据，不触发浏览器自动下载。
			expires := time.Now().Add(30 * time.Minute)
			if r.expires.Before(expires) {
				expires = r.expires
			}
			f.cancel()
			f.ctx, f.cancel = context.WithDeadline(r.ctx, expires)
			f.tool = &transferTool{expires: expires, requests: map[string]*transferRangeRequest{}}
			secret := transferSecret()
			f.downloadToken = secretHash(secret)
			connections := 4
			if h.maxRelays > 0 {
				connections = min(connections, h.maxRelays)
			}
			h.sendLocked(m, map[string]any{"type": "download-tool", "id": f.id, "url": "/api/transfer/download/" + f.id + "/tool?ticket=" + secret, "expires_at": expires, "max_connections": connections})
		} else {
			h.sendLocked(r.members[f.sender], map[string]any{"type": "start", "id": f.id, "mode": "p2p"})
		}
		h.broadcastLocked(r, map[string]any{"type": "state", "id": f.id, "state": "active", "mode": f.mode})
		// 文件夹由整体事件汇总，单独文件及图片保留各自的开始日志。
		if f.folder == nil {
			action := "文件传输开始"
			if f.imageType != "" {
				action = "图片传输开始"
			}
			h.auditTransfer(r.members[f.sender], action, f.id, "success", f.progress, transferAuditDetails(f))
		}
		if f.folder != nil && f.mode == "relay" {
			h.dispatchFolderQueuesLocked()
		}
	case "cancel":
		if f := r.files[msg.ID]; f != nil {
			h.finishLocked(f, "cancelled")
		}
	case "fail":
		// 客户端读文件、写盘或直连异常只结束当前活动任务，保留与主动取消的区别。
		if f := r.files[msg.ID]; f != nil && f.state == "active" {
			h.finishLocked(f, "failed")
		}
	case "range-failed":
		// 单段失败只取消该请求，下载工具可以继续重试，不影响其他分段。
		if f := r.files[msg.ID]; f != nil && f.tool != nil && f.sender == m.index {
			if request := f.tool.requests[msg.RequestID]; request != nil {
				request.cancel()
			}
		}
	case "finish-download":
		f := r.files[msg.ID]
		if f == nil || f.mode != "tool" || f.state != "active" || f.sender == m.index {
			return forbidden()
		}
		if f.tool.round == 0 || f.tool.loggedRound != f.tool.round {
			return badRequest("文件尚未传完，请下载完成后确认接收")
		}
		// 服务端无法得知外部软件的落盘结果，由接收方确认完成后撤销链接。
		h.finishLocked(f, "complete")
	case "progress", "complete":
		f := r.files[msg.ID]
		folderRelay := f != nil && f.folder != nil && f.mode == "relay" && msg.Type == "complete"
		if f == nil || f.state != "active" || (f.mode != "p2p" && !folderRelay) || f.sender == m.index || msg.Bytes < f.bytes || msg.Bytes > f.size {
			return forbidden()
		}
		f.bytes = msg.Bytes
		f.progress = time.Now()
		if msg.Type == "complete" {
			if f.bytes != f.size {
				return badRequest("文件长度不一致")
			}
			f.connection = msg.Connection
			if folderRelay {
				f.folderSaved = true
				if !f.forwarded {
					return nil
				}
			}
			h.finishLocked(f, "complete")
		} else {
			h.broadcastLocked(r, map[string]any{"type": "progress", "id": f.id, "bytes": f.bytes})
		}
	default:
		return errors.New("不支持的互传操作")
	}
	return nil
}
