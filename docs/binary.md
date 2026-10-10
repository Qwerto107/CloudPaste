# CloudPaste 二进制部署

CloudPaste 后端提供 HTTP API，使用 SQLite 保存数据，文件可存储在本地目录或 S3。

## 下载与启动

从 https://github.com/Qwerto107/CloudPaste/releases 下载对应架构的压缩包及 SHA-256 校验文件。Linux x86-64 使用 amd64，Linux ARM64 使用 arm64。

```sh
sha256sum -c cloudpaste-go-linux-amd64.tar.gz.sha256
tar -xzf cloudpaste-go-linux-amd64.tar.gz
cd cloudpaste-go-linux-amd64
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD='请替换为至少12字节的强密码'
export DATA_DIR=./data
export PORT=8787
./cloudpaste
```

以上命令以前台方式运行；长期运行时通过系统服务管理进程，保护环境配置文件的读取权限。后端监听指定端口，公网部署应通过 HTTPS 反向代理，并限制对内部端口的直接访问。

## 数据与文件

运行账号必须能够读写 DATA_DIR。该目录包含 cloudpaste-go.db、SQLite WAL、encryption.key 和上传暂存文件。持久保存整个目录；加密密钥用于读取已保存的存储凭据。

本地存储配置使用后端进程可访问的真实目录。S3 存储需要配置 Bucket、Region、Endpoint 和凭据。删除或过期分享仅停止访问，不删除存储中的原文件。

## 前端

二进制不包含前端静态资源。可单独部署 frontend/dist，或使用配套前端镜像。浏览器通过同源 /api 访问后端。

管理员页面入口为 /admin/login，API Key 登录入口为 /login。管理员 API 前缀 /api/admin 可在外部反向代理配置 IP 白名单或 mTLS。

完整部署、配置和 API 文档：https://github.com/Qwerto107/CloudPaste/tree/main/docs
