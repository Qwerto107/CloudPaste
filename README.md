# CloudPaste

[中文](README.md) · [English](README_EN.md)

面向 Docker 和普通服务器的文件浏览、上传及文本分享服务。后端使用 Go，前端使用 Vue / Vite 构建为静态页面，由 Nginx 提供服务。运行时无需 Node.js；Node.js 仅用于构建前端。

本仓库已移除旧 Node 后端及其 Cloudflare、Docker Hub 部署流程，Go 后端统一位于 `backend/`。当前 Go 数据库与旧 Node 数据库不兼容，尚未提供旧数据迁移工具。

## 功能与限制

- 文本 / Markdown 分享，支持密码、过期时间和访问次数限制。
- LOCAL、S3 兼容存储、WebDAV 存储的配置、挂载、浏览、流式下载和 Range 请求。
- 流式上传、文件表单上传、上传后分享；挂载上传支持分片恢复，同名文件拒绝覆盖。
- 管理员认证、API Key 权限、路径范围、存储 ACL、目录密码和基础站点设置。
- `/dav` 提供只读 WebDAV 访问。

暂不支持其他存储驱动、远程 URL 导入、浏览器直传上游、对外 WebDAV 写入、移动 / 复制 / 重命名 / 删除原文件、后台任务调度和全盘搜索。前端部分高级入口仍来自原界面，不能据此认为后端支持对应功能。完整范围见 [后端说明](backend/README.md)。

## 管理员接口隔离

管理员统一使用 `/api/admin/*`，普通 API Key 使用 `/api/*`，访客通过公开分享接口读取。管理员 WebDAV 为 `/api/admin/dav`，普通 API Key WebDAV 为 `/dav`；管理员下载与分片票据也只能在管理前缀使用。公开站点配置为 `/api/public/settings`。

IP 白名单或 mTLS 由外部 Nginx 自行配置，需覆盖 `/api/admin` 本身及其全部子路径，并避免直接暴露内部服务绕过代理。入口控制不代替管理员登录。完整接口与兼容变化见 [API 说明](Api-doc.md)。
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
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.2.0
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.2.0
LOCAL_FILES_DIR=./files
```

Linux 上让 UID `65532` 对文件目录有读写及进入目录的权限；可按实际目录所有权使用权限或 ACL 授权，避免使用 `chmod 777`。仅浏览时设置 `LOCAL_FILES_READ_ONLY=true`，并将管理界面中的 LOCAL 存储设为只读。

```sh
docker compose --env-file backend/.env config
docker compose --env-file backend/.env pull
docker compose --env-file backend/.env up -d --no-build
```

访问 `http://127.0.0.1:8081`，使用刚设置的管理员账号登录。创建 LOCAL 存储时填写 `root_path=/files`，再创建挂载，例如 `mount_path=/local`、`root_path=/`。S3 和 WebDAV 配置可在管理界面填写。

默认只绑定宿主机回环地址。公网访问应通过 HTTPS 反向代理；前端以同源 `/api`、`/dav` 转发到后端，后端不单独暴露宿主机端口。局域网访问可在 `.env` 中调整 `BIND_ADDRESS`。

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
