# 部署指南

[文档中心](README.md) · [使用指南](usage.md) · [二进制部署](binary.md)

## 单容器部署（推荐）

一个容器提供完整界面和 API，适合大多数部署。安装 Docker 与 Compose 后，只需准备运行目录、Compose 文件和 `.env`。

### 1. 创建运行目录并下载 Compose 文件

Linux 容器使用 UID/GID `65532`。以下权限命令用于新建专用目录；已有目录按原所有权或 ACL 授权，Docker Desktop 按宿主机设置访问权限。

```sh
mkdir -p cloudpaste/data cloudpaste/files
sudo chown 65532:65532 cloudpaste/data cloudpaste/files
sudo chmod 700 cloudpaste/data cloudpaste/files
cd cloudpaste
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.yml -o docker-compose.yml
```

### 2. 创建环境文件并启动

在运行目录新建 `.env`，下面是完整的容器部署配置示例。替换 `ADMIN_PASSWORD` 为自己的密码，至少 6 个字符、最多 72 字节，其余可先使用默认值；可选项留空即可。

```dotenv
# 管理员：首次初始化使用，之后在管理后台修改账号和密码。
ADMIN_USERNAME=admin
ADMIN_PASSWORD='请替换为自己的长随机密码'

# 文件目录：宿主机路径，容器内仍使用 /files。
LOCAL_FILES_DIR=./files
LOCAL_FILES_READ_ONLY=false

# 资源与普通文件上传，大小单位 MiB。
GOMEMLIMIT=80MiB
MAX_DOWNLOADS=48
MAX_UPLOADS=16
MAX_UPLOAD_SIZE_MB=10240
MAX_STAGED_SIZE_MB=20480

# 设备互传：授权用户创建，访客用配对码加入。
TRANSFER_ENABLED=true
TRANSFER_MAX_FILE_MB=10240
TRANSFER_RATE_MB=64
# 可选 STUN 地址，格式 stun:主机:端口。
TRANSFER_STUN=

# 每分钟限频：除 WebSocket 消息按成员外，其余按真实来源 IP。
PASSWORD_ATTEMPTS_PER_MINUTE=10
TRANSFER_CREATE_PER_MINUTE=30
TRANSFER_JOIN_PER_MINUTE=30
TRANSFER_RELAY_PER_MINUTE=180
TRANSFER_WS_CONNECT_PER_MINUTE=30
TRANSFER_WS_MESSAGES_PER_MINUTE=240
TRANSFER_TOOL_PER_MINUTE=240
TRANSFER_FOLDER_WS_MESSAGES_PER_MINUTE=12000
TRANSFER_FOLDER_RELAY_PER_MINUTE=12000

# 可信反向代理：直接访问时留空；只填实际可信代理 IP、CIDR 或主机名。
# 多容器后端由 Compose 固定信任 frontend，此项用于单容器。
TRUSTED_PROXIES=

# 同源部署留空；前后端异源时填写页面 Origin，多个值用逗号分隔。
ALLOWED_ORIGINS=

# 留空时自动生成并保存 ./data/encryption.key；已有数据不要随意换密钥。
# 自定义值必须是 32 字节随机数据的 Base64 编码。
ENCRYPTION_SECRET=
```

然后拉取镜像并启动：

```sh
docker compose up -d --pull always
```

此命令使用 [docker-compose.yml](../docker-compose.yml)，默认拉取 `ghcr.io/qwerto107/cloudpaste:0.8.6`。镜像已包含前后端，无需安装 Go、Node.js 或本地打包。

单容器镜像支持 Linux amd64、arm64、arm/v6、arm/v5、loong64，Docker 按宿主机架构选择。Windows 和 macOS 的原生单文件程序见 [二进制部署](binary.md)；Apple Silicon 上的 Docker 使用 Linux arm64 镜像。

### 3. 登录并添加存储

打开 `http://127.0.0.1:8081/admin/login`，使用 `admin` 和刚填写的密码登录。在「存储管理」添加 LOCAL 存储，根目录填 **`/files`**，随后即可上传文件；也可添加 S3 存储。文本分享和设备互传不依赖文件存储。

默认入口仅本机可访问。远程服务器可先使用 SSH 转发：

```sh
ssh -L 8081:127.0.0.1:8081 用户名@服务器地址
```

保持 SSH 连接，再从自己电脑打开上述地址。正式对外服务请参考 [Nginx HTTPS 反向代理示例](#nginx-https-反向代理示例)。

## 多容器部署（前后端分离）

需要单独管理静态前端和 Go 后端时，使用 [多容器配置模板](../docker-compose.split.yml)。先按上方步骤创建目录并设置权限，进入运行目录并新建 `.env`，下载后仍保存为 **`docker-compose.yml`**，启动命令与单容器相同。已有运行中的部署请按下文的切换步骤操作。

```sh
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.split.yml -o docker-compose.yml
docker compose up -d --pull always
```

默认拉取 `cloudpaste-frontend:0.8.6`、`cloudpaste-backend:0.8.6` 两个 GHCR 镜像。前端 Nginx 提供页面并转发 API，浏览器入口仍为 `http://127.0.0.1:8081`。

分离版的镜像和独立后端二进制仅提供 Linux amd64、arm64；ARMv5、ARMv6 和 Loong64 使用单容器或完整程序。

## 切换部署方式

两种模式使用相同项目名 **`cloudpaste`**，共用运行目录下的 **`./data`** 和文件目录。继续在同一运行目录操作，保留 `.env`，**先停止当前模式，再下载另一种配置替换 `docker-compose.yml` 并启动**。如果修改过镜像版本、端口或挂载路径，替换后恢复自己的设置再启动。

多容器 → 单容器：

```sh
docker compose down
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.yml -o docker-compose.yml
docker compose up -d --pull always
```

单容器 → 多容器：

```sh
docker compose down
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.split.yml -o docker-compose.yml
docker compose up -d --pull always
```

`down` 停止并移除容器，`./data` 和文件目录仍保留。不要让两种模式同时访问同一数据库。

## 配置在哪里改

**应用参数改 `.env`，镜像和端口改 Compose。** Compose 自动读取同目录 `.env` 以解析文件挂载路径、校验密码及 GC 目标；服务的 `env_file: .env` 将应用参数传给容器。`environment` 仅保留这些启动约束、容器内 `/data` 与 `8787`，以及分离模式的可信内部代理。参考 [Docker 环境文件说明](https://docs.docker.com/compose/how-tos/environment-variables/set-environment-variables/)。

| 要调整什么           | 修改位置                                       | 示例                                                                 |
| -------------------- | ---------------------------------------------- | -------------------------------------------------------------------- |
| 管理员账号           | `.env`                                         | `ADMIN_USERNAME`、`ADMIN_PASSWORD`（首次初始化使用；之后在后台修改） |
| 单容器镜像版本       | `docker-compose.yml` 的 `image`                | `ghcr.io/qwerto107/cloudpaste:版本号`                                |
| 多容器镜像版本       | 运行目录的 `docker-compose.yml` 中两处 `image` | 前后端填写相同版本                                                   |
| 监听地址和宿主机端口 | 对应 Compose 的 `ports`                        | 单容器 `127.0.0.1:8081:8787`，多容器前端 `127.0.0.1:8081:8080`       |
| 文件目录             | `.env`                                         | `LOCAL_FILES_DIR=./files`，后台仍填 `/files`                         |
| 上传与互传限制       | `.env`                                         | `MAX_*`、`TRANSFER_*`；可在后台调整的项目见下文                      |

修改后重新执行对应模式的启动命令；环境变量仅在创建或重建容器时更新，`restart` 不会加载新配置。密码含 `$` 或 `#` 时可用单引号包住值，例如 `ADMIN_PASSWORD='你的长随机密码'`。局域网访问时，把端口映射中的 `127.0.0.1` 改为服务器的局域网 IP；公网使用 HTTPS。

上面的完整示例也保存在 [环境配置示例](../backend/.env.example)。README 的快速部署只列必填密码；完整配置的各项作用如下。

### `.env` 参数说明

| 变量                              | 默认值    | 说明                                                                                                |
| --------------------------------- | --------- | --------------------------------------------------------------------------------------------------- |
| `ADMIN_USERNAME`                  | `admin`   | 首次创建管理员的用户名；已有账号不随此值更新                                                        |
| `ADMIN_PASSWORD`                  | 无，必填  | 首次创建管理员的密码，至少 6 个字符、最多 72 字节；已有密码在后台修改，Compose 仍要求此项非空       |
| `LOCAL_FILES_DIR`                 | `./files` | 宿主机文件目录；启动前创建，Linux 上授权 UID/GID `65532` 读写                                       |
| `LOCAL_FILES_READ_ONLY`           | `false`   | 设为 `true` 只读挂载文件目录；后台相应存储也应设置只读，不影响 `./data`                             |
| `GOMEMLIMIT`                      | `80MiB`   | Go GC 软目标；容器内存上限仍由 Compose 的 `mem_limit` 控制                                          |
| `MAX_DOWNLOADS`                   | `48`      | 普通文件下载总并发，范围 1–65535；设备互传的中继并发独立设置                                        |
| `MAX_UPLOADS`                     | `16`      | 普通文件上传并发，范围 1–65535                                                                      |
| `MAX_UPLOAD_SIZE_MB`              | `10240`   | 单文件上传硬上限，默认 10 GiB，范围 1–10240 MiB；后台上传大小不能高于此值                           |
| `MAX_STAGED_SIZE_MB`              | `20480`   | 上传暂存总额度，默认 20 GiB，范围 1–102400 MiB，不能小于单文件上传上限                              |
| `TRANSFER_ENABLED`                | `true`    | 设为 `false` 关闭设备互传                                                                           |
| `TRANSFER_MAX_FILE_MB`            | `10240`   | 互传文件硬上限，范围 1–10240 MiB；后台只能设置不高于此值的上限                                      |
| `TRANSFER_RATE_MB`                | `64`      | 初始中继总速率，范围 0–10240 MiB/s，0 表示不限速；后台保存的值优先                                  |
| `TRANSFER_STUN`                   | 空        | 可选 `stun:主机:端口`，用于跨网络直连；留空不联系第三方 STUN                                        |
| `PASSWORD_ATTEMPTS_PER_MINUTE`    | `10`      | 每 IP 的密码验证次数；管理入口和访客入口分别计数                                                    |
| `TRANSFER_CREATE_PER_MINUTE`      | `30`      | 每 IP 创建互传连接次数                                                                              |
| `TRANSFER_JOIN_PER_MINUTE`        | `30`      | 每 IP 输入配对码加入连接次数                                                                        |
| `TRANSFER_RELAY_PER_MINUTE`       | `180`     | 每 IP 普通中继上传、下载共享次数                                                                    |
| `TRANSFER_WS_CONNECT_PER_MINUTE`  | `30`      | 每 IP 的 WebSocket 连接尝试次数                                                                     |
| `TRANSFER_WS_MESSAGES_PER_MINUTE` | `240`     | 每个已认证成员发送 WebSocket 应用消息的次数                                                         |
| `TRANSFER_TOOL_PER_MINUTE`        | `240`     | 每 IP 的下载工具 HEAD、GET 和分段 GET 共享次数                                                      |
| `TRANSFER_FOLDER_WS_MESSAGES_PER_MINUTE` | `12000` | 每个已认证成员的已授权文件夹内部消息次数 |
| `TRANSFER_FOLDER_RELAY_PER_MINUTE` | `12000` | 有效文件夹票据的中继上传、下载共享每 IP 的次数 |
| `TRUSTED_PROXIES`                 | 空        | 单容器的可信代理 IP、CIDR 或主机名，多值逗号分隔；多容器由 Compose 固定为 `frontend`                |
| `ALLOWED_ORIGINS`                 | 空        | 额外允许的浏览器页面 Origin，例如 `https://share.example.com`；多个值逗号分隔，同源部署留空         |
| `ENCRYPTION_SECRET`               | 空        | 可选加密密钥，必须是 32 字节随机值的 Base64；留空生成 `./data/encryption.key`，备份时连同数据库保存 |

`DATA_DIR` 和 `PORT` 也是后端支持的变量，但容器配置固定为 `/data` 和 `8787`，在 `.env` 中填写其他值会被 Compose 覆盖。宿主机数据路径修改 `volumes`，对外访问端口修改 `ports`。容器项目名、镜像版本、内存硬限制和日志配置也直接在 `docker-compose.yml` 修改。

修改 `.env` 后重新执行 `docker compose up -d --pull always`。后台已保存的业务配置不会全部被环境文件重置：上传硬上限限制后台值，互传总速率优先使用后台保存值，中继并发在「系统设置 → 设备互传」配置。修改管理员环境变量不会覆盖已有账号。

加密密钥用于读取存储凭据和 API Key 等已加密数据。已有部署应继续使用原密钥；切换为不同的 `ENCRYPTION_SECRET` 或丢失 `encryption.key` 会导致原加密数据无法正常读取。

### 已有部署迁移

旧部署使用 `backend/.env`、`cloudpaste-go` 项目名及命名数据卷，旧 `docker-compose.yml` 为多容器配置。迁移前保留旧文件，先用**旧 Compose 文件和原项目名**停止旧容器，完整备份数据；将原环境文件复制到运行目录的 `.env`，把旧卷中数据库、WAL、`encryption.key` 和暂存文件一起迁移到 `./data`，授予 UID/GID `65532` 读写权限，再使用新版 Compose 启动。文件存储继续使用原路径。**只改变项目名和挂载配置不会自动搬迁数据**，确认新服务正常前保留旧卷。

旧环境文件中的 `GO_*_IMAGE`、`BIND_ADDRESS`、`HTTP_PORT` 不再控制新版 Compose；把原镜像版本和端口写到对应的 `image`、`ports` 中。

<details>
<summary><strong>进阶配置：端口、目录、资源限制、设备互传与反向代理</strong></summary>

单容器默认不信任转发头；如果前面还有 HTTPS 代理，使用 `TRUSTED_PROXIES` 配置 Go 进程实际看到的可信代理地址。单容器无需配置前端 Nginx 的 `set_real_ip_from`。

## 端口与目录

| 项目           | 默认值           | 说明                                       |
| -------------- | ---------------- | ------------------------------------------ |
| 宿主机入口     | `127.0.0.1:8081` | 在对应 Compose 的 `ports` 中修改           |
| 前端容器端口   | `8080`           | Nginx 提供页面和同源 API 转发              |
| 后端容器端口   | `8787`           | Compose 不单独公开宿主机端口               |
| 合并版容器端口 | `8787`           | 同时提供页面和 API，映射到宿主机 `8081`    |
| 数据目录       | `/data`          | 直接挂载运行目录下的 `./data`              |
| 本地文件目录   | `/files`         | 来自 `LOCAL_FILES_DIR`，宿主机目录必须存在 |
| Compose 项目名 | `cloudpaste`     | 两种部署模式使用相同名称                   |

`data` 和文件目录必须在启动前创建。Linux 上需授予 UID/GID `65532` 读写权限；现有数据库、WAL 及加密密钥也需可读写。出现 `permission denied` 时优先核对挂载路径与所有权，不要用全局可写权限替代排查。

`LOCAL_FILES_READ_ONLY=true` 会将文件目录只读挂载，管理界面也应将对应存储标记为只读。

## 资源配置

| 环境变量             | 默认值  | 含义                                            |
| -------------------- | ------- | ----------------------------------------------- |
| `MAX_UPLOAD_SIZE_MB` | `10240` | 单文件硬上限，单位 MiB，默认 10 GiB             |
| `MAX_STAGED_SIZE_MB` | `20480` | 上传暂存总额度，默认 20 GiB，不能小于单文件上限 |
| `MAX_UPLOADS`        | `16`    | 同时上传数量，范围 1–65535                      |
| `MAX_DOWNLOADS`      | `48`    | 同时下载数量，范围 1–65535                      |
| `GOMEMLIMIT`         | `80MiB` | Go GC 软目标，不是进程内存硬限制                |

分离版 Compose 另设后端 `128m`、前端 `64m` 容器内存限制，合并版为单容器 `128m`。系统设置的上传大小只能降低环境硬上限。按实际负载使用 `docker stats` 观察内存、CPU 和 I/O。

单个来源 IP 最多同时下载 2 个文件；当全局下载额度低于 4 时，单来源额度会进一步降低，为其他来源留出位置（全局额度为 1 时只能串行下载）。超出单来源额度返回 429，全局额度耗尽返回 503。下载每个 32 KiB 数据块的写入最多等待 30 秒，正常持续传输可超过 30 秒；停读连接超时后释放额度。使用反向代理时需按下文配置可信来源，避免多个用户共享代理 IP 的额度。

Windows 的 LOCAL 存储只接受与磁盘实际名称一致的路径，拒绝不同大小写、8.3 短名称以及尾点、尾空格等别名，防止绕过目录或单文件密码。配置挂载根内路径、API Key 的 `basic_path` 和目录元信息时，应使用文件实际的名称和大小写；Linux 与 S3 的路径规则保持不变。

## 设备互传与实时中继

设备互传支持保留目录结构的文件夹发送和接收。接收端需使用 HTTPS（localhost 例外）及支持 `showDirectoryPicker` 的浏览器，选择保存目录后并行流式写入；文件夹默认并发 32 个，可手动设置为 1–128 个，超过并发额度的条目排队，不受普通文件 32 个累计额度限制。单文件大小及本次连接总大小（单文件上限的四倍）仍有限制，同名目录不会覆盖。中继上传、下载成对启动，避免同一浏览器双标签页争抢共享连接；实际并行数量还受浏览器和代理限制。排队文件不会因为其他文件传输较久而提前失效，已接入文件仍有独立的停滞保护。发送过程中请勿修改源文件夹，取消后已保存的部分内容会保留。更多协议和兼容性说明见 [文件夹队列](api.md#文件夹队列)。

### 可选 IP 归属地数据库

管理员自行下载免费的 [IP2Location LITE DB3](https://lite.ip2location.com/database/db3-ip-country-region-city)，选择 **IPv4 BIN 格式**并解压。在“系统设置 → IP 归属地”独立选项卡选择文件，点击“加载数据库”后立即生效；已加载时按钮为“更新数据库”。这是全站共享的定位库，当前互传页面和互传日志共用这份数据。站点不自动下载或更新数据库，也不自动扫描加载本地文件。仅接受 DB3 IPv4 BIN，不接受 ZIP、CSV 或 IPv6 库；最大 512 MiB。

手动加载的文件保存在 `DATA_DIR`，使用随机文件名和私有权限，重启后恢复该选择。更新时再次选择新文件加载；验证失败保留当前有效数据库。点击“卸载数据库”会关闭定位、移除数据库文件并隐藏当前连接的归属地，已有日志不删除。容器继续使用现有数据卷，不需要额外挂载或设置数据库文件权限。

未加载数据库时，授权区、连接信息和新日志不显示归属地；IPv6 始终不显示位置。加载后，IPv4 可查询国家、省份，省份缺失时仅显示国家，未命中或读取失败时显示“未知”；内网、回环地址分别显示“局域网”“本机”。归属地描述网络出口，不代表设备实际位置，也不采用浏览器上报的点对点地址。

查询使用可信代理规则识别的来源 IP，完全在本地完成，不向外部服务发送地址。数据库按需读取，不整体载入应用内存；连接建立时查询，消息和文件事件复用结果，仅在管理员更换数据库时刷新当前连接。上传使用固定缓冲并限制同时更新数量，不阻塞互传消息转发。互传日志遵循总开关、模块开关及保留时间，LITE 结果界面保留来源署名。数据库不随源码、二进制或镜像分发。

CloudPaste uses the IP2Location LITE database for [IP geolocation](https://www.ip2location.com).

### 互传参数

单文件程序和合并容器已包含配对、WebSocket 消息与 HTTP 实时中继，全部沿用网站端口，无需开放其他服务端口或部署 TURN。直连由两端浏览器尝试建立；网络不允许直连时选择中继。自建防火墙或浏览器网络策略仍可能限制 WebRTC 的动态连接，中继可通过现有网站入口使用。

| 环境变量               | 默认值  | 含义                                                            |
| ---------------------- | ------- | --------------------------------------------------------------- |
| `TRANSFER_ENABLED`     | `true`  | 设为 `false` 关闭设备互传                                       |
| `TRANSFER_MAX_FILE_MB` | `10240` | 单个互传文件硬上限，1–10240 MiB；后台可设置更小的值             |
| `TRANSFER_RATE_MB`     | `64`    | 初始中继合计发送速率，0–10240 MiB/s；0 为不限速，后台保存值优先 |
| `TRANSFER_STUN`        | 空      | 可选 `stun:主机:端口`；默认不联系第三方 STUN                    |

管理后台的「系统设置 → 设备互传」可保存中继总速率、并发请求数（默认 64，非负整数，无固定上限，0 表示不限制总并发）、文件夹默认并发（默认 16，范围 1–128）和单文件大小。每个分段下载请求各占一路并发。文件夹接收前和传输过程中均可在“接收选项”中单独调整并发数，传输中点击“应用”生效；调低不会中断已安排的文件，只影响后续调度，不修改系统默认值。实际中继并发还受总额度约束，额度不足时排队。已有部署保留已保存的设置。速率保存后立即生效；降低并发只阻止新的超额传输，不中断已有文件；大小限制用于新建配对连接。设置保存到数据库，重启后恢复。中继总并发设置为 0 时不再限制全站请求数量，文件夹自身仍按 1–128 的并发数调度，下载工具每文件仍最多 4 路。背压、固定缓冲和限频策略继续生效。

「点对点调试日志」默认关闭，保存后新建或重新加入连接生效。开启后，在参与设备的浏览器控制台启用 Debug/Verbose 级别，筛选 `[CloudPaste P2P]`，查看 WebSocket、WebRTC/ICE、数据通道状态、双方候选地址和端口、实际选中链路、往返延迟及协商耗时。文件终态记录已确认字节、耗时和平均速率，直连同时记录链路累计字节；只取事件快照，不轮询或逐块打印。浏览器隐藏的地址可能显示为 mDNS 名称或空值。日志不输出凭证、配对码、用户内容或完整 SDP。

固定上限为 16 个连接房间、40 条 WebSocket（每 IP 最多 8 条）；同一创建者或 IP 最多创建 2 个房间。中继使用小块缓冲和背压，不将文件完整放入内存或服务器磁盘。浏览器、操作系统和代理仍有各自缓冲，内存不等于零；超额请求会失败，不无限排队。默认 64 MiB/s 是整个服务的中继发送总预算，与普通分享下载分开，也不限制 WebRTC 直连。

**局域网或公网的点对点文件下载需要 HTTPS**，证书须被接收设备的浏览器信任，且浏览器支持 `showSaveFilePicker`。普通 HTTP 局域网 IP 地址即使已建立 WebRTC 连接，也无法直接保存文件，页面只提供中继下载。本机 `localhost`、`127.0.0.1` 是安全上下文例外；配置 STUN 不会解除浏览器的保存限制。

HTTPS 反向代理需允许 WebSocket Upgrade，关闭 `/api/transfer/upload/` 的请求缓冲和 `/api/transfer/download/` 的响应缓冲，并允许所设单文件大小。仓库自带的前端 Nginx 已配置，外层代理也必须关闭缓冲；完整配置见 [Nginx HTTPS 反向代理示例](#nginx-https-反向代理示例)。

边收边转发依赖每一层代理都支持流式处理；CDN 的请求大小或持续时间限制仍然生效。服务自身在约 1 分钟无传输进展后清理文件，连接最长 2 小时。取消、断网或重启后没有服务端副本用于续传。

临时下载链接含下载票据，服务代码不记录票据。普通中继票据仅使用一次，下载工具入口的票据可在 30 分钟内探测、分段和重试，闲置 10 分钟失效，结束任务或房间结束后撤销。自建代理应避免在访问日志中记录此路由的查询串（例如使用 `$uri` 而不是 `$request_uri`）。下载工具分段占用现有中继并发额度，不开启新监听端口，不在服务器缓存文件。公网使用 HTTPS/WSS；若前后端异源，需明确配置 `ALLOWED_ORIGINS`，推荐仍使用同源代理。

## 每分钟限频配置

以上九个 `*_PER_MINUTE` 变量均可自定义，留空或未配置时使用默认值。只接受 1–1000000 的整数，`0` 不表示关闭限频；非法配置会阻止服务启动。容器模式在 `.env` 设置后重建容器，二进制模式设置进程环境后重启服务；后台没有这些次数的设置项，也不会用数据库配置覆盖环境变量。

除 WebSocket 应用消息按已认证成员计数外，其他限频按真实来源 IP 计数。普通中继的上传和下载共用每 IP 每分钟 180 次，下载工具的 HEAD、GET、分段 GET 共用另一份每 IP 每分钟 240 次。文件夹内部消息独立使用每成员每分钟 12000 条，有效文件夹票据的中继上传和下载共享每 IP 每分钟 12000 次；无效票据、重放和未知编号仍走普通限制。文件完成即按空闲并发槽补位，超过首批 32 个文件后也不会人为按秒串行等待。密码验证成功和失败均计数，管理与访客入口分别计算。没有跨 IP 的每分钟总次数，多个用户不会消耗彼此的限频额度。

计数为内存中的一分钟固定窗口，从各计数项首次请求起算；进程重启清空，多实例各自计数。HTTP 超限返回 429，WebSocket 应用消息超限断开连接。完整计数规则见 [API 限频策略](api.md#限频策略)。

全站并发、在线房间容量、单文件累计请求和总带宽保护仍然保留，防止同时请求耗尽资源；它们与每分钟限频分开计算。放宽次数不会提高并发容量或带宽。

## HTTPS 与管理员入口

前端、后端可独立部署。推荐浏览器统一访问站点域名，由反向代理将 `/api` 转发到后端；静态文件请求交给前端。默认 Compose 已配置同源转发。

### Nginx HTTPS 反向代理示例

以下适用于**宿主机 Nginx → 默认 Compose 入口 `127.0.0.1:8081`**，单容器和多容器均可使用。完整二进制默认监听 `8787`，需相应修改 `proxy_pass`；若 Nginx 运行在独立容器中，应使用其能够访问的服务地址。

替换域名、证书路径和上游端口，将配置放入 `http {}` 内包含的站点文件，例如 `/etc/nginx/conf.d/cloudpaste.conf`。`map` 和 `log_format` 放在 `server` 外，不要额外套一层 `http {}`。

```nginx
# 普通请求不携带 Upgrade，WebSocket 保留升级头。
map $http_upgrade $cloudpaste_connection {
    default upgrade;
    ''      '';
}

# 访问日志只记录路径，避免下载票据等查询参数落入日志。
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
        # 默认 Compose 入口；完整二进制默认使用 127.0.0.1:8787。
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto $scheme;
        # 直接面向客户端的入口覆盖转发头，防止客户端伪造来源。
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $cloudpaste_connection;

        # 边接收边转发，避免中继先等待整个文件上传完毕才开始下载。
        proxy_request_buffering off;
        proxy_buffering off;
        proxy_cache off;

        proxy_connect_timeout 10s;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

保存后检查配置，通过后重载：

```sh
sudo nginx -t && sudo nginx -s reload
```

**中继下载长时间显示 `0 B / 文件大小 · 0 B/s`，随后突然开始传输时，优先检查外层 `proxy_request_buffering` 是否仍为 `on`。** 开启时 Nginx 会先接收完整请求体，再发送给后端；关闭后才会边收边转发。[Nginx 请求缓冲说明](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_request_buffering)

已有反代面板或多个 `location` 时，将关闭缓冲和 WebSocket 配置合并到实际匹配请求的配置中；新增的 `location /` 不会覆盖更具体的 `/api/` 规则。每一层代理都需支持流式转发。点对点文件数据不经过中继，因此点对点正常也不能排除中继代理配置问题。

真实 IP 仍需按[可信代理设置](#真实客户端地址与可信代理)配置：二进制同机代理可信任 `127.0.0.1,::1`；单容器填写 Go 实际看到的代理地址，可能是 Docker 网关地址；多容器按下文配置内层 Nginx。若入口前还有 CDN 或其他代理，先通过明确的可信来源规则恢复真实 IP，再转发，不能直接信任客户端提交的头。

### 管理员入口保护

管理员 API 是 `/api/admin` 及全部子路径，包含登录、文件读取与上传。可在外部反向代理为这个完整前缀配置 IP 白名单或 mTLS，同时保持账号认证。确保内部服务端口不能绕过代理直接访问。

从普通页面进入管理员区域会执行整页导航，服务器会收到 `/admin` 或 `/admin/*` 的页面请求。若还需要限制管理员页面的访问，可对这两个页面入口配置相同的代理访问规则；管理员 API 的保护仍需单独覆盖 `/api/admin` 及全部子路径。

若使用 PROXY protocol，IP 白名单应依据受信任代理提供的真实客户端地址；仅接受可信上游的 PROXY protocol 数据。普通用户 API 为 `/api/*`，公开分享不应被管理员白名单整体拦截。页面路由与 API 路由见 [API 参考](api.md)。

### 真实客户端地址与可信代理

密码尝试按真实来源分别限流，访客与管理入口独立计数，没有跨 IP 的每分钟总额度。后端默认忽略所有转发头。二进制部署可设置 `TRUSTED_PROXIES`，填写逗号分隔的明确代理 IP、CIDR 或可信 DNS 管理的代理主机名；例如本机代理可使用 `127.0.0.1,::1`。不要填写包含普通客户端的宽泛网段，后端拒绝 `/0` 和 `*`。解析主机名最多等待一秒，地址缓存 30 秒；DNS 失败时不继续使用已过期的信任地址。

多容器 Compose 只信任内部服务名 `frontend`，通过 Docker DNS 识别前端容器，无需固定 Docker 子网。前端 Nginx 用自己的 `$remote_addr` **覆盖** `X-Forwarded-For`，客户端自行发送该头不能伪造限流身份。后端仅接受可信 TCP 对端提供的转发链，并从右向左取首个不受信的地址。容器网络与配置中的 DNS 服务均属于信任边界，不应接入不受信任的容器。

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

</details>

## 升级与备份

运行目录的 `./data` 包含 `cloudpaste-go.db`、SQLite WAL 文件、`encryption.key` 和上传暂存数据，容器内对应 `/data`。备份前停止写入，保存完整 `./data` 与需要保留的文件存储；数据库和加密密钥应一起保存。

升级前先备份。修改运行目录 `docker-compose.yml` 的镜像标签：单容器修改一处 `image`，多容器修改前后端两处并使用相同版本。两种方式都执行：

```sh
docker compose up -d --pull always
```

继续使用同一运行目录和数据、文件路径。升级不需要重新初始化数据库；停止或删除容器不会删除宿主机的 `./data`。分享过期不清除原文件；删除分享时默认同时删除原文件，可选择仅删除记录。原部署若配置了较小上传上限，需同步调整 `MAX_UPLOAD_SIZE_MB`、`MAX_STAGED_SIZE_MB` 与后台上传设置，才能使用 10 GiB 上限。更新镜像标签时无需重新下载 Compose，以免覆盖自定义端口或挂载路径。

## 常见问题

- **查看运行状态或日志**：两种方式都执行 `docker compose ps` 或 `docker compose logs --tail=100`。
- **服务器启动了，但自己的电脑打不开 127.0.0.1**：该地址只指向当前电脑。先用上面的 SSH 转发，或配置域名与 HTTPS 反向代理。
- **配置改了没生效**：重新执行 `up -d`，不要只执行 `restart`。自定义环境文件时，同时修改 Compose 中的 `env_file` 路径及命令中的 `--env-file` 路径。
- **登录报 CORS**：确认浏览器的 API 地址与页面同源。不要混用 `localhost` 和 `127.0.0.1`；开发配置见 [开发指南](development.md)。
- **后端日志已启动但无法连接宿主机 8787**：默认没有公开后端端口，通过前端入口访问 `/api/health`。
- **文件无法上传**：核对存储公开标记、API Key 权限、存储 ACL、路径范围、目录写权限与上传额度。
- **界面没有更新**：确认已拉取目标版本并重新创建容器，然后刷新浏览器；多容器还需确认前后端镜像版本一致。

需要修改代码并构建镜像时，见 [开发指南](development.md#从源码构建-docker-镜像)。
