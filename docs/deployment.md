# 部署指南

[文档中心](README.md) · [使用指南](usage.md) · [二进制部署](binary.md)

## Docker Compose

需要 Docker 和 Compose。仓库根目录的 [docker-compose.yml](../docker-compose.yml) 包含静态前端和 Go 后端两个服务。

```sh
cp backend/.env.example backend/.env
mkdir -p files
```

编辑 `backend/.env`：

```dotenv
ADMIN_USERNAME=admin
ADMIN_PASSWORD=请替换为至少12字节的强密码
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.4.1
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.4.1
LOCAL_FILES_DIR=./files
BIND_ADDRESS=127.0.0.1
HTTP_PORT=8081
```

Linux 上，新建的专用文件目录可使用 `sudo chown 65532:65532 files` 授权。已有目录应按实际所有权设置权限或 ACL，确保 UID/GID `65532` 可进入、读取和写入。

```sh
docker compose --env-file backend/.env pull
docker compose --env-file backend/.env up -d --no-build
docker compose --env-file backend/.env ps
```

管理员访问 `http://127.0.0.1:8081/admin/login`。添加 LOCAL 存储时，根目录填容器内路径 `/files`；S3 填写 Bucket、Region、Endpoint 和凭据。

从源码构建时，注释两个 `GO_*_IMAGE` 变量，然后执行 `docker compose --env-file backend/.env up -d --build`。

## 端口与目录

| 项目 | 默认值 | 说明 |
| --- | --- | --- |
| 前端宿主机入口 | `127.0.0.1:8081` | 由 `BIND_ADDRESS`、`HTTP_PORT` 配置 |
| 前端容器端口 | `8080` | Nginx 提供页面和同源 API 转发 |
| 后端容器端口 | `8787` | Compose 不单独公开宿主机端口 |
| 数据目录 | `/data` | 挂载名为 `go-data` 的数据卷 |
| 本地文件目录 | `/files` | 来自 `LOCAL_FILES_DIR`，宿主机目录必须存在 |
| Compose 项目名 | `cloudpaste-go` | 更改项目名可能创建另一套数据卷 |

可以把 `go-data:/data` 改为 `./data:/data` 直接绑定目录。此时应先创建 `data` 并授予 UID/GID `65532` 读写权限；现有数据库、WAL 及加密密钥也需可读写。出现 `permission denied` 时优先核对挂载路径与所有权，不要用全局可写权限替代排查。

`LOCAL_FILES_READ_ONLY=true` 会将文件目录只读挂载，管理界面也应将对应存储标记为只读。

## 资源配置

| 环境变量 | 默认值 | 含义 |
| --- | --- | --- |
| `MAX_UPLOAD_SIZE_MB` | `100` | 单文件硬上限，单位 MiB |
| `MAX_STAGED_SIZE_MB` | `1024` | 上传暂存总额度，不能小于单文件上限 |
| `MAX_UPLOADS` | `2` | 同时上传数量，范围 1–8 |
| `MAX_DOWNLOADS` | `8` | 同时下载数量，范围 1–128 |
| `GOMEMLIMIT` | `80MiB` | Go GC 软目标，不是进程内存硬限制 |

Compose 另设后端 `128m`、前端 `64m` 容器内存限制。系统设置的上传大小只能降低环境硬上限。按实际负载使用 `docker stats` 观察内存、CPU 和 I/O。

## HTTPS 与管理员入口

前端、后端可独立部署。推荐浏览器统一访问站点域名，由反向代理将 `/api` 转发到后端；静态文件请求交给前端。默认 Compose 已配置同源转发。

管理员 API 是 `/api/admin` 及全部子路径，包含登录、文件读取与上传。可在外部反向代理为这个完整前缀配置 IP 白名单或 mTLS，同时保持账号认证。确保内部服务端口不能绕过代理直接访问。

若使用 PROXY protocol，IP 白名单应依据受信任代理提供的真实客户端地址；仅接受可信上游的 PROXY protocol 数据。普通用户 API 为 `/api/*`，公开分享不应被管理员白名单整体拦截。页面路由与 API 路由见 [API 参考](api.md)。

## 数据与升级

`/data` 包含 `cloudpaste-go.db`、SQLite WAL 文件、`encryption.key` 和上传暂存数据。备份前停止写入，保存完整数据目录与需要保留的文件存储；数据库和加密密钥应一起保存。

升级时将两个镜像变量改为相同版本，再执行：

```sh
docker compose --env-file backend/.env pull
docker compose --env-file backend/.env up -d --no-build
```

继续使用同一项目名、数据卷及文件路径。升级不需要重新初始化数据库；`docker compose down -v` 会删除命名数据卷。分享过期或删除记录只撤销访问，不会清除原文件。

## 常见问题

- **登录报 CORS**：确认浏览器的 API 地址与页面同源。不要混用 `localhost` 和 `127.0.0.1`；开发配置见 [开发指南](development.md)。
- **后端日志已启动但无法连接宿主机 8787**：默认没有公开后端端口，通过前端入口访问 `/api/health`。
- **文件无法上传**：核对存储公开标记、API Key 权限、存储 ACL、路径范围、目录写权限与上传额度。
- **界面没有更新**：确认两个镜像版本一致并已重新创建容器，然后刷新浏览器。
