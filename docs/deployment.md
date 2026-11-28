# 部署指南

[文档中心](README.md) · [使用指南](usage.md) · [二进制部署](binary.md)

**目录**

- [单容器部署（推荐）](#单容器部署推荐)
- [多容器部署与模式切换](#多容器部署与模式切换)
- [配置位置与 `.env` 参数](#配置位置与-env-参数)
- [HTTPS 与管理员入口](#https-与管理员入口)
- [资源配置与设备互传](#资源配置)
- [升级、备份与常见问题](#升级与备份)

---

## 单容器部署（推荐）

单容器镜像内置完整前端与 Go 后端（支持 Linux `amd64` / `arm64` / `arm/v6` / `arm/v5` / `loong64`），适合绝大多数自托管场景。

### 1. 创建目录并下载 Compose 文件

Linux 容器使用 UID/GID `65532`（Docker Desktop 按宿主机设置权限）：

```sh
mkdir -p cloudpaste/data cloudpaste/files
sudo chown 65532:65532 cloudpaste/data cloudpaste/files
sudo chmod 700 cloudpaste/data cloudpaste/files
cd cloudpaste
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.yml -o docker-compose.yml
```

### 2. 新建 `.env` 并启动

在 `cloudpaste` 目录创建 `.env`，至少填写 `ADMIN_PASSWORD`（6 字符–72 字节）：

```dotenv
ADMIN_USERNAME=admin
ADMIN_PASSWORD='请替换为自己的长随机密码'

# 可选配置（留空使用默认值）
LOCAL_FILES_DIR=./files
LOCAL_FILES_READ_ONLY=false
GOMEMLIMIT=80MiB
TRUSTED_PROXIES=
ALLOWED_ORIGINS=
ENCRYPTION_SECRET=
```

拉取镜像（[docker-compose.yml](../docker-compose.yml)，默认 `ghcr.io/qwerto107/cloudpaste:1.0.7`）并启动：

```sh
docker compose up -d --pull always
```

### 3. 登录并添加存储

1. 打开 `http://127.0.0.1:8081/admin/login`，用 `admin` 和刚设置的密码登录。
2. 在「存储管理」新增 LOCAL 存储（根目录填 **`/files`**）或 S3 存储。文本分享与设备互传不依赖文件存储。

> [!TIP]
> 默认仅绑定本机 `127.0.0.1:8081`。远程服务器初始化可先用 `ssh -L 8081:127.0.0.1:8081 user@server` 转发端口，正式对外服务请参考 [Nginx HTTPS 反向代理示例](#nginx-https-反向代理示例)。

---

## 多容器部署与模式切换

需要分离管理 Nginx 前端和 Go 后端时（支持 Linux `amd64` / `arm64`，拉取 `cloudpaste-frontend:1.0.7` 与 `cloudpaste-backend:1.0.7`），下载 [docker-compose.split.yml](../docker-compose.split.yml) 保存为 `docker-compose.yml` 即可：

```sh
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.split.yml -o docker-compose.yml
docker compose up -d --pull always
```

### 单容器 ↔ 多容器切换

两种模式共用项目名 `cloudpaste`、`.env`、`./data` 与文件目录。在同一运行目录**先停后换**（不要同时运行两种模式）：

```sh
docker compose down
# 切换为单容器：
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.yml -o docker-compose.yml
# 或切换为多容器：
# curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.split.yml -o docker-compose.yml
docker compose up -d --pull always
```

---

## 配置位置与 `.env` 参数

**原则：应用启动参数改 `.env`，镜像版本/端口/资源硬限制改 `docker-compose.yml`，业务限额与开关在后台「系统设置」修改。**

### `.env` 参数说明

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ADMIN_USERNAME` | `admin` | 首次启动创建的管理员用户名（已有账号在后台修改） |
| `ADMIN_PASSWORD` | 必填 | 首次管理员密码（6 字符–72 字节；含 `$` 或 `#` 请用单引号包裹） |
| `LOCAL_FILES_DIR` | `./files` | 宿主机文件目录，容器内固定映射到 `/files` |
| `LOCAL_FILES_READ_ONLY` | `false` | 设为 `true` 时只读挂载文件目录（后台存储也应设为只读） |
| `GOMEMLIMIT` | `80MiB` | Go GC 软目标（硬内存上限由 Compose `mem_limit` 控制） |
| `TRUSTED_PROXIES` | 空 | 单容器可信代理 IP / CIDR / 主机名（逗号分隔）；多容器由 Compose 固定为 `frontend` |
| `ALLOWED_ORIGINS` | 空 | 前后端异源时允许的页面 Origin（逗号分隔）；同源留空 |
| `ENCRYPTION_SECRET` | 空 | 可选 32 字节随机 Base64 密钥；留空自动生成 `./data/encryption.key` |

> [!WARNING]
> 修改 `.env` 后需重新执行 `docker compose up -d` 才会重建容器生效（仅 `restart` 不会加载新环境变量）。已有部署**切勿随意更换或丢失加密密钥**，否则无法解密已保存的存储凭据与 API Key。

<details>
<summary><strong>旧版部署迁移说明（backend/.env 与命名数据卷）</strong></summary>

1. 先用**旧 Compose 文件和原项目名**停止旧容器并备份数据。
2. 将原 `backend/.env` 复制到运行目录 `.env`，把旧卷里的 `cloudpaste-go.db`、WAL、`encryption.key` 和暂存文件迁入 `./data` 并授权 UID/GID `65532`。
3. 旧变量 `GO_*_IMAGE`、`BIND_ADDRESS`、`HTTP_PORT` 已废弃，改为直接修改新版 `docker-compose.yml` 中的 `image` 与 `ports`。

</details>

---

## HTTPS 与管理员入口

### Nginx HTTPS 反向代理示例

以下适用于**宿主机 Nginx → 默认 Compose 入口 `127.0.0.1:8081`**（单文件二进制默认改为 `127.0.0.1:8787`），保存至 `/etc/nginx/conf.d/cloudpaste.conf`：

```nginx
map $http_upgrade $cloudpaste_connection {
    default upgrade;
    ''      '';
}

# 访问日志仅记录 $uri，避免下载票据查询参数落入日志
log_format cloudpaste '$remote_addr [$time_local] '
                     '"$request_method $uri $server_protocol" $status $body_bytes_sent';

server {
    listen 80;
    server_name share.example.com;
    access_log /var/log/nginx/cloudpaste.access.log cloudpaste;
    return 308 https://share.example.com$request_uri;
}

server {
    listen 443 ssl;
    server_name share.example.com;

    ssl_certificate     /etc/nginx/ssl/share.example.com/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/share.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    client_max_body_size 10g;
    client_body_timeout 120s;
    access_log /var/log/nginx/cloudpaste.access.log cloudpaste;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $cloudpaste_connection;

        # 必须关闭缓冲，保证大文件上传与设备互传实时流式转发
        proxy_request_buffering off;
        proxy_buffering off;
        proxy_cache off;

        proxy_connect_timeout 10s;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

校验并重载：`sudo nginx -t && sudo nginx -s reload`。

> [!IMPORTANT]
> 若设备互传中继长时间停留在 `0 B · 0 B/s` 后突然开始，优先检查外层所有代理的 `proxy_request_buffering` 是否均为 `off`。

### 管理员入口保护

- 管理员 API 统一在 `/api/admin` 及全部子路径，页面入口在 `/admin` 与 `/admin/*`；可在外部反代为这些前缀叠加 IP 白名单或 mTLS，同时放行 `/api/*` 供公开分享与普通 API Key 使用。

### 真实客户端地址与可信代理

后端默认忽略所有转发头，防止客户端伪造来源 IP 绕过限频：

- **单容器 / 二进制**：在 `TRUSTED_PROXIES` 填写后端实际看到的可信代理 IP、CIDR 或主机名（如本机反代填 `127.0.0.1,::1`，拒绝 `/0` 和 `*`）。
- **多容器**：后端默认信任 `frontend`；若前端 Nginx 外层还有反代/CDN，需在 `docker/frontend/nginx.conf` 的 `server` 内添加：

```nginx
set_real_ip_from 192.0.2.10; # 替换为外层代理实际地址
real_ip_header X-Forwarded-For;
real_ip_recursive on;
```

---

## 资源配置

业务限制统一在后台「系统设置」调整，保存后无需重启（原 `MAX_*`、`TRANSFER_*` 等环境变量已不再读取）：

| 后台设置 | 默认值 | 范围与说明 |
| --- | --- | --- |
| 常规 → 单文件上传上限 | 10240 MiB | 1–10240 MiB，不能大于暂存总额度 |
| 常规 → 上传暂存总额度 | 20480 MiB | 1–102400 MiB |
| 常规 → 上传 / 下载并发数 | 16 / 48 | 1–65535；单来源 IP 最多同时下载 2 个文件 |
| 常规 → 过期分享自动清理 | 关闭 / 180 天 | 开启后后台异步回收超期记录及存储原件 |
| 设备互传 → 启用设备互传 | 开启 | 关闭后仅阻止新配对，已有连接继续传输 |
| 设备互传 → 断线恢复保留时长 | 5 分钟 | 0–60 分钟，0 表示断线立即结束 |
| 设备互传 → 全站 / 单用户最大房间数 | 32 / 5 | 全站 1–256，单用户/单 IP 1–32（WebSocket 上限自动联动） |
| 设备互传 → 中继总速率上限 | 64 MiB/s | 0–10240，0 表示不限速 |
| 设备互传 → 互传单文件上限 | 10240 MiB | 1–10240 MiB，对新连接生效 |

- 容器默认内存硬限制：单容器 `128m`；多容器后端 `128m`、前端 `64m`。
- Windows 下 LOCAL 存储严格校验磁盘真实文件名大小写，拒绝 8.3 短名、尾点与尾空格别名。

---

## 设备互传与实时中继

设备互传的信令（WebSocket）与 HTTP 实时中继全部复用网站端口，**无需额外开放端口或部署 TURN**。

- **房间容量**：默认最多 32 个在线房间、同创建者/IP 最多 5 个（均可在后台「设备互传」设置调整，WebSocket 连接上限自动联动）；单文件传输停滞超 1 分钟自动清理，房间最长 2 小时。
- **HTTPS 要求**：局域网/公网使用「点对点下载」或「文件夹接收」需要受信 HTTPS（`localhost` 例外）及支持 File System Access API 的浏览器；普通 HTTP 自动使用中继。
- **文件夹队列**：单文件夹最多 10,000 项，默认并发 16（可调 1–128），详见 [API 参考：文件夹队列](api.md#文件夹队列)。

### 可选 IP 归属地数据库

1. 自行下载免费的 [IP2Location LITE DB3](https://lite.ip2location.com/database/db3-ip-country-region-city)（**仅支持 IPv4 BIN 格式**）并解压。
2. 在后台「系统设置 → IP 归属地」上传文件（最大 512 MiB），加载后立即生效并保存在 `DATA_DIR`（重启保留，卸载即删除）。
3. 查询完全在本地按需读取，不向外部发请求、不常驻内存；IPv6 始终不显示位置。

> CloudPaste uses the IP2Location LITE database for [IP geolocation](https://www.ip2location.com).

### 互传参数

- **STUN 列表**：在「系统设置 → 设备互传 → 连接设置」配置，每行一个（格式 `stun:主机:端口`，最多 5 个，留空停用），未保存时默认内置：

```text
stun:stun.miwifi.com:3478
stun:stun.hitv.com:3478
stun:stun.cloudflare.com:3478
stun:stun.l.google.com:19302
stun:stun.nextcloud.com:3478
```

- **中继并发**：默认 `64`（`0` 表示不限制总并发）；下载工具分段每路各占 1 个并发，单文件最多 4 路。

---

## 每分钟限频配置

在后台「系统设置 → 安全与限频」统一配置 9 项每分钟频率限制（范围 `1–1000000`，`0` 不表示关闭），保存后立即生效、重启保留，采用单进程内存 1 分钟固定窗口。完整维度与默认值见 [API 参考：限频策略](api.md#限频策略)。

---

## 升级与备份

- **备份**：停止写入后备份运行目录下的 `./data`（含 `cloudpaste-go.db`、WAL、`encryption.key`、`operation-logs.db`）以及本地文件目录 `./files`。
- **升级**：修改运行目录 `docker-compose.yml` 中的 `image` 标签（多容器同步修改前后端两处），然后执行：

```sh
docker compose up -d --pull always
```

无需重新下载 Compose 文件或初始化数据库，停止/重建容器不会删除宿主机 `./data`。

---

## 常见问题

| 现象 | 排查方向 |
| --- | --- |
| **查看状态或日志** | 运行 `docker compose ps` 或 `docker compose logs --tail=100` |
| **本机打不开远程的 `127.0.0.1:8081`** | 使用 `ssh -L 8081:127.0.0.1:8081` 转发，或配置 HTTPS 反向代理 |
| **修改 `.env` 后未生效** | 执行 `docker compose up -d` 重建容器，而非 `restart` |
| **登录报 CORS 错误** | 确保访问页面与 API 同源，不要混用 `localhost` 与 `127.0.0.1` |
| **文件无法上传** | 核对存储状态/公开标记/只读标记、API Key 权限、宿主机目录 `65532` 写权限与上传额度 |
| **中继下载卡在 `0 B/s`** | 检查所有外层代理的 `proxy_request_buffering off` 是否生效 |

需要修改源码并自行构建镜像，见 [开发指南](development.md#从源码构建-docker-镜像)。
