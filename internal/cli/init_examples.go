package cli

// init_examples.go 存放 `supd init` 生成的示例服务与扩展字面量。
//
// 设计目标：用最少的示例覆盖 supd 尽可能多的核心特性。
//
// 5 个服务覆盖：
//   - 4 种 readiness（http_check / tcp_check / fd_notify / script）
//   - 3 种 restart policy（always 默认 / on-failure / no）
//   - depends_on 拓扑排序、signals 自定义信号、stop、logging、autostart、tags
//   - Docker SSH 集成（dropbear-ssh：autostart:false + run.sh + env.yaml + run_as root）
//
// 5 个扩展（4 全局 + 1 服务级）覆盖：
//   - 4 种触发器（on_demand / on_schedule / supd_lifecycle / service_lifecycle）
//   - 4 种并发策略（replace / serialize / parallel / debounce:Ns）
//   - stdout 协议、多 action、button_style、ui.icon、args、env.yaml password 字段
//   - run_as root、enabled false、环境变量驱动的实用扩展
//
// 与 test_workdir/ 的关系：示例内容复用 test_workdir 已验证实现，并做以下调整：
//   - demo-lifecycle 并发策略由 parallel 改为 debounce:5s（覆盖第 4 种并发策略）
//   - supd-startup-hook 新增 env.yaml 演示 password 字段
//   - scheduled-task 为全新创建（on_schedule + serialize）
//   - dropbear-ssh 为全新创建（Docker 在线开发集成，自带 run.sh + env.yaml）

// =============================================================================
// 示例服务
// =============================================================================

// --- web-demo：http_check readiness + depends_on + autostart + 服务级扩展载体 ---
const webDemoServiceYAML = `name: web-demo
version: "1.0.0"
description: "Python HTTP 演示服务（http_check readiness）"
icon: globe
autostart: true
command:
  - python3
  - run.py
tags:
  - demo
  - http
# depends_on: 拓扑排序演示（规格 §2.1.2）
# web-demo 依赖 tcp-echo，supd 会先启动 tcp-echo 再启动 web-demo
# 反向停止时会先停 web-demo 再停 tcp-echo
depends_on:
  - tcp-echo
readiness:
  type: http_check
  url: http://127.0.0.1:9001/health
  expected_status: 200
  interval_seconds: 1
  timeout_seconds: 10
stop:
  grace_seconds: 5
  timeout_seconds: 30
logging:
  enabled: true
  max_size_mb: 5
  max_files: 3
`

const webDemoRunPY = `#!/usr/bin/env python3
# web-demo 主进程：Python 内置 HTTP 服务
# 提供 /health（200 JSON）和 /（200 text）端点供 http_check readiness 使用
import http.server
import socketserver
import sys

PORT = 9001


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(b'{"status":"ok"}')
        else:
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain')
            self.end_headers()
            self.wfile.write(b'web-demo running\n')

    def do_POST(self):
        # POST 也返回 200，便于扩展测试
        self.do_GET()

    def log_message(self, fmt, *args):
        # 日志输出到 stderr，被 supd 捕获
        sys.stderr.write(fmt % args + '\n')


socketserver.TCPServer.allow_reuse_address = True
with socketserver.TCPServer(("0.0.0.0", PORT), Handler) as httpd:
    httpd.serve_forever()
`

// --- tcp-echo：tcp_check readiness + 被依赖 + autostart ---
const tcpEchoServiceYAML = `name: tcp-echo
version: "1.0.0"
description: "TCP 回显服务（tcp_check readiness，端口 9002）"
icon: radio
autostart: true
command:
  - bash
  - run.sh
tags:
  - demo
  - tcp
readiness:
  type: tcp_check
  port: 9002
  interval_seconds: 1
  timeout_seconds: 10
stop:
  grace_seconds: 5
  timeout_seconds: 30
logging:
  enabled: true
  max_size_mb: 5
  max_files: 3
`

const tcpEchoRunSH = `#!/bin/bash
# tcp-echo 主进程：TCP 回显服务（端口 9002）
# 优先使用 nc，不可用时使用 python3 兜底
PORT=9002

if command -v nc &>/dev/null && nc -h 2>&1 | grep -q "listen"; then
    # nc 支持 -l (listen) 和 -k (keep-alive，多连接)
    if nc -l -p "$PORT" -k 2>/dev/null; then
        exit 0
    fi
fi

# Python 兜底实现 TCP echo
python3 -c "
import socketserver
import sys

PORT = $PORT

class Handler(socketserver.BaseRequestHandler):
    def handle(self):
        while True:
            try:
                data = self.request.recv(4096)
                if not data:
                    break
                self.request.sendall(data)
            except Exception:
                break

class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

with Server(('0.0.0.0', PORT), Handler) as srv:
    srv.serve_forever()
"
`

// --- signals-demo：fd_notify readiness + signals + restart: on-failure ---
const signalsDemoServiceYAML = `name: signals-demo
version: "1.0.0"
description: "信号处理演示服务（响应 HUP/USR1/USR2/TERM，restart: on-failure）"
icon: zap
autostart: false
command:
  - bash
  - run.sh
tags:
  - demo
  - signals
readiness:
  type: fd_notify
  fd: 3
  timeout_seconds: 10
# restart: on-failure 演示（规格 §2.1.3: 仅退出码非 0 时重启）
restart:
  policy: on-failure
  max_retries: 3
  backoff_ms: 1000
  max_backoff_ms: 10000
  multiplier: 2
  reset_after_seconds: 60
# signals: 自定义信号映射（规格 §2.3.2）
# run.sh 已 trap HUP/USR1/USR2/TERM/INT/QUIT，此处映射为语义化按钮
signals:
  reload: HUP
  rotate_logs: USR1
  graceful_quit: QUIT
stop:
  grace_seconds: 5
  timeout_seconds: 30
logging:
  enabled: true
  max_size_mb: 5
  max_files: 3
`

const signalsDemoRunSH = `#!/bin/bash
# signals-demo 主进程：信号处理演示
# 通过 trap 捕获 HUP/USR1/USR2/TERM/INT/QUIT 信号，打印日志

# fd_notify 通知就绪（向 fd 3 写入 "READY=1"）
echo "READY=1" >&3 2>/dev/null || echo "[WARN] fd 3 不可用（独立运行模式）"

echo "signals-demo 已就绪，等待信号..."

# 信号处理函数
handle_hup() {
    echo "[SIGNAL] 收到 SIGHUP（重新加载配置）"
}

handle_usr1() {
    echo "[SIGNAL] 收到 SIGUSR1（自定义操作 1）"
}

handle_usr2() {
    echo "[SIGNAL] 收到 SIGUSR2（自定义操作 2）"
}

handle_term() {
    echo "[SIGNAL] 收到 SIGTERM，准备退出"
    exit 0
}

handle_int() {
    echo "[SIGNAL] 收到 SIGINT，准备退出"
    exit 0
}

handle_quit() {
    echo "[SIGNAL] 收到 SIGQUIT，优雅退出（graceful_quit）"
    exit 0
}

trap handle_hup HUP
trap handle_usr1 USR1
trap handle_usr2 USR2
trap handle_term TERM
trap handle_int INT
trap handle_quit QUIT

# 主循环：等待信号
counter=0
while true; do
    counter=$((counter + 1))
    echo "[heartbeat] signals-demo 运行中, counter=$counter"
    sleep 5
done
`

// --- script-ready-demo：script readiness + restart: never ---
const scriptReadyDemoServiceYAML = `name: script-ready-demo
version: "1.0.0"
description: "script readiness 演示（外部脚本检查就绪状态，restart: never）"
icon: check-circle
autostart: false
command:
  - bash
  - run.sh
tags:
  - demo
  - script
readiness:
  type: script
  check:
    - bash
    - check_ready.sh
  interval_seconds: 2
  timeout_seconds: 15
# restart: never 演示（规格 §2.1.3: 从不重启）
restart:
  policy: never
stop:
  grace_seconds: 5
  timeout_seconds: 30
logging:
  enabled: true
  max_size_mb: 5
  max_files: 3
`

const scriptReadyDemoRunSH = `#!/bin/bash
# script-ready-demo 主进程
# 启动时延迟 3 秒后创建 ready 标记文件（模拟慢启动）
# readiness 脚本 check_ready.sh 检查该文件是否存在

echo "script-ready-demo 启动中，3 秒后进入就绪状态..."
sleep 3

# 创建就绪标记文件
touch /tmp/script-ready-demo.ready
echo "已创建就绪标记 /tmp/script-ready-demo.ready"

# 主循环
counter=0
while true; do
    counter=$((counter + 1))
    echo "[heartbeat] script-ready-demo 运行中, counter=$counter"
    sleep 5
done
`

const scriptReadyDemoCheckSH = `#!/bin/bash
# readiness 检查脚本（被 supd 调用）
# 退出码 0 = 就绪，非 0 = 未就绪
# 检查 /tmp/script-ready-demo.ready 文件是否存在
if [ -f /tmp/script-ready-demo.ready ]; then
    exit 0
else
    exit 1
fi
`

// --- dropbear-ssh：tcp_check readiness + run_as root + autostart:false + env.yaml 驱动 ---
// 功能：Dropbear SSH/SFTP 服务（端口 2222），供智能 IDE Remote-SSH 在线开发
// Docker 镜像内置 dropbear + openssh-sftp-server；本地需手动安装 dropbear
// 默认不自动启动：用户按需通过 Web UI/API 启动
// 认证模式由 env.yaml 中的 SSH_PUBLIC_KEY 控制：
//   - 非空 → 公钥认证（dropbear -s 禁用密码登录）
//   - 空   → 空白密码免认证（dropbear -B 允许空白密码，仅内网可信场景）
// host key 由 -R 参数在首次启动时动态生成（每容器独立，避免镜像硬编码）
const dropbearSshServiceYAML = `name: dropbear-ssh
version: "1.0.0"
description: "Dropbear SSH/SFTP 服务（端口 2222，供智能 IDE Remote-SSH 在线开发，默认不自动启动）"
icon: terminal
# autostart: false — 默认不自动启动，按需通过 Web UI/API 启动
# 原因：SSH 服务暴露登录入口，由用户显式启用更安全
autostart: false
# command: 通过 run.sh 启动，根据 env.yaml 中的 SSH_PUBLIC_KEY 自动选择认证模式
command:
  - bash
  - run.sh
# run_as: root — dropbear 需要 root 权限：
#   1. 写入 /etc/dropbear 生成 host key
#   2. 公钥认证时的用户切换（登录为 supd/root 等不同用户）
#   3. 空白密码模式下设置 supd/root 用户密码
# 非 root 环境下需以 root 启动 supd，否则此服务启动失败
run_as: root
readiness:
  type: tcp_check
  port: 2222
  interval_seconds: 1
  timeout_seconds: 5
stop:
  grace_seconds: 3
  timeout_seconds: 10
logging:
  enabled: true
  max_size_mb: 2
  max_files: 2
tags:
  - ssh
  - sftp
  - remote-dev
`

// dropbearSshRunSH — dropbear-ssh 服务启动脚本
// 规格 §2.2.4: 服务进程会合并 services/<svc>/env.yaml，因此 SSH_PUBLIC_KEY 直接通过环境变量读取
// 认证模式自动选择：
//   - SSH_PUBLIC_KEY 非空 → 公钥认证（写 authorized_keys + dropbear -s 禁用密码）
//   - SSH_PUBLIC_KEY 为空 → 空白密码免认证（dropbear -B 允许空白密码，需先 passwd -d 清空密码）
const dropbearSshRunSH = `#!/bin/bash
# dropbear-ssh 启动脚本：根据 SSH_PUBLIC_KEY 环境变量自动配置认证模式
#
# 环境变量（来自 services/dropbear-ssh/env.yaml，由 supd 注入）：
#   SSH_PUBLIC_KEY — 公钥内容（ssh-ed25519/ssh-rsa 开头），多个公钥用换行分隔
#                    留空（默认）→ 空白密码免认证模式（仅内网可信场景）
#
# 启动模式：
#   1. 公钥认证模式（SSH_PUBLIC_KEY 非空）：
#      - 写入 /etc/supd/.ssh/authorized_keys 和 /root/.ssh/authorized_keys
#      - dropbear -s 禁用密码登录，仅公钥认证
#   2. 空白密码免认证模式（SSH_PUBLIC_KEY 为空）：
#      - passwd -d supd / passwd -d root 清空密码
#      - dropbear -B 允许空白密码登录
#
# host key 由 -R 参数在首次启动时动态生成（每容器独立密钥，避免镜像硬编码）

set -e

# 从环境变量读取（由 supd 从 env.yaml 注入，规格 §2.2.4）
SSH_PUBLIC_KEY="${SSH_PUBLIC_KEY:-}"
PORT="${DROPBEAR_PORT:-2222}"

# 检查 root 权限（dropbear 需要 root 写 host key + 用户切换）
if [ "$(id -u)" -ne 0 ]; then
    echo "[ERROR] dropbear-ssh 需要 root 权限启动，请以 root 运行 supd 或设置 run_as: root" >&2
    exit 1
fi

# 检查 dropbear 是否安装
if ! command -v dropbear >/dev/null 2>&1; then
    echo "[ERROR] dropbear 未安装，Docker 镜像已内置；本地需手动安装（apk add dropbear openssh-sftp-server）" >&2
    exit 1
fi

# 确保 host key 目录存在
mkdir -p /etc/dropbear

if [ -n "$SSH_PUBLIC_KEY" ]; then
    # ===== 公钥认证模式 =====
    echo "[INFO] 认证模式：公钥认证（SSH_PUBLIC_KEY 已配置）"

    # 配置 supd 和 root 用户的 authorized_keys
    for home_dir in /etc/supd /root; do
        mkdir -p "$home_dir/.ssh"
        printf '%s\n' "$SSH_PUBLIC_KEY" > "$home_dir/.ssh/authorized_keys"
        chmod 700 "$home_dir/.ssh"
        chmod 600 "$home_dir/.ssh/authorized_keys"
        # 设置正确的 owner（home_dir 的属主即为 authorized_keys 的属主）
        owner=$(stat -c '%U:%G' "$home_dir" 2>/dev/null || echo "root:root")
        chown -R "$owner" "$home_dir/.ssh" 2>/dev/null || true
    done
    echo "[INFO] authorized_keys 已写入 /etc/supd/.ssh/ 和 /root/.ssh/"

    # dropbear 参数：
    #   -R  缺少 host key 时自动生成
    #   -s  禁用密码登录（仅公钥认证）
    #   -F  前台模式（supd 需要前台进程才能监管）
    #   -p  监听端口
    echo "[INFO] 启动 dropbear（公钥认证，端口 $PORT）..."
    exec dropbear -R -s -F -p "$PORT"
else
    # ===== 空白密码免认证模式 =====
    echo "[INFO] 认证模式：空白密码免认证（SSH_PUBLIC_KEY 未配置）"
    echo "[WARN] 此模式允许无密码登录，仅适用于内网完全可信场景"
    echo "[WARN] 如需公钥认证，请在 services/dropbear-ssh/env.yaml 中配置 SSH_PUBLIC_KEY"

    # 清空 supd 和 root 用户密码（允许空白密码登录）
    for user in supd root; do
        if id "$user" >/dev/null 2>&1; then
            # passwd -d 删除用户密码（允许无密码登录）
            if passwd -d "$user" >/dev/null 2>&1; then
                echo "[INFO] 已清空 $user 用户密码"
            else
                echo "[WARN] 清空 $user 密码失败（用户可能不存在或无权限）"
            fi
        fi
    done

    # dropbear 参数：
    #   -R  缺少 host key 时自动生成
    #   -B  允许空白密码登录
    #   -F  前台模式
    #   -p  监听端口
    echo "[INFO] 启动 dropbear（空白密码免认证，端口 $PORT）..."
    exec dropbear -R -B -F -p "$PORT"
fi
`

// dropbearSshEnvYAML — dropbear-ssh 服务私有环境变量
// 规格 §2.3.3 / §2.2.4: services/<svc>/env.yaml 由 supd 注入服务进程
// SSH_PUBLIC_KEY 默认空 = 空白密码免认证模式（仅内网可信场景）
// 配置公钥认证时填入公钥内容（ssh-ed25519/ssh-rsa 开头，多个公钥用换行分隔）
const dropbearSshEnvYAML = `# dropbear-ssh 服务私有环境变量
# 规格 §2.2.4: 此文件由 supd 注入到 dropbear-ssh 服务进程环境
# 修改后需重启 dropbear-ssh 服务生效（REQ-F-027）

env:
  # SSH_PUBLIC_KEY — SSH 公钥内容
  # 留空（默认）→ 空白密码免认证模式（dropbear -B，仅内网可信场景）
  # 填入公钥  → 公钥认证模式（dropbear -s 禁用密码登录）
  # 多个公钥用换行分隔；支持 ssh-ed25519/ssh-rsa/ecdsa-sha2-nistp256 等格式
  SSH_PUBLIC_KEY:
    value: ""
    hint: "SSH 公钥内容，留空=空白密码免认证，填入=公钥认证"
  # DROPBEAR_PORT — dropbear 监听端口（默认 2222，避开宿主机 22）
  DROPBEAR_PORT:
    value: "2222"
    hint: "dropbear 监听端口"
`

// =============================================================================
// 示例扩展（全局）
// =============================================================================

// --- on-demand-tool：on_demand + replace + 多 action + stdout + button_style 三种 + ui.icon ---
const onDemandToolMetaYAML = `name: on-demand-tool
version: "1.0.0"
description: "on_demand 演示扩展（多 action、stdout 协议、replace 并发）"
enabled: true
runtime: bash
entry: run.sh
timeout_seconds: 30
# concurrency: replace — 新任务启动时终止同扩展的旧任务
concurrency: replace
ui:
  show_logs: true
  button_style: default
  icon: wrench
actions:
  - id: ping
    label: "Ping 测试"
    button_style: default
  - id: status
    label: "系统状态"
    button_style: primary
  - id: progress
    label: "进度演示"
    button_style: default
triggers:
  on_demand: true
`

const onDemandToolRunSH = `#!/bin/bash
# on-demand-tool: on_demand 演示扩展
# 演示 ::progress:: / ::result:: 协议、多 action 分发

ACTION="${SUPD_ACTION:-ping}"

case "$ACTION" in
    ping)
        echo "::progress:: 10 \"开始 ping 测试...\""
        sleep 0.5
        echo "::progress:: 50 \"ping 127.0.0.1...\""
        if command -v ping &>/dev/null; then
            ping_result=$(ping -c 1 -W 1 127.0.0.1 2>&1 | head -2 | tail -1)
            echo "  $ping_result"
        else
            echo "  [INFO] ping 不可用，跳过"
        fi
        sleep 0.5
        echo "::progress:: 100 \"ping 完成\""
        echo "::result:: success \"ping 测试完成\""
        ;;
    status)
        echo "::progress:: 20 \"收集系统状态...\""
        uptime_info=$(uptime 2>/dev/null || echo "未知")
        echo "  uptime: $uptime_info"
        sleep 0.5

        echo "::progress:: 50 \"检查内存...\""
        mem_info=$(free -h 2>/dev/null | grep Mem | awk '{print $2 " total, " $7 " avail"}' || echo "未知")
        echo "  memory: $mem_info"
        sleep 0.5

        echo "::progress:: 80 \"检查磁盘...\""
        disk_info=$(df -h . | tail -1 | awk '{print $2 " total, " $4 " free, " $5 " used"}')
        echo "  disk: $disk_info"
        sleep 0.5

        echo "::progress:: 100 \"状态收集完成\""
        echo "::result:: success \"系统状态 | mem=${mem_info} | disk=${disk_info}\""
        ;;
    progress)
        # 进度演示：每 0.5 秒 10% 进度
        for i in 10 20 30 40 50 60 70 80 90 100; do
            echo "::progress:: $i \"进度演示: ${i}%\""
            sleep 0.5
        done
        echo "::result:: success \"进度演示完成\""
        ;;
    *)
        echo "Unknown action: $ACTION" >&2
        exit 1
        ;;
esac
`

// --- scheduled-task：on_schedule + serialize + cron 表达式（新增） ---
const scheduledTaskMetaYAML = `name: scheduled-task
version: "1.0.0"
description: "on_schedule 演示扩展（每 6 小时执行系统快照，serialize 并发）"
enabled: true
runtime: bash
entry: run.sh
timeout_seconds: 60
# concurrency: serialize — 串行执行，新任务排队等待旧任务完成
concurrency: serialize
ui:
  show_logs: true
  button_style: default
  icon: clock
actions:
  - id: snapshot
    label: "立即执行快照"
    button_style: primary
triggers:
  # on_schedule: 标准 5 段 cron 表达式（分 时 日 月 周）
  # "0 */6 * * *" = 每 6 小时执行一次
  on_schedule:
    - cron: "0 */6 * * *"
      action: snapshot
  # 同时支持手动触发
  on_demand: true
`

const scheduledTaskRunSH = `#!/bin/bash
# scheduled-task: 定时任务演示扩展
# 触发器: on_schedule（cron "0 */6 * * *"，每 6 小时）+ on_demand
# 并发策略: serialize（串行执行，避免并发冲突）
# action: snapshot — 输出系统快照

ACTION="${SUPD_ACTION:-snapshot}"

case "$ACTION" in
    snapshot)
        echo "::progress:: 20 \"开始系统快照...\""
        sleep 0.3
        echo "  时间: $(date '+%Y-%m-%d %H:%M:%S')"
        echo "  uptime: $(uptime 2>/dev/null || echo '未知')"

        echo "::progress:: 50 \"收集磁盘信息...\""
        sleep 0.3
        disk_info=$(df -h . | tail -1 | awk '{print $2 " total, " $4 " free, " $5 " used"}')
        echo "  磁盘: $disk_info"

        echo "::progress:: 80 \"收集内存信息...\""
        sleep 0.3
        mem_info=$(free -h 2>/dev/null | grep Mem | awk '{print $2 " total, " $7 " avail"}' || echo "未知")
        echo "  内存: $mem_info"

        echo "::progress:: 100 \"快照完成\""
        echo "::result:: success \"系统快照完成 | disk=${disk_info} | mem=${mem_info}\""
        ;;
    *)
        echo "Unknown action: $ACTION" >&2
        exit 1
        ;;
esac
`

// --- supd-startup-hook：supd_lifecycle + parallel + env.yaml password 字段 ---
const supdStartupHookMetaYAML = `name: supd-startup-hook
version: "1.0.0"
description: "supd 生命周期钩子扩展（post_ready/pre_shutdown，parallel 并发）"
enabled: true
runtime: bash
entry: run.sh
timeout_seconds: 60
# concurrency: parallel — 允许多个 action 同时执行
concurrency: parallel
triggers:
  # supd_lifecycle: supd 自身生命周期事件
  # post_ready: supd 启动并就绪后触发
  # pre_shutdown: supd 收到退出信号、停止服务前触发
  supd_lifecycle:
    - event: post_ready
      action: notify
    - event: pre_shutdown
      action: cleanup
actions:
  - id: notify
    label: "启动通知"
    button_style: primary
  - id: cleanup
    label: "关闭清理"
    button_style: default
`

const supdStartupHookRunSH = `#!/bin/bash
set -e

# supd 生命周期钩子扩展
# action: notify — supd post_ready 时执行，通知系统就绪
# action: cleanup — supd pre_shutdown 时执行，优雅清理

ACTION="${SUPD_ACTION:-notify}"

# 从 env.yaml 注入的私有环境变量
NOTIFY_CHANNEL="${SUPD_NOTIFY_CHANNEL:-#system}"
# WEBHOOK_URL 为 password 字段，API 返回时掩码，但脚本内可见
WEBHOOK_URL="${SUPD_WEBHOOK_URL:-}"

if [ "$ACTION" = "cleanup" ]; then
    # pre_shutdown: 优雅清理
    echo "::progress:: 10 \"收到 supd 关闭信号，开始清理...\""
    sleep 1

    echo "::progress:: 50 \"清理临时文件...\""
    tmp_count=$(find /tmp -name "supd-*" -type f 2>/dev/null | wc -l || echo "0")
    echo "  [INFO] 发现临时文件: ${tmp_count}"
    sleep 1

    echo "::progress:: 100 \"清理完成，准备关闭\""
    echo "::result:: success \"supd 关闭清理完成 | 临时文件: ${tmp_count}\""
    exit 0
fi

# 默认 notify: post_ready 启动通知
echo "::progress:: 20 \"supd 已就绪，初始化启动钩子...\""
sleep 1

echo "::progress:: 50 \"检查系统资源...\""
disk_info=$(df -h . | tail -1 | awk '{print $2 " total, " $4 " free"}')
echo "  [INFO] 磁盘: ${disk_info}"
echo "  [INFO] 通知频道: ${NOTIFY_CHANNEL}"
if [ -n "$WEBHOOK_URL" ]; then
    echo "  [INFO] Webhook: 已配置（值已隐藏）"
fi
sleep 1

echo "::progress:: 80 \"加载扩展配置...\""
sleep 1

echo "::progress:: 100 \"启动钩子执行完成\""
echo "::result:: success \"supd 启动完成 | 磁盘: ${disk_info}\""
`

const supdStartupHookEnvYAML = `# supd-startup-hook 私有环境变量
# 该文件与 meta.yaml 同目录，仅本扩展可见
# 全局 env 与服务 env 中的同名变量会被此处覆盖

# 普通字段：明文存储
notify_channel: "#system"

# password 字段：标记为敏感信息
# 前端 UI 显示为 ***，API 返回时掩码（如 "https://***"），但脚本中通过 SUPD_WEBHOOK_URL 可读
webhook_url:
  value: "https://example.com/webhook"
  password: true
`

// =============================================================================
// 示例扩展（服务级，归属 web-demo）
// =============================================================================

// --- demo-lifecycle：service_lifecycle + debounce:5s + SUPD_SERVICE 上下文变量 ---
const demoLifecycleMetaYAML = `name: demo-lifecycle
version: "1.0.0"
description: "web-demo 服务级 service_lifecycle 扩展（post_ready/pre_stop，debounce:5s）"
enabled: true
runtime: bash
entry: run.sh
timeout_seconds: 30
# concurrency: debounce:5s — 5 秒内的重复触发合并为一次执行
# 适用场景：服务频繁重启时，避免钩子被密集调用
concurrency: debounce:5s
triggers:
  # service_lifecycle: 服务生命周期事件
  # post_ready: 服务进入 ready 状态后触发
  # pre_stop: 服务收到停止信号前触发
  service_lifecycle:
    - event: post_ready
      action: on_ready
    - event: pre_stop
      action: on_stop
actions:
  - id: on_ready
    label: "服务就绪钩子"
    button_style: default
  - id: on_stop
    label: "服务停止钩子"
    button_style: default
`

const demoLifecycleRunSH = `#!/bin/bash
# demo-lifecycle: web-demo 服务级 service_lifecycle 扩展
# action: on_ready — web-demo 进入 ready 状态时触发
# action: on_stop  — web-demo 收到停止信号前触发

ACTION="${SUPD_ACTION:-on_ready}"
# SUPD_SERVICE: supd 注入的上下文变量，值为所属服务名
SERVICE="${SUPD_SERVICE:-unknown}"

case "$ACTION" in
    on_ready)
        echo "::progress:: 30 \"${SERVICE} 已就绪，执行初始化...\""
        sleep 0.5
        echo "  [INFO] 服务 $SERVICE 进入 ready 状态"
        echo "  [INFO] 执行服务级初始化任务"
        sleep 0.5
        echo "::progress:: 100 \"初始化完成\""
        echo "::result:: success \"${SERVICE} 就绪钩子完成\""
        ;;
    on_stop)
        echo "::progress:: 30 \"${SERVICE} 准备停止，执行清理...\""
        sleep 0.5
        echo "  [INFO] 服务 $SERVICE 即将停止"
        echo "  [INFO] 执行服务级清理任务"
        sleep 0.5
        echo "::progress:: 100 \"清理完成\""
        echo "::result:: success \"${SERVICE} 停止钩子完成\""
        ;;
    *)
        echo "Unknown action: $ACTION" >&2
        exit 1
        ;;
esac
`

// =============================================================================
// 实用扩展（全局，默认禁用，按需启用）
// =============================================================================

// --- auto-create-users：supd_lifecycle post_ready + run_as root + 环境变量驱动 ---
// 功能：supd 启动就绪后，根据 ALLID 环境变量（逗号分隔）自动创建系统用户
// 默认禁用（enabled: false），需手动改为 true 启用
// 需要 root 权限（run_as: root），非 root 环境下需以 root 启动 supd
const autoCreateUsersMetaYAML = `name: auto-create-users
version: "1.0.0"
description: "supd 启动时根据 ALLID 环境变量自动创建系统用户（默认禁用，需 root）"
# 默认禁用：将 enabled 改为 true 后生效
# 启用前提：supd 需以 root 运行（创建用户需要 root 权限）
enabled: false
runtime: bash
entry: run.sh
timeout_seconds: 30
# run_as: root — 以 root 身份执行（创建用户需要 CAP_SETUID/CAP_SETGID）
run_as: root
concurrency: replace
triggers:
  # supd_lifecycle: supd 启动就绪后自动触发
  # post_ready: supd 完成初始化、服务全部启动后触发
  supd_lifecycle:
    - event: post_ready
      action: create
actions:
  - id: create
    label: "创建用户"
    button_style: default
`

const autoCreateUsersRunSH = `#!/bin/bash
# auto-create-users: 根据 ALLID 环境变量自动创建系统用户
#
# 触发时机：supd_lifecycle post_ready（supd 启动就绪后）
# 环境变量：
#   ALLID — 逗号分隔的用户名列表，如 "user1,user2,user3"
#           支持空格分隔（自动 trim），空值跳过
#
# 创建的用户特征：
#   - 无密码（无法交互登录）
#   - 无 home 目录
#   - shell 为 /sbin/nologin（禁止登录）
#   - 系统用户（UID 从系统范围分配）
#
# 注意：需要 root 权限。非 root 运行时会报错并提示解决方法。

# 读取 ALLID 环境变量（宿主机/Docker 传入，非 SUPD_* 变量）
ALLID="${ALLID:-}"

if [ -z "$ALLID" ]; then
    echo "::result:: success \"ALLID 未设置，跳过用户创建\""
    exit 0
fi

# 按逗号分割用户名
IFS=',' read -ra USERS <<< "$ALLID"

TOTAL=${#USERS[@]}
if [ "$TOTAL" -eq 0 ]; then
    echo "::result:: success \"ALLID 为空，跳过用户创建\""
    exit 0
fi

# 检查 root 权限
if [ "$(id -u)" -ne 0 ]; then
    echo "::result:: error \"需要 root 权限创建用户，请以 root 启动 supd 或设置 run_as: root\""
    exit 1
fi

# create_user: 兼容 Alpine(adduser) / Arch/Debian/RHEL(useradd) 创建系统用户
# 参数: $1 = 用户名
# 返回: 0 成功, 非 0 失败
create_user() {
    local user="$1"
    CREATE_USER_ERR=""
    if command -v useradd >/dev/null 2>&1; then
        # useradd（Arch/RHEL/Debian）: --system 系统用户 / --no-create-home 无 home / --shell 登录 shell
        # 捕获 stderr 到 CREATE_USER_ERR，失败时附加到错误消息供用户排查
        CREATE_USER_ERR=$(useradd --system --no-create-home --shell /sbin/nologin "$user" 2>&1)
        return $?
    elif command -v adduser >/dev/null 2>&1; then
        # Alpine busybox adduser: -D 无密码 / -H 无 home / -S 系统用户 / -s 登录 shell
        CREATE_USER_ERR=$(adduser -D -H -S -s /sbin/nologin "$user" 2>&1)
        return $?
    else
        CREATE_USER_ERR="useradd/adduser 命令均不可用"
        return 1
    fi
}

echo "::progress:: 10 \"准备处理 $TOTAL 个用户...\""

CREATED=0
SKIPPED=0
FAILED=0
ERRORS=""

for i in "${!USERS[@]}"; do
    # 去除前后空白（使用 username 避免覆盖 bash $USER 环境变量）
    username=$(echo "${USERS[$i]}" | tr -d '[:space:]')

    # 跳过空用户名
    [ -z "$username" ] && continue

    # 验证用户名合法性（字母/下划线开头，字母数字连字符下划线）
    if ! echo "$username" | grep -qE '^[a-z_][a-z0-9_-]*$'; then
        ERRORS="${ERRORS}用户名 '${username}' 不合法(需匹配 ^[a-z_][a-z0-9_-]*$); "
        FAILED=$((FAILED + 1))
        continue
    fi

    # 检查用户是否已存在
    if id "$username" >/dev/null 2>&1; then
        echo "  [SKIP] 用户 $username 已存在"
        SKIPPED=$((SKIPPED + 1))
    else
        # 创建系统用户（兼容 useradd / adduser）
        if create_user "$username"; then
            echo "  [OK]   用户 $username 创建成功"
            CREATED=$((CREATED + 1))
        else
            ERRORS="${ERRORS}创建用户 ${username} 失败: ${CREATE_USER_ERR}; "
            FAILED=$((FAILED + 1))
        fi
    fi

    # 进度：10% ~ 90%
    PROGRESS=$((10 + (i + 1) * 80 / TOTAL))
    echo "::progress:: ${PROGRESS} \"已处理 $((i + 1))/${TOTAL}\""
done

echo "::progress:: 100 \"处理完成\""

# 汇总结果
SUMMARY="创建: ${CREATED} | 跳过: ${SKIPPED} | 失败: ${FAILED}"
if [ "$FAILED" -gt 0 ]; then
    echo "::result:: warning \"${SUMMARY} | 错误: ${ERRORS}\""
else
    echo "::result:: success \"${SUMMARY}\""
fi
`
