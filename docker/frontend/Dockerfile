# 静态资源与目标 CPU 架构无关，使用构建主机架构执行 npm 和前端测试。
FROM --platform=$BUILDPLATFORM node:22-alpine AS build
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json frontend/.npmrc ./
RUN npm ci --cache /tmp/npm-cache --no-audit --no-fund
COPY frontend/ ./
ENV VITE_BACKEND_URL="" VITE_IS_DOCKER=true
# 镜像构建同时验证上传身份与公开内容的脚本注入防护。
RUN npm run test:go-upload && npm run test:security && npm run build

# 前端也以非 root 用户运行，并通过同源 /api 与 Go 后端通信。
FROM nginxinc/nginx-unprivileged:stable-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY docker/frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY docker/frontend/config.js /usr/share/nginx/html/config.js
EXPOSE 8080
ENTRYPOINT ["nginx", "-g", "daemon off;"]
