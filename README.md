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

## 分享与管理

创建文本或上传文件时，可填写「自定义链接名称」，例如 `/paste/my-note`、`/file/my-file`。留空自动生成；支持 1–100 个英文字母、数字、短横线或下划线。同类分享名称不能重复，批量上传可逐文件指定。

API Key 用户从 `/login` 登录，在 `/manage/*` 管理自己的分享；管理员通过登录页下方的“管理员登录”进入 `/admin/login`，在 `/admin/*` 管理站点。登录后的普通用户管理页不包含管理员菜单或链接。

文本和文件分享均直接显示有效期、访问密码及最多查看次数（0 表示不限）。有效期可选 5 / 10 / 30 分钟、1 / 3 / 6 / 12 小时、1 / 3 / 7 / 14 / 30 / 90 天，以及长期保留；其他站点默认值仍会显示。文件备注放在「更多选项」中。批量上传成功项会移出队列，失败重试只上传剩余文件。

新增 API Key 可填写自定义密钥（24–128 位英文字母、数字或半角符号，不含空格），留空自动生成。完整密钥仅在创建结果中展示，列表显示脱敏值；重复密钥无法创建，编辑权限不会更换密钥。权限分别控制创建文本、上传并分享文件、管理自己的文本及文件，另可限制存储和路径。

系统设置分为「常规」「文件预览」「站点外观」，提供分组说明、开关与数值单位；高级 HTML 默认折叠。修改后需保存，切换页面前会提示未保存内容，固定安全策略仅供查看。

界面采用清爽薄荷配色，支持浅色、暗色切换。首次访问跟随系统主题，手动选择后在当前浏览器保存；手机端使用底部导航。图标为本站 SVG，不依赖远程字体或图标库。

默认 favicon 为「链云」：`frontend/public/chain-cloud.svg` 随浏览器配色切换，另提供 16 / 32 / 48 像素 ICO 与 180 像素触屏图标。站点外观的图标 URL 可覆盖标签页图标，留空恢复默认；触屏图标仍使用本站文件。在 `frontend/` 运行 `node scripts/generate-icons.mjs` 可从 SVG 同步生成位图，需要本机 Chrome / Edge 或 Playwright Chromium。

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
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.4.1
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.4.1
LOCAL_FILES_DIR=./files
```

Linux 上让 UID `65532` 对文件目录有读写及进入目录的权限；可按实际目录所有权使用权限或 ACL 授权，避免使用 `chmod 777`。仅浏览时设置 `LOCAL_FILES_READ_ONLY=true`，并将管理界面中的 LOCAL 存储设为只读。

```sh
docker compose --env-file backend/.env config
docker compose --env-file backend/.env pull
docker compose --env-file backend/.env up -d --no-build
```

打开 `http://127.0.0.1:8081/admin/login`，使用配置的管理员账号登录。创建 LOCAL 存储，`root_path` 填 `/files`，或配置 S3；随后在文件分享页选择保存位置。为普通用户创建 API Key 后，让其从 `/login` 登录。

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

前后端镜像均支持 `linux/amd64`、`linux/arm64`。预发行标签不更新 `latest`。发布会等待 Go 竞态 / 静态 / 依赖漏洞检查、双架构编译、真实 Docker 上传 / 分享验证及浏览器回归全部通过。失败的构建标签不代表已有可下载的 Release 或镜像。发布权限与详细步骤见 [构建与发布说明](backend/README.md#github-ci-构建与发布)。

## 数据与升级

从 Go `0.3.1` 升级：保留原数据卷、文件目录和 `backend/.env`，将两个镜像变量改为相同的新版本，再执行上述 `pull` 与 `up -d --no-build`。无需重新初始化数据库，不要运行 `down -v`。前端旧资源未更新时刷新页面，必要时清除旧站点缓存。版本变化见 [更新记录](CHANGELOG.md)。

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

另开终端，从仓库根目录启动前端，需要 Node.js 22 及 npm。浏览器统一使用 `http://127.0.0.1:3000`，Vite 将同源 `/api` 代理到 `http://127.0.0.1:8787`。启动时覆盖旧开发配置中的直连地址，避免混用 `localhost` 与 `127.0.0.1` 引发 CORS：

```sh
cd frontend
npm ci
npm run test:go-upload
VITE_BACKEND_URL=http://127.0.0.1:3000 npm run dev -- --host 127.0.0.1
```

PowerShell 使用 `$env:VITE_BACKEND_URL='http://127.0.0.1:3000'` 后再执行 `npm run dev -- --host 127.0.0.1`。更改前端端口时同步调整该地址。生产默认使用同源 `/api`。

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
