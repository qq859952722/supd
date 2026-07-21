#!/bin/bash
# 信号处理演示服务
# 通过 trap 捕获 HUP/USR1/USR2/TERM/INT 信号，打印日志

# fd_notify 通知就绪
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
