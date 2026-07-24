# supd

**supd** 是一款专为家庭 NAS 场景打造的轻量级进程监督器（Supervisor）。

它借鉴了 [s6](https://skarnet.org/software/s6/) 的进程管理核心思想（如监督树、自动重启、Readiness 通知、单服务独立日志等），但不兼容其配置与二进制格式。`supd` 提供了现代化、傻瓜式的 Web UI 和高度灵活的扩展脚本系统，目标是为极客和家庭用户提供低资源占用、高可靠性的服务管理体验。

---

## ✨ 核心特性

- **轻量克制**：单二进制分发，无复杂依赖。满载 20 服务 + 50 扩展场景下常驻内存 < 50MB，CPU 占用极低。
- **现代化 Web UI**：内置美观、响应式的控制面板，支持服务状态总览、实时日志流（长轮询实现，无 WebSocket）、配置在线编辑等。
- **服务依赖与状态机**：支持严格的服务启动依赖拓扑图，内置7种服务状态与4种 Readiness 探针（fd, tcp, http, script）。
- **强大的扩展系统**：
  - 将 hook、定时任务（cron）、生命周期回调和手动触发统一抽象为“扩展（Extension）”。
  - 支持全局扩展与服务级扩展。
  - 支持多层级环境变量合并，及自动注入运行上下文（如 `SUPD_EVENT`, `SUPD_PHASE` 等）。
- **自动化日志管理**：按服务独立收集日志，自动进行按大小轮转（Rotation），历史日志可搜索。
- **优雅的进程管理**：使用进程组（Process Group）发信号，确保无僵尸进程，不依赖 cgroup。

---

## 🛠️ 安装

### 方式一：Docker（推荐）

```bash
# 拉取镜像（自动选择当前平台架构：amd64 / arm64）
docker pull ghcr.io/qq859952722/supd:latest

# 初始化配置目录
docker run --rm \
  -v /opt/supd/etc:/etc/supd \
  ghcr.io/qq859952722/supd:latest init --workdir /etc/supd

# 启动
docker run -d \
  --name supd \
  --restart unless-stopped \
  -p 7979:7979 \
  -v /opt/supd/etc:/etc/supd \
  -v /opt/supd/logs:/var/log/supd \
  ghcr.io/qq859952722/supd:latest
```

### 方式二：预编译二进制

从 [Releases 页面](https://github.com/qq859952722/supd/releases) 下载对应平台的 tar.gz 包：

```bash
# Linux x86_64
curl -L https://github.com/qq859952722/supd/releases/latest/download/supd-linux-amd64.tar.gz | tar xz
sudo mv supd /usr/local/bin/

# Linux ARM64
curl -L https://github.com/qq859952722/supd/releases/latest/download/supd-linux-arm64.tar.gz | tar xz
sudo mv supd /usr/local/bin/

# 初始化并启动
supd init --workdir /etc/supd
supd run --workdir /etc/supd
```

### 方式三：源码编译

需要预先安装 `Go >= 1.25` 和 `Node.js 20+ + pnpm`。

```bash
# 获取源码
git clone https://github.com/qq859952722/supd.git
cd supd

# 一键编译前端和后端（最终前端静态资源会嵌入至 Go 二进制中）
make all

# 编译产物为当前目录下的 supd 文件
ls -l supd
```

---

## 🚀 快速开始

### 1. 初始化工作目录

`supd` 需要一个工作目录来存放服务配置和扩展。默认路径为 `/etc/supd/`（如果不需要 root 权限，也可以指定任意用户目录，如 `~/.supd`）。

```bash
# 初始化配置目录（包含基础模板和生成的 auth_token）
./supd init --workdir /etc/supd
```
> **注意**：非 `root` 用户初始化时，不能监听 1024 以下的端口，且无法使用 `run_as` 切换其他用户执行服务。

### 2. 启动 supd 守护进程

```bash
./supd run --workdir /etc/supd
```

### 3. 访问 Web 控制台

打开浏览器访问：[http://localhost:7979](http://localhost:7979)
（如果在初始化时修改了配置，以您的 `config.yaml` 中配置的 `settings.http_listen` 为准）。

---

## 📖 核心概念与配置使用

`supd` 通过扫描工作目录自动发现服务和扩展。无需在中心注册表手动注册。

### 1. 服务 (Service)

服务是 `supd` 监督的核心 longrun 进程。每个服务需放置在 `services/<服务名>/` 目录下，并包含 `service.yaml`。

**示例：`services/my-web/service.yaml`**
```yaml
name: my-web
version: 1.0.0
command: ["python3", "-m", "http.server", "8000"]
restart:
  policy: always
  backoff_ms: 1000
readiness:
  type: tcp_check
  port: 8000
```
- **Readiness** 支持 `fd_notify` / `tcp_check` / `http_check` / `script`。
- **依赖管理**：可通过 `depends_on: ["other-svc"]` 指定服务启动顺序。

### 2. 扩展 (Extension)

扩展是一段可被触发执行的短生命周期脚本，可以是 bash、python 或 Node.js 脚本。
支持放置在全局 (`extensions/`) 或服务私有目录 (`services/<服务名>/extensions/`)。

**示例：`extensions/system-backup/meta.yaml`**
```yaml
name: system-backup
version: 1.0.0
entry: run.sh
timeout_seconds: 600
actions:
  - id: start_backup
    label: "立即备份"
    button_style: primary
triggers:
  - type: on_schedule          # 定时触发
    schedule: "0 2 * * *"      # 每天凌晨 2 点
    action: start_backup
  - type: on_demand            # WebUI 手动触发
    action: start_backup
```
在对应的 `run.sh` 中即可编写备份逻辑，`supd` 提供了标准 stdout 协议 (`::progress::`, `::result::`) 以便向 Web UI 实时报告进度和结果。

---

## 💻 CLI 常用命令参考

除了 Web UI，`supd` 也提供完善的 CLI 用于脚本自动化或终端管理。所有命令均通过 `--workdir`（或工作目录中的 `config.yaml`）定位实例：

```bash
# 查看所有命令帮助
./supd --help
./supd version                    # 查看版本与构建信息

# 核心管理命令（必须携带 --workdir 或通过配置指定）
./supd status                     # 列出所有服务及当前状态
./supd start <服务名>             # 手动启动服务
./supd stop <服务名>              # 优雅停止服务
./supd restart <服务名>           # 重启服务
./supd signal <服务名> <信号>      # 向服务发送特定信号 (例如 SIGUSR1)
./supd logs <服务名>              # 持续查看服务日志流
./supd reload                     # 热重载配置 (相当于发 SIGHUP)

# 扩展执行
./supd ext list                   # 列出所有扩展
./supd ext show <扩展名>          # 查看扩展详情
./supd ext run <扩展名> --action <action>   # 触发扩展执行
./supd ext status <扩展名>        # 查看扩展运行状态

# 打包与导入
./supd export <类型> <名称> --output <文件>   # 导出服务/扩展为 .supd 包
./supd import <文件>                            # 从 .supd 包导入服务/扩展

# 鉴权 Token 管理
./supd token generate            # 生成新 Token
./supd token show                # 查看当前 Token
./supd token verify <token>      # 验证 Token 有效性

# 运行时管理（注册脚本运行时别名，如 bun / deno / tjs）
./supd runtimes list             # 列出已注册运行时
./supd runtimes install <名称> <绝对路径>
./supd runtimes remove <名称>

# 配置校验（启动前预检 services/ 与 extensions/）
./supd validate                  # 校验工作目录结构与配置文件
```

---

## 📂 目录结构参考

初始化后的工作目录结构如下：

```text
/etc/supd/                      # 配置与服务定义目录
├── config.yaml                 # 核心配置 (监听端口、鉴权策略等)
├── env/                        # 全局环境变量
├── extensions/                 # 全局扩展目录
├── services/                   # 被监督的服务列表
│   └── <service-name>/
│       ├── service.yaml        # 服务主配置
│       ├── env.yaml            # 服务环境变量
│       └── extensions/         # 服务私有扩展
└── script_tmp/                 # 扩展运行时的工作临时目录

/var/log/supd/                  # 统一日志落盘目录
├── supd.log                    # 守护进程自身日志
├── events.log                  # 全局事件流文件
├── extensions/                 # 扩展运行日志
└── services/                   # 各个服务的运行日志
```

---

## 📝 最佳实践建议

1. **服务进程必须前台运行**：`supd` 依赖 `wait` 系统调用跟踪进程。请勿在 `service.yaml` 的命令中使用 daemonize 标志（例如 nginx 的 `daemon off;`）。
2. **利用 Readiness 防止请求黑洞**：为服务配置准确的 `readiness`，确保在服务真正能处理请求后，再向外暴露或启动依赖它的下游服务。
3. **无状态原则**：`supd` 的任务队列及运行状态主要存放在内存，重启 `supd` 会清空临时状态队列，请确保您的架构允许这种“重启丢失”设计。

---

## 🐳 Docker 部署

`supd` 自带 PID 1 能力（`PR_SET_CHILD_SUBREAPER` + `SIGCHLD` 僵尸进程回收），**无需 tini / dumb-init 等外部 init**，可作为容器入口直接运行。

官方镜像已发布至 GHCR，**内置 [tjs (txiki.js)](https://github.com/saghul/txiki.js) 运行时**，扩展脚本可直接通过 `#!/usr/bin/env tjs` 使用 JavaScript 编写，无需额外安装 Node.js。

### 1. 拉取镜像

```bash
# 自动选择当前平台架构（linux/amd64 或 linux/arm64）
docker pull ghcr.io/qq859952722/supd:latest

# 或指定具体版本
docker pull ghcr.io/qq859952722/supd:v0.0.19
```

> 也可从源码自行构建：`docker build -t supd:latest .`（多阶段：`node:24-alpine` → `golang:1.25-alpine` → `alpine:3.20`，最终以非 root 用户 `supd` 运行）。

### 2. 启动容器

```bash
# 准备宿主机持久化目录
mkdir -p /opt/supd/etc /opt/supd/logs

# 初始化配置（首次运行）
docker run --rm \
  -v /opt/supd/etc:/etc/supd \
  ghcr.io/qq859952722/supd:latest init --workdir /etc/supd

# 启动监督器（持久化配置 + 日志 + 端口映射）
docker run -d \
  --name supd \
  --restart unless-stopped \
  -p 7979:7979 \
  -v /opt/supd/etc:/etc/supd \
  -v /opt/supd/logs:/var/log/supd \
  ghcr.io/qq859952722/supd:latest
```

### 3. 优雅停止

`supd` 已显式注册 `SIGTERM` 处理器，`docker stop` 默认 10 秒后强制 `SIGKILL`。建议将超时延长至与 `shutdown_grace_seconds`（默认 30 秒）对齐：

```bash
docker stop -t 30 supd
```

或在 `docker run` 时指定：

```bash
docker run -d --stop-grace-period 30s ...
```

### 4. 注意事项

- **不要使用 `--no-pid1`**：该参数仅适用于 systemd 场景。在 Docker 中禁用 PID 1 模式会导致僵尸进程无法回收，且 `supd` 无法接管孤儿子进程。
- **资源限制**：如需限制内存/CPU，请通过 `docker run --memory --cpus` 设置。`supd` 满载 20 服务 + 50 扩展常驻内存 < 50MB。
- **扩展脚本依赖**：基础镜像含 `sh`、`bash`、`curl` 与 `tjs`。若扩展脚本额外需要 `python3`/`node` 等，请在自定义镜像中追加：
  ```dockerfile
  FROM ghcr.io/qq859952722/supd:latest
  USER root
  RUN apk add --no-cache python3 nodejs
  USER supd
  ```
- **日志持久化**：`/var/log/supd` 必须通过 volume 挂载，否则容器重启后日志丢失。

---

## 📊 项目状态

| 项目 | 说明 |
|------|------|
| 当前版本 | `v0.0.19` |
| 平台支持 | Linux amd64 / Linux arm64 |
| 后端语言 | Go 1.25+ |
| 前端技术栈 | React 19 + TypeScript + Vite + Tailwind CSS 4 |
| 镜像运行时 | Alpine 3.20 + 内置 tjs (txiki.js) |
| 架构约束 | 无数据库、无 WebSocket（长轮询）、单二进制 |

---

## 📚 相关文档

- [需求规格说明书 v1.5](docs/需求规格说明_v1.5.md) — 业务规则唯一权威来源
- [服务与扩展开发指南](docs/服务扩展开发指南.md) — 服务/扩展的配置、开发、打包全流程
- [Releases](https://github.com/qq859952722/supd/releases) — 版本发布与变更记录

---

## 🤝 参与贡献

欢迎提交 Issue 与 Pull Request。提交前请确保：

```bash
go build ./... && go vet ./... && go test ./... -count=1
cd web && pnpm build
```

> 业务逻辑相关变更请先对照 `docs/需求规格说明_v1.5.md`，避免引入与规格冲突的行为。
