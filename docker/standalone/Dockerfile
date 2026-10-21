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
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend /src/backend/internal/webui/dist ./internal/webui/dist
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
# ARMv5、ARMv6 必须明确指令集，其他架构不设置 GOARM。
RUN case "${TARGETARCH}/${TARGETVARIANT}" in \
      arm/v5) export GOARM=5 ;; \
      arm/v6) export GOARM=6 ;; \
      arm/*) echo "不支持的 ARM 变体: ${TARGETVARIANT}" >&2; exit 1 ;; \
    esac \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -tags with_frontend -trimpath -ldflags="-s -w" -o /cloudpaste ./cmd/cloudpaste \
    && mkdir /runtime-data /runtime-tmp \
    && chmod 1777 /runtime-tmp

# 只取构建主机镜像中的架构无关文件，避免目标架构受基础镜像限制。
FROM --platform=$BUILDPLATFORM gcr.io/distroless/static-debian13:nonroot AS runtime-files

# 纯 Go 程序无需系统运行库，scratch 可覆盖全部五种 Linux 架构。
FROM scratch
COPY --from=runtime-files /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=runtime-files /etc/passwd /etc/passwd
COPY --from=runtime-files /etc/group /etc/group
COPY --from=runtime-files /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /cloudpaste /cloudpaste
COPY --chown=65532:65532 --from=build /runtime-data /data
COPY --chmod=1777 --from=build /runtime-tmp /tmp
USER 65532:65532
ENV DATA_DIR=/data PORT=8787
EXPOSE 8787
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/cloudpaste", "healthcheck"]
ENTRYPOINT ["/cloudpaste"]
