# API 参考

[文档中心](README.md) · [使用指南](usage.md) · [部署指南](deployment.md)

本文档覆盖 CloudPaste 当前前端使用的全部 HTTP API 与 WebSocket 协议。

**目录**

- [通用约定与认证](#通用约定与认证)
- [快速调用示例](#快速调用示例)
- [公开接口](#公开接口)
- [创建与管理分享](#创建与管理分享)
- [管理员接口](#管理员接口)
- [临时设备互传](#临时设备互传)
- [操作日志](#操作日志)
- [限频策略](#限频策略)

---

## 通用约定与认证

### 身份与入口隔离

| 身份 | 接口前缀 | 认证头 |
| --- | --- | --- |
| **未登录访客** | `/api/*`（公开及已开放的访客创建入口） | 无（访客批量上传使用 `X-Guest-Token`） |
| **API Key 用户** | `/api/*` | `Authorization: ApiKey <key>` 或 `ApiKeyBase64 <base64>` |
| **管理员** | `/api/admin/*` | `Authorization: Bearer <session_token>` |

- **严格隔离**：管理员凭据只能访问 `/api/admin/*`；API Key 不能进入 `/api/admin/*`。外部反代保护管理员入口时应覆盖 `/api/admin` 及全部子路径。
- **非 ASCII 密钥**：API Key 含中文、空白或控制字符时，使用 `Authorization: ApiKeyBase64 <UTF-8标准Base64>`。
- **统一响应**：普通 JSON 接口返回 `{"success": true, "code": "OK", "message": "...", "data": ...}`；创建资源一般返回 HTTP `201`，下载接口直接返回文件流。

### 访客开关（分组 1）

常规设置中的 4 个布尔开关控制匿名访问：`guest_text_share_enabled`（默认 `false`）、`guest_file_upload_enabled`（默认 `false`）、`guest_transfer_create_enabled`（默认 `false`）、`guest_transfer_join_enabled`（默认 `true`）。关闭时前 3 项匿名调用返回 `401`，加入互传返回 `403`。

---

## 快速调用示例

### 1. 创建文本分享（API Key）

```sh
curl -X POST https://share.example.com/api/paste \
  -H "Authorization: ApiKey your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "部署备忘",
    "content": "# Hello CloudPaste",
    "slug": "my-note",
    "password": "optional-password",
    "max_views": 10
  }'
```

### 2. 流式上传文件并创建分享

请求体直接为**原始文件二进制**，文件名与选项通过请求头传递：

```sh
# X-Share-Options 为 UTF-8 JSON 的标准 Base64
OPTIONS_B64=$(printf '{"storage_config_id":"<storage_id>","expires_in":24}' | base64 -w 0)

curl -X PUT https://share.example.com/api/share/upload \
  -H "Authorization: ApiKey your-api-key" \
  -H "X-Share-Filename: demo.txt" \
  -H "X-Share-Options: ${OPTIONS_B64}" \
  --data-binary @demo.txt
```

---

## 公开接口

公开接口不接受管理员凭据；`GET` 路由同时支持 `HEAD`，`OPTIONS` 提供 CORS 预检响应。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/health` | 程序与容器健康检查 |
| `GET` | `/api/public/settings` | 公开站点设置白名单，支持 `?group=` |
| `GET` / `POST` | `/api/paste/{slug}` | 读取文本或提交密码验证 |
| `GET` | `/api/share/get/{slug}` | 获取文件/合集信息或密码提示 |
| `POST` | `/api/share/verify/{slug}` | 验证分享密码并签发读取票据 |
| `GET` | `/api/s/{slug}`、`/api/s/{slug}/{filename}` | 文件预览、下载、ZIP 打包与 Range 请求 |

- **密码解锁凭证**：带密码的分享验证成功后返回 `access_token`、`access_expires_at`；后续携带 `X-Share-Access` 请求头 10 分钟内免重复输密码（每次打开仍计次）。
- **读取票据**：文件预览/下载使用服务端返回的 `contentUrl`、`downloadUrl`（10 分钟有效，期间 Range 不重复计次）。

---

## 创建与管理分享

下表业务接口同时拥有 `/api/admin/*` 对应路径（如 `/api/paste` 对应 `/api/admin/paste`），供管理员使用。

| 方法 | 路径 | API Key 权限位 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/test/api-key` | 有效身份 | 校验 Key 并返回权限（仅普通路径） |
| `GET` | `/api/storage` | 文件浏览/上传 | 可用存储白名单（不含真实路径与凭据） |
| `POST` | `/api/paste` | `1` | 创建文本分享 |
| `GET` | `/api/pastes`、`/api/pastes/{id}` | `4` | 文本列表与详情 |
| `PUT` | `/api/pastes/{slug}` | `4` | 修改文本（建议携带 `id` 防后缀复用冲突） |
| `DELETE` | `/api/pastes/batch-delete` | `4` | 批量删除文本记录 |
| `GET` | `/api/files` | `8` | 文件分享列表 |
| `PUT` | `/api/files/{id}` | `8` | 修改文件分享（`filename` 仅改显示名） |
| `DELETE` | `/api/files/batch-delete` | `8` | 批量删除文件分享（支持保留原件） |
| `PUT` | `/api/share/upload` | `2 + 512` | 原始文件流式上传并创建分享 |
| `POST` | `/api/share/collection` | `2` | 将同一所有者的文件分享合并为合集 |
| `POST` | `/api/transfer/create` | `1 + 2` | 创建互传连接（详见[临时设备互传](#临时设备互传)） |

### 创建者查看失效分享（`/api/owner/*`）

持有效 API Key 且具备对应创建或管理权限的用户，可通过独立入口查看本人已过期或次数耗尽的分享（不消耗查看次数、不要求分享密码）：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/owner/paste/{slug}` | 本人文本内容 |
| `GET` | `/api/owner/share/get/{slug}` | 本人文件/合集信息并签发 `owner-share` 票据 |
| `GET` / `HEAD` | `/api/owner/s/{slug}`、`/api/owner/s/{slug}/{filename}` | 文件、子项或 ZIP 下载 |

### 关键规则

- **自定义链接（`slug`）**：1–100 个 `[A-Za-z0-9_-]`，留空自动生成。过期或次数耗尽的后缀可被新分享复用（旧记录自动重分配随机后缀并保留）。
- **有效期**：文本使用 `expires_at`（RFC 3339 或 `null`），文件上传使用 `expires_in`（小时，`0` 表示长期）；省略时采用站点默认有效期。
- **批量删除文件**：`DELETE /api/files/batch-delete` 请求体为 `{"ids":["..."],"delete_mode":"both"}`；`both`（默认）同时删存储原件，`record_only` 仅删记录。
- **上传状态码**：名称冲突返回 `409`，超过单文件上限（默认 10 GiB）返回 `413`，暂存总额度（默认 20 GiB）不足返回 `507`。

<details>
<summary><strong>补充细节：访客上传凭证、密码加密副本与嵌入图片限制</strong></summary>

- **访客上传**：仅能写入公开可写存储的隔离目录（`path` 为空或 `/`）。同一批文件使用 32 字节随机 Base64URL 的 `X-Guest-Token` 请求头归属合集，不可用于登录或管理。
- **本地/S3 上传**：本地写入隐藏临时文件校验刷盘后原子发布；S3 先写私有暂存再按需分片上传。已移除 HTTP 分片与 POST 表单上传。
- **密码明文与加密**：新建/重设密码同时保存校验哈希与 AES-GCM 加密副本，仅在管理列表/详情接口返回 `password` 与 `password_available`；所有公开/创建者/管理员预览接口均不返回密码。
- **Markdown 嵌入图片**：支持 PNG/JPEG/GIF/WebP 的 Base64 嵌入，单张 ≤ 1 MiB，整条 JSON 请求 ≤ 2 MiB，拒绝 SVG 数据 URI 与脚本协议。

</details>

---

## 管理员接口

所有管理员接口均要求 `Authorization: Bearer <token>` 且路径以 `/api/admin` 开头。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/api/admin/login` / `logout` / `change-password` | 登录、登出、修改账号密码 |
| `GET` | `/api/admin/test/admin-token` | 检查管理员会话有效性 |
| `GET` | `/api/admin/paste/{slug}` | 管理员查看文本（忽略密码与失效状态，不计次） |
| `GET` | `/api/admin/share/get/{slug}` | 管理员查看文件/合集（返回 `expiry_reason` 与 `admin-share` 票据） |
| `GET` | `/api/admin/s/{slug}`、`/api/admin/s/{slug}/{filename}` | 管理员预览、下载或 ZIP 打包 |
| `GET` / `POST` | `/api/admin/api-keys` | 列出或创建 API Key |
| `PUT` / `DELETE` | `/api/admin/api-keys/{id}` | 修改或删除 API Key |
| `GET` | `/api/admin/api-keys/{id}/reveal` | 读取完整密钥（`Cache-Control: no-store`） |
| `POST` | `/api/admin/storage` | 新增 LOCAL / S3 存储 |
| `PUT` / `DELETE` | `/api/admin/storage/{id}` | 修改或删除存储配置 |
| `PUT` | `/api/admin/storage/{id}/set-default` | 设为默认存储 |
| `POST` | `/api/admin/storage/{id}/test` | 测试存储连接 |
| `GET` | `/api/admin/settings` | 读取全部或指定分组设置（`?group=1..6`） |
| `PUT` | `/api/admin/settings/group/{group}` | 更新指定分组设置（立即生效、重启保留） |
| `GET` / `PUT` / `DELETE` | `/api/admin/ip-location` | 查询、上传（最大 512 MiB 原始 DB3 IPv4 BIN）或卸载 IP 归属地库 |
| `GET` / `DELETE` | `/api/admin/operation-logs` | 查询或按日期/模块批量清理操作日志 |

### 设置分组一览（`PUT /api/admin/settings/group/{group}`）

| 分组 ID | 名称 | 核心可写键 |
| --- | --- | --- |
| `1` | 常规 | `guest_*_enabled`、`share_default_expiry_hours`、`share_auto_cleanup_enabled`（默认 `false`）、`share_expired_retention_days`（`0–3650`，默认 `180`）、`max_upload_size`、`max_uploads`、`max_downloads`、`max_staged_size_mb` |
| `2` | 文件预览 | `preview_max_text_kb`（`16–2048`）、`preview_*_types`、`preview_allow_external_images` |
| `3` | 设备互传 | `transfer_enabled`、`transfer_reconnect_minutes`（`0–60`，默认 `5`）、`transfer_max_rooms`（`1–256`，默认 `32`）、`transfer_max_rooms_per_owner`（`1–32`，默认 `5`）、`transfer_rate_mb`、`transfer_max_relays`、`transfer_folder_concurrency`（`1–128`）、`transfer_max_file_mb`、`transfer_stun`（最多 5 行）、`transfer_debug_logs` |
| `4` | 站点外观 | `site_title`、`site_favicon_url`、`site_footer_markdown`、`site_home_editor_enabled`、`site_upload_page_enabled`、`site_github_enabled`、`site_github_url`、`site_announcement_*`、`site_custom_head/body` |
| `5` | 日志设置 | `audit_enabled`、`audit_retention_days`（`1–3650`，默认 `30`）、`audit_<module>_enabled` |
| `6` | 安全与限频 | 9 项每分钟限频值（`1–1000000`，详见[限频策略](#限频策略)） |

### 创建自定义 API Key 示例

```json
// POST /api/admin/api-keys
{
  "name": "临时分享用户",
  "custom_key": "replace-with-your-own-random-key",
  "permissions": 527,
  "role": "GENERAL",
  "is_enable": true,
  "basic_path": "/",
  "storage_config_ids": [],
  "expires_at": null
}
```

- `custom_key` 留空自动生成；`permissions` 为位掩码组合（`1` 创建文本、`2` 文件分享、`4` 管理文本、`8` 管理文件、`512` 上传能力，`527 = 1+2+4+8+512`）。

---

## 临时设备互传

房间与传输状态仅保存在进程内存中，不写入持久化存储。

### HTTP 入口

| 方法与路径 | 说明 |
| --- | --- |
| `POST /api/transfer/create` | 创建房间，返回 `token`、`code`（4 位数字）、`member`、`expires_at`、`max_file_size`（管理员走 `/api/admin/transfer/create`） |
| `POST /api/transfer/join` | 携带 `code` 申请加入（自动去除空白和横杠），返回第二席临时 `token` |
| `GET /api/transfer/socket` | WebSocket 入口（管理员与普通用户共用此路径） |
| `POST /api/transfer/leave` | 页面关闭/离开时通过 Beacon/keepalive 提交 `{"token":"..."}` 结束配对 |
| `GET /api/transfer/download/{id}?ticket=…` | 普通中继一次性下载流 |
| `PUT /api/transfer/upload/{id}` | 发送方 `Authorization: Transfer <token>` 原始文件流上传 |
| `GET /api/transfer/download/{id}/tool?ticket=…` | 外部下载工具短期票据，支持 `HEAD`、单个 `Range` 与重试 |
| `PUT /api/transfer/upload/{id}/{request}` | 外部下载工具分段上传入口 |

### WebSocket 连接与认证

- 握手要求同源或匹配 `ALLOWED_ORIGINS` 的 `Origin`，**禁止**在握手头或 URL 中携带长期凭据。
- 建立连接后 10 秒内发送首条认证帧，成功返回 `{"type":"ready","member":0,"paired":false}`：

```javascript
const url = new URL("/api/transfer/socket", backendBase);
url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
const socket = new WebSocket(url);
socket.addEventListener("open", () => {
  socket.send(JSON.stringify({ type: "auth", token: data.token }));
});
```

- **断线恢复**：已配对成员意外断线默认保留 5 分钟（可通过 `transfer_reconnect_minutes` 配置 0–60 分钟），重连首帧携带 `"resume": true`，认证后收到 `ready`、`sync` 及 `paired`（含 `generation`）或 `peer-offline`。

### WebSocket 消息速查

所有消息均为 UTF-8 JSON 文本（单帧 ≤ 16 KiB，不发送二进制帧，无 HTTP 响应包装）。

#### 客户端 → 服务端

| `type` | 关键字段 | 说明 |
| --- | --- | --- |
| `auth` | `token`（可选 `resume`） | 首帧认证 |
| `confirm` / `reject` / `leave` | 无 | 创建者确认配对 / 任一方拒绝或结束房间 |
| `text` | `text`（1–4096 字节） | 发送文本消息 |
| `signal` | `data`（≤ 12000 字节） | 转发 WebRTC 信令（非强制中继模式） |
| `offer` | `client_id`、`name`、`size`（可选 `image_type`、`folder`） | 发起文件/图片/文件夹条目传输 |
| `accept` | `id`、`mode`（`relay`/`p2p`/`tool`/`directory`） | 接收方确认接收方式 |
| `cancel` / `fail` | `id` | 取消或报告传输失败 |
| `progress` / `complete` | `id`、`bytes` | P2P 进度上报与写入完成确认 |
| `range-failed` / `finish-download` | `id`（及 `request_id`） | 下载工具分段失败取消 / 确认完整下载结束 |
| `folder-offer` / `accept-folder` / `cancel-folder` | `id`/`name`/`count`/`size`/`mode`/`concurrency` | 文件夹队列发起、接收与取消 |
| `folder-concurrency` / `folder-ready` / `folder-progress` | `id`/`concurrency`/`progress` | 运行中动态调整并发与批量进度上报 |

#### 服务端 → 客户端

| `type` | 关键字段 | 说明 |
| --- | --- | --- |
| `ready` | `member`、`paired` | 认证成功 |
| `joining` | `name`、`ip`、`device`、`location` | 通知创建者有设备请求加入 |
| `paired` | `names`、`ips`、`devices`、`locations`、`relay_only`、`stun`、`stun_servers` | 双方配对成功 |
| `locations` | `locations` 或 `joining_location` | 管理员更换/卸载 IP 库时实时刷新 |
| `text` | `text`、`from`、`sent_at` | 广播文字消息（服务端统一记录 UTC RFC 3339 时间） |
| `signal` | `data` | 转发对端信令 |
| `file` / `state` / `progress` | `id`、`state`、`mode`、`bytes`、`round` | 任务创建、状态流转（`offered → active → complete/failed/cancelled`）与进度 |
| `download` / `download-tool` | `id`、`url`（及 `expires_at`、`max_connections`） | 通知接收方中继或下载工具链接 |
| `start` / `range-start` / `range-stop` | `id`、`mode`、`token`、`request_id`、`start`、`end` | 通知发送方开始上传或分段上传 |
| `folder` / `folder-next` / `folder-state` / `folder-reset` / `relay-ready` | `id`、`count`、`state`、`bytes`、`completed` | 文件夹调度、并发重置与中继配对就绪 |
| `error` | `message`、`id`、`client_id` | 业务操作被拒绝（不含 HTTP 状态码） |

### 传输模式与协议细节

#### 文件夹队列

- 发送端通过 `folder-offer` 声明 `count`（1–10000，含空目录）和总 `size`；同一房间同时只运行一个文件夹队列，占 1 个逻辑任务额度（不受 32 个单文件计数限制，但共享单文件大小与总字节 `max_file_size × 4` 上限）。
- 接收端选好可写目录后发 `accept-folder`（并发 `1–128`，默认 `16`）；服务端通过 `folder-next` 发放条目额度，传输中支持 `folder-concurrency` 动态调整（经 `folder-reset` / `folder-ready` 同步，不中断在途文件）。
- 路径限制：≤ 1024 UTF-8 字节、≤ 16 层、单段 ≤ 180 字节；拒绝绝对路径、`.`/`..`、反斜杠、控制/格式字符、Windows 保留名及忽略大小写的重复/冲突路径。
- 中继模式下服务端在确认上传接入后发送 `relay-ready` 才启动下一条下载，避免浏览器连接池死锁；P2P 模式共享 8 MiB 未确认窗口与 4 MiB 发送缓冲。

#### 单文件和图片

- **聊天图片**：`offer` 携带 `image_type`（仅 `image/png` 或 `image/jpeg`，1 B–4 MiB），走 HTTP 中继不进 WebSocket。
- **普通中继**：接收端实际发起 `GET` 后服务端才给发送端下发 `start` 和一次性上传票据；上传与下载票据单次使用、不可互换。
- **下载工具（`mode:"tool"`）**：支持 `HEAD`、单段 `Range`（`206`/`416`）、`ETag`/`If-Range`；链接最长 30 分钟有效、闲置 10 分钟失效，每文件最多 4 路并发、256 次 GET、累计转发 ≤ `max(1 MiB, size×4)`；整轮完成推送 `tool-complete`，接收端发送 `finish-download` 后撤销链接。
- **P2P 分块与重协商**：支持通过 `signal.data` 协商 `max_chunk_size`（最高 256 KiB）与 `receive_window`（最高 8 MiB），失败/取消后递增 `generation` 自动重协商。

<details>
<summary><strong>补充协议：设备信息提示、IP 归属地与心跳超时</strong></summary>

- **设备提示**：创建/加入请求可附带 `device`（总请求体 ≤ 2 KiB），含 `platform`、`platform_version`、`mobile`、`brands`、`full_version_list`、`model`、`form_factors`、`touch_points`；后端结合 UA 生成摘要返回，原始 UA 不存房间也不写日志。
- **IP 归属地**：加载 IP2Location LITE DB3 IPv4 BIN 后，返回 `{country_code, country, region, summary, source:"ip2location"}`；IPv6 或未加载返回 `null`。CloudPaste uses the IP2Location LITE database for [IP geolocation](https://www.ip2location.com).
- **心跳与队列**：服务端每 20 秒发协议 Ping（等待 Pong 8 秒），单成员发送队列上限 416 条；房间最长存活 2 小时，配对码 5 分钟有效。

</details>

---

## 操作日志

- **查询**：`GET /api/admin/operation-logs` 支持 `limit`（默认 50，最大 200）、`offset`、`module`、`actor_type`、`actor`、`result`、`from`/`to`（RFC 3339），返回 `items`、`has_more`、`retention_days`、`enabled`（查询超时 3 秒，不统计全表总数）。
- **清理**：`DELETE /api/admin/operation-logs` 提交 `{"from":"...","to":"...","module":""}`，每次最多删 1000 条并返回 `deleted` 与 `has_more`。
- **枚举值**：
  - `module`：`auth`、`text`、`file`、`transfer`、`storage`、`keys`、`settings`
  - `actor_type`：`admin`、`apikey`、`guest`、`unknown`
  - `result`：`success`、`failure`、`cancelled`
- **合并与脱敏**：同 IP 高频同类失败或重复读取分享在 1 分钟内合并（`merged_requests`）；不记录密码、密钥、令牌、查询参数、分享正文或文件内容，**互传文字会记录正文**（≤ 4096 字节）。

---

## 限频策略

所有限频采用单进程内存内 **1 分钟固定窗口**（配置范围 `1–1000000`，`0` 不表示关闭），超限 HTTP 返回 `429`，WebSocket 消息超限直接断开。

| 操作 | 计数维度 | 默认值/分钟 | 设置键（分组 6） |
| --- | --- | --- | --- |
| 密码验证 | 每 IP（管理/访客独立） | 10 | `password_attempts_per_minute` |
| API Key 认证失败 | 每 IP（IPv6 按 `/64`） | 10 | `password_attempts_per_minute` |
| 创建互传连接 | 每 IP（IPv6 按 `/64`） | 30 | `transfer_create_per_minute` |
| 加入互传连接 | 每 IP（IPv6 按 `/64`） | 30 | `transfer_join_per_minute` |
| WebSocket 建连 | 每 IP（IPv6 按 `/64`） | 30 | `transfer_ws_connect_per_minute` |
| 普通 WebSocket 消息 | 每已认证成员 | 240 | `transfer_ws_messages_per_minute` |
| 普通中继上传/下载 | 每 IP 共享（IPv6 按 `/64`） | 180 | `transfer_relay_per_minute` |
| 下载工具 HEAD/GET/Range | 每 IP 共享（IPv6 按 `/64`） | 240 | `transfer_tool_per_minute` |
| 文件夹内部 WS 消息 | 每来源 IP（IPv6 按 `/64`） | 12000 | `transfer_folder_ws_messages_per_minute` |
| 文件夹中继上传/下载 | 每 IP 共享（IPv6 按 `/64`） | 12000 | `transfer_folder_relay_per_minute` |

- **独立探测表与 LRU 淘汰**：未知/无效票据使用独立探测表（上限 256 项），不挤占正常传输额度；限频表满时按 LRU 淘汰旧项，不拒绝新来源。
- **并发与房间上限**：普通 API 并发 16、密码验证并发 2；互传在线房间默认 32（同创建者/IP 默认 5，均可在分组 3 配置，WebSocket 连接上限随房间配额自动联动）。更多运维说明见 [部署指南：每分钟限频配置](deployment.md#每分钟限频配置)。
