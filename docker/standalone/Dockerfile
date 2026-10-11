# 前端与目标 CPU 架构无关，只在构建主机上生成一次静态资源。
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json frontend/.npmrc ./
RUN npm ci --cache /tmp/npm-cache --no-audit --no-fund
COPY frontend/ ./
ENV VITE_BACKEND_URL="" VITE_IS_DOCKER=true
RUN npm run test:go-upload && npm run test:security && npm run build
WORKDIR /src
COPY scripts/ ./scripts/
RUN node --test scripts/prepare-webui.test.mjs && node scripts/prepare-webui.mjs

# 使用构建主机编译器交叉编译，前后端最终嵌入同一个纯 Go 可执行文件。
FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS build
WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend /src/backend/internal/webui/dist ./internal/webui/dist
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -tags with_frontend -trimpath -ldflags="-s -w" -o /cloudpaste ./cmd/cloudpaste \
    && mkdir /runtime-data

# 无需 Nginx 或 Node 运行时；沿用现有数据目录、非 root 用户与健康检查。
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /cloudpaste /cloudpaste
COPY --chown=65532:65532 --from=build /runtime-data /data
USER 65532:65532
ENV DATA_DIR=/data PORT=8787
EXPOSE 8787
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/cloudpaste", "healthcheck"]
ENTRYPOINT ["/cloudpaste"]
