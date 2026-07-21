package cli

// init_examples.go 存放 `supd init` 生成的示例服务与扩展字面量。
//
// 设计目标：用最少的示例覆盖 supd 尽可能多的核心特性。
//
// 4 个服务覆盖：
//   - 4 种 readiness（http_check / tcp_check / fd_notify / script）
//   - 3 种 restart policy（always 默认 / on-failure / no）
//   - depends_on 拓扑排序、signals 自定义信号、stop、logging、autostart、tags
//
// 4 个扩展（3 全局 + 1 服务级）覆盖：
//   - 4 种触发器（on_demand / on_schedule / supd_lifecycle / service_lifecycle）
//   - 4 种并发策略（replace / serialize / parallel / debounce:Ns）
//   - stdout 协议、多 action、button_style、ui.icon、args、env.yaml password 字段
//
// 与 test_workdir/ 的关系：示例内容复用 test_workdir 已验证实现，并做以下调整：
//   - demo-lifecycle 并发策略由 parallel 改为 debounce:5s（覆盖第 4 种并发策略）
//   - supd-startup-hook 新增 env.yaml 演示 password 字段
//   - scheduled-task 为全新创建（on_schedule + serialize）

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
