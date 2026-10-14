<div align="center">

<img src="frontend/public/chain-cloud.svg" width="76" height="76" alt="CloudPaste">

# CloudPaste

**轻盈分享，让文字与文件即刻抵达。**

一个可自托管的临时文本与文件分享站。为链接设置有效期、访问密码与查看次数，让每一次分享都恰到好处。

[快速部署](#快速部署) · [文档中心](docs/README.md) · [API 参考](docs/api.md) · [下载版本](https://github.com/Qwerto107/CloudPaste/releases) · [English](docs/en.md)

**Go + Vue**　 / 　**LOCAL · S3**　 / 　**Docker · amd64 · arm64**

</div>

---

## 界面一览

清爽薄荷配色，支持 Markdown 编辑、多文件合集分享与设备互传。浅色、暗色自由切换，手机与桌面都能顺手使用。

![CloudPaste 产品效果图：Markdown 编辑、移动端多文件合集与设备互传](docs/assets/product-showcase.png)

*产品界面效果图，展示 Markdown 编辑、移动端多文件合集与设备互传。实际界面见下方截图。*

<details>
<summary><strong>查看完整截图：编辑、上传、互传、管理与设置</strong></summary>

![文本分享：浅色 Markdown 编辑器与访问控制](docs/assets/share-light.png)

<table>
  <tr>
    <th width="70%">暗色模式</th>
    <th width="30%">移动端多文件合集</th>
  </tr>
  <tr>
    <td valign="top"><img src="docs/assets/share-dark.png" alt="暗色模式下的 Markdown 编辑与分享"></td>
    <td valign="top"><img src="docs/assets/upload-mobile.png" alt="移动端上传多个文件，共用一个合集链接"></td>
  </tr>
</table>

![设备互传：连接后双向发送文字、图片与文件](docs/assets/transfer.png)

![分享管理：有效状态与统一操作](docs/assets/management.png)

![系统设置：分类配置与清晰说明](docs/assets/settings.png)

*以上截图使用演示内容。*

</details>

## 为分享而设计

| 能力 | 使用体验 |
| --- | --- |
| **文本分享** | 粘贴文字或 Markdown，编辑与预览随时切换 |
| **文件分享** | 拖拽上传、批量选择、逐个命名，失败后仅重试剩余文件 |
| **设备互传** | 配对码连接两台设备，双向发送文字、实时中继文件，也可按浏览器能力直连保存 |
| **访问控制** | 自定义链接、访问密码、最多查看次数，以及分钟到天的有效期 |
| **文件预览** | 文本、图片、视频和音频在线预览，其他格式直接下载 |
| **存储管理** | 本地目录与 S3，支持连接测试、默认存储及访问范围配置 |
| **密钥与权限** | 自动生成或自定义 API Key，分别授权文本、文件分享与管理 |
| **管理与设置** | 管理分享、管理员账号、上传限制、预览规则、站点外观及公告 |

新分享默认 **24 小时**后到期，也可选择长期有效。过期只停止公开访问，创建者与管理员仍可查看。删除文件分享默认同时删除存储中的原文件，也可选择仅删除记录。单文件上传默认最大 **10 GiB**。

## 快速部署

推荐使用**单容器镜像**，一个容器提供界面和 API。安装 Docker 与 Compose 后，创建运行目录并下载 Compose 文件即可：

```sh
mkdir -p cloudpaste/data cloudpaste/files
cd cloudpaste
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.yml -o docker-compose.yml
```

在 `cloudpaste` 目录新建 `.env`，填写自己的管理员密码（至少 12 字节）：

```dotenv
ADMIN_PASSWORD='请替换为自己的长随机密码'
```

Linux 上，新建的专用目录执行 `sudo chown 65532:65532 data files`。然后拉取镜像并启动：

```sh
docker compose up -d --pull always
```

1. 打开 `http://127.0.0.1:8081/admin/login`，使用 `admin` 和刚设置的密码登录。
2. 添加本地存储（根目录填 `/files`）或 S3 存储。
3. 创建文本或上传文件，生成分享链接。需要多人使用时，创建 API Key 并按需授权。

需要**前后端分离的多容器部署**时，将对应配置也保存为 `docker-compose.yml`。如果单容器已在运行，先执行 `docker compose down`，再替换配置：

```sh
curl -fL https://raw.githubusercontent.com/Qwerto107/CloudPaste/main/docker-compose.split.yml -o docker-compose.yml
docker compose up -d --pull always
```

单容器镜像 `cloudpaste` 支持 Linux amd64、arm64、armv6、armv5、loong64；多容器的 `cloudpaste-frontend`、`cloudpaste-backend` 仅支持 Linux amd64、arm64。两种方式入口相同，已有运行中的容器需先停止当前模式再切换。默认仅本机可访问；服务器部署、模式切换与升级见 [部署指南](docs/deployment.md)。不使用 Docker 时，可下载内置完整界面的 [Linux、Windows 或 macOS 单文件程序](docs/binary.md)。

## 轻量，也有边界

- **本地资源**：前端为纯静态页面，脚本、样式、图标由本站提供；Markdown 按需加载。
- **流式上传**：后端暂存并校验文件，再写入本地或 S3；不将完整文件缓存在内存中。
- **实时互传**：输入配对码互发文字、图片和文件，支持 WebRTC 点对点与同端口实时中继；中继可直接下载或交给外部下载工具，不暂存文件。双方需保持在线，详见 [设备互传](docs/usage.md#设备互传)。
- **身份分离**：API Key 用户从 `/login` 登录，管理员从 `/admin/login` 登录；管理页面与 API 按身份划分。
- **可控访问**：管理员 API 使用 `/api/admin/*`，可在外部反向代理配置 IP 白名单或 mTLS。
- **内容过滤**：Markdown 默认不加载站外图片，自定义 HTML 经过安全过滤。

更多说明见 [使用指南](docs/usage.md) 和 [API 参考](docs/api.md)。

## 文档导航

| 想了解什么 | 从这里开始 |
| --- | --- |
| 创建分享、密钥权限与系统设置 | [使用指南](docs/usage.md) |
| Docker、反向代理、数据与升级 | [部署指南](docs/deployment.md) |
| 单文件运行前后端、独立后端 | [二进制部署](docs/binary.md) |
| 访客、API Key 和管理员接口 | [API 参考](docs/api.md) |
| 本地运行、测试与图标维护 | [开发指南](docs/development.md) |
| 多架构构建与 GHCR 发布 | [发布流程](docs/releasing.md) |
| 版本变化 | [更新记录](docs/changelog.md) |

## 免责声明

本项目是开源的文本与文件分享工具，旨在支持合法、合规的信息存储与分享。**严禁利用本项目从事违法违规、侵犯他人合法权益或危害网络安全的活动。**

部署者和使用者应遵守适用的法律法规，并对上传、存储、传播的内容及相关使用行为负责。请勿上传或传播违法信息、侵权内容及未经授权的个人信息，并妥善配置访问权限与数据保护措施。

本项目按现状提供。开源代码的提供不代表项目维护者认可或授权任何违法违规用途，亦不免除相关责任主体依法应承担的责任。

---

<div align="center">

基于 Go、SQLite 与 Vue 构建 · [Apache License 2.0](LICENSE)

</div>
