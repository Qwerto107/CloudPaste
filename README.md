# CloudPaste

[中文](README.md) · [English](README_EN.md)

面向 Docker 和普通服务器的临时文本、文件分享站。Go 后端负责认证、存储和分享；Vue 前端构建成纯静态资源，由 Nginx 提供。运行时不需要 Node.js。

## 保留的功能

- 文本 / Markdown 分享与预览；可设置有效期、访问密码和查看次数。
- 文件上传与分享；文本、图片、视频、音频使用本站资源和浏览器原生能力预览，其他格式直接下载。
- 文本、文件分享管理：列表、预览、编辑与删除。文件名称编辑只修改显示名；删除分享记录不删除存储中的原文件。
- LOCAL / S3 存储管理：配置、连接测试、默认存储、只读标志和公开标志。
- 密钥与权限管理：创建、停用、有效期、文本 / 文件的分享和管理权限、存储 ACL。
- 管理员账号管理：修改用户名与密码，撤销旧会话。
- 全局、预览和站点设置：上传大小、默认有效期、预览格式与大小、站外图片、入口开关、站点标题、图标、页脚、公告与安全过滤后的自定义 HTML。

精简版从 `0.3.1` 开始提供，`0.2.0` 镜像仍是旧界面。

新分享默认 24 小时后停止访问，可修改默认值或单独选择不过期。过期和删除分享记录都不会自动清理文件本体。

已移除备份还原、WebDAV、仪表盘、复杂文件浏览、任务、索引、Office / PDF / 压缩包等高级预览和旧编辑器。旧 Go 数据库、挂载与目录权限数据保留；后端兼容接口仍服务已有分享与受限路径密钥，新页面无需创建挂载。

## 资源与安全

页面、Markdown 解析器、样式与脚本均由本站提供；不加载 CDN、远程字体、代码编辑器或图表引擎。Markdown 按需加载，纯文本编辑不请求代码高亮主题。PWA 全量预缓存已关闭，升级脚本会撤销旧 Worker 并清理本站缓存。Markdown 默认不加载站外图片；管理员可在预览设置中显式允许。自定义 HTML 不执行脚本、事件属性、表单或 iframe。

管理员统一使用 `/api/admin/*`，普通 API Key 使用 `/api/*`，访客读取公开分享。IP 白名单或 mTLS 由外部 Nginx 实施，必须覆盖 `/api/admin` 及全部子路径；管理账号登录验证仍保留。不要公开可绕过反向代理的内部端口。接口列表见 [API 说明](Api-doc.md)。

## Docker 快速部署

以下命令从仓库根目录执行。需要 Docker Compose 和一个已存在的本地文件目录：

```sh
cp backend/.env.example backend/.env
mkdir -p files
```

编辑 `backend/.env`，填写至少 12 字节的管理员密码。使用已发布镜像时，同时填写两套镜像的相同版本：

```dotenv
ADMIN_USERNAME=admin
ADMIN_PASSWORD=请替换为你自己的强密码
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.3.1
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.3.1
LOCAL_FILES_DIR=./files
```

Linux 上让 UID `65532` 对文件目录有读写及进入目录的权限；可按实际目录所有权使用权限或 ACL 授权，避免使用 `chmod 777`。仅浏览时设置 `LOCAL_FILES_READ_ONLY=true`，并将管理界面中的 LOCAL 存储设为只读。

```sh
docker compose --env-file backend/.env config
docker compose --env-file backend/.env pull
docker compose --env-file backend/.env up -d --no-build
```

登录后创建 LOCAL 存储，`root_path` 填 `/files`，或配置 S3；随后在文件分享页选择保存位置。

默认只绑定宿主机回环地址。公网访问应通过 HTTPS 反向代理；前端以同源 `/api` 转发到后端，后端不单独暴露宿主机端口。局域网访问可在 `.env` 中调整 `BIND_ADDRESS`。

需要从源码构建时，删除或注释 `.env` 的两个 `GO_*_IMAGE` 变量，执行：

```sh
docker compose --env-file backend/.env up -d --build
```

## 二进制与 GitHub 发布

[Release](https://github.com/Qwerto107/CloudPaste/releases) 提供 Linux `amd64`、`arm64` 后端二进制及 SHA-256 校验文件。前端仍需单独部署静态页面或使用配套 Nginx 镜像。

工作流为 [.github/workflows/backend.yml](.github/workflows/backend.yml)，Actions 名称为 **Go backend**：

| 触发方式 | 构建检查和二进制 | GHCR 镜像 | Release |
| --- | --- | --- | --- |
| 相关分支提交 / PR | 执行 | 不发布 | 不创建 |
| 手动运行，默认选项 | 执行 | 不发布 | 不创建 |
| 手动勾选 `publish_images` | 执行 | 发布提交号标签 | 不创建 |
| 推送 `go-v数字.数字.数字` 标签 | 执行 | 发布版本、提交号及 `latest` | 创建正式版本 |
| 推送 `go-v数字.数字.数字-rc.1` 等标签 | 执行 | 发布版本及提交号 | 创建预发行版本 |

前后端镜像均支持 `linux/amd64`、`linux/arm64`。预发行标签不更新 `latest`。发布会等待 Go 检查、双架构编译及真实 Docker 上传 / 分享验证全部通过。发布权限与详细步骤见 [构建与发布说明](backend/README.md#github-ci-构建与发布)。

## 数据与升级

数据位于 Compose 的 `go-data` 卷，包含 `cloudpaste-go.db`、SQLite WAL 文件、`encryption.key` 以及上传临时文件。备份前停止服务并保存整个数据卷；仅复制数据库会丢失解密密钥。`docker compose down` 保留数据卷，`down -v` 会删除它。

本次目录整理保留了 Compose 项目名 `cloudpaste-go`、卷名 `go-data`、数据库文件名及 `GO_BACKEND_IMAGE` / `GO_FRONTEND_IMAGE` 变量。已有 Go 部署更新仓库后，应将原 `backend-go/.env` 放到 `backend/.env`，改用默认 `docker-compose.yml`，并从同一项目目录执行 Compose 命令，以继续使用原卷。

旧 Node 数据库、账号密码格式、存储凭据及会话不能直接用于 Go 版。旧分享、挂载和 API Key 也不会自动出现。移除旧代码不会执行数据库迁移；使用旧数据前需要单独实现并验证迁移工具。

## 本地开发

后端使用 Go `1.27.2` 工具链及纯 Go SQLite，无需 C 编译器：

```sh
cd backend
go test ./... -count=1
go vet ./...
go build -o bin/cloudpaste ./cmd/cloudpaste
ADMIN_PASSWORD='replace-with-a-strong-password' DATA_DIR=./data go run ./cmd/cloudpaste
```

另开终端，从仓库根目录启动前端。需要 Node.js 22 及 npm，开发代理默认指向 `http://localhost:8787`：

```sh
cd frontend
npm ci
npm run test:go-upload
npm run dev
```

生产静态资源通过 `npm run build` 输出到 `frontend/dist/`。安全边界、上传额度、二进制启动及验证方式见 [后端说明](backend/README.md)；核心接口见 [API 说明](Api-doc.md)。

## 目录

```text
backend/                  Go 后端、测试、Dockerfile 和环境变量示例
frontend/                 Vue 前端及静态资源构建
docker/frontend/         Nginx 静态页面与同源代理配置
.github/workflows/backend.yml  构建、检查与 GHCR / Release 发布
docker-compose.yml        默认 Docker 部署
README.md                 中文首页
README_EN.md              English guide
```

## 许可证

沿用 [Apache License 2.0](LICENSE)。
