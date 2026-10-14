# CloudPaste 二进制部署

优先使用 `cloudpaste` 完整程序：前端页面与 API 已编译在同一个文件中，不需要 Node.js、Nginx 或额外静态文件。分离部署提供 `cloudpaste-backend` 独立后端，前端使用配套 `cloudpaste-frontend` 镜像或自行部署静态资源。

## 选择下载包

从 [GitHub Releases](https://github.com/Qwerto107/CloudPaste/releases) 下载压缩包和同名 `.sha256` 校验文件。

| 系统 / 架构 | 完整程序压缩包 | 独立后端压缩包 |
| --- | --- | --- |
| Linux x86-64 | `cloudpaste-linux-amd64.tar.gz` | `cloudpaste-backend-linux-amd64.tar.gz` |
| Linux ARM64 | `cloudpaste-linux-arm64.tar.gz` | `cloudpaste-backend-linux-arm64.tar.gz` |
| Linux ARMv6 | `cloudpaste-linux-armv6.tar.gz` | — |
| Linux ARMv5 | `cloudpaste-linux-armv5.tar.gz` | — |
| Linux LoongArch64 | `cloudpaste-linux-loong64.tar.gz` | — |
| Windows x86-64 | `cloudpaste-windows-amd64.zip` | — |
| macOS Apple Silicon | `cloudpaste-darwin-arm64.tar.gz` | — |

压缩包包含程序、许可证和本说明。ARMv5 和 ARMv6 使用各自的指令集构建，请按设备选择；macOS 的文件名使用 Go 平台标识 `darwin`。

## Linux 启动

以下以 x86-64 完整程序为例，其他 Linux 平台替换压缩包名称即可：

```sh
sha256sum -c cloudpaste-linux-amd64.tar.gz.sha256
tar -xzf cloudpaste-linux-amd64.tar.gz
cd cloudpaste-linux-amd64
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD='请替换为至少12字节的强密码'
export DATA_DIR=./data
export PORT=8787
./cloudpaste
```

打开 `http://127.0.0.1:8787/admin/login` 即可使用完整界面。只需要 API 时，下载 `cloudpaste-backend-linux-amd64.tar.gz`，解压进入对应目录，设置同样的环境变量后运行 `./cloudpaste-backend`。

## Windows 启动

在 PowerShell 中校验、解压并启动：

```powershell
$archive = 'cloudpaste-windows-amd64.zip'
$expected = (Get-Content "$archive.sha256").Split(' ')[0]
if ((Get-FileHash $archive -Algorithm SHA256).Hash -ne $expected) {
    throw '文件校验失败，请重新下载'
}
Expand-Archive $archive -DestinationPath .
Set-Location cloudpaste-windows-amd64
$env:ADMIN_USERNAME = 'admin'
$env:ADMIN_PASSWORD = '请替换为至少12字节的强密码'
$env:DATA_DIR = './data'
$env:PORT = '8787'
./cloudpaste.exe
```

## macOS 启动

```sh
shasum -a 256 -c cloudpaste-darwin-arm64.tar.gz.sha256
tar -xzf cloudpaste-darwin-arm64.tar.gz
cd cloudpaste-darwin-arm64
export ADMIN_PASSWORD='请替换为至少12字节的强密码'
export DATA_DIR=./data
export PORT=8787
./cloudpaste
```

这些命令以前台方式运行，环境变量对当前终端生效；程序不会自动读取 `.env`。长期运行时通过系统服务管理进程，并保护配置文件的读取权限。公网部署使用 HTTPS 反向代理，限制内部端口的直接访问。

## 数据与配置

运行账号必须能读写 `DATA_DIR`。该目录保存数据库、SQLite WAL、`encryption.key` 和上传暂存文件；历史数据库文件名 `cloudpaste-go.db` 为兼容已有数据继续保留。备份整个目录，加密密钥用于读取已保存的存储凭据。

本地存储使用进程可访问的真实路径；S3 存储配置 Bucket、Region、Endpoint 和凭据。删除或过期分享仅停止访问，不删除存储中的原文件。

`cloudpaste` 的页面与 `/api` 使用同一地址，支持页面路由直接访问和刷新；升级前端时替换完整程序。`cloudpaste-backend` 只提供 API，前端需要通过同源 `/api` 访问它。分离版二进制和镜像仅提供 Linux amd64、arm64；其他 Linux 架构使用完整程序或单容器镜像。

管理员入口为 `/admin/login`，API Key 登录入口为 `/login`。外部反向代理可为 `/api/admin` 配置 IP 白名单或 mTLS。

程序默认不信任转发头。使用反向代理时，将 `TRUSTED_PROXIES` 设置为明确的代理 IP、CIDR 或主机名，让入口代理覆盖客户端自带的 `X-Forwarded-For`，不能填写 `*` 或 `/0`。

完整环境变量说明见 [部署文档](https://github.com/Qwerto107/CloudPaste/blob/main/docs/deployment.md)。
