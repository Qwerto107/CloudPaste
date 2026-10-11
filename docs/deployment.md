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
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.5.0
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.5.0
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

## 合并运行：一个容器提供前后端

[docker-compose.standalone.yml](../docker-compose.standalone.yml) 使用同一个 Go 进程提供内嵌前端与 API，不依赖 Nginx。在上面的 `backend/.env` 中增加：

```dotenv
GO_STANDALONE_IMAGE=ghcr.io/qwerto107/cloudpaste-standalone:0.5.0
```

启动命令：

```sh
docker compose --env-file backend/.env -f docker-compose.standalone.yml pull
docker compose --env-file backend/.env -f docker-compose.standalone.yml up -d --no-build
```

页面入口仍为 `http://127.0.0.1:8081/admin/login`，容器内监听 `8787`。从源码构建时，注释 `GO_STANDALONE_IMAGE` 后执行 `docker compose --env-file backend/.env -f docker-compose.standalone.yml up -d --build`。

两种模式使用相同项目名 `cloudpaste-go`、`go-data` 数据卷和 `/files` 挂载。已有分离部署切换前，先执行 `docker compose --env-file backend/.env down`；反向切换时，先执行 `docker compose --env-file backend/.env -f docker-compose.standalone.yml down`。保留原环境文件和项目名，不加 `-v`，不要让两种模式同时访问同一数据库。

合并版默认不信任转发头；如果前面还有 HTTPS 代理，使用 `TRUSTED_PROXIES` 配置 Go 进程实际看到的可信代理地址。配置方法见下文。合并版无需配置 Nginx 的 `set_real_ip_from`。

## 端口与目录

| 项目 | 默认值 | 说明 |
| --- | --- | --- |
| 前端宿主机入口 | `127.0.0.1:8081` | 由 `BIND_ADDRESS`、`HTTP_PORT` 配置 |
| 前端容器端口 | `8080` | Nginx 提供页面和同源 API 转发 |
| 后端容器端口 | `8787` | Compose 不单独公开宿主机端口 |
| 合并版容器端口 | `8787` | 同时提供页面和 API，映射到宿主机 `HTTP_PORT` |
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

分离版 Compose 另设后端 `128m`、前端 `64m` 容器内存限制，合并版为单容器 `128m`。系统设置的上传大小只能降低环境硬上限。按实际负载使用 `docker stats` 观察内存、CPU 和 I/O。

单个来源 IP 最多同时下载 2 个文件；当全局下载额度低于 4 时，单来源额度会进一步降低，为其他来源留出位置（全局额度为 1 时只能串行下载）。超出单来源额度返回 429，全局额度耗尽返回 503。下载每个 32 KiB 数据块的写入最多等待 30 秒，正常持续传输可超过 30 秒；停读连接超时后释放额度。使用反向代理时需按下文配置可信来源，避免多个用户共享代理 IP 的额度。

Windows 的 LOCAL 存储只接受与磁盘实际名称一致的路径，拒绝不同大小写、8.3 短名称以及尾点、尾空格等别名，防止绕过目录或单文件密码。配置挂载根内路径、API Key 的 `basic_path` 和目录元信息时，应使用文件实际的名称和大小写；Linux 与 S3 的路径规则保持不变。

## HTTPS 与管理员入口

前端、后端可独立部署。推荐浏览器统一访问站点域名，由反向代理将 `/api` 转发到后端；静态文件请求交给前端。默认 Compose 已配置同源转发。

管理员 API 是 `/api/admin` 及全部子路径，包含登录、文件读取与上传。可在外部反向代理为这个完整前缀配置 IP 白名单或 mTLS，同时保持账号认证。确保内部服务端口不能绕过代理直接访问。

从普通页面进入管理员区域会执行整页导航，服务器会收到 `/admin` 或 `/admin/*` 的页面请求。若还需要限制管理员页面的访问，可对这两个页面入口配置相同的代理访问规则；管理员 API 的保护仍需单独覆盖 `/api/admin` 及全部子路径。

若使用 PROXY protocol，IP 白名单应依据受信任代理提供的真实客户端地址；仅接受可信上游的 PROXY protocol 数据。普通用户 API 为 `/api/*`，公开分享不应被管理员白名单整体拦截。页面路由与 API 路由见 [API 参考](api.md)。

### 真实客户端地址与可信代理

密码尝试按真实来源分别限流，同时保留访客/管理入口隔离和全站总额度。后端默认忽略所有转发头。二进制部署可设置 `TRUSTED_PROXIES`，填写逗号分隔的明确代理 IP、CIDR 或可信 DNS 管理的代理主机名；例如本机代理可使用 `127.0.0.1,::1`。不要填写包含普通客户端的宽泛网段，后端拒绝 `/0` 和 `*`。解析主机名最多等待一秒，地址缓存 30 秒；DNS 失败时不继续使用已过期的信任地址。

默认 Compose 只信任内部服务名 `frontend`，通过 Docker DNS 识别前端容器，无需固定 Docker 子网。前端 Nginx 用自己的 `$remote_addr` **覆盖** `X-Forwarded-For`，客户端自行发送该头不能伪造限流身份。后端仅接受可信 TCP 对端提供的转发链，并从右向左取首个不受信的地址。容器网络与配置中的 DNS 服务均属于信任边界，不应接入不受信任的容器。

如果前端前面还有宿主机 HTTPS 代理、负载均衡器或 CDN，需要在**每一层**恢复并校验来源，否则这一层后面的访客仍会共享同一个限流地址：

1. 最外层代理覆盖客户端自带的 `X-Forwarded-For`；若还有可信上游，则先依据该上游的明确地址名单恢复来源。
2. 在 `docker/frontend/nginx.conf` 的 `server` 内增加 `set_real_ip_from`，只填写 Nginx 实际看到的上层代理来源地址，再设置 `real_ip_header X-Forwarded-For;` 和 `real_ip_recursive on;`。不要信任 `0.0.0.0/0`、整个公网或所有私网；可从前端访问日志确认实际对端。若对端是 Docker 网关，需确保入口仅绑定回环地址、只有受信宿主机代理能够接入，避免把其他客户端的转发头一并信任。
3. 修改配置后重新构建前端镜像；使用预构建镜像时，将完整配置只读挂载到 `/etc/nginx/conf.d/default.conf`，校验配置后重新创建前端容器。外层代理使用独立容器时，应通过受控网络连接，并且只信任该代理实际使用的地址。

例如，上层代理实际地址为 `192.0.2.10` 时，前端配置如下（此文档地址必须替换为部署环境的真实地址）：

```nginx
# 仅该代理能够声明真实客户端来源；/api/ 中仍覆盖转发头为 $remote_addr。
set_real_ip_from 192.0.2.10;
real_ip_header X-Forwarded-For;
real_ip_recursive on;
```

部署后用两个不同来源验证：一个来源达到登录尝试上限时，另一个来源应仍可登录；同时验证不受信客户端自行填写转发头不能更换身份。不要通过直接信任用户的转发头解决共享限流问题。

## 数据与升级

`/data` 包含 `cloudpaste-go.db`、SQLite WAL 文件、`encryption.key` 和上传暂存数据。备份前停止写入，保存完整数据目录与需要保留的文件存储；数据库和加密密钥应一起保存。

分离版升级时将两个镜像变量改为相同版本，再执行：

```sh
docker compose --env-file backend/.env pull
docker compose --env-file backend/.env up -d --no-build
```

合并版升级只需修改 `GO_STANDALONE_IMAGE` 的版本，并在上述两条命令中添加 `-f docker-compose.standalone.yml`。

继续使用同一项目名、数据卷及文件路径。升级不需要重新初始化数据库；`docker compose down -v` 会删除命名数据卷。分享过期或删除记录只撤销访问，不会清除原文件。

## 常见问题

- **登录报 CORS**：确认浏览器的 API 地址与页面同源。不要混用 `localhost` 和 `127.0.0.1`；开发配置见 [开发指南](development.md)。
- **后端日志已启动但无法连接宿主机 8787**：默认没有公开后端端口，通过前端入口访问 `/api/health`。
- **文件无法上传**：核对存储公开标记、API Key 权限、存储 ACL、路径范围、目录写权限与上传额度。
- **界面没有更新**：确认两个镜像版本一致并已重新创建容器，然后刷新浏览器。
