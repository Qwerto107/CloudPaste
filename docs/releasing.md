# 构建与发布

[文档中心](README.md) · [更新记录](changelog.md)

工作流为 [.github/workflows/backend.yml](../.github/workflows/backend.yml)，Actions 名称为 **构建、检查与发布**，任务和步骤名称统一使用中文。

## 发布触发与检查

工作流只在推送以 `v` 开头的标签时启动，并在打包前校验版本格式。普通分支推送、PR 和手动运行都不会触发此工作流。

| 触发 | 检查 | 二进制打包 | 镜像与版本 |
| --- | --- | --- | --- |
| `vX.Y.Z` 标签 | 单元测试、前端构建、静态检查、内嵌页面安全测试、漏洞扫描 | 全部平台 | 版本与提交号镜像、`latest`、正式 Release |
| `vX.Y.Z-rc.1` 等标签 | 与正式版本相同 | 全部平台 | 版本与提交号镜像、预发行 Release |

前端任务保存已验证的静态资源，全部平台的二进制构建复用同一份产物。镜像发布要求后端、前端及全部二进制构建成功。三个镜像发布成功、附件齐全且校验通过后才公开 Release。额外的竞态、浏览器和 Docker 检查可在本地运行，详见[开发指南](development.md)。

## 镜像名称与架构

| 镜像 | 用途 | Linux 架构 |
| --- | --- | --- |
| `ghcr.io/qwerto107/cloudpaste` | 单容器，内置前端与后端 | amd64、arm64、arm/v6、arm/v5、loong64 |
| `ghcr.io/qwerto107/cloudpaste-frontend` | 分离部署的 Nginx 前端 | amd64、arm64 |
| `ghcr.io/qwerto107/cloudpaste-backend` | 分离部署的独立后端 | amd64、arm64 |

单容器镜像使用纯 Go 程序和 `scratch`，从构建主机上的基础镜像取得证书、时区和非 root 用户信息，无需目标架构的系统运行库。ARM 构建明确设置 `GOARM=5` 或 `GOARM=6`。各编译阶段使用构建主机架构，运行层没有需要模拟执行的构建步骤，因此不需要安装架构模拟器。

macOS 和 Windows 只提供完整程序二进制；Apple Silicon 运行 Docker 时使用 Linux arm64 镜像。预发行版本不更新 `latest`，失败的构建标签不表示已发布镜像或 Release。

发布使用仓库提供的 `GITHUB_TOKEN`：镜像任务申请 `packages: write`，Release 任务申请 `contents: write`。GHCR 包需允许当前仓库写入，公开拉取要求包可见性为公开。单容器镜像改名后需检查新的 `cloudpaste` 包权限，并将部署配置中的旧 `cloudpaste-standalone` 镜像名称替换为 `cloudpaste`。

## 二进制与附件

完整程序支持 Linux amd64、arm64、armv6、armv5、loong64，Windows amd64，以及 macOS arm64；独立后端只提供 Linux amd64、arm64。

- 完整程序：`cloudpaste-{系统}-{架构}`，压缩包内为 `cloudpaste`，Windows 为 `cloudpaste.exe`。
- 独立后端：`cloudpaste-backend-linux-{amd64,arm64}`，压缩包内为 `cloudpaste-backend`。
- 分离前端使用 `cloudpaste-frontend` 镜像或静态资源，不额外提供前端可执行程序。

Actions 附件名为 `cloudpaste-binaries-{系统}-{架构}`，保留 30 天。Linux amd64、arm64 附件同时包含完整程序和独立后端，其他平台只包含完整程序。

每次 Release 提供九份压缩包（八份 `.tar.gz`、一份 `.zip`）及九份 `.sha256` 文件。压缩包包含可执行文件、许可证和 [二进制启动说明](binary.md)。Windows 使用 ZIP，其余平台使用 tar.gz。

Release 先建立草稿，全部附件上传成功并校验服务器上的实际文件后公开。重跑只补充缺失附件，已有附件的校验文件必须与本次构建一致。

## 维护步骤

1. 检查测试、版本号、镜像示例及更新记录。
2. 提交并推送代码，创建并推送符合格式的新版本标签。
3. 等待发布流水线完成，失败时依据日志修复。
4. 核对镜像架构、版本与 `latest`，检查全部二进制附件校验值，再更新部署。
