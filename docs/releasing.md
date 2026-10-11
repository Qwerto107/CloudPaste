# 构建与发布

[文档中心](README.md) · [更新记录](changelog.md)

工作流为 [.github/workflows/backend.yml](../.github/workflows/backend.yml)，Actions 名称 **Go backend**。

## 触发方式

| 触发 | 检查和二进制 | GHCR 镜像 | Release |
| --- | --- | --- | --- |
| 相关分支提交 / PR | 执行 | 不发布 | 不创建 |
| 手动运行 | 执行 | 仅勾选 `publish_images` 时发布提交号标签 | 不创建 |
| `vX.Y.Z` 标签 | 执行 | 版本、提交号和 `latest` | 正式版本 |
| `vX.Y.Z-rc.1` 等标签 | 执行 | 版本和提交号 | 预发行版本 |

分离版前后端镜像及合并版镜像均支持 `linux/amd64`、`linux/arm64`：

```text
ghcr.io/qwerto107/cloudpaste-backend
ghcr.io/qwerto107/cloudpaste-frontend
ghcr.io/qwerto107/cloudpaste-standalone
```

预发行版本不更新 `latest`。失败的构建标签不表示已发布镜像或 Release。

## 发布检查

镜像发布前必须通过 Go 竞态、静态和依赖漏洞检查，两种模式的双架构二进制编译、真实 Docker 上传与分享检查，以及分离代理和合并直连两种模式的浏览器回归。前端任务保存已验证的静态资源，二进制构建复用同一份产物；合并镜像通过多阶段构建内嵌前端。

发布使用仓库提供的 `GITHUB_TOKEN`。镜像任务申请 `packages: write`，Release 任务申请 `contents: write`。GHCR 包需允许当前仓库写入；公开拉取要求包可见性为公开。

## 二进制与附件

Actions 二进制附件保留 30 天，分别为 `cloudpaste-linux-amd64` 与 `cloudpaste-linux-arm64`，每份包含仅后端和合并运行两种构建。Release 提供四份 `.tar.gz` 和四份 `.tar.gz.sha256`：

- `cloudpaste-go-linux-{amd64,arm64}`：仅后端，保持现有部署方式。
- `cloudpaste-standalone-linux-{amd64,arm64}`：包含前端静态资源，单个程序提供页面和 API。

压缩包包含可执行文件、许可证及独立可读的 [二进制启动说明](binary.md)。工作流从 `docs/binary.md` 复制说明到压缩包的 `README.md`。

Release 先建立草稿，上传附件并校验服务器上的实际文件后公开。重跑补充缺失附件。发布后核对三套镜像架构、版本与 `latest` 摘要，以及附件校验值。

## 维护步骤

1. 检查测试、版本号、镜像示例及更新记录。
2. 提交并推送代码后创建符合格式的新版本标签。
3. 等待发布流水线完成，失败时依据日志修复。
4. 验证镜像和附件，补充发布说明，再更新部署。
