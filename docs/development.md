# 开发指南

[文档中心](README.md) · [发布流程](releasing.md)

## 项目结构

```text
backend/                  Go 服务、SQLite、存储和测试
frontend/                 Vue 界面、静态资源与浏览器回归
docker/frontend/          Nginx 镜像与同源代理
docker/standalone/        前后端合并镜像
scripts/prepare-webui.mjs  将前端构建产物准备到 Go 嵌入目录
docs/                     产品、API、部署、开发文档及截图
.github/workflows/        检查与发布流水线
docker-compose.yml        默认部署
docker-compose.standalone.yml 合并运行部署
```

使用 Go 1.27.2 与 npm 构建工具链；具体构建环境和依赖版本以工作流及锁文件为准。SQLite 使用纯 Go 实现，无需 C 编译器。

## 启动后端

```sh
cd backend
export ADMIN_PASSWORD='请替换为至少12字节的强密码'
export DATA_DIR=./data
go run ./cmd/cloudpaste
```

PowerShell 通过 `$env:ADMIN_PASSWORD`、`$env:DATA_DIR` 设置变量。后端默认端口 8787，数据目录应与测试用临时目录分开。

## 启动前端

另开终端，从仓库根目录执行：

```sh
cd frontend
npm ci
VITE_BACKEND_URL=http://127.0.0.1:3000 npm run dev -- --host 127.0.0.1
```

PowerShell：

```powershell
cd frontend
npm ci
$env:VITE_BACKEND_URL='http://127.0.0.1:3000'
npm run dev -- --host 127.0.0.1
```

浏览器统一打开 `http://127.0.0.1:3000`。Vite 将同源 `/api` 代理到 `127.0.0.1:8787`。该变量覆盖开发配置中的后端直连地址；不要混用主机名或端口，否则会产生跨域请求。

`npm run build` 生成 `frontend/dist/`；部署时用静态服务器提供文件，将页面路由回退到 `index.html`，并配置同源 API 转发。

## 构建前后端单文件程序

从仓库根目录执行（产物为当前操作系统和架构）：

```sh
npm --prefix frontend ci
npm --prefix frontend run build
node scripts/prepare-webui.mjs
cd backend
go build -tags with_frontend -trimpath -ldflags='-s -w' -o bin/cloudpaste ./cmd/cloudpaste
```

Windows 可将输出路径改为 `bin/cloudpaste.exe`。设置管理员密码后运行产物，直接访问 `http://127.0.0.1:8787`。不传 `with_frontend` 时仍构建仅 API 的后端。

准备脚本只复制已构建的 `frontend/dist`，输出到被 Git 忽略的 `backend/internal/webui/dist`；构建合并版前必须重新构建并准备前端，避免嵌入旧资源。缺少 `dist/index.html` 时，带标签的 Go 构建会失败。二进制通过嵌入文件系统提供页面，不会读取工作目录中的其他文件。

## 检查

```sh
cd backend
go test ./... -count=1
go vet ./...
go build -o bin/sharing-backend ./cmd/cloudpaste
cd ../frontend
npm run test:security
npm run test:go-upload
npm run build
npx playwright install chromium
SHARING_TEST_BINARY=../backend/bin/sharing-backend npm run test:e2e
```

Windows 可构建 `backend/bin/sharing-backend.exe`，测试默认使用这个路径；也可设置 `SHARING_TEST_BINARY`。浏览器使用本机 Chrome / Edge 或 Playwright Chromium，支持 `SHARING_TEST_BROWSER` 指定可执行文件。

端到端检查启动独立临时服务与数据库，覆盖登录、分享、上传、媒体预览、设置、权限、主题和屏宽。设置 `SHARING_TEST_STANDALONE=1` 并将 `SHARING_TEST_BINARY` 指向带 `with_frontend` 的程序，可运行相同回归直接验证 Go 提供的页面。CI 检查这两种模式，另运行 `go test -race ./... -count=1 -timeout=300s`、依赖漏洞检查及两套真实 Docker 集成测试。

## 图标与截图

图标源文件为 [chain-cloud.svg](../frontend/public/chain-cloud.svg)，导出脚本为 [generate-icons.mjs](../frontend/scripts/generate-icons.mjs)。它使用本地浏览器导出 16 / 32 / 48 像素 ICO、32 像素 PNG 和 180 像素触屏图标。修改图形后同步导出并检查浅暗背景。

产品展示图位于 `docs/assets/`。`product-showcase.png` 是基于当前界面制作的整版产品效果图，实际截图单独保存并在 README 中折叠展示。更新图片时不要包含真实密码、API Key、存储凭据或用户分享内容。文档图片不参与前端运行时加载。
