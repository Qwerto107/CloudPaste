# CloudPaste Go 后端

面向 Docker / 独立服务器的临时分享服务，使用 Go、SQLite、LOCAL 与 S3。运行时无 Node.js、Serverless 或 Cloudflare 依赖。中文主说明见 [README](../README.md)，详细接口见 [Api-doc](../Api-doc.md)。

## 功能与边界

文本分享、文件上传分享、流式下载与 Range；分享密码、有效期和次数限制；文本 / 文件管理、存储配置、密钥权限与 ACL、管理员账号和系统设置。WebDAV 服务及存储驱动已删除，备份还原、任务、索引不提供入口。

文件管理编辑的是分享记录，文件显示名改变不会移动原文件。删除记录和分享到期仅停止访问，不自动删除本地 / S3 文件。已有 Go 库可直接使用，旧 Node 库不可直接迁移。保留内部挂载和目录授权兼容 API，以继续验证历史分享与路径受限密钥；新页面无需挂载。

API Key 权限为 `1` 创建文本、`2` 创建文件分享、`4` 管理自己的文本、`8` 管理自己的文件、`512` 上传。前端勾选文件分享时同时授予 `2+512`。`256` 挂载读取作为旧接口兼容权限保留。管理员能管理全部分享，普通 Key 仅能管理自己创建的记录。

## Docker 部署

从仓库根目录运行：

```sh
cp backend/.env.example backend/.env
mkdir -p files
# 本地绑定目录需允许镜像用户 65532 写入。
sudo chown -R 65532:65532 files
docker compose --env-file backend/.env up -d --build
```

在 `.env` 设置至少 12 字节的 `ADMIN_PASSWORD`。默认前端监听 `127.0.0.1:8081`，内部端口是 8080；后端为 8787，Compose 不单独公开后端端口。LOCAL 存储根目录填写 `/files`；S3 填 Bucket、Region、Endpoint 与凭据。设置默认存储后可直接上传。

数据卷 `/data` 包含 `cloudpaste-go.db`、SQLite WAL、`encryption.key` 和上传临时区。绑定宿主机目录时，目录、数据库及 WAL 必须允许 UID/GID 65532 读写；容器能启动不代表宿主机端口已经映射。数据卷名 `go-data` 和 Compose 项目名保留，以继续使用原 Go 数据。

## 设置与上传

全局设置可降低单文件上传上限，不能突破 `MAX_UPLOAD_SIZE_MB` 的环境硬上限，重启后仍生效。新分享默认 24 小时到期，可在全局设置修改。API 省略文本 `expires_at` 或文件 `expires_in` 时使用默认值；文本显式 `expires_at:null`、文件 `expires_in:0` 表示不过期。

预览设置包含文本最大读取量、文本 / 图片 / 视频 / 音频扩展名及站外 Markdown 图片开关。关闭某种预览可将对应扩展名列表清空。站点设置包含全部现存分享站选项；自定义顶部 / 底部 HTML 在前端安全过滤，不支持任意脚本。

上传通过本站 Go 流式接收并暂存，成功后发布到 LOCAL / S3；不在内存中缓冲完整文件。LOCAL 使用临时文件与硬链接原子发布，S3 使用条件写入，拒绝覆盖已有文件。随机物理文件名和原始显示名分开保存。默认最多 2 个上传、8 个下载、16 个业务请求；暂存额度默认 1 GiB。原有分片协议保留供兼容 API 使用，精简页面使用原始 PUT 与原生进度。

## GitHub CI 构建与发布

镜像发布还必须通过独立浏览器回归，覆盖登录、上传、媒体预览、管理操作、权限与 CSP。

工作流为 `.github/workflows/backend.yml`，Actions 页面名称是 **Go backend**。支持 Linux `amd64` 与 `arm64`，Go 二进制使用 `CGO_ENABLED=0`；Docker 镜像包含两个平台，拉取时由 Docker 选择对应架构。[Docker 多平台构建文档](https://docs.docker.com/build/ci/github-actions/multi-platform/)

| 触发方式 | 二进制 | GHCR 镜像 | GitHub Release |
| --- | --- | --- | --- |
| 涉及本项目的分支提交 / PR | 构建并上传 Actions 附件 | 不发布 | 不创建 |
| 手动运行，默认选项 | 构建并上传 Actions 附件 | 不发布 | 不创建 |
| 手动勾选 `publish_images` | 构建并上传 Actions 附件 | 发布 `sha-完整提交号` | 不创建 |
| 推送 `go-vX.Y.Z` 稳定标签 | 构建并上传 Actions 附件 | 发布版本号、提交号及 `latest` | 发布二进制与校验文件 |
| 推送 `go-vX.Y.Z-rc.1` 等预发布标签 | 构建并上传 Actions 附件 | 发布版本号及提交号 | 发布预发行版本，不更新镜像 `latest` |

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
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.4.1
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.4.1
```

```sh
docker compose --env-file backend/.env -f docker-compose.yml pull
docker compose --env-file backend/.env -f docker-compose.yml up -d --no-build
```

这两条命令使用预构建镜像；留空镜像变量时，仍按前述 `up -d --build` 方式本地构建。下载包、GHCR 镜像及 Release 必须等待 GitHub 实际执行后才存在，本地配置验证不等于已经发布。


## 管理入口与安全

管理员使用 `/api/admin/*`，API Key 使用 `/api/*`。外部 Nginx 的 IP 白名单 / mTLS 必须覆盖完整管理前缀，并防止绕过代理访问内部服务。公开分享使用普通前缀，公开配置使用 `/api/public/settings`。

文件访问继续验证当前 Key / 会话、ACL、来源版本、目录保护和分享状态；撤销凭据后旧票据失效。本地存储拒绝穿越、符号链接别名、目录联接及特殊文件。普通用户的存储响应使用字段白名单，不返回真实路径、上游凭据、地址和桶名称。

分享文件读取票据有效期为 10 分钟，期间 Range 请求不再次消耗查看次数；超过有效期后重新打开分享可能消耗一次查看额度。浏览器原生播放器的编解码支持取决于浏览器，不提供转码或外部播放器。

Markdown / 自定义 HTML 均过滤脚本、事件属性、危险链接和嵌入页面，默认不加载外部图片。后端文件响应带沙箱 CSP，HTML / SVG 文件不会成为本站脚本入口。JSON 最大 2 MiB；管理密码与访客分享密码分别限流，不信任客户端伪造的转发地址。

## 开发与验证

```sh
cd backend
go test ./... -count=1
go vet ./...
go build -o bin/cloudpaste ./cmd/cloudpaste
ADMIN_PASSWORD='replace-with-a-strong-password' DATA_DIR=./data go run ./cmd/cloudpaste
```

前端使用 Node.js 22：

```sh
cd frontend
npm ci
npm run test:go-upload
npm run test:security
npm run build
VITE_BACKEND_URL=http://127.0.0.1:3000 npm run dev -- --host 127.0.0.1
```

前端端到端验证 `npm run test:e2e` 会启动独立临时 Go 服务和无界面浏览器，验证登录跳转、中文上传、预览、管理操作、设置、权限及无第三方请求。先构建 `backend/bin/sharing-backend.exe`，Linux 可用 `SHARING_TEST_BINARY` 指定二进制。浏览器使用本机 Chrome / Edge 或 Playwright Chromium；可通过 `SHARING_TEST_BROWSER` 指定。

前端开发统一访问 `http://127.0.0.1:3000`，通过 Vite 代理 `/api` 到 `127.0.0.1:8787`。上述环境变量覆盖旧 `.env.development` 的直连后端配置；PowerShell 先设置 `$env:VITE_BACKEND_URL='http://127.0.0.1:3000'` 再启动。生产环境使用同源反向代理。

CI 使用 `go test -race ./... -count=1 -timeout=300s`；浏览器验证等待管理员退出完成后再检查 API Key 流程，避免把尚未结束的管理员请求计入普通用户请求。部署端资源用量应在实际主机和负载下测量，前端资源减少不等于保证 Go RSS 下降。
