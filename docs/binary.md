# CloudPaste 二进制部署

CloudPaste 提供内置前端的单文件程序和独立后端两种构建，均使用 SQLite 保存数据，文件可存储在本地目录或 S3。

| 压缩包 | 内容 |
| --- | --- |
| `cloudpaste-standalone-linux-{amd64,arm64}.tar.gz` | 完整界面和 API，运行一个程序即可使用 |
| `cloudpaste-go-linux-{amd64,arm64}.tar.gz` | 仅 HTTP API，需另外部署前端 |

## 下载与启动

从 [GitHub Releases](https://github.com/Qwerto107/CloudPaste/releases) 下载对应架构的压缩包及 SHA-256 校验文件。Linux x86-64 使用 amd64，Linux ARM64 使用 arm64。下面以包含前端的 amd64 版本为例：

```sh
sha256sum -c cloudpaste-standalone-linux-amd64.tar.gz.sha256
tar -xzf cloudpaste-standalone-linux-amd64.tar.gz
cd cloudpaste-standalone-linux-amd64
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD='请替换为至少12字节的强密码'
export DATA_DIR=./data
export PORT=8787
./cloudpaste
```

打开 `http://127.0.0.1:8787/admin/login` 即可使用完整界面。程序已嵌入静态文件，运行目录无需 Node.js、Nginx 或 `frontend/dist`。

以上命令以前台方式运行；长期运行时通过系统服务管理进程，保护环境配置文件的读取权限。程序监听指定端口，公网部署应通过 HTTPS 反向代理，并限制对内部端口的直接访问。只需要 API 时，将下载与解压命令中的 `cloudpaste-standalone` 改为 `cloudpaste-go`，其余启动配置相同。

## 数据与文件

运行账号必须能够读写 DATA_DIR。该目录包含 cloudpaste-go.db、SQLite WAL、encryption.key 和上传暂存文件。持久保存整个目录；加密密钥用于读取已保存的存储凭据。

本地存储配置使用后端进程可访问的真实目录。S3 存储需要配置 Bucket、Region、Endpoint 和凭据。删除或过期分享仅停止访问，不删除存储中的原文件。

## 前端

`cloudpaste-standalone` 将前端静态资源编译进程序，页面与 `/api` 使用同一地址；页面路由支持直接访问和刷新。升级前端需要替换完整程序。

`cloudpaste-go` 仅包含 API。可单独部署 `frontend/dist`，或使用配套前端镜像。浏览器通过同源 `/api` 访问后端。

管理员页面入口为 /admin/login，API Key 登录入口为 /login。管理员 API 前缀 /api/admin 可在外部反向代理配置 IP 白名单或 mTLS。

程序默认不信任转发头；置于反向代理后时，设置 `TRUSTED_PROXIES` 为明确的可信代理 IP、CIDR 或主机名，并让入口代理覆盖客户端自行发送的 `X-Forwarded-For`。不能填写 `*` 或 `/0`。

完整部署、配置和 API 文档：[文档中心](https://github.com/Qwerto107/CloudPaste/tree/main/docs)。
