# CloudPaste Go 后端

这是面向 Docker / 普通服务器的第一版 Go 后端。保留现有 Vue 前端的核心 API 约定，使用独立 SQLite 数据库；不包含 Serverless 适配，也不会自动迁移旧 Node 数据。

## 已实现的范围

| 功能 | 当前行为 |
| --- | --- |
| 管理员认证 | 登录、退出、修改账号与密码；最多保留 5 个有效会话 |
| API Key | 创建、编辑、停用、删除、过期时间、路径范围及存储 ACL；完整密钥只在创建时返回 |
| 文本 / Markdown 分享 | 创建、访问、编辑、删除、密码、过期时间、查看次数、原始文本访问 |
| 存储与挂载管理 | LOCAL、S3 兼容存储、WebDAV；连接读取测试和默认存储设置 |
| 已有文件 | 分页浏览、详情、预览入口、流式下载、Range、HEAD |
| 文件分享 | 从挂载中已有的单个文件创建分享；密码、过期、次数限制、记录管理 |
| 文件上传 | LOCAL、S3、WebDAV；流式 PUT、文件表单、上传后创建分享；同名文件拒绝覆盖 |
| 挂载分片上传 | 服务端校验分片大小与 SHA-256、查询分片、恢复、取消和完成；会话保留 24 小时 |
| 目录保护 | 目录密码、继承、页头 / 页尾 Markdown、名称隐藏规则 |
| WebDAV 服务 | `/dav`；OPTIONS、PROPFIND（Depth 0/1）、GET、HEAD，只读 |
| 基础管理页面 | 统计数量、最近七天新增量、站点标题、公告、页脚设置 |

暂不实现远程 URL 导入、浏览器直接上传到上游、对外 WebDAV 写入、移动、复制、重命名、删除原文件、后台任务调度、全盘搜索、容量扫描、外部 Office 预览转换和 HLS 播放列表重写。相关入口返回明确的错误；已有前端中尚未适配的高级页面也不代表该功能已实现。游客模式及自定义 HTML/脚本注入关闭。

API Key 可选权限位：`1` 文本分享、`2` 文件分享、`4` 文本管理、`8` 文件管理、`256` 挂载浏览、`512` 上传、`65536` WebDAV 读取。分享已有文件需要 `2+256`，上传并分享需要 `2+512`，浏览并上传需要 `256+512`。原有只读 Key 不会自动获得上传权限。管理员不受 API Key 路径和目录密码限制。

## 接口分离与外部反向代理

管理员所有操作统一使用 `/api/admin/*`，包括管理登录、存储 / 挂载 / 元信息管理，以及管理员自己的文本、上传、下载和分片请求。普通 API Key 使用原 `/api/*` 业务路径；访客仅能访问公开信息及分享。管理员专属操作的旧普通路径已移除，管理员凭据也不能在普通路径使用，API Key 不能进入管理员前缀。

公开站点配置改为 `/api/public/settings`，原 `/api/admin/settings` 现在要求管理员认证。普通 WebDAV 为 `/dav`，管理员 WebDAV 为 `/api/admin/dav`。管理员签名下载与分片 URL 也位于管理前缀，票据不能通过更换前缀跨入口使用。管理员主动创建的公开文件分享仍使用普通分享路径，供访客按分享设置读取。

IP 白名单和 mTLS 由你的外部 Nginx 配置，Go 不读取来源或证书验证头来代替这项验证。代理策略必须同时覆盖 `/api/admin` 和 `/api/admin/` 下全部路径，尤其不能漏掉下载、分片及 WebDAV。后端和配套前端的内部 HTTP 入口应仅供可信代理访问，避免绕过外部策略。网络入口验证后仍需管理员登录或绑定原会话的短期票据。

前端会根据请求的实际认证身份自动选择入口，管理员登录和公开站点配置不会继承已保存的凭据。接口清单见 [API 说明](../Api-doc.md)。外部 Nginx 策略需要部署者自行配置和验证；本项目没有启用 IP 或证书校验。
## Docker 部署

从仓库根目录执行，需要支持 `docker compose` 的 Docker 环境：

```sh
cp backend/.env.example backend/.env
mkdir -p files
```

编辑 `backend/.env`，填写至少 12 字节的 `ADMIN_PASSWORD`，设置 `LOCAL_FILES_DIR` 为文件目录。使用绝对路径可以减少部署目录变动带来的歧义。目录必须已存在，并让后端 UID `65532` 拥有读写文件和进入目录的权限；通过针对该 UID 的权限或 ACL 授权，避免使用 `chmod 777`。仅浏览本地文件时，可设置 `LOCAL_FILES_READ_ONLY=true` 并将 LOCAL 存储配置设为只读。

```sh
docker compose --env-file backend/.env -f docker-compose.yml config
docker compose --env-file backend/.env -f docker-compose.yml up -d --build
docker compose --env-file backend/.env -f docker-compose.yml ps
```

默认访问 `http://127.0.0.1:8081`，管理员用户名为 `admin`，密码为刚设置的值。前端使用同源 `/api` 与 `/dav` 转发，后端没有单独发布到宿主机的端口。公网使用时，由宿主机的 HTTPS 反向代理转发到这个回环地址；若有远程上游存储，也应使用 HTTPS，避免凭证在链路上明文传输。

默认绑定回环地址；确实需要局域网直接访问时，将 `BIND_ADDRESS` 设置为相应网卡地址或 `0.0.0.0`。应用仍要求登录与权限校验。若构建时无法连接 Go 官方模块代理，可在 `.env` 中设置 `GOPROXY` 为你信任的代理。

登录管理界面后，创建一个 LOCAL 存储，`root_path` 填 `/files`，再创建挂载，例如 `mount_path=/local`、`root_path=/`。S3 和 WebDAV 配置从动态表单填写；挂载 `root_path` 表示存储内的子路径。默认所有下载经过 Go 后端，不向调用者披露上游凭证或直接签发上游地址。

原前端会缓存站点配置与登录状态。第一次切换到 Go 服务时，请退出旧登录，并清理该站点缓存或使用新的浏览器配置，避免使用旧令牌。管理界面删除文件分享时，选择“仅删除记录”；“同时删除文件”明确不支持。

## 上传行为与限制

默认单文件上限 `MAX_UPLOAD_SIZE_MB=100`、同时上传 `MAX_UPLOADS=2`、临时区额度 `MAX_STAGED_SIZE_MB=1024`，大小单位为 MiB。单文件上限可调至 10240 MiB，临时额度必须不小于单文件上限。上传页面支持流式与表单上传，挂载上传另支持分片恢复。Go 部署返回独立上传协议标记，前端不会调用旧后端的直传接口。

文件先保存在 `/data/uploads`，用 32 KiB 缓冲流式写入并校验实际大小，再保存到目标存储。分片大小为 5–16 MiB；持久会话按声明文件大小预留额度，最多 128 个。接收分片还会短暂占用每个并发上传最多 16 MiB 的独立临时文件，因此额外磁盘预算为 `MAX_UPLOADS × 16 MiB`。LOCAL 发布还需要目标盘上单文件大小的临时空间。额度是应用限制，不代替宿主机磁盘容量监控。单次请求最长一小时，读取闲置超时一分钟。

分片会话保留 24 小时；启动、开始上传和查询会话时清理过期会话，启动时也清理中断请求的孤立临时文件。没有后台轮询。恢复绑定原凭证；退出管理员会话、轮换 Key 后需要重新上传。完成和各次分片提交会检查当前权限、存储 ACL、路径范围及目标/父目录密码版本。

默认拒绝覆盖：LOCAL 通过目标目录内临时文件和硬链接原子发布，目标文件系统需支持硬链接；S3 使用 `If-None-Match: *`，大于 64 MiB 时分片发送并在完成时条件发布；WebDAV 使用临时 PUT 和 `MOVE Overwrite:F`。协议依据：[S3 条件写入](https://docs.aws.amazon.com/AmazonS3/latest/userguide/conditional-writes.html)、[WebDAV Overwrite](https://datatracker.ietf.org/doc/html/rfc4918#section-10.6)。上游需正确实现这些条件操作，不能兼容时请求失败，不降级覆盖。存储配置的只读标志可禁止上传。

上传分享默认使用随机存储文件名，展示原文件名；可选择保留原存储文件名。未挂载存储只允许根路径作用域账号使用，并始终检查存储 ACL；受限 Key 必须通过可解析的挂载路径上传。分享引用保存后的文件，删除分享记录不删除文件。存储发布与 SQLite 记录保存不属于同一事务；极少数数据库错误或进程中断可能留下已保存但未登记分享的文件，系统不会擅自删除它。发布前进程中断也可能留下目标存储的临时文件或 S3 未完成分片，可通过存储端生命周期规则或定期维护清理。`.cloudpaste-upload-` 前缀保留给临时文件，不能通过浏览和下载接口访问。

## GitHub CI 构建与发布

工作流为 `.github/workflows/backend.yml`，Actions 页面名称是 **Go backend**。支持 Linux `amd64` 与 `arm64`，Go 二进制使用 `CGO_ENABLED=0`；Docker 镜像包含两个平台，拉取时由 Docker 选择对应架构。[Docker 多平台构建文档](https://docs.docker.com/build/ci/github-actions/multi-platform/)

| 触发方式 | 二进制 | GHCR 镜像 | GitHub Release |
| --- | --- | --- | --- |
| 涉及本项目的分支提交 / PR | 构建并上传 Actions 附件 | 不发布 | 不创建 |
| 手动运行，默认选项 | 构建并上传 Actions 附件 | 不发布 | 不创建 |
| 手动勾选 `publish_images` | 构建并上传 Actions 附件 | 发布 `sha-完整提交号` | 不创建 |
| 推送 `go-v0.2.0` 等稳定标签 | 构建并上传 Actions 附件 | 发布版本号、提交号及 `latest` | 发布二进制与校验文件 |
| 推送 `go-v0.2.0-rc.1` 等预发布标签 | 构建并上传 Actions 附件 | 发布版本号及提交号 | 发布预发行版本，不更新镜像 `latest` |

普通 Actions 附件保留 30 天，名称为 `cloudpaste-go-linux-amd64`、`cloudpaste-go-linux-arm64`；里面包含同名 `.tar.gz` 和 `.tar.gz.sha256` 文件。压缩包内包含可执行文件 `cloudpaste`、README 和许可证。解压前可在附件所在目录运行 `sha256sum -c cloudpaste-go-linux-amd64.tar.gz.sha256`。二进制提供 Go API，前端仍需通过静态资源或配套前端镜像部署。

镜像名称由当前仓库自动生成。当前仓库对应：

```text
ghcr.io/qwerto107/cloudpaste-backend
ghcr.io/qwerto107/cloudpaste-frontend
```

发布使用自动提供的 `GITHUB_TOKEN`，仅发布镜像的任务申请 `packages: write`，仅版本发行任务申请 `contents: write`；不需要配置 Docker Hub 密钥。第三方 Actions 固定到已核对的提交号。PR 不会获得发布权限。镜像通过源码标签关联仓库，既有 GHCR 包还需允许当前仓库写入；首次发布后的可见性由 GitHub 包设置决定，匿名拉取需要将包设为公开。[GitHub GHCR 权限与可见性说明](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)

发布前会等待 Go 检查、两个二进制构建，以及真实 Compose 上传 / 分享冒烟全部通过。GitHub Release 先以草稿上传附件，校验实际附件后再公开；重跑只补充缺失附件。Go 版本标签必须采用 `go-v数字.数字.数字`，可附加 `-rc.1` 等预发布后缀，保持已发布 Go 版本的标签约定。

手动运行使用 **Actions → Go backend → Run workflow**。新工作流通常需要先进入默认分支才会显示手动运行入口；合并由你决定。分支推送的自动 CI 可以先验证当前开发分支。

使用已发布镜像部署时，在 `.env` 中同时设置两个镜像，使用相同版本：

```dotenv
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.2.0
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.2.0
```

```sh
docker compose --env-file backend/.env -f docker-compose.yml pull
docker compose --env-file backend/.env -f docker-compose.yml up -d --no-build
```

这两条命令使用预构建镜像；留空镜像变量时，仍按前述 `up -d --build` 方式本地构建。下载包、GHCR 镜像及 Release 必须等待 GitHub 实际执行后才存在，本地配置验证不等于已经发布。

## 数据、回退与备份

默认部署文件为根目录 `docker-compose.yml`，使用项目名 `cloudpaste-go` 和数据卷 `go-data`，端口为 `8081`。这些名称沿用已发布 Go 版本的配置，目录重命名不会改变数据库格式或主动迁移数据。当前仓库已移除旧 Node 后端及其部署入口。

数据卷内包括 `cloudpaste-go.db`、SQLite 的 WAL 文件以及自动生成的 `encryption.key`。必须备份整套数据与密钥；仅复制数据库无法解密存储凭证。建议停止新服务后做一致性备份，恢复时保持 UID `65532` 可读写数据卷。`docker compose down` 保留数据卷；不要在需要保留数据时使用 `down -v`。

目前没有旧数据库导入工具。旧分享、存储配置、会话和 API Key 不能直接复用，需要在新后端创建。首次启动的环境变量密码只用于初始化；以后通过修改密码接口更新，改变 `.env` 不会重置已有管理员。

如需回退到旧 Node 服务，需要使用原部署备份或 Git 历史中的旧代码，并使用旧数据库。当前源码不包含 Node 后端，Go 数据库不能直接交给旧服务使用。

## 安全边界与资源控制

- 密码使用 bcrypt，存储凭证和保存的 API Key 使用 AES-GCM；会话令牌及认证查询使用 SHA-256 摘要。加密密钥自动随机生成，没有默认共享密钥。
- 文件链接绑定身份、当前凭证、授权路径、来源配置和目录密码版本，有效期 5 分钟。停用或删除 Key、修改权限或目录密码、管理员退出，以及修改来源配置，都可能使旧链接立即失效。
- 文件分享验证后签发 10 分钟读取票据，供预览和 Range 请求复用；一次已授权查看在票据有效期内可以多次读取。分享过期、修改分享设置或删除记录仍会阻止后续请求。已开始的传输不会因设置变化被追回。
- 公开分享在全局写锁之外查询存储；查询结束后重新检查分享版本、创建者权限、来源配置、密码保护、票据有效期及查看次数，慢存储不会持有全局写锁。
- 分享引用当前路径上的文件，不是不可变副本。修改来源挂载、存储配置或目录密码会要求重新创建分享；在宿主机直接替换同路径文件，会使分享读到新内容。
- LOCAL 不支持路径中的符号链接或 Windows 目录联接，即使目标仍位于存储根内也会拒绝；列表不显示这些链接，已有文件分享和上传同样受此限制。逐层固定真实目录句柄并核对文件身份，防止链接绕过按路径配置的目录密码；`os.Root` 继续约束挂载根及 API Key 授权子目录。不把 `/data`、Docker socket 或整个宿主机根目录作为文件存储挂载。
- 不提供任意 URL 代理。远程地址由管理员配置，可以访问内网存储；这不等于管理员配置接口提供了内网隔离。上游重定向不会被跟随，认证头和 Cookie 不会传回浏览器。
- 普通存储查询要求挂载浏览、上传或 WebDAV 读取权限，只返回存储选择和能力白名单字段。纯文本权限或仅分享记录管理权限不能查询存储，真实路径、上游地址、桶名称、区域、用户名、备注及管理字段仅供管理员查看。
- 目录“隐藏规则”只影响列表展示，不是访问控制；真正的隔离通过路径、存储 ACL 和目录密码完成。目录密码采用最近一层有效配置的继承规则。
- 请求 JSON 上限 2 MiB，同时最多处理 16 个请求、默认 8 个下载、2 个 bcrypt 验证。管理认证与普通入口的密码验证分别按来源和总量限流，各自限制为每来源 10 次/分钟、总计 60 次/分钟，互不消耗额度。普通 WebDAV 不接受管理员密码认证。反向代理后的用户共享代理地址的来源限额，避免信任可伪造的转发头。
- 文本和文件分享更新在全局写锁外接收请求、计算密码哈希并写入响应，锁内重新认证、读取最新记录并只应用请求指定的字段。创建已有文件分享也在锁外查询存储，保存前复核当前权限、来源配置及目录密码，避免慢客户端或慢上游阻塞无关操作。
- 下载使用固定大小缓冲区；不缓存完整文件，没有后台扫描或任务轮询池。文本管理列表只返回 200 字符摘要，完整内容通过详情读取。
- Nginx 访问日志只记录路径，不记录查询参数，避免保存短期读取票据。兼容的原始文本接口仍允许密码查询参数，不应将这种完整 URL 当作无需密码的分享链接传播。
- LOCAL 使用分页读取，顺序由文件系统决定；目录变化时游标不是快照。S3 使用上游游标。WebDAV 上游一般没有分页协议，需要读取有界的 Depth 1 响应，再分页返回：XML 上限 8 MiB、条目上限 10000。WebDAV 服务也限制单目录 10000 项，避免无限 Depth。
- Compose 后端内存上限 128 MiB，`GOMEMLIMIT=80MiB` 是 GC 软目标，不是 RSS 承诺。前端 Nginx 上限 64 MiB。大元信息或高并发仍可能触及容器上限，应按真实负载调整。
- Markdown 图表采用 Mermaid 严格模式，解析错误只作为文本展示；源码和实际加载的压缩资源均有安全回归测试。
- 前端 HTML 预览去掉同源脚本权限；文件响应附加沙箱 CSP。此改动不会消除旧前端全部依赖漏洞，依赖升级及完整前端安全复核仍需单独处理。

容器文件系统只读、去除 Linux capabilities，并启用 `no-new-privileges`。数据卷及配置的文件目录可写，其余文件系统只读；日志有大小和轮转上限。运行镜像来自 [Distroless](https://github.com/GoogleContainerTools/distroless) 与 [NGINX unprivileged](https://github.com/nginx/docker-nginx-unprivileged)。

## 本地开发与验证

项目使用 Go `1.27.2` 工具链，纯 Go SQLite，无需本地 C 编译器。下列命令从 `backend` 目录执行：

```sh
go test ./... -count=1
go vet ./...
go test ./internal/server -run '^$' -bench BenchmarkLocalDownload -benchmem -benchtime=3x
go test ./internal/server -run '^$' -bench BenchmarkLocalUpload -benchmem -benchtime=3x
GOTOOLCHAIN=go1.27.2 go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/cloudpaste ./cmd/cloudpaste
ADMIN_PASSWORD='replace-with-a-strong-password' DATA_DIR=./data ./bin/cloudpaste
```

如果使用独立开发前端地址，通过 `ALLOWED_ORIGINS` 指定完整来源，例如 `http://localhost:5173`。Docker 同源部署无需配置 CORS。`ENCRYPTION_SECRET` 可选；如设置，必须是随机 32 字节的标准 Base64 编码，并自行持久保存，不能直接使用旧后端的任意字符串密钥。

回归测试覆盖认证、退出与 Key 撤销、路径穿越、本地符号链接 / Windows 目录联接、目录密码的各访问入口、分享密码、并发次数限制、Range、WebDAV 只读与 XML、上游凭证隔离、S3 签名读取、本地分页及数据重启恢复。上传测试另外覆盖实际大小上限、表单字段顺序、中文元信息、受限 Key、只读存储、同名并发、分片校验和重试、会话恢复与清理、临时区额度、S3/WebDAV 条件发布及上游分片失败清理。

在 `frontend` 目录运行 `npm run test:go-upload`，验证真实前端驱动的能力选择、中文上传参数、目录票据、分享回调和分片钩子；Docker 前端构建也会运行此检查。新增 HTTP 冒烟覆盖 3 MiB 上传、5 MiB 分片合并、中文表单和密码分享，已在本地独立 Go 服务通过。

Windows 的完整 HTTP 上传处理基准（私有暂存、授权和 LOCAL 发布）中，1 MiB 与 64 MiB 文件每次约分配 184 KiB 与 174 KiB；文件体积增大不会造成整文件内存缓冲。此结果不是容器 RSS，也不包含上游 S3/WebDAV 的资源测量。

Windows 文件流基准中，1 MiB 与 64 MiB 文件每次下载均约分配 40 KiB；该结果只说明下载内存分配不随文件体积线性增长，不能用于声称容器 RSS 更低，也不等同于真实网络吞吐量。实际资源占用应在目标主机、相同数据和并发下测量：

```sh
docker compose --env-file backend/.env -f docker-compose.yml stats --no-stream
```

本次 Go `1.27.2` 的官方漏洞扫描未发现可达代码或实际导入包的已知漏洞。模块层还报告 `golang.org/x/crypto/openpgp` 的 [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)：该包未被后端导入，使用的是同模块的 bcrypt，报告没有可升级的修复版本。此结果不等于证明不存在未知漏洞。

已发布的 `go-v0.1.0` 在 GitHub CI 中通过竞态检查、双架构编译、Docker 上传 / 分享验证，以及 GHCR 和 Release 发布。本地环境没有 Docker 引擎，无法在此运行本次目录调整后的容器验证；目录调整仍需由后续 CI 确认。浏览器页面点击联调和实际 Linux 内存测量尚未完成。
