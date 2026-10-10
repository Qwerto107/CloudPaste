# CloudPaste Go 后端 API

本文对应 `backend/` 中的 Go 后端。管理员接口与普通用户接口已分离，管理员前缀为 `/api/admin`。外部 Nginx 的 IP 白名单或 mTLS 由部署者配置，后端本身不验证这些网络策略。

## 身份与入口

| 身份 | 入口 | 认证 |
| --- | --- | --- |
| 未登录访客 | 公开 `/api/*` 分享及站点信息 | 公开分享仍校验密码、过期、次数及读取票据 |
| API Key 用户 | 普通 `/api/*` | `Authorization: ApiKey <密钥>` |
| 管理员 | `/api/admin/*` | `Authorization: Bearer <会话令牌>` |

目前没有普通用户注册体系。API Key 必须启用且未过期，并满足权限、路径范围和存储 ACL；非管理员内容管理还检查归属。

前端页面也按身份分离：API Key 登录页为 `/login`，管理页为 `/manage/*`；管理员登录页为 `/admin/login`，管理页为 `/admin/*`。页面路由与下表 API 路径不同，外部反向代理的管理员 API 访问控制仍需覆盖 `/api/admin/*`。

管理员凭据不能用于普通用户入口，API Key 不能用于管理员入口。管理员下载和分片票据只能在管理员前缀中使用，换到普通路径会失败。外部代理必须覆盖 `/api/admin` 本身和 `/api/admin/` 下全部请求，包括登录、下载与分片；不要暴露可绕过代理的后端端口。

共享业务接口为管理员提供对应路径：普通用户的 `/api/fs/list` 对应管理员的 `/api/admin/fs/list`，`/api/paste` 对应 `/api/admin/paste`。管理员路径仍要求管理员身份，不能依靠改 URL 提权。前端根据此次请求的认证身份选择路径。

## 未登录访客

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查 |
| GET | `/api/version` | 基础版本信息 |
| GET | `/api/system/max-upload-size` | 上传上限，不授予上传权限 |
| GET | `/api/public/guest-config` | 当前游客模式关闭 |
| GET | `/api/public/settings` | 公开站点设置白名单，支持 `group` 参数 |
| POST | `/api/admin/login` | 管理员登录；唯一不要求已有管理员会话的登录入口，仍位于代理保护范围 |
| GET / POST | `/api/paste/{slug}` | 读取公开文本 / 提交分享密码；私有文本不对访客开放 |
| GET | `/api/raw/{slug}` | 原始文本，检查密码、过期和次数 |
| GET | `/api/share/get/{slug}` | 分享信息或密码提示 |
| POST | `/api/share/verify/{slug}` | 验证分享密码并获取读取票据 |
| GET | `/api/s/{slug}`、`/api/s/{slug}/{filename}` | 文件分享读取；密码分享要求有效 `token` |
| GET | `/api/share/content/{slug}`、`/api/share/content/{slug}/{filename}` | 文件分享读取的兼容入口 |
| GET | `/api/p/{path...}` | API Key 用户签发的短期读取链接，必须携带有效 `sign` |
| OPTIONS | 任意路径 | 预检响应，不授予资源访问权限 |

公开文件分享是创建者主动授权的访问方式，管理员创建的分享也可供访客读取。分享链接仍使用普通路径；它与管理员私有文件读取票据不同。

## 普通用户 / API Key

下列路径供 API Key 使用。管理员调用对应的 `/api/admin/...` 路径；管理员不受 API Key 权限位限制。

| 方法 | 路径 | 用途 / 权限位 |
| --- | --- | --- |
| GET | `/api/test/api-key` | 当前身份及权限，有效认证 |
| GET | `/api/storage`、`/api/storage/{id}` | 需具备 `256`、`512`  中至少一项权限；仅返回存储选择和能力字段 |
| GET | `/api/storage-types` | 支持的存储和配置字段 |
| POST | `/api/paste` | 创建文本，`1` |
| GET | `/api/pastes`、`/api/pastes/{id}` | 列表和详情，`4` |
| PUT | `/api/pastes/{slug}` | 编辑文本，`4` |
| DELETE | `/api/pastes/batch-delete` | 批量删除文本，`4` |
| GET | `/api/files`、`/api/files/{id}` | 文件分享列表和详情，`8` |
| PUT | `/api/files/{id}` | 编辑分享设置，`8` |
| DELETE | `/api/files/batch-delete` | 删除分享记录，`8`；不删除文件本体 |
| GET | `/api/mount/list` | 可访问挂载，`256` |
| GET | `/api/fs/list`、`/api/fs/get` | 目录列表 / 文件详情，`256`，使用 `path` 参数 |
| GET | `/api/fs/content`、`/api/fs/download` | 内容 / 下载，`256`，使用 `path` 参数 |
| GET | `/api/fs/file-link` | 短期读取链接，`256` |
| GET | `/api/storage-types/{type}/capabilities` | 查询存储能力，`256` |
| POST | `/api/fs/meta/password/verify` | 验证目录密码，`256` |
| POST | `/api/fs/create-share` | 分享已有文件，`2+256` |
| PUT / POST | `/api/fs/upload` | 挂载上传，`512` |
| PUT / POST | `/api/share/upload` | 上传并分享，`2+512` |
| POST | `/api/fs/multipart/init` | 创建分片会话，`512` |
| POST | `/api/fs/multipart/sign-parts` | 签发分片票据，`512` |
| POST | `/api/fs/multipart/list-parts` | 查询分片，`512` |
| POST | `/api/fs/multipart/list-uploads` | 查询可恢复上传，`512` |
| POST | `/api/fs/multipart/abort` | 取消会话，`512` |
| POST | `/api/fs/multipart/complete` | 校验、合并并发布，`512` |
| PUT | `/api/fs/multipart/part/{id}/{number}` | 专用 `sign`；复核原身份、会话及上传权限 |

旧的 `POST /api/proxy/link` 及其管理员对应路径仍明确返回不支持，不能用于任意代理。

普通存储响应的字段白名单为 `id`、`name`、`storage_type`、`is_public`、`is_default`、`status`、`readonly`、`capabilities` 和 `upload_protocol`。真实路径、上游地址、桶名称、区域、用户名、备注、凭据和管理时间字段仅供管理员查看；`reveal` 参数不能扩大 API Key 的响应范围。存储 ACL 和公开标记仍同时生效。

## 管理员专属

原来散落在普通路径中的管理员专属操作已移入下列前缀，旧路径不保留管理能力。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/api/admin/logout` | 撤销当前管理员会话 |
| POST | `/api/admin/change-password` | 修改用户名或密码，撤销已有会话 |
| GET | `/api/admin/test/admin-token` | 检查管理员会话 |
| GET / POST | `/api/admin/api-keys` | 列表 / 创建 API Key |
| PUT / DELETE | `/api/admin/api-keys/{id}` | 修改 / 删除 API Key |
| GET / PUT | `/api/admin/api-keys/{id}/storage-acl` | 查询 / 修改存储 ACL |
| POST | `/api/admin/storage` | 创建存储 |
| PUT / DELETE | `/api/admin/storage/{id}` | 修改 / 删除存储 |
| GET | `/api/admin/storage/{id}?reveal=plain或masked` | 查看完整或脱敏凭据 |
| PUT | `/api/admin/storage/{id}/set-default` | 设置默认存储 |
| POST | `/api/admin/storage/{id}/test` | 测试连接 |
| POST | `/api/admin/mount/create` | 创建挂载 |
| PUT / DELETE | `/api/admin/mount/{id}` | 修改 / 删除挂载 |
| GET | `/api/admin/mount-schema` | 挂载配置字段 |
| GET | `/api/admin/fs-meta/list` | 元信息列表 |
| POST | `/api/admin/fs-meta/create` | 创建元信息 |
| GET / PUT / DELETE | `/api/admin/fs-meta/{id}` | 查询 / 修改 / 删除元信息 |
| POST | `/api/admin/pastes/clear-expired` | 清理过期文本 |
| GET | `/api/admin/settings` | 读取设置，支持 `group` 参数 |
| GET | `/api/admin/settings/groups` | 设置分组 |
| GET | `/api/admin/settings/metadata` | 设置字段元信息 |
| PUT | `/api/admin/settings/group/{group}` | 修改可写设置 |
| POST | `/api/admin/cache/clear` | 清理上游连接缓存 |
| GET / HEAD | `/api/admin/p/{path...}` | 管理员短期读取票据，仅管理前缀有效 |
| PUT | `/api/admin/fs/multipart/part/{id}/{number}` | 管理员分片票据，仅管理前缀有效 |

管理员共享业务接口还包括 `/api/admin/storage` 的 GET、`/api/admin/fs/*`、`/api/admin/paste`、`/api/admin/pastes/*`、`/api/admin/files/*`、`/api/admin/share/upload` 等，按前一节的路径映射调用。

## 返回格式与上传

普通 JSON 成功响应为 `{"success":true,"code":"OK","message":"操作成功","data":{}}`；创建资源通常返回 HTTP `201` 和 `code="CREATED"`。错误使用相应 HTTP 状态，包含 `success=false`、字符串 `code` 和 `message`。下载和原始文本返回各自格式。GET 路由也匹配 HEAD，文件读取支持 Range。

原始上传使用 `Content-Type: application/octet-stream` 和 `X-FS-Filename`；表单上传使用 `multipart/form-data`，文件字段名为 `file`。中文文件名和元信息编码可参考 [分享客户端](frontend/src/sharing/api.js)。

普通 API Key 上传示例：

```sh
curl --fail-with-body -X PUT 'http://127.0.0.1:8081/api/fs/upload?path=/local' \
  -H "Authorization: ApiKey $CLOUDPASTE_API_KEY" \
  -H 'Content-Type: application/octet-stream' \
  -H 'X-FS-Filename: example.txt' \
  --data-binary @example.txt
```

管理员改用 `/api/admin/fs/upload` 和 Bearer 会话令牌。上传先私有暂存，再检查实际大小、当前授权和目标配置；冲突返回 `409`，超过单文件上限返回 `413`，临时额度不足返回 `507`。分片会话保留 24 小时，分片大小为 5–16 MiB。

`/api/upload-direct`、浏览器上游直传、删除文件本体尚未实现；WebDAV 已移除。完整限制见 [后端说明](backend/README.md)，路由与权限见 [后端源码](backend/internal/server/)。

## 精简分享站设置

全局、预览和站点设置分组分别为 `1`、`2`、`4`。已删除 WebDAV 设置、服务和存储驱动；`/dav` 及 `/api/admin/dav` 返回 404，Basic 认证不再支持。

可写全局设置：`max_upload_size`（MiB，只能降低环境硬上限）、`share_default_expiry_hours`（0–876000 小时）。可写预览设置：`preview_max_text_kb`（16–2048）、`preview_text_types`、`preview_image_types`、`preview_video_types`、`preview_audio_types`（逗号分隔扩展名）、`preview_allow_external_images`（布尔）。站点设置包括标题、图标、页脚、顶部 / 底部安全 HTML、文本 / 文件入口开关及公告；只读设置不能修改。

`PUT /api/files/{id}` 支持 `filename`、`remark`、`password`、`clearPassword`、`expires_at`、`max_views` 和 `slug`。`filename` 仅修改显示名，必须是安全的单个文件名。省略 `max_views` 保留当前查看次数；显式修改查看限制会重置次数。

分享上传使用 `PUT /api/share/upload`，管理员使用 `/api/admin/share/upload`。`X-Share-Filename` 是 URL 编码文件名；`X-Share-Options` 是 UTF-8 JSON 的标准 Base64，包含 `storage_config_id`、`expires_in`（小时，可用小数，例如 `0.5` 为 30 分钟）、`password`、`remark`、`max_views`、可选 `path` 和 `slug`；请求体是原始文件。

新文本省略 `expires_at`、新文件省略 `expires_in` 时使用站点默认有效期。文本显式 `expires_at:null`、文件 `expires_in:0` 表示不过期。过期分享和删除分享记录不自动删除存储文件。

挂载、目录密码、分片接口仍保留供已有数据与受限路径授权兼容；精简前端不再显示挂载浏览和相关高级入口。`/api/admin/dashboard/stats` 已移除。

## 自定义分享链接

`POST /api/paste` 的 JSON 或文件上传选项中的 `slug` 用于自定义链接末尾名称，例如 `my-note` 对应前端 `/paste/my-note`，文件则对应 `/file/my-note`。留空自动生成；只允许 1–100 个英文字母、数字、短横线和下划线。同类分享名称重复会返回错误，不覆盖原分享。

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

`custom_key` 省略或空字符串时自动生成；后端要求非空自定义值长度为 24–128 字节，前端进一步限制为不含空格的可见 ASCII 字符，便于放入认证请求头。重复值创建失败。`527 = 1 + 2 + 4 + 8 + 512` 表示文本与文件分享、管理自己记录及上传权限；按需要减少权限。空存储列表仍受存储公开标记和其他授权检查约束。

创建响应的 `data.key` 为完整密钥，列表只返回脱敏密钥。`PUT /api/admin/api-keys/{id}` 编辑名称、权限和有效期，不更换已有密钥；需要换密钥时创建新 Key 后停用或删除旧 Key。API Key 用户不能管理密钥。
