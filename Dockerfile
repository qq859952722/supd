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

# 安装运行时依赖与常用工具（总增量约 12.6 MB，控制在 20 MB 预算内）
# - ca-certificates/tzdata: TLS 根证书 + 时区数据（基础）
# - bash/curl: Shell + HTTP 客户端（基础，扩展脚本常用）
# - openssl: TLS 工具（生成证书、测试 HTTPS、加解密）
# - wget: 下载工具（扩展脚本常用，比 curl 更简单）
# - unzip/bzip2/xz/zstd: 常见解压格式（zip/bz2/xz/zst）
# - 7zip: 7z + RAR 解压（Alpine 3.20 中 7zip 包同时提供 p7zip 兼容与 7z/7zz 命令）
# - coreutils: GNU 核心工具完整版（stat/dd/sha256sum 等，替代 busybox 简化版）
# - findutils: GNU find/xargs（比 busybox 版支持更多参数）
# - lsof: 查看打开的文件/端口占用（诊断文件锁、端口冲突）
# - file: 文件类型检测
# - tree: 目录树可视化（调试用）
# - iproute2: ip/ss 命令（现代 Linux 网络配置与 socket 查看）
# - iputils: ping/tracepath/arping（网络诊断，meta 包含 4 个子工具）
# - bind-tools: dig/nslookup/host（DNS 查询）
# - socat: 网络中继/端口转发（Docker 内端口映射调试常用）
# - netcat-openbsd: nc 命令（网络调试瑞士军刀）
# - psmisc: killall/pstree/fuser（进程管理）
# - procps-ng: ps/top/free 完整版（比 busybox 版功能更全）
# - util-linux: 更多系统工具
# - jq: JSON 处理（API 响应解析、配置生成）
# - nano: 轻量编辑器（容器内快速编辑配置）
RUN apk add --no-cache \
        ca-certificates tzdata bash curl \
        openssl wget \
        unzip bzip2 xz zstd 7zip \
        coreutils findutils lsof file tree \
        iproute2 iputils bind-tools socat netcat-openbsd \
        psmisc procps-ng util-linux \
        jq nano \
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
EXPOSE 7979
VOLUME ["/etc/supd", "/var/log/supd"]
ENTRYPOINT ["/usr/local/bin/supd"]
CMD ["--workdir", "/etc/supd", "run"]
STOPSIGNAL SIGTERM
