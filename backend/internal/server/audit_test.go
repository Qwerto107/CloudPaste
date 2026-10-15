package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func waitAudit(t *testing.T, f *fixture, predicate string, count int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var got int
		if err := f.app.audit.db.QueryRow("SELECT count(*) FROM operation_logs WHERE " + predicate).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got >= count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("日志未按时写入")
}

// 加入请求和配对成功是两件事，创建者确认前不能出现成功加入的日志。
func TestAuditTransferJoinAfterConfirmation(t *testing.T) {
	f := newFixture(t)
	f.app.audit.enabled.Store(true)
	created := dataMap(t, f.request(t, "POST", "/api/transfer/create", f.admin, map[string]any{"name": "创建者"}, nil, 201))
	h := f.app.transfers
	h.mu.Lock()
	var room *transferRoom
	for _, candidate := range h.rooms {
		if candidate.code == created["code"] {
			room = candidate
			room.members[0].connected = true
			break
		}
	}
	h.mu.Unlock()
	if room == nil {
		t.Fatal("创建的连接不存在")
	}
	f.request(t, "POST", "/api/transfer/join", "", map[string]any{"code": created["code"], "name": "加入者"}, nil, 200)
	waitAudit(t, f, "action='申请加入互传'", 1)
	var count int
	if err := f.app.audit.db.QueryRow("SELECT count(*) FROM operation_logs WHERE action='加入互传'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("确认前已记录加入成功: %d, %v", count, err)
	}
	h.mu.Lock()
	room.members[1].connected = true
	err := h.messageLocked(room.members[0], transferMessage{Type: "confirm"})
	h.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	waitAudit(t, f, "action='加入互传'", 1)
	var actorType, method, endpoint string
	if err := f.app.audit.db.QueryRow("SELECT actor_type,method,endpoint FROM operation_logs WHERE action='加入互传'").Scan(&actorType, &method, &endpoint); err != nil || actorType != "guest" || method != "WS" || endpoint != "/api/transfer/socket" {
		t.Fatalf("配对日志身份或来源错误: %q %q %q, %v", actorType, method, endpoint, err)
	}
}

// 日志必须能追溯身份和结果，但不能带出请求正文、密码或下载票据。
func TestAuditAccessIdentityAndSecrets(t *testing.T) {
	f := newFixture(t)
	f.app.audit.enabled.Store(true)
	key, _ := f.key(t, "test-user", "/", TextShare)
	f.request(t, "POST", "/api/paste", key, map[string]any{"content": "private-body-sentinel", "password": "password-sentinel", "slug": "audit-text"}, nil, 201)
	f.request(t, "GET", "/api/paste/audit-text?token=query-secret-sentinel", "", nil, nil, 200)
	f.request(t, "POST", "/api/admin/login", "", map[string]any{"username": "admin", "password": "wrong-secret-sentinel"}, nil, 401)
	f.request(t, "GET", "/api/admin/operation-logs", "", nil, nil, 401)
	f.request(t, "GET", "/api/admin/operation-logs", key, nil, nil, 403)
	f.request(t, "GET", "/api/operation-logs", key, nil, nil, 404)
	waitAudit(t, f, "module='text'", 2)
	waitAudit(t, f, "module='auth' AND result='failure'", 1)
	response := f.request(t, "GET", "/api/admin/operation-logs?module=text&actor_type=apikey&result=success", f.admin, nil, nil, 200)
	var decoded struct {
		Data struct {
			Items []auditEvent `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data.Items) != 1 || decoded.Data.Items[0].ActorName != "test-user" || decoded.Data.Items[0].Action != "创建文本" {
		t.Fatalf("日志身份不正确: %s", response.Body.String())
	}
	all := f.request(t, "GET", "/api/admin/operation-logs", f.admin, nil, nil, 200).Body.String()
	for _, secret := range []string{"private-body-sentinel", "password-sentinel", "query-secret-sentinel", "wrong-secret-sentinel", strings.TrimPrefix(key, "ApiKey "), strings.TrimPrefix(f.admin, "Bearer ")} {
		if strings.Contains(all, secret) {
			t.Fatal("日志泄露敏感信息")
		}
	}
	// 参数必须作为值绑定，不能改变 WHERE 条件。
	data := dataMap(t, f.request(t, "GET", "/api/admin/operation-logs?module=%27%20OR%201%3D1--", f.admin, nil, nil, 200))
	if len(data["items"].([]any)) != 0 {
		t.Fatal("筛选条件可注入")
	}
	f.request(t, "GET", "/api/admin/operation-logs?from=invalid", f.admin, nil, nil, 400)
}

func TestAuditSettingsRetentionAndRedaction(t *testing.T) {
	f := newFixture(t)
	f.app.audit.enabled.Store(true)
	f.request(t, "PUT", "/api/admin/settings/group/5", f.admin, map[string]any{"audit_retention_days": 7}, nil, 200)
	f.request(t, "PUT", "/api/admin/settings/group/4", f.admin, map[string]any{"site_custom_head": "hidden-html-sentinel", "site_title": "new-title"}, nil, 200)
	for _, days := range []int{0, -1, 3651} {
		f.request(t, "PUT", "/api/admin/settings/group/5", f.admin, map[string]any{"audit_retention_days": days}, nil, 400)
	}
	if f.app.audit.days.Load() != 7 {
		t.Fatal("无效设置改变了保留天数")
	}
	waitAudit(t, f, "module='settings' AND result='success'", 2)
	response := f.request(t, "GET", "/api/admin/operation-logs?module=settings&result=success", f.admin, nil, nil, 200).Body.String()
	if strings.Contains(response, "hidden-html-sentinel") || !strings.Contains(response, "new-title") || !strings.Contains(response, "before") || !strings.Contains(response, "内容已隐藏") {
		t.Fatal("配置变更记录或脱敏错误")
	}
	f.app.audit.writeBatch([]auditEvent{{Time: time.Now().Add(-8 * 24 * time.Hour).UnixMilli(), Module: "text", Action: "expired-marker", Result: "success"}})
	if strings.Contains(f.request(t, "GET", "/api/admin/operation-logs", f.admin, nil, nil, 200).Body.String(), "expired-marker") {
		t.Fatal("列表展示了到期日志")
	}
	if f.app.audit.prune() != 1 {
		t.Fatal("到期日志未删除")
	}
	var raw string
	if err := f.app.db.QueryRow("SELECT value FROM settings WHERE key='audit_retention_days'").Scan(&raw); err != nil || raw != "7" {
		t.Fatal("保留时间未持久化")
	}
}

// 模拟日志库连接被占用并填满队列，业务数据库和请求仍必须可以继续运行。
func TestAuditBackpressureDoesNotBlockService(t *testing.T) {
	f := newFixture(t)
	f.app.audit.enabled.Store(true)
	conn, err := f.app.audit.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i := 0; i < 2000; i++ {
		f.app.recordAudit(auditEvent{Module: "text", Action: "load-test"})
	}
	if f.app.audit.dropped.Load() == 0 {
		t.Fatal("队列未按预期保持有界")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := httptest.NewRequest("POST", "/api/admin/paste", strings.NewReader(`{"content":"still available"}`)).WithContext(ctx)
	req.Header.Set("Authorization", f.admin)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("日志堵塞影响业务: %d %s", w.Code, w.Body.String())
	}
}

func TestAuditIPDoesNotResolveOrTrustSpoofedHeaders(t *testing.T) {
	proxies, err := parseTrustedProxies("frontend")
	if err != nil {
		t.Fatal(err)
	}
	proxies.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		t.Fatal("日志不应触发 DNS 查询")
		return nil, nil
	}
	a := &App{cfg: Config{TrustedProxies: proxies}}
	r := httptest.NewRequest("GET", "/api/paste/example", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.10")
	if a.auditClientIP(r) != "127.0.0.1" {
		t.Fatal("信任了未知代理的转发头")
	}
	proxies.cache["frontend"] = proxyDNSCache{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, expires: time.Now().Add(time.Minute)}
	if a.auditClientIP(r) != "203.0.113.10" {
		t.Fatal("未使用有效可信代理缓存")
	}
	proxies.mu.Lock()
	if a.auditClientIP(r) != "127.0.0.1" {
		t.Error("缓存繁忙时应使用连接地址")
	}
	proxies.mu.Unlock()
}

func TestAuditTransferAndStreaming(t *testing.T) {
	f := newFixture(t)
	f.app.audit.enabled.Store(true)
	m := &transferMember{audit: auditEvent{Module: "transfer", ActorType: "apikey", ActorID: "test-id", ActorName: "test-user", IP: "192.0.2.10"}}
	receiver := &transferMember{audit: auditEvent{IP: "198.51.100.20"}, out: make(chan any, 4)}
	room := &transferRoom{id: "room-id", members: [2]*transferMember{m, receiver}}
	m.out = make(chan any, 4)
	m.room = room
	receiver.room = room
	file := &transferFile{id: "file-id", name: "示例.txt", state: "active", mode: "p2p", room: room, created: time.Now(), size: 5, bytes: 5,
		connection: &transferConnection{Local: transferEndpoint{Address: "192.168.1.20", Port: 62000}, Remote: transferEndpoint{Address: "203.0.113.10", Port: 54000}}}
	f.app.transfers.finishLocked(file, "complete")
	f.app.transfers.finishLocked(file, "complete")
	waitAudit(t, f, "module='transfer'", 1)
	var count int
	if err := f.app.audit.db.QueryRow("SELECT count(*) FROM operation_logs WHERE module='transfer'").Scan(&count); err != nil || count != 1 {
		t.Fatal("传输结束重复记录")
	}
	var details string
	if err := f.app.audit.db.QueryRow("SELECT details FROM operation_logs WHERE target='file-id'").Scan(&details); err != nil || !strings.Contains(details, `"name":"示例.txt"`) || !strings.Contains(details, `"size":5`) || !strings.Contains(details, `"started_at"`) || !strings.Contains(details, `"sender_ip":"192.0.2.10"`) || !strings.Contains(details, `"receiver_ip":"198.51.100.20"`) || !strings.Contains(details, `"p2p_local":"192.168.1.20:62000"`) || !strings.Contains(details, `"p2p_remote":"203.0.113.10:54000"`) {
		t.Fatalf("文件详情未记录名称、大小和时间: %s, %v", details, err)
	}
	// 包装器保留刷新和 ResponseController 解包能力，不缓存响应正文。
	base := httptest.NewRecorder()
	writer := &auditWriter{ResponseWriter: base}
	if _, err := writer.Write([]byte("stream")); err != nil {
		t.Fatal(err)
	}
	if err := http.NewResponseController(writer).Flush(); err != nil {
		t.Fatal(err)
	}
	if !base.Flushed || base.Body.String() != "stream" {
		t.Fatal("日志包装影响流式响应")
	}
	for _, url := range []string{"/api/health", "/api/public/settings", "/api/transfer/download/file-id?ticket=secret", "/api/transfer/socket", "/api/owner", "/api/owner/owner/owner", "/paste/example"} {
		if _, record := auditRoute(httptest.NewRequest("GET", url, nil)); record {
			t.Fatal("高频请求不应逐条记录")
		}
	}
}

func TestAuditTransferTextAndImageMetadata(t *testing.T) {
	f := newFixture(t)
	f.app.audit.enabled.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sender := &transferMember{index: 0, out: make(chan any, 4), audit: auditEvent{Module: "transfer", ActorType: "guest", ActorName: "访客", IP: "192.0.2.10"}}
	receiver := &transferMember{index: 1, out: make(chan any, 4), audit: auditEvent{IP: "198.51.100.20"}}
	room := &transferRoom{id: "room-id", paired: true, members: [2]*transferMember{sender, receiver}, files: map[string]*transferFile{}, maxFile: 10 << 20, ctx: ctx}
	sender.room, receiver.room = room, room
	message := "第一行\n<script>alert(1)</script>"
	if err := f.app.transfers.messageLocked(sender, transferMessage{Type: "text", Text: message}); err != nil {
		t.Fatal(err)
	}
	if err := f.app.transfers.messageLocked(sender, transferMessage{Type: "offer", ClientID: "image-1", Name: "截图.png", ImageType: "image/png", Size: 1234}); err != nil {
		t.Fatal(err)
	}
	for _, image := range room.files {
		image.mode = "relay"
		image.state = "active"
		image.bytes = image.size
		f.app.transfers.finishLocked(image, "complete")
	}
	waitAudit(t, f, "module='transfer'", 2)
	var textDetails, imageDetails string
	if err := f.app.audit.db.QueryRow("SELECT details FROM operation_logs WHERE action='发送文本'").Scan(&textDetails); err != nil {
		t.Fatal(err)
	}
	if err := f.app.audit.db.QueryRow("SELECT details FROM operation_logs WHERE action='图片传输结束'").Scan(&imageDetails); err != nil {
		t.Fatal(err)
	}
	var textData, imageData map[string]any
	if json.Unmarshal([]byte(textDetails), &textData) != nil || textData["text"] != message || textData["bytes"] != float64(len(message)) {
		t.Fatalf("文字内容或大小记录不正确: %s", textDetails)
	}
	if json.Unmarshal([]byte(imageDetails), &imageData) != nil || imageData["image_type"] != "image/png" || imageData["size"] != float64(1234) || imageData["name"] != "截图.png" || imageData["sender_ip"] != "192.0.2.10" || imageData["receiver_ip"] != "198.51.100.20" {
		t.Fatalf("图片元数据记录不正确: %s", imageDetails)
	}
	if strings.Contains(imageDetails, "data:") {
		t.Fatal("图片日志不应保存原始内容")
	}
}

func TestTransferEndpointLabelRejectsUntrustedAddress(t *testing.T) {
	for _, test := range []struct {
		input transferEndpoint
		want  string
	}{
		{transferEndpoint{Address: "2001:db8::1", Port: 1234}, "[2001:db8::1]:1234"},
		{transferEndpoint{Address: "<script>", Port: 1234}, "浏览器未公开（端口 1234）"},
		{transferEndpoint{Address: "192.0.2.1", Port: 0}, "不可用"},
	} {
		if got := transferEndpointLabel(test.input); got != test.want {
			t.Fatalf("直连地址未安全格式化: %q", got)
		}
	}
}

func TestAuditSwitchDefaultsOffAndChangesImmediately(t *testing.T) {
	f := newFixture(t)
	if f.app.audit.enabled.Load() {
		t.Fatal("操作日志默认应关闭")
	}
	f.request(t, "GET", "/api/paste/disabled-audit-marker", "", nil, nil, 404)
	time.Sleep(300 * time.Millisecond)
	var count int
	if err := f.app.audit.db.QueryRow("SELECT count(*) FROM operation_logs").Scan(&count); err != nil || count != 0 {
		t.Fatal("关闭时仍写入日志")
	}
	f.request(t, "PUT", "/api/admin/settings/group/5", f.admin, map[string]any{"audit_enabled": true}, nil, 200)
	if !f.app.audit.enabled.Load() {
		t.Fatal("开启设置未立即生效")
	}
	f.request(t, "GET", "/api/paste/enabled-audit-marker", "", nil, nil, 404)
	waitAudit(t, f, "module='text'", 1)
	f.request(t, "PUT", "/api/admin/settings/group/5", f.admin, map[string]any{"audit_enabled": false}, nil, 200)
	if f.app.audit.enabled.Load() {
		t.Fatal("关闭设置未立即生效")
	}
	f.request(t, "GET", "/api/paste/disabled-again-marker", "", nil, nil, 404)
	time.Sleep(300 * time.Millisecond)
	if err := f.app.audit.db.QueryRow("SELECT count(*) FROM operation_logs WHERE module='text'").Scan(&count); err != nil || count != 1 {
		t.Fatal("关闭后仍写入新的文本日志")
	}
	var raw string
	if err := f.app.db.QueryRow("SELECT value FROM settings WHERE key='audit_enabled'").Scan(&raw); err != nil || raw != "false" {
		t.Fatal("开关状态未持久化")
	}
}

// 模块开关同时覆盖 HTTP 中间件与内部事件，保存后生效且重启后仍保留。
func TestAuditModuleSwitches(t *testing.T) {
	f := newFixture(t)
	settings := map[string]any{"audit_enabled": true}
	for module := range auditModuleBits {
		if f.app.audit.disabledModules.Load()&auditModuleBits[module] != 0 {
			t.Fatal("模块默认不应关闭")
		}
		settings["audit_"+module+"_enabled"] = false
	}
	f.request(t, "PUT", "/api/admin/settings/group/5", "", settings, nil, 401)
	key, _ := f.key(t, "audit-user", "/", TextShare)
	f.request(t, "PUT", "/api/admin/settings/group/5", key, settings, nil, 403)
	f.request(t, "PUT", "/api/admin/settings/group/5", f.admin, settings, nil, 200)
	f.request(t, "PUT", "/api/admin/settings/group/5", f.admin, map[string]any{"audit_text_enabled": "invalid"}, nil, 400)
	if raw := f.request(t, "GET", "/api/public/settings", "", nil, nil, 200).Body.String(); strings.Contains(raw, "audit_") {
		t.Fatal("公开设置暴露了日志配置")
	}
	f.request(t, "GET", "/api/public/settings?group=5", "", nil, nil, 403)
	data := f.request(t, "GET", "/api/admin/settings?group=5", f.admin, nil, nil, 200).Body.String()
	if !strings.Contains(data, "audit_transfer_enabled") {
		t.Fatal("日志设置分组缺少模块开关")
	}
	routes := map[string]string{
		"auth": "/api/admin/login", "text": "/api/paste", "file": "/api/share/upload",
		"transfer": "/api/transfer/create", "storage": "/api/storage", "keys": "/api/api-keys",
		"settings": "/api/admin/settings/group/5",
	}
	emit := func(marker string) {
		for module, path := range routes {
			method := "POST"
			if module == "file" || module == "settings" {
				method = "PUT"
			}
			// 业务响应应照常完成，开关只控制是否记录日志。
			called := false
			handler := f.app.auditHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				setAuditTarget(r, marker)
				w.WriteHeader(204)
			}))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if !called || w.Code != 204 {
				t.Fatal("模块开关影响业务响应")
			}
			f.app.recordAudit(auditEvent{Module: module, Action: marker, Target: marker})
		}
		member := &transferMember{audit: auditEvent{Module: "transfer"}}
		f.app.transfers.auditTransfer(member, marker, marker, "success", time.Now(), nil)
	}
	emit("disabled-module-marker")
	// 重启日志组件，校验持久化开关；关闭会排空队列，因此后面的空记录判断无需延时。
	f.app.audit.close()
	if err := f.app.startAudit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.app.audit.db.QueryRow("SELECT count(*) FROM operation_logs WHERE target='disabled-module-marker'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("关闭模块仍写入日志: %d, %v", count, err)
	}
	for module := range auditModuleBits {
		if f.app.audit.allows(module) {
			t.Fatalf("重启后模块开关未保留: %s", module)
		}
		settings["audit_"+module+"_enabled"] = true
	}
	f.request(t, "PUT", "/api/admin/settings/group/5", f.admin, settings, nil, 200)
	emit("enabled-module-marker")
	waitAudit(t, f, "target='enabled-module-marker'", 15)
	for module := range auditModuleBits {
		var count int
		if err := f.app.audit.db.QueryRow("SELECT count(*) FROM operation_logs WHERE module=? AND target='enabled-module-marker'", module).Scan(&count); err != nil || count < 2 {
			t.Fatalf("开启模块后未记录 HTTP 或内部事件: %s, %d, %v", module, count, err)
		}
	}
}

func TestAuditAdminDownloadNameAndLegacyDisplay(t *testing.T) {
	f := newFixture(t)
	var adminID string
	if err := f.app.db.QueryRow("SELECT id FROM admins WHERE username='admin'").Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	// 文件由浏览器原生请求读取时，票据要恢复完整的管理员身份。
	token := f.app.ticket(Ticket{
		Purpose: "admin-share", Path: "audit-file", Subject: "share-id",
		Session: secretHash(strings.TrimPrefix(f.admin, "Bearer ")),
		Expires: time.Now().Add(time.Minute).Unix(),
	})
	req := httptest.NewRequest("GET", "/api/admin/s/audit-file/name?token="+token, nil)
	actor, err := f.app.adminDownloadPrincipal(req)
	if err != nil || actor.ID != adminID || actor.Name != "admin" {
		t.Fatalf("文件票据丢失管理员姓名: actor=%+v, error=%v", actor, err)
	}

	// 旧记录保持原样存储，但列表应补全仍存在的管理员用户名。
	f.app.audit.writeBatch([]auditEvent{{Time: time.Now().UnixMilli(), Module: "file", Action: "读取文件", ActorType: "admin", ActorID: adminID}})
	data := dataMap(t, f.request(t, "GET", "/api/admin/operation-logs?module=file", f.admin, nil, nil, 200))
	items := data["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["actor_name"] != "admin" {
		t.Fatalf("旧日志仍缺少管理员用户名: %+v", items)
	}
	var stored string
	if err := f.app.audit.db.QueryRow("SELECT actor_name FROM operation_logs WHERE action='读取文件'").Scan(&stored); err != nil || stored != "" {
		t.Fatal("展示补全不应修改历史记录")
	}
}

func TestAuditDeleteByTimeAndModule(t *testing.T) {
	f := newFixture(t)
	now := time.Now().Truncate(time.Second)
	f.app.audit.writeBatch([]auditEvent{
		{Time: now.Add(-2 * time.Hour).UnixMilli(), Module: "text", Action: "before"},
		{Time: now.Add(-time.Hour).UnixMilli(), Module: "text", Action: "remove"},
		{Time: now.Add(-time.Hour).UnixMilli(), Module: "file", Action: "keep-module"},
		{Time: now.UnixMilli(), Module: "text", Action: "after"},
	})
	from, to := now.Add(-90*time.Minute).Format(time.RFC3339), now.Add(-30*time.Minute).Format(time.RFC3339)
	body := map[string]any{"from": from, "to": to, "module": "text"}
	f.request(t, "DELETE", "/api/admin/operation-logs", "", body, nil, 401)
	key, _ := f.key(t, "reader", "/", TextShare)
	f.request(t, "DELETE", "/api/admin/operation-logs", key, body, nil, 403)
	for _, invalid := range []map[string]any{
		{"from": "invalid", "to": to},
		{"from": to, "to": from},
		{"from": from, "to": to, "module": "text' OR 1=1 --"},
	} {
		f.request(t, "DELETE", "/api/admin/operation-logs", f.admin, invalid, nil, 400)
	}
	data := dataMap(t, f.request(t, "DELETE", "/api/admin/operation-logs", f.admin, body, nil, 200))
	if data["deleted"] != float64(1) || data["has_more"] != false {
		t.Fatalf("清理结果不正确: %+v", data)
	}
	rows, err := f.app.audit.db.Query("SELECT action FROM operation_logs ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actions := []string{}
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(actions, ",") != "before,keep-module,after" {
		t.Fatalf("清理影响了范围外的日志: %v", actions)
	}
}

func TestAuditDeleteContinuesInBoundedBatches(t *testing.T) {
	f := newFixture(t)
	now := time.Now().Truncate(time.Second)
	// 超过单次上限时必须返回剩余标记，下一次调用继续删除。
	events := make([]auditEvent, 1001)
	for i := range events {
		events[i] = auditEvent{Time: now.UnixMilli(), Module: "text", Action: "batch"}
	}
	f.app.audit.writeBatch(events)
	var count int
	if err := f.app.audit.db.QueryRow("SELECT count(*) FROM operation_logs").Scan(&count); err != nil || count != len(events) {
		t.Fatalf("测试日志未写入: count=%d, error=%v", count, err)
	}
	body := map[string]any{
		"from": now.Add(-time.Second).Format(time.RFC3339),
		"to":   now.Add(time.Second).Format(time.RFC3339),
	}
	first := dataMap(t, f.request(t, "DELETE", "/api/admin/operation-logs", f.admin, body, nil, 200))
	if first["deleted"] != float64(1000) || first["has_more"] != true {
		t.Fatalf("首批清理结果不正确: %+v", first)
	}
	second := dataMap(t, f.request(t, "DELETE", "/api/admin/operation-logs", f.admin, body, nil, 200))
	if second["deleted"] != float64(1) || second["has_more"] != false {
		t.Fatalf("第二批清理结果不正确: %+v", second)
	}
}
