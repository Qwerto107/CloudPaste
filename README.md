<div align="center">

<img src="frontend/public/chain-cloud.svg" width="76" height="76" alt="CloudPaste 链云">

# CloudPaste

**轻盈分享，让文字与文件即刻抵达。**

一个可自托管的临时文本与文件分享站。为链接设置有效期、访问密码与查看次数，让每一次分享都恰到好处。

[快速部署](#快速部署) · [文档中心](docs/README.md) · [API 参考](docs/api.md) · [下载版本](https://github.com/Qwerto107/CloudPaste/releases) · [English](docs/en.md)

**Go + Vue**　 / 　**LOCAL · S3**　 / 　**Docker · amd64 · arm64**

</div>

---

## 界面一览

清爽薄荷配色，专注编辑与分享。浅色、暗色自由切换，手机与桌面都能顺手使用。

![CloudPaste 清爽薄荷产品效果图：桌面分享、管理列表与手机界面](docs/assets/product-showcase.png)

*产品界面效果图，展示桌面分享、管理列表与移动端的整体设计。*

<details>
<summary><strong>查看实际界面截图：浅色、暗色、移动端与系统设置</strong></summary>

![CloudPaste 文本分享：浅色界面](docs/assets/share-light.png)

<table>
  <tr>
    <th width="70%">暗色模式</th>
    <th width="30%">移动端文件分享</th>
  </tr>
  <tr>
    <td valign="top"><img src="docs/assets/share-dark.png" alt="暗色文本分享界面"></td>
    <td valign="top"><img src="docs/assets/upload-mobile.png" alt="移动端文件分享界面"></td>
  </tr>
</table>

![系统设置：分组配置与清晰说明](docs/assets/settings.png)

*以上截图使用演示内容。*

</details>

## 为分享而设计

| 能力 | 使用体验 |
| --- | --- |
| **文本分享** | 粘贴文字或 Markdown，编辑与预览随时切换 |
| **文件分享** | 拖拽上传、批量选择、逐个命名，失败后仅重试剩余文件 |
| **访问控制** | 自定义链接、访问密码、最多查看次数，以及分钟到天的有效期 |
| **文件预览** | 文本、图片、视频和音频在线预览，其他格式直接下载 |
| **存储管理** | 本地目录与 S3，支持连接测试、默认存储及访问范围配置 |
| **密钥与权限** | 自动生成或自定义 API Key，分别授权文本、文件分享与管理 |
| **管理与设置** | 管理分享、管理员账号、上传限制、预览规则、站点外观及公告 |

新分享默认 **24 小时**后到期，也可选择长期有效。过期或删除分享记录会停止链接访问，**不会删除存储中的原文件**。

## 快速部署

准备 Docker Compose。在仓库根目录执行：

```sh
cp backend/.env.example backend/.env
mkdir -p files
```

编辑 `backend/.env`，设置自己的强密码和前后端镜像：

```dotenv
ADMIN_USERNAME=admin
ADMIN_PASSWORD=请替换为至少12字节的强密码
GO_BACKEND_IMAGE=ghcr.io/qwerto107/cloudpaste-backend:0.5.0
GO_FRONTEND_IMAGE=ghcr.io/qwerto107/cloudpaste-frontend:0.5.0
LOCAL_FILES_DIR=./files
```

Linux 上需确保 UID/GID `65532` 能读写文件目录；新建的专用目录可执行 `sudo chown 65532:65532 files`。然后启动：

```sh
docker compose --env-file backend/.env pull
docker compose --env-file backend/.env up -d --no-build
```

1. 打开 `http://127.0.0.1:8081/admin/login`，使用配置的管理员账号登录。
2. 添加本地存储（根目录填 `/files`）或 S3 存储。
3. 创建文本或上传文件，生成分享链接。需要多人使用时，创建 API Key 并按需授权。

也可用 **一个容器运行前后端**。在 `backend/.env` 增加 `GO_STANDALONE_IMAGE=ghcr.io/qwerto107/cloudpaste-standalone:0.5.0`，使用以下命令启动（入口仍为 `http://127.0.0.1:8081`）：

```sh
docker compose --env-file backend/.env -f docker-compose.standalone.yml pull
docker compose --env-file backend/.env -f docker-compose.standalone.yml up -d --no-build
```

两种方式共用数据卷；切换前先停止原服务。公网部署、目录权限、资源限制与升级步骤见 [部署指南](docs/deployment.md)。不使用 Docker 时，可下载内置完整界面的 [Linux 单文件程序](docs/binary.md)。

## 轻量，也有边界

- **本地资源**：前端为纯静态页面，脚本、样式、图标由本站提供；Markdown 按需加载。
- **流式上传**：后端暂存并校验文件，再写入本地或 S3；不将完整文件缓存在内存中。
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
