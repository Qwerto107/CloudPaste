# API 参考

本文对应当前前端使用的 HTTP API 与 WebSocket 协议。返回 [文档中心](README.md)。

## 身份与入口

| 身份         | 入口               | 认证                                  |
| ------------ | ------------------ | ------------------------------------- |
| 未登录访客   | 公开入口及允许的创建入口 | 分享仍校验密码、过期、次数和读取票据 |
| API Key 用户 | 普通 /api 路径     | Authorization: ApiKey 或 ApiKeyBase64 |
| 管理员       | /api/admin 路径    | Authorization: Bearer                 |

API Key 必须启用且未过期，并满足权限、路径范围与存储 ACL；分享管理还检查归属。密钥含中文、空白或控制字符时，用 ApiKeyBase64 传输完整密钥的 UTF-8 标准 Base64，前端自动处理。

前端身份入口为 /login、/manage/* 和 /admin/login、/admin/*。管理员凭据只能用于管理员 API，API Key 不能进入管理员 API。外部反向代理的管理访问控制应覆盖 /api/admin 本身和全部子路径，包括登录。

常规设置（分组 1）支持四个布尔开关：`guest_text_share_enabled`、`guest_file_upload_enabled`、`guest_transfer_create_enabled` 默认 `false`，`guest_transfer_join_enabled` 默认 `true`。分别允许无认证请求调用 `POST /api/paste`、`PUT /api/share/upload`（含 `GET /api/storage`、`POST /api/share/collection`）、`POST /api/transfer/create`、`POST /api/transfer/join`。前三项关闭时匿名请求返回 401，加入关闭时返回 403；已认证请求沿用原权限。访客不能创建私有文本或使用管理、创建者预览接口。

访客上传仅可使用公开、启用且可写的存储，`path` 只能为空或 `/`，实际写入隔离目录。浏览器为同一批上传生成 32 字节随机凭证，以无填充 Base64URL 的 `X-Guest-Token` 请求头发送，创建合集时沿用同一凭证；服务端只保存其摘要作为归属。此凭证不能用于登录、管理或查看失效内容。自定义客户端合并文件时也需保持该随机凭证，禁止使用可猜测的值。

## 操作日志

`GET /api/admin/operation-logs` 仅管理员可调用，返回 `items`、`has_more`、`retention_days`、`enabled`。支持 `limit`（默认 50，最大 200）、`offset`、`module`、`actor_type`、`actor`（名称包含匹配或完整 ID）、`result`、`from` / `to`（RFC3339）。查询参数均绑定，查询超时为 3 秒；不执行全表总数统计。

`DELETE /api/admin/operation-logs` 仅管理员可调用，JSON 请求体必须包含 RFC3339 格式的 `from`、`to`，可选 `module`（留空表示全部模块）；删除闭区间内已保存的日志。每次最多删除 1000 条，返回 `deleted` 和 `has_more`；若 `has_more=true`，用相同条件继续调用。删除不可恢复，刚进入异步队列的记录可能稍后写入。

模块：`auth`、`text`、`file`、`transfer`、`storage`、`keys`、`settings`；身份：`admin`、`apikey`、`guest`、`unknown`；结果：`success`、`failure`、`cancelled`。记录字段包含 `id`、`time`（Unix 毫秒）、`module`、`action`、`actor_type`、`actor_id`、`actor_name`、`ip`、`target`、`method`、`endpoint`（不含查询串的路由模板）、`status`（WebSocket 事件为 0）、`result`、`duration_ms`、`details`（JSON 字符串）。互传直连完成状态由接收端上报。

通过 `PUT /api/admin/settings/group/5` 设置 `audit_enabled`（默认 `false`）和 `audit_retention_days`（1–3650，默认 30）。日志设置对应“系统设置 → 日志设置”选项卡，并可通过 `GET /api/admin/settings?group=5` 读取；公开设置不返回此分组。七个模块分别使用 `audit_auth_enabled`、`audit_text_enabled`、`audit_file_enabled`、`audit_transfer_enabled`、`audit_storage_enabled`、`audit_keys_enabled`、`audit_settings_enabled`（布尔值，默认 `true`）。仅当总开关与对应模块开关均开启时记录新操作；保存后立即生效并持久化。关闭开关后不再接收新日志，已有记录仍可查询或清理。日志写入为异步有界批处理，可能稍有延迟；不提供公开接口、修改接口或跨用户访问。互传文字会记录正文（单条最多 4096 字节），文件与图片仅记录名称、大小、时间等元数据，不保存文件或图片内容；密码、凭据和分享正文不进入日志。文件与图片被接受时记录开始，任务结束时记录最终结果；下载工具中继每轮完整下载另记“下载完成”，重复下载分别记录，完成传输不自动撤销链接。互传文件和图片日志包含两端服务端可见 IP；点对点完成事件还可包含接收端浏览器上报的直连 `IP:端口`，若浏览器隐藏地址或统计不可用则可能缺失。普通文件 Range、互传分段和进度请求不逐条记录。后台日志页面读取本身不产生日志。

同一 IP 的高频同类失败请求，以及重复读取同一分享的请求，在一分钟内合并。首条请求单独记录，其余请求写成带 `merged_requests` 数量的汇总；扫描不同无效路径不会逐条落库。来源过多时按模块汇总。此机制只限制日志增长，不改变请求响应。

互传申请成功的“申请加入互传”日志包含 `room`、`creator_name/type/account/ip`、`joiner_name/type/account/ip`（分别为四个独立字段）、`requested_at`、`relay_only` 和 `join_state:"pending"`。创建者同意后另记“加入互传”，保留相同申请时间，并补充 `approved_at` 和 `join_state:"approved"`；来源 IP 使用该阶段服务端保存的地址。时间为 UTC RFC3339，身份值沿用 `admin/apikey/guest`。不记录配对码或成员凭证，不提前记录尚未建立的点对点地址。

## 运行参数设置

管理员通过 `PUT /api/admin/settings/group/1` 更新 `max_uploads`、`max_downloads`（1–65535）、`max_upload_size`（MiB，1–10240）和 `max_staged_size_mb`（MiB，1–102400）。暂存额度不能小于单文件上限，按整次提交合并校验，不会部分保存。并发或额度调低后，已有请求继续完成，新请求按当前占用与新额度判断。

`PUT /api/admin/settings/group/3` 中的 `transfer_enabled` 控制新建和加入互传，关闭不影响已有连接继续传输。中继速率、互传单文件上限沿用同组现有设置，不再受环境变量上限覆盖。

限频使用 `PUT /api/admin/settings/group/6` 更新下表的九个设置项。第 6 组、普通上传下载并发及暂存额度仅向管理员返回，公开设置仍提供单文件上传上限和互传开关。原业务和限频环境变量均不再读取，已保存的后台设置优先，未保存时使用默认值。

## 限频策略

不再设置跨 IP 的每分钟总次数。下表使用一分钟固定窗口，在“系统设置 → 安全与限频”配置，范围为 1–1000000 的整数；`0` 不表示关闭限频，非法值无法保存。

| 操作                         | 计数范围                 | 默认次数/分钟 | 后台设置项                          |
| ---------------------------- | ------------------------ | ------------- | --------------------------------- |
| 密码验证                     | 每 IP，管理/访客入口独立 | 10            | `password_attempts_per_minute`    |
| API Key 失败验证             | 每 IP，与密码额度独立    | 10            | `password_attempts_per_minute`    |
| 创建互传连接                 | 每 IP                    | 30            | `transfer_create_per_minute`      |
| 输入配对码加入连接           | 每 IP                    | 30            | `transfer_join_per_minute`        |
| 普通实时中继上传、下载       | 两者共享每 IP 的额度     | 180           | `transfer_relay_per_minute`       |
| WebSocket 连接尝试           | 每 IP                    | 30            | `transfer_ws_connect_per_minute`  |
| 已认证 WebSocket 应用消息    | 每成员                   | 240           | `transfer_ws_messages_per_minute` |
| 下载工具 HEAD、GET、分段 GET | 共享每 IP 的额度         | 240           | `transfer_tool_per_minute`        |
| 已授权文件夹内部 WebSocket 消息 | 每成员 | 12000 | `transfer_folder_ws_messages_per_minute` |
| 有效文件夹票据的中继上传、下载 | 两者共享每 IP 的额度 | 12000 | `transfer_folder_relay_per_minute` |

密码验证成功或失败均计数，配对码未匹配也计数；格式或授权在计数前被拒绝的请求不计数。中继在票据长度合法后计数：有效票据使用普通中继或文件夹中继额度；未知编号、无效票据和票据重放使用独立探测表，次数由 `transfer_relay_per_minute` 控制，不挤占正常传输额度。下载工具在确认有效票据后计数，HEAD 和非法 Range 也计入每分钟限频。创建文件夹、确认接收、修改并发及聊天、信令仍计入普通 WebSocket 消息额度。各操作的额度独立，普通中继、文件夹中继与下载工具不共享次数。HTTP 超限返回 429；WebSocket 应用消息超限直接断开。

API Key 仅认证失败计数，包括无效、停用、过期及格式错误；成功认证和后续权限不足不计数。`ApiKey`、`ApiKeyBase64`、`X-Custom-Auth-Key` 共用来源额度，达到次数后该来源的 API Key 认证均返回 429，直到窗口结束。管理登录和分享密码验证不受此额度影响。

互传按 IP 计数的操作及 API Key 失败验证，将 IPv6 按 `/64` 前缀聚合，防止同一网段轮换地址绕过限频；日志仍保留完整来源 IP。互传正常表最多 1024 项，探测表最多 256 项，API Key 失败表最多 2048 项；表满时按 LRU 淘汰旧条目，继续接纳新来源，不因表满拒绝所有新 IP。

窗口从各计数项首次请求起算，一分钟后恢复额度。计数保存在单个服务进程内，重启清空，多实例之间不共享。设置保存后立即生效且重启保留，修改不清空当前计数，详情见 [部署指南的限频配置](deployment.md#每分钟限频配置)。

并发、房间容量和带宽保护继续生效：普通 API 总并发 16，密码验证总并发 2；互传在线房间最多 16，同一创建者或 IP 最多 2 个房间；WebSocket 总连接最多 40、每 IP 最多 8。中继并发与总带宽仍由后台保护设置控制，下载工具每文件并发、累计请求数和流量上限也保留。限频放宽不会增加这些容量。

## 公开接口

| 方法       | 路径                                    | 用途                                |
| ---------- | --------------------------------------- | ----------------------------------- |
| GET        | /api/health                             | 程序、容器的健康检查                |
| GET        | /api/public/settings                    | 公开站点设置白名单，支持 group 参数 |
| GET / POST | /api/paste/{slug}                       | 读取文本或提交密码                  |
| GET        | /api/share/get/{slug}                   | 分享信息或密码提示                  |
| POST       | /api/share/verify/{slug}                | 验证分享密码并签发读取票据          |
| GET        | /api/s/{slug}、/api/s/{slug}/{filename} | 文件或合集读取，支持下载与 Range    |

这些公开接口不接受管理员凭据。私有文本不向访客开放；访客查看管理员创建的分享仍使用普通路径。GET 路由同时匹配 HEAD；OPTIONS 提供预检响应。

## 创建与管理分享

下面的业务接口同时提供对应的管理员路径，例如 /api/paste 对应 /api/admin/paste。

| 方法   | 普通路径                      | API Key 权限                       |
| ------ | ----------------------------- | ---------------------------------- |
| GET    | /api/test/api-key             | 有效身份；仅普通路径               |
| GET    | /api/storage                  | 存储选择列表，需文件浏览或上传权限 |
| POST   | /api/paste                    | 创建文本，1                        |
| GET    | /api/pastes、/api/pastes/{id} | 文本列表与详情，4                  |
| PUT    | /api/pastes/{slug}            | 修改文本，4                        |
| DELETE | /api/pastes/batch-delete      | 删除文本记录，4                    |
| GET    | /api/files                    | 文件分享列表，8                    |
| PUT    | /api/files/{id}               | 修改文件分享，8                    |
| DELETE | /api/files/batch-delete       | 删除文件分享，8；可选择保留原文件  |
| PUT    | /api/share/upload             | 原始文件上传并分享，2+512          |
| POST   | /api/share/collection         | 将同一所有者的文件分享组成合集，2  |
| POST   | /api/transfer/create          | 创建互传连接，1+2；详见下文        |

普通存储列表使用字段白名单：id、name、storage_type、is_public、is_default、status、readonly、capabilities、upload_protocol。真实目录、桶、上游地址和备注仅供管理员查看；列表不返回存储凭据。

## 管理员接口

| 方法         | 路径                                                | 用途                                                            |
| ------------ | --------------------------------------------------- | --------------------------------------------------------------- |
| POST         | /api/admin/login                                    | 管理员登录                                                      |
| POST         | /api/admin/logout                                   | 撤销当前会话                                                    |
| POST         | /api/admin/change-password                          | 修改用户名或密码，并撤销会话                                    |
| GET          | /api/admin/test/admin-token                         | 检查会话                                                        |
| GET          | /api/admin/paste/{slug}                             | 管理员查看文本，忽略密码、期限和查看上限，不计次                |
| GET          | /api/admin/share/get/{slug}                         | 管理员查看文件或合集，返回失效原因和管理员下载票据，不计次      |
| GET          | /api/admin/s/{slug}、/api/admin/s/{slug}/{filename} | 管理员预览、下载或 ZIP 打包，支持 Range；需会话或管理员短期票据 |
| GET / POST   | /api/admin/api-keys                                 | 列表或创建密钥                                                  |
| PUT / DELETE | /api/admin/api-keys/{id}                            | 修改或删除密钥                                                  |
| GET          | /api/admin/api-keys/{id}/reveal                     | 读取完整密钥，响应禁止缓存                                      |
| POST         | /api/admin/storage                                  | 创建存储                                                        |
| PUT / DELETE | /api/admin/storage/{id}                             | 修改或删除存储配置                                              |
| PUT          | /api/admin/storage/{id}/set-default                 | 设置默认存储                                                    |
| POST         | /api/admin/storage/{id}/test                        | 测试存储连接                                                    |
| GET          | /api/admin/settings                                 | 读取设置，支持 group 参数                                       |
| PUT          | /api/admin/settings/group/{group}                   | 修改可写设置                                                    |

密钥的 storage_config_ids 随创建或修改密钥提交。配置表单由前端定义，后端负责校验。历史数据库中的挂载和目录密码仍参与路径授权；不再提供挂载管理、文件浏览、目录元信息管理、HTTP 分片上传或动态表单 schema 接口。

站点外观设置通过 `PUT /api/admin/settings/group/4` 保存，并由公开设置接口返回：`site_announcement_default_open`（布尔值，默认 false）控制启用且有内容的公告是否默认展开；`site_github_enabled`（布尔值，默认 true）控制右上角 GitHub 图标；`site_github_url` 默认 `https://github.com/Qwerto107/CloudPaste`，支持不含用户凭据的 HTTP/HTTPS 地址，留空隐藏图标。设置保存在数据库，重启保留。

## 上传与访问控制

普通 JSON 响应包含 success、code、message、data；创建资源一般返回 HTTP 201。下载直接返回文件内容。

文件上传只使用 PUT /api/share/upload，管理员使用 /api/admin/share/upload。请求体是原始文件，X-Share-Filename 为 URL 编码文件名；X-Share-Options 为 UTF-8 JSON 的标准 Base64，包含 storage_config_id、expires_in（小时，允许小数）、password、remark、max_views、可选 path、slug 和 original_filename。默认以随机名称保存文件，original_filename:true 时保留原名但仍禁止覆盖。

本地上传直接写入目标目录的隐藏临时文件，校验实际大小、当前授权和目标配置并刷盘后原子发布，不再完整复制文件。上传失败时清理临时文件；进程中断遗留的本地临时文件在下次启动回收。S3 上传仍先写私有暂存，再按需分片保存。单文件默认最大 10 GiB（10240 MiB），可在后台调低；两种存储共用默认 20 GiB 暂存总额度。冲突返回 409，超过单文件上限返回 413，临时额度不足返回 507。HTTP 分片及 POST 表单上传已移除。

新文本省略 expires_at、新文件省略 expires_in 时使用站点默认有效期。文本 expires_at:null、文件 expires_in:0 表示不过期。分享过期只撤销访客访问，不自动删除原文件。

`DELETE /api/files/batch-delete` 请求体为 `{"ids":["记录ID"],"delete_mode":"both"}`。省略 `delete_mode` 或使用 `both` 时同时删除原文件；`record_only` 仅删除记录。合集会删除全部子文件。删除检查归属、当前路径与存储授权、来源版本和只读状态；文件已不存在时允许继续清理记录。存储删除失败时当前记录保留，可重试；合集或批量删除可能部分成功，数据库与外部存储不构成跨系统事务。

管理员查看返回 `admin_preview:true` 和 `expiry_reason`（空、已过期、查看次数已用完），失效分享仍可读取现有内容；来源配置与权限变化仍需复核。管理员下载票据用途为 `admin-share`，绑定记录版本和管理员会话，10 分钟后失效，退出或修改账号后立即撤销；不能用于公开入口。访客和 API Key 不能调用管理员查看入口。

创建者使用独立普通认证入口查看本人分享，持有效 API Key 且有对应创建或管理权限即可访问：

| 方法       | 路径                                                    | 用途                                     |
| ---------- | ------------------------------------------------------- | ---------------------------------------- |
| GET        | `/api/owner/paste/{slug}`                               | 本人文本内容，支持过期及次数耗尽，不计次 |
| GET        | `/api/owner/share/get/{slug}`                           | 本人文件或合集信息，签发读取票据，不计次 |
| GET / HEAD | `/api/owner/s/{slug}`、`/api/owner/s/{slug}/{filename}` | 文件、合集子项或 ZIP 下载，不计次        |

创建者查看返回 `owner_preview:true`、`expiry_reason`，不要求分享密码。其他创建者返回 404，匿名信息请求返回 401。读取票据用途为 `owner-share`，绑定分享 ID、版本和 API Key 摘要，10 分钟有效；原生下载不需要在 URL 中携带 API Key。每次读取重新校验凭据启用、有效期、权限、归属及文件来源；禁用、删除或更换 Key 后旧票据不可用，也不能转到管理员或公开入口。

分享创建、管理列表、管理详情和修改响应返回 `password` 与 `password_available`，创建结果可据此生成带密码的分享文字。新建和重新设置的密码按验证哈希与 AES-GCM 加密副本保存，哈希及密文不进入响应。旧记录没有加密副本时 `password_available:false`，省略 `password`，原保护仍有效；必须重新设置才能查看原文。分享预览响应均不返回密码或加密副本，包括认证的创建者、管理员预览；公开密码验证响应也不返回这些字段。编辑 `password` 可直接替换，显式空字符串移除保护，省略则保留；兼容 `clearPassword:true`。

文本编辑器支持粘贴图片和通过工具栏插入图片链接。PNG、JPEG、GIF、WebP 图片以 Base64 嵌入 Markdown，单张最多 1 MiB，随文本一起保存和受密码保护；完整 JSON 请求仍限制为 2 MiB。站外图片链接是否展示由 `preview_allow_external_images` 控制，不允许 SVG 数据图片或脚本协议。

带密码的文本、文件和合集验证成功后返回 access_token、access_expires_at。后续使用 X-Share-Access 携带凭证，10 分钟内免于再次输入密码，每次成功打开仍消耗查看次数。刷新不延长凭证期限，修改密码、有效期或权限后会重新校验。

文件预览使用服务端返回的 contentUrl、downloadUrl；合集中的每个文件和 ZIP 下载共用同一次查看票据。客户端不能自行构造来源地址。

PUT /api/files/{id} 支持 filename、remark、password、clearPassword、expires_at、max_views、slug。filename 只修改显示名。省略 max_views 保留当前计数；显式修改上限重置计数。

常规、预览、互传、站点、日志、安全与限频分组分别为 1、2、3、4、5、6。只读设置不能修改；上传和互传单文件最大 10 GiB。文本预览限制为 16–2048 KiB，Markdown 默认禁止站外图片，自定义 HTML 始终经过清洗。

## 临时设备互传

管理员通过 `PUT /api/admin/settings/group/3` 保存保护设置：`transfer_rate_mb`（0–10240 MiB/s，0 为不限速）、`transfer_max_relays`（默认 64，非负整数，无固定上限，0 为不限制总并发）、`transfer_folder_concurrency`（默认 16，1–128）和 `transfer_max_file_mb`（1–10240 MiB）。速率立即应用，并发限制用于新传输，文件大小用于新房间。GET 同组返回当前有效值；保存后重启仍保留。

同组支持 `transfer_debug_logs`（布尔值，默认 `false`），控制互传浏览器控制台的 `[CloudPaste P2P]` 调试日志。保存后新建或加入连接读取最新值；`GET /api/public/settings?group=3` 返回该开关，方便未登录的加入者使用。开启时记录双方候选地址、端口、协议、选中的链路、往返延迟、协商耗时和传输摘要；浏览器隐藏的地址保留为空或 mDNS 名称。不输出凭证、配对码、用户内容或完整 SDP，也不轮询或逐块打印。

页面入口为 `/transfer`。此功能与持久分享、存储和查看次数无关，房间与传输状态仅保存在进程内存中，文件内容不保存。

| 方法与路径                                      | 凭据与用途                                                                       |
| ----------------------------------------------- | -------------------------------------------------------------------------------- |
| `POST /api/transfer/create`                     | 允许访客时无需登录；API Key 必须同时具备文本与文件分享权限，管理员改用 `/api/admin/transfer/create` |
| `POST /api/transfer/join`                       | 通过配对码申请第二席；匿名加入受访客开关控制，登录用户附带对应凭据（管理员走 `/api/admin/transfer/join`） |
| `GET /api/transfer/socket`                      | WebSocket，首条 JSON 消息提交临时成员票据                                        |
| `GET /api/transfer/download/{id}?ticket=…`      | 接收方一次性票据，返回附件流                                                     |
| `PUT /api/transfer/upload/{id}`                 | 发送方 `Authorization: Transfer …`，请求体为原文件流                             |
| `GET /api/transfer/download/{id}/tool?ticket=…` | 下载工具短期票据，支持 HEAD、单个 HTTP Range 和重试                              |
| `PUT /api/transfer/upload/{id}/{request}`       | 独立分段上传票据，请求体为所需文件区间                                           |

创建请求为 `{"name":"设备 A","relay_only":false}`，返回 `token`、`code`、`member`、`expires_at`、`max_file_size`。加入请求多一个 `code` 字段，返回第二席的临时票据。WebSocket 要求有效的 `Origin`，首条消息为 `{"type":"auth","token":"…"}`；长期 API Key 和管理员凭据不得放在 WebSocket URL 中。

创建和加入均可附带 `device` 原始浏览器提示，整个请求体最多 2 KiB。前端只读取并上传原始字段，由后端结合 HTTP `User-Agent` 统一识别设备类型、浏览器、系统和型号，并生成 `summary` 展示文案；前端不判断系统版本或设备类型。原始 UA、品牌列表和触控点数不保存在房间或日志中，只保留后端生成的摘要。字段均可省略，旧客户端仍可使用；这些信息仅供参考，不参与鉴权或覆盖服务端 IP。

原始字段为 `platform`、`platform_version`（各最多 32 字节）、`mobile`（布尔值）、`brands`、`full_version_list`（各最多 8 项，每项为 `brand/version`，分别最多 48/32 字节）、`model`（最多 96 字节）、`form_factors`（最多 4 项，每项 24 字节）和 `touch_points`（0–100，仅辅助识别桌面模式平板）。字符串按 UTF-8 字节限制，拒绝控制字符、双向覆盖符及未知字段。浏览器在配对时读取一次，详细提示最多等待 250 ms，失败或受限时由后端使用基础信息识别。

识别结果包含 `type`、`browser`、`browser_version`、`os`、`os_version`、`model` 及 `summary`，通过 `joining.device`、`paired.devices` 返回，缺失结果为 `null`。结果只在当前房间的内存中保留。开启操作日志总开关及设备互传模块后，创建、申请加入和后续互传事件会包含 `device`；配对后的互传事件还会包含 `peer_device`。日志沿用现有保留时间及清理设置，关闭日志不会额外保存设备摘要。

管理员手动加载 IP2Location LITE DB3 IPv4 BIN 后，后端根据可信来源 IP 查询国家、省级归属地，通过 `joining.location` 和 `paired.locations` 返回。`locations` 按创建者、加入者顺序排列；字段为 `country_code`、`country`、`region`、`summary`、`source`，空字段省略，LITE 数据来源为 `source: "ip2location"`。国内示例：`{"country_code":"CN","country":"中国","region":"广东","summary":"中国 · 广东","source":"ip2location"}`。未加载数据库或来源为 IPv6 时对应位置为 `null`，客户端应隐藏该位置字段；已加载的 IPv4 未命中或读取失败时返回 `{"summary":"未知"}`，内网、回环分别返回“局域网”“本机”。创建者只在授权配对后向加入者披露来源信息。页面仅展示归属地摘要，数据库来源说明保留在本文档中。

日志开启且对应归属地非空时，成功创建、申请加入的 HTTP 日志包含 `location`；后续互传事件复用连接建立时的归属地快照，配对后另含 `peer_location`。未加载及 IPv6 不写入位置字段。归属地仅描述来源 IP，不采用浏览器上报的点对点地址，不保存城市或坐标。

管理员数据库管理入口（均需管理员登录）：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/admin/ip-location` | 状态：`loaded`，加载后另含 `date`、`size`（字节） |
| `PUT` | `/api/admin/ip-location` | 原始 BIN 请求体，`Content-Type: application/octet-stream`，最大 512 MiB；仅接收 DB3 IPv4 |
| `DELETE` | `/api/admin/ip-location` | 卸载并删除数据库，停止显示位置，保留旧日志 |

文件由管理员自行下载选择，服务端不接受远程 URL 或文件路径。上传采用固定缓冲，最多同时更新一个数据库，冲突返回 `409 IP_LOCATION_BUSY`；无效文件返回 `400` 并保留原库，超限返回 `413`。成功加载的选择在重启后保留。更换或卸载时，已配对双方收到 `{"type":"locations","locations":[位置或null,位置或null]}`；未配对时仅创建者收到 `{"type":"locations","joining_location":位置或null}`，不会因此自动授权或改变连接状态。

CloudPaste uses the IP2Location LITE database for [IP geolocation](https://www.ip2location.com).

`code` 为一个大写字母加四位数字（包含前导零），例如 `A1234`。页面显示 `A-1234`，复制得到 `A1234`；加入接口也接受 `a-1234` 与包含空白的形式，统一解析后再匹配。配对仍需创建者确认，且保留有效期、猜测限流和两人席位限制。

页面「自动同意设备加入」默认为关闭；开启后由创建者客户端在收到 `joining` 时自动发送原有 `confirm` 消息，不增加新的接口字段或绕过服务端配对检查。

创建者收到 `joining` 后发送 `{"type":"confirm"}`，双方收到 `paired` 后方可交流。消息类型包括 `text`、`signal`（WebRTC 协商）、`offer`（`client_id/name/size`）、`accept`（`id/mode`，模式为 `relay`、`p2p` 或 `tool`）、`cancel`、`fail`（客户端传输异常）和 `leave`。服务端通过 `file/state/progress/error` 通知状态。直连接收方用 `progress/complete` 报告已写入字节数，服务端不为直连内容作完整性背书。

服务端广播的 `text` 和 `file` 通知携带 `sent_at`（UTC RFC 3339 时间），表示服务端收到文字或文件发送请求的时间；双方收到同一时间值。客户端提交的时间不会被采用，图片完成接收时不改变其原始发送时间。

### WebSocket 连接与认证

连接地址为 `ws://后端地址/api/transfer/socket`，HTTPS 部署使用 `wss://后端地址/api/transfer/socket`。管理员和 API Key 用户均连接此普通路径，没有 `/api/admin/transfer/socket` 入口。

连接前先调用创建或加入接口，取得响应 `data.token`。它是当前房间某一成员的临时凭证，与管理员会话、API Key、HTTP 上传票据和下载票据各自独立。

握手要求 `Origin` 为后端同主机及端口的 HTTP/HTTPS 来源，或精确匹配 `ALLOWED_ORIGINS` 中的来源。浏览器自动附带 `Origin`。握手请求不得携带 `Authorization`；成员凭证只在连接成功后的第一条消息中提交：

```json
{ "type": "auth", "token": "创建或加入接口返回的成员凭证" }
```

第一条消息必须在连接成功后 10 秒内发送，并且是有效的 JSON 文本消息；`token` 必须是服务端返回的完整 43 字符凭证。认证成功后返回：

```json
{ "type": "ready", "member": 0 }
```

`member:0` 为创建者，`member:1` 为加入者。同一成员不能同时连接两条 WebSocket。房间不提供断线恢复，任一已认证成员断开即结束房间，再次使用需要重新创建和配对。

前端连接示例，`data` 为创建或加入接口返回的 `data` 对象，`backendBase` 为后端 HTTP 地址：

```javascript
// 临时成员凭证通过首条消息发送，避免写入 URL 或浏览器存储。
const url = new URL("/api/transfer/socket", backendBase);
url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
const socket = new WebSocket(url);
socket.addEventListener("open", () => {
  socket.send(JSON.stringify({ type: "auth", token: data.token }));
});
```

### WebSocket 消息格式

应用消息均使用 UTF-8 JSON 文本，顶层 `type` 区分操作。WebSocket 不使用 HTTP API 的 `success/code/message/data` 响应包装。文件内容通过 HTTP 中继或 WebRTC DataChannel 传输，不发送 WebSocket 二进制消息。

`id` 为服务端通过 `file` 通知生成的文件任务 ID；`client_id` 为发送端自选的请求关联 ID。`size`、`bytes`、`start`、`end` 的单位均为字节，成员编号和字节数均使用 JSON 整数。

#### 客户端 → 服务端

| `type`            | 字段                                           | 发送者与生效条件                                                                     |
| ----------------- | ---------------------------------------------- | ------------------------------------------------------------------------------------ |
| `auth`            | `token`                                        | 连接后的第一条消息；后续不重复认证                                                   |
| `confirm`         | 无                                             | 仅创建者，在加入者连接且收到 `joining` 后确认配对                                    |
| `reject`          | 无                                             | 任一成员均可结束整个房间；没有仅移除加入者的操作                                     |
| `leave`           | 无                                             | 任一成员主动结束整个房间                                                             |
| `text`            | `text`                                         | 配对后任一成员发送；长度为 1–4096 UTF-8 字节                                         |
| `signal`          | `data`                                         | 配对后发送 WebRTC 协商数据；仅非强制中继房间可用，`data` 最大 12000 字节             |
| `offer`           | `client_id`、`name`、`size`，可选 `image_type`、`folder` | 配对后发送文件申请；`client_id` 长度为 1–64 字节，普通文件允许 0 字节；文件夹条目详见下文 |
| `accept`          | `id`、`mode`                                   | 仅该文件的接收方，在 `offered` 状态接受；文件模式为 `relay`、`p2p` 或 `tool`，空目录用 `directory` |
| `cancel`          | `id`                                           | 任一成员取消指定文件；活动或待接收任务变为 `cancelled`                               |
| `fail`            | `id`                                           | 任一成员报告指定活动文件异常，任务变为 `failed`                                      |
| `progress`        | `id`、`bytes`                                  | 仅 P2P 接收方报告累计写入量；不得倒退或超过文件大小                                  |
| `complete`        | `id`、`bytes`                                  | P2P 或文件夹中继的接收方确认本地写入完成；`bytes` 必须等于文件大小                    |
| `range-failed`    | `id`、`request_id`                             | 仅下载工具模式的发送方取消失败分段，其余分段和任务仍可继续                           |
| `finish-download` | `id`                                           | 仅下载工具模式的接收方确认完成；服务端必须已经转发完整文件，且至少开始过一轮实际 GET |

`reject`、`leave`、`confirm` 在配对前即可发送；其他操作需要双方已经收到 `paired`。一个房间同时只能有一个 `active` 文件任务。文件名经去除首尾空白后应为 1–180 字节，不能包含路径分隔符、控制字符或双向文字覆盖控制符。

#### 服务端 → 客户端

| `type`          | 字段                                                                        | 接收者与含义                                                           |
| --------------- | --------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `ready`         | `member`                                                                    | 当前成员，首帧认证成功                                                 |
| `joining`       | `name`、`ip`、`device`、`location`                                          | 仅创建者，加入者已经连接，等待确认；`ip` 为服务端识别的 WebSocket 来源 IP |
| `paired`        | `names`、`ips`、`devices`、`locations`、`relay_only`、`stun`                  | 双方；名称、IP、设备摘要、归属地均按创建者、加入者顺序排列；缺失设备摘要为 `null`，空 `stun` 表示未配置 STUN |
| `locations`     | `locations` 或 `joining_location` | 管理员手动加载、更新或卸载数据库时刷新位置；不改变授权状态，不向未授权加入者披露创建者信息 |
| `text`          | `text`、`from`、`sent_at`                                                   | 双方，包括发送者；`from` 为发送成员编号                                |
| `signal`        | `data`                                                                      | 仅对端，原样转发协商数据                                               |
| `file`          | `id`、`client_id`、`name`、`size`、`from`、`state`、`image_type`、`sent_at` | 双方，新任务初始 `state:"offered"`，非图片的 `image_type` 为空字符串   |
| `state`         | `id`、`state`、`mode`，终态带 `bytes`                                       | 双方，文件状态变化                                                     |
| `progress`      | `id`、`bytes`，下载工具模式另带 `round`                                     | 双方，累计进度；`round` 为下载工具的当前下载轮次                       |
| `download`      | `id`、`url`                                                                 | 仅接收方，普通中继的一次性 HTTP 下载地址                               |
| `download-tool` | `id`、`url`、`expires_at`、`max_connections`                                | 仅接收方，可重试和分段的下载工具地址                                   |
| `start`         | `id`、`mode`，普通中继另带 `token`                                          | 仅发送方；P2P 开始直传，普通中继开始 PUT 上传                          |
| `range-start`   | `id`、`request_id`、`token`、`start`、`end`                                 | 仅发送方，下载工具请求的文件区间，`end` 不包含在区间内                 |
| `range-stop`    | `id`、`request_id`                                                          | 仅发送方，该 HTTP 分段请求已结束或中止，释放对应上传                   |
| `error`         | `message`、`id`、`client_id`                                                | 仅当前操作发送者，业务操作被拒绝；无关联文件或申请时相应 ID 为空字符串 |

`created`、`direct`、`image`、`retry` 等是前端 `TransferClient` 生成的本地事件，不是服务端 WebSocket 消息。时间字段使用 UTC RFC 3339，可包含小数秒。客户端不应假定每一小块文件数据都对应一个进度消息。

### WebSocket 配对与文件状态

配对流程如下：

1. 创建者调用 `create`，取得配对码和成员凭证，立即连接并发送 `auth`，收到 `ready`。
2. 加入者调用 `join`，取得第二席的凭证，连接并发送 `auth`，收到 `ready`；创建者收到 `joining`。
3. 创建者发送 `{"type":"confirm"}`，双方收到 `paired`，配对码随即失效。
4. 此后双方可发送文字、协商直连、申请文件或主动离开。

`paired.stun` 来自管理员保存的 `transfer_stun` 设置；仅在配对确认后下发，不在公开设置中返回。管理员通过 `PUT /api/admin/settings/group/3` 保存字符串，格式为 `stun:主机:端口`，端口 1–65535，最长 256 字节；IPv6 使用方括号。不支持凭据、路径、查询参数或多个服务器。空字符串停用，默认为空，原 `TRANSFER_STUN` 环境变量不再生效。保存后新配对使用新值，已有连接不重新协商，重启保留。

成功配对通知示例：

```json
{
  "type": "paired",
  "names": ["设备 A", "设备 B"],
  "relay_only": false,
  "stun": ""
}
```

文件申请及接收示例，消息分开发送：

```json
{
  "type": "offer",
  "client_id": "local-request-1",
  "name": "示例.txt",
  "size": 1024
}
```

服务端向双方返回任务 ID：

```json
{
  "type": "file",
  "id": "服务端任务ID",
  "client_id": "local-request-1",
  "name": "示例.txt",
  "size": 1024,
  "from": 0,
  "state": "offered",
  "image_type": "",
  "sent_at": "2026-10-14T08:00:00Z"
}
```

接收方使用此 `id` 接受普通中继：

```json
{ "type": "accept", "id": "服务端任务ID", "mode": "relay" }
```

文件状态为 `offered → active → complete / failed / cancelled`，待接收任务也可直接进入 `cancelled`。终态不能重新启动；重发需新的 `offer/client_id`，并继续计入房间的累计额度。普通中继的 `complete` 表示服务端已转发完整文件；P2P 的 `complete` 来自接收方报告；下载工具的 `complete` 还要求接收方发送 `finish-download`。服务端不能证明浏览器或外部下载软件最终落盘成功。

### WebSocket 错误、心跳与关闭

握手阶段仍返回 HTTP 错误响应：无效或缺少 `Origin`、附带 `Authorization` 时返回 `403 / FORBIDDEN`；关闭互传后已有房间仍可继续建立信令和传输，创建与加入新连接返回 `404 / TRANSFER_UNAVAILABLE`；连接或连接尝试超额时返回 `429 / TRANSFER_BUSY`。Upgrade 后的首帧超时、认证失败、非 JSON 或非文本消息会直接断开，不保证收到 `error` 消息或指定的关闭码。

已认证连接中的业务错误使用以下格式，连接通常仍可继续使用，客户端应按关联 ID 更新该次操作：

```json
{
  "type": "error",
  "message": "请先完成当前文件传输",
  "id": "服务端任务ID",
  "client_id": ""
}
```

WebSocket `error` 消息不包含 HTTP 状态或 `code`。HTTP 创建、加入与文件传输请求的错误仍按 HTTP API 格式返回；客户端应分别处理两种错误来源。

服务端每 20 秒发送一次协议级 Ping，等待 Pong 最长 8 秒；浏览器自动回应，不需要发送 `{"type":"ping"}`。发送一条服务端消息的期限为 10 秒，单成员待发送队列最多 288 条。队列已满、写入失败或心跳失败会结束整个房间。

单条 JSON 文本消息最大 16 KiB，认证后普通应用消息每成员每分钟默认最多 240 条，可通过 `transfer_ws_messages_per_minute` 调整；已授权文件夹的内部条目消息独立使用 `transfer_folder_ws_messages_per_minute`，默认 12000 条。任一消息额度超额会直接断开。房间从创建起最多存活 2 小时，配对码最多有效 5 分钟；创建者应在创建后 15 秒内连接，加入者取得第二席后应在 1 分钟内完成配对确认。期限由定时清理处理，不能依赖精确的关闭时刻。任一成员离开、拒绝或断线后，另一端也会断开，未完成文件被取消，相关 HTTP 票据失效；关闭前不保证送达所有状态通知。

### 传输模式与 HTTP 配合

#### 文件夹队列

发送端用 `folder-offer` 提交 `client_id/name/count/size`（count 包含空目录标记，size 为文件总字节）。服务端广播 `folder`，含上述字段、服务端 `id/from/state/sent_at/concurrency`。接收方选好可写目录后发送 `accept-folder`（`id/mode/concurrency`，mode 为 `relay` 或 `p2p`）。同一房间同时只运行一个文件夹队列，普通活动文件也不能与其交错接收。

服务端用 `folder-next`（`id/count`）发放下一批条目的提交额度。发送端通过原 `offer` 附带 `folder:{id,path,directory}`，path 为文件夹内的相对路径；directory 为 true 表示空目录，大小必须为 0。`file` 通知携带该 folder 字段。目录创建成功后接收方用 `accept` 的 `mode:"directory"` 确认；普通条目沿用选定的中继或直连方式。接收时可传 `concurrency`（1–128，未传使用系统默认 16）；`folder-next.count` 是本次允许提交的条目数。条目完成后服务端立即释放其状态并补充额度。中继条目先预留全站并发额度，总额度默认 64，额度不足时排队。双方通过 `folder-state`（`id/state/mode/bytes/completed/concurrency`）更新整体任务，使用 `cancel-folder`（`id`）取消整个队列。

设置中继总并发上限时，全站尚未开始的文件夹预留合计最多占该上限的一半（向下取整，至少 1 路），为普通中继保留接入余量。接收方接受条目后继续补位，实际传输仍可达到配置的总并发；总并发为 0 时不限制预留。已取得额度但没有活动文件、连续 1 分钟未推进的文件夹按失败结束并归还资源；未取得额度的排队任务继续等待。已开始的文件仍使用自身的停滞保护。

传输中，接收方可发送 `folder-concurrency`（`id/concurrency`，1–128）修改当前文件夹并发，不修改系统默认值。服务端暂停发放额度并向发送方发送 `folder-reset`（`id`）；发送方清除未使用额度后，在同一 WebSocket 上回复 `folder-ready`（`id`）。服务端处理完确认之前的在途条目，再释放剩余预留额度并重新调度。已安排的条目不中断，调低后等其完成再补充；调高仍受中继总额度和限频约束。双方收到 `folder-state`，接收方另外收到 `folder-concurrency`（`id/concurrency`）确认。调整失败的 `error.action` 为 `folder-concurrency`，只提示错误，不取消文件夹。

发送方超过 1 分钟未确认 `folder-reset`，整个文件夹按失败结束并回收预留；其他条目的活动不会延长该确认期限。

文件夹只占一个逻辑任务，内部条目不受 32 个累计文件额度限制；同一连接仍最多 32 个逻辑任务，文件夹与普通文件共同受单文件大小及累计 `max_file_size × 4` 的总字节额度约束。发送端仅按服务端发放的并发额度提交条目，条目完成后立即补位；不存在首批 32 项之后按秒逐项等待的客户端节拍。内部消息和有效文件夹票据的中继请求使用独立限频，仍受路径、权限、一次性票据、并发、带宽和缓冲保护。零字节普通文件与空目录分别处理。

路径最大 1024 UTF-8 字节、16 层，单个名称最大 180 字节；拒绝绝对路径、空路径分量、`.`、`..`、反斜杠、控制字符及 Windows 特殊名称。客户端同样校验，并拒绝覆盖已有同名目录或文件。文件夹中继在本地文件写入并关闭后才发送 `complete`（`id/bytes`）；服务端同时确认转发完成后才将条目出队。取消或失败会保留已经成功保存的部分内容，不自动删除本地目录。

浏览器网页接收目录需要安全上下文和 showDirectoryPicker 写入能力，不支持时明确禁用文件夹接收，不提供 ZIP 降级。原生目录选择支持保留空目录；兼容的 webkitdirectory 文件选择入口只能枚举文件，无法保留空目录。发送期间请勿修改源目录。开启操作日志总开关和设备互传模块后，文件夹只记录“文件夹传输开始”和“文件夹传输结束”，不逐个记录内部文件。详情包含 folder_name、count（含空目录）、completed、total_size、bytes、started_at、ended_at（仅结束事件）、mode、state 和双方来源 IP；点对点结束事件附带最近一次完成文件上报的双方 IP:端口（若浏览器提供）。失败时附带 failed_path，不累计文件清单或保存文件内容。取消、断线或失败均只记录一次整体结束事件；单独文件、图片和下载工具的原有日志保持不变。

文件夹中继收到多条 `download` 通知后，接收端一次只启动一条尚未配对的下载请求。服务端校验对应一次性上传票据、确认上传接入后，向接收端发送 `relay-ready`（`id`），接收端再启动下一条下载；已接入的文件内容继续并行传输。这样避免同一浏览器双标签页共享连接池时，下载请求占满连接导致上传无法开始。实际 HTTP 并发仍取决于浏览器、协议和代理能力，设置值是任务并发上限。尚未成对接入的排队文件参考整个文件夹的活动时间判断停滞；已接入的文件继续使用独立的停滞保护。

文件夹点对点传输为每个活动文件建立独立数据通道，所有文件共享 8 MiB 未确认窗口和 4 MiB 发送缓冲上限，确认仅在写入完成后发送。接收方每秒合并发送一条 `folder-progress`，`progress` 为最多 128 个 `{id,bytes}` 项；服务端整批校验任务归属、状态与字节范围后广播。

#### 单文件和图片

直连客户端在 `signal.data.description` 的同级字段 `max_chunk_size` 声明应用可接收的分块上限，`receive_window` 声明待写盘数据窗口。双方支持时使用最高 256 KiB 分块；对方未声明分块上限时兼容旧页面的 64 KiB 上限，并始终遵守 SCTP 协商的消息上限。发送缓冲限制为 4 MiB，等待接收端写盘确认的数据不超过双方窗口中的较小值，最高 8 MiB；对方未声明窗口时兼容旧页面的 2 MiB 窗口。以上限制不随文件总大小增长，并非浏览器进程总内存上限。

`signal.data.generation` 表示本次直连协商代次，初始为 0。双方收到直连任务取消或失败的终态后，关闭旧连接、递增代次并重新协商；迟到的旧代次 SDP 和 ICE 消息会被丢弃。未提供代次的旧页面按 0 处理。

聊天图片在 `offer` 中额外携带 `image_type`，仅允许 `image/png` 或 `image/jpeg` 且大小为 1 字节至 4 MiB。`file` 通知原样携带该字段；网页在空闲时自动以中继接收，并在解码前校验图片格式和像素上限。图片内容走现有 HTTP 中继，不放进 WebSocket 消息。失败重发使用新的 `offer/client_id` 创建独立任务，沿用大小、文件数量、累计额度与接收确认规则，旧票据不可复用。

中继接收方先 `accept`，得到 `download` 消息内的临时 URL。**实际 GET 开始后**发送方才收到 `start` 及独立上传票据；上传和下载票据不能互换，单次使用、绑定文件、房间结束即失效。HEAD、Range 和重新消费票据不提供文件；上传字节数必须与申报大小一致。中途失败会截断下载而不追加 JSON，请检查下载是否完整。

下载工具接收使用 `accept` 的 `mode:"tool"`，收到 `download-tool`（`url/expires_at/max_connections`）后，前端立即发起原生下载，并保留链接供复制和重新下载。HEAD 仅返回元数据；GET 支持 200 全文件、206 单个区间（含开放结尾与后缀）、416 不可满足区间，并提供稳定 ETag、Last-Modified 和 If-Range 校验。多段 multipart 请求不支持；并行分段使用多个独立 GET。

每个实际下载请求向发送页面发送 `range-start`（`id/request_id/token/start/end`，end 不包含），页面对原 File 做 slice 后 PUT 到分段上传入口。票据只用于该次分段上传，不能互换或重复消费；`range-stop` 取消相应上传，发送端也可用 `range-failed`（`id/request_id`）结束失败分段。单段中止不终止任务，可在票据期限内重试。整轮所有分段正常完成后，双方收到 `tool-complete`（`id/round/bytes`）；此时会写入一条“下载完成”日志，链接仍可再次下载。仅接收端的 `finish-download` 结束任务并撤销下载链接；取消、配对断开或权限撤销也使链接失效。

下载工具链接最长 30 分钟有效，受房间有效期约束，闲置 10 分钟失效。每文件最多 4 个并发请求，并共享 `transfer_max_relays`，最多 256 次实际 GET，累计转发额度为 `max(1 MiB, 文件大小×4)`；HEAD 和非法 Range 不消耗 GET 额度。每请求停滞上限 1 分钟，上传、下载各使用 32 KiB 固定缓冲，服务器不缓存文件。进度按当前轮次合并已转发区间，单轮内重试不重复计数；完整下载后再次 GET 开启新一轮，`progress.round` 递增，双方清零显示进度和速率。HEAD 不开启新轮次，历史流量和请求额度不清零；上一轮 HTTP 请求的迟到退出不会影响新一轮进度。`tool-complete` 仅证明服务器完整转发，不能证明下载工具最终落盘。

单帧最大 16 KiB，单条文字最大 4096 字节。配对码猜测、连接数、房间数、文件并发、带宽和传输停滞均有限制，详见 [部署指南](deployment.md#设备互传与实时中继)。HTTP 加入或文件传输入口的连接、票据失效返回 `404 / TRANSFER_UNAVAILABLE`，超额返回 `429 / TRANSFER_BUSY`；WebSocket 握手错误与连接后的业务错误分别按上文处理。

## 自定义分享链接

`POST /api/paste` 的 JSON 或文件上传选项中的 `slug` 用于自定义链接末尾名称，例如 `my-note` 对应前端 `/paste/my-note`，文件则对应 `/file/my-note`。留空自动生成；只允许 1–100 个英文字母、数字、短横线和下划线。有效分享仍占用后缀，包括隐藏或有密码的分享。已过期或查看次数耗尽的后缀可在创建、改名时重新使用；文件与合集共用后缀空间，文本独立。

回收时保留旧分享记录与文件，为旧记录分配随机后缀，再把原后缀交给新分享。原地址此后指向新分享；旧解锁凭证和下载票据不能访问新记录。文本修改请求 `PUT /api/pastes/{slug}` 可携带 `id`，前端编辑窗口始终携带此字段，防止后缀复用后误改新记录。

文本使用 `expires_at`（RFC 3339 时间或 `null`），文件上传使用 `expires_in`（小时，0 表示不过期）。`max_views:0` 表示不限次数，正整数表示查看上限；前端直接显示访问密码和查看次数，不需要展开更多选项。批量上传按每个文件独立创建分享。

## 创建自定义 API Key

管理员调用 `POST /api/admin/api-keys`，示例请求体如下（示例密钥仅展示格式，请替换为自己的随机值）：

```json
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

`custom_key` 省略或空字符串时自动生成；非空自定义值不限制长度和字符，不裁剪首尾空白。通用请求体、HTTP 请求头及反向代理的大小上限仍适用。重复值创建失败。列表只返回固定掩码，管理员点击显示按钮时才读取完整密钥，隐藏或刷新后清理页面中的明文。`527 = 1 + 2 + 4 + 8 + 512` 表示文本与文件分享、管理自己记录及上传权限；按需要减少权限。空存储列表仍受存储公开标记和其他授权检查约束。

创建响应的 `data.key` 为完整密钥，列表只返回脱敏密钥。`PUT /api/admin/api-keys/{id}` 编辑名称、权限和有效期，不更换已有密钥；需要换密钥时创建新 Key 后停用或删除旧 Key。API Key 用户不能管理密钥。
