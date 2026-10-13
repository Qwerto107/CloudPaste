package server

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coder/websocket"
)

const (
	transferRooms       = 16
	transferConnections = 40
	transferFiles       = 32
	// 一批最多 32 个文件通知，再为控制消息留出空间，慢客户端仍会被断开。
	transferQueue    = 64
	transferFrame    = 16 << 10
	transferLifetime = 2 * time.Hour
	transferIdle     = time.Minute
)

// 房间、成员和文件状态只在 mu 下修改；网络读写从不持有这把锁。
// 不保存消息历史或文件内容，固定队列满时主动断开慢客户端。
type transferHub struct {
	mu                      sync.Mutex
	app                     *App
	ctx                     context.Context
	cancel                  context.CancelFunc
	wg                      sync.WaitGroup
	rooms                   map[string]*transferRoom
	limits                  map[string]attemptWindow
	connections             map[string]int
	connectionCount         int
	relays                  int
	maxRelays               int
	nextByte                time.Time
	maxFile, bytesPerSecond int64
}

type transferRoom struct {
	id, code          string
	owner             Principal
	ip                string
	created, expires  time.Time
	members           [2]*transferMember
	paired, relayOnly bool
	files             map[string]*transferFile
	offeredBytes      int64
	maxFile           int64
	ctx               context.Context
	cancel            context.CancelFunc
}

type transferMember struct {
	room        *transferRoom
	index       int
	name, token string
	created     time.Time
	connected   bool
	out         chan any
}

type transferMessage struct {
	Type      string          `json:"type"`
	Token     string          `json:"token,omitempty"`
	ID        string          `json:"id,omitempty"`
	ClientID  string          `json:"client_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Text      string          `json:"text,omitempty"`
	Mode      string          `json:"mode,omitempty"`
	Size      int64           `json:"size,omitempty"`
	Bytes     int64           `json:"bytes,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	ImageType string          `json:"image_type,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
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
	h := &transferHub{app: a, ctx: ctx, cancel: cancel, rooms: map[string]*transferRoom{}, limits: map[string]attemptWindow{}, connections: map[string]int{}, maxFile: a.cfg.TransferMaxFileBytes, bytesPerSecond: a.cfg.TransferBytesPerSecond}
	if h.maxFile <= 0 {
		h.maxFile = 10 << 30
	}
	h.maxRelays = 4
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
	room.cancel()
	for _, file := range room.files {
		h.finishLocked(file, "cancelled")
	}
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
				for _, file := range room.files {
					if file.state == "offered" && now.Sub(file.created) > 5*time.Minute {
						h.finishLocked(file, "cancelled")
					}
					idle := transferIdle
					if file.tool != nil {
						// 下载工具可能等待用户确认或暂停；每个实际请求仍有一分钟停滞保护。
						idle = 10 * time.Minute
					}
					if file.state == "active" && (now.Sub(file.progress) > idle || (file.tool != nil && !now.Before(file.tool.expires))) {
						h.finishLocked(file, "failed")
					}
				}
				if checks%6 == 0 {
					owners = append(owners, room)
				}
			}
			for key, window := range h.limits {
				if now.Sub(window.Started) > time.Minute {
					delete(h.limits, key)
				}
			}
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
	window := h.limits[key]
	if time.Since(window.Started) > time.Minute {
		window = attemptWindow{Started: time.Now()}
	}
	if window.Count >= maxCount || (window.Count == 0 && len(h.limits) >= 1024) {
		return false
	}
	window.Count++
	h.limits[key] = window
	return true
}

func transferUnavailable() error {
	return &apiError{404, "TRANSFER_UNAVAILABLE", "连接已失效或不可用"}
}
func transferBusy() error {
	return &apiError{429, "TRANSFER_BUSY", "互传请求过多，请稍后重试"}
}

func transferDecode(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
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
	mux.HandleFunc("POST /api/transfer/create", a.protected(TextShare|FileShare, false, a.createTransfer))
	// 访客与临时会话入口不生成管理员别名，不接收管理员长期凭证。
	mux.ServeMux.HandleFunc("POST /api/transfer/join", a.handle(a.joinTransfer))
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
	if a.cfg.TransferDisabled {
		return transferUnavailable()
	}
	if !a.transferOrigin(r, false) {
		return forbidden()
	}
	var body struct {
		Name      string `json:"name"`
		RelayOnly bool   `json:"relay_only"`
	}
	if err := transferDecode(w, r, &body); err != nil {
		return err
	}
	name := transferName(body.Name)
	if name == "" {
		return badRequest("请输入有效的设备名称")
	}
	h := a.transfers
	ip := a.clientIP(r)
	h.mu.Lock()
	if h.ctx.Err() != nil || !h.allowLocked("create:"+ip, 4) || !h.allowLocked("create:all", 30) || len(h.rooms) >= transferRooms {
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
		var b [5]byte
		if _, err := rand.Read(b[:]); err != nil {
			h.mu.Unlock()
			return err
		}
		code = base32.NewEncoding("ABCDEFGHJKLMNPQRSTUVWXYZ23456789").EncodeToString(b[:])
		for _, room := range h.rooms {
			if room.code == code {
				code = ""
				break
			}
		}
	}
	ctx, cancel := context.WithDeadline(h.ctx, time.Now().Add(transferLifetime))
	room := &transferRoom{id: randomID(), code: code, owner: *p, ip: ip, created: time.Now(), expires: time.Now().Add(transferLifetime), files: map[string]*transferFile{}, ctx: ctx, cancel: cancel, relayOnly: body.RelayOnly, maxFile: h.maxFile}
	member := &transferMember{room: room, index: 0, name: name, created: time.Now(), out: make(chan any, transferQueue)}
	// 返回给浏览器的凭证只保留在页面内存，服务端只保存其摘要。
	secret := transferSecret()
	member.token = secretHash(secret)
	room.members[0] = member
	h.rooms[room.id] = room
	h.mu.Unlock()
	a.write(w, 201, map[string]any{"token": secret, "code": code, "member": 0, "expires_at": room.expires, "max_file_size": room.maxFile}, "连接已创建")
	return nil
}

func (a *App) joinTransfer(w http.ResponseWriter, r *http.Request) error {
	if a.cfg.TransferDisabled {
		return transferUnavailable()
	}
	if !a.transferOrigin(r, false) {
		return forbidden()
	}
	var body struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		RelayOnly bool   `json:"relay_only"`
	}
	if err := transferDecode(w, r, &body); err != nil {
		return err
	}
	h := a.transfers
	ip := a.clientIP(r)
	h.mu.Lock()
	if !h.allowLocked("join:"+ip, 10) || !h.allowLocked("join:all", 120) {
		h.mu.Unlock()
		return transferBusy()
	}
	code := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(body.Code), "-", ""))
	for _, room := range h.rooms {
		if code != room.code || len(code) != 8 || room.paired || room.members[1] != nil || !room.members[0].connected || time.Since(room.created) > 5*time.Minute || room.ctx.Err() != nil {
			continue
		}
		name := transferName(body.Name)
		if name == "" {
			h.mu.Unlock()
			return badRequest("请输入有效的设备名称")
		}
		secret := transferSecret()
		room.members[1] = &transferMember{room: room, index: 1, name: name, token: secretHash(secret), created: time.Now(), out: make(chan any, transferQueue)}
		room.relayOnly = room.relayOnly || body.RelayOnly
		h.mu.Unlock()
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
	if a.cfg.TransferDisabled || !a.transferOrigin(r, true) || r.Header.Get("Authorization") != "" {
		a.fail(w, forbidden())
		return
	}
	h := a.transfers
	ip := a.clientIP(r)
	h.mu.Lock()
	if h.ctx.Err() != nil || h.connectionCount >= transferConnections || h.connections[ip] >= 8 || !h.allowLocked("socket:"+ip, 30) {
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
	member.connected = true
	h.sendLocked(member, map[string]any{"type": "ready", "member": member.index})
	if member.index == 1 {
		h.sendLocked(member.room.members[0], map[string]any{"type": "joining", "name": member.name})
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
		if !h.allowLocked("message:"+member.token, 240) {
			h.mu.Unlock()
			return
		}
		err = h.messageLocked(member, message)
		if err != nil {
			h.sendLocked(member, map[string]any{"type": "error", "message": err.Error(), "id": message.ID, "client_id": message.ClientID})
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
		h.broadcastLocked(r, map[string]any{"type": "paired", "names": []string{r.members[0].name, r.members[1].name}, "relay_only": r.relayOnly, "stun": h.app.cfg.TransferSTUN})
		return nil
	}
	if !r.paired {
		return forbidden()
	}
	switch msg.Type {
	case "text":
		if len(msg.Text) == 0 || len(msg.Text) > 4096 {
			return badRequest("文字长度应为 1-4096 字节")
		}
		// 服务端统一记录发送时间，双方看到同一个时间值，不接受客户端伪造时间。
		h.broadcastLocked(r, map[string]any{"type": "text", "text": msg.Text, "from": m.index, "sent_at": time.Now().UTC().Format(time.RFC3339Nano)})
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
		if transferName(msg.Name) == "" || len(msg.ClientID) > 64 || msg.ClientID == "" || msg.Size < 0 || msg.Size > r.maxFile || len(r.files) >= transferFiles || r.offeredBytes+msg.Size > r.maxFile*4 {
			return badRequest("文件名称、大小或本次连接的文件额度不符合限制")
		}
		f := &transferFile{id: randomID(), clientID: msg.ClientID, name: transferName(msg.Name), size: msg.Size, sender: m.index, room: r, state: "offered", created: time.Now()}
		r.files[f.id] = f
		r.offeredBytes += f.size
		h.broadcastLocked(r, map[string]any{"type": "file", "id": f.id, "client_id": f.clientID, "name": f.name, "size": f.size, "from": f.sender, "state": "offered", "image_type": msg.ImageType, "sent_at": f.created.UTC().Format(time.RFC3339Nano)})
	case "accept":
		f := r.files[msg.ID]
		if f == nil || f.sender == m.index || f.state != "offered" {
			return forbidden()
		}
		for _, other := range r.files {
			if other.state == "active" {
				return badRequest("请先完成当前文件传输")
			}
		}
		if msg.Mode != "relay" && msg.Mode != "tool" && (msg.Mode != "p2p" || r.relayOnly) {
			return badRequest("传输方式无效")
		}
		if msg.Mode == "relay" && h.relays >= h.maxRelays {
			return transferBusy()
		}
		f.mode = msg.Mode
		f.state = "active"
		f.progress = time.Now()
		f.ctx, f.cancel = context.WithCancel(r.ctx)
		if f.mode == "relay" {
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
			h.sendLocked(m, map[string]any{"type": "download-tool", "id": f.id, "url": "/api/transfer/download/" + f.id + "/tool?ticket=" + secret, "expires_at": expires, "max_connections": min(4, h.maxRelays)})
		} else {
			h.sendLocked(r.members[f.sender], map[string]any{"type": "start", "id": f.id, "mode": "p2p"})
		}
		h.broadcastLocked(r, map[string]any{"type": "state", "id": f.id, "state": "active", "mode": f.mode})
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
		if f.bytes != f.size || f.tool.round == 0 {
			return badRequest("文件尚未传完，请下载完成后确认接收")
		}
		// 服务端无法得知外部软件的落盘结果，由接收方确认完成后撤销链接。
		h.finishLocked(f, "complete")
	case "progress", "complete":
		f := r.files[msg.ID]
		if f == nil || f.state != "active" || f.mode != "p2p" || f.sender == m.index || msg.Bytes < f.bytes || msg.Bytes > f.size {
			return forbidden()
		}
		f.bytes = msg.Bytes
		f.progress = time.Now()
		if msg.Type == "complete" {
			if f.bytes != f.size {
				return badRequest("文件长度不一致")
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
