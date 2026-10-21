# 使用纯 Go SQLite，构建和运行都不依赖 CGO。
# 使用构建主机架构运行编译器，再交叉编译目标架构，避免模拟器拖慢 Go 构建。
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /cloudpaste ./cmd/cloudpaste \
    && mkdir /runtime-data

# 非 root 运行，运行镜像只包含二进制、证书和必要的基础文件。
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /cloudpaste /cloudpaste
COPY --chown=65532:65532 --from=build /runtime-data /data
USER 65532:65532
ENV DATA_DIR=/data PORT=8787
EXPOSE 8787
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/cloudpaste", "healthcheck"]
ENTRYPOINT ["/cloudpaste"]
