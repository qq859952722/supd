# supd Dockerfile — 多阶段 CI 构建版
# 两个平台（amd64 / arm64）均集成 tjs 运行时
# tjs 二进制由 CI 原生编译后注入 build context，路径：./tjs
# supd 自带 PID 1 能力（PR_SET_CHILD_SUBREAPER + SIGCHLD 回收），无需 tini/dumb-init

# ---------- Stage 1: 前端构建 ----------
FROM node:20-alpine AS web-builder
WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
# 用 npx 临时调用 pnpm@10（避免 corepack/npm 全局安装的兼容性问题）
RUN npx -y pnpm@10 install --frozen-lockfile
COPY web/ ./
RUN npx -y pnpm@10 build

# ---------- Stage 2: 后端构建 ----------
FROM golang:1.25-alpine AS go-builder
ARG VERSION=dev
ARG BUILD_TIME=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -tags '!dev' \
    -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o /supd ./cmd/supd/

# ---------- Stage 3: 最终运行时（amd64 / arm64 统一，均含 tjs）----------
FROM alpine:3.20 AS runtime
LABEL org.opencontainers.image.title="supd" \
      org.opencontainers.image.description="Lightweight process supervisor for home NAS" \
      org.opencontainers.image.source="https://github.com/qq859952722/supd" \
      org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates tzdata bash curl \
    && addgroup -S supd && adduser -S -G supd -h /etc/supd supd

COPY --from=go-builder /supd /usr/local/bin/supd

# tjs 二进制：由 CI 在对应平台原生编译后复制到 build context 根目录（./tjs）
COPY tjs /usr/local/bin/tjs-bin

# tjs 包装脚本：兼容 `tjs script.js` 和 `tjs run script.js` 两种调用方式
RUN printf '#!/bin/sh\ncase "$1" in\n  run|eval|test|serve|bundle|compile|app|-v|--version|-h|--help)\n    exec /usr/local/bin/tjs-bin "$@" ;;\n  *)\n    exec /usr/local/bin/tjs-bin run "$@" ;;\nesac\n' \
      > /usr/local/bin/tjs \
    && chmod +x /usr/local/bin/tjs /usr/local/bin/tjs-bin /usr/local/bin/supd \
    && mkdir -p /etc/supd/runtimes /var/log/supd \
    && ln -s /usr/local/bin/tjs /etc/supd/runtimes/tjs \
    && chown -R supd:supd /etc/supd /var/log/supd

WORKDIR /etc/supd
USER supd
EXPOSE 8080
VOLUME ["/etc/supd", "/var/log/supd"]
ENTRYPOINT ["/usr/local/bin/supd"]
CMD ["--workdir", "/etc/supd", "run"]
STOPSIGNAL SIGTERM
