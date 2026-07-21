#!/bin/bash
# fd_notify readiness 演示
# 通过 fd 3 写入 "READY=1\n" 通知 supd 进程已就绪
# 之后进入主循环（保持运行）

echo "fd-notify-demo 启动中..."

# fd_notify 协议：向 fd 3 写入 "READY=1\n" 通知就绪
echo "READY=1" >&3 2>/dev/null || {
    # 如果 fd 3 不可写（独立运行测试时），打印警告但继续
    echo "[WARN] fd 3 不可用（独立运行模式），跳过 readiness 通知"
}

echo "fd-notify-demo 已就绪，进入主循环"

# 主循环：每 5 秒打印心跳，保持进程运行
counter=0
while true; do
    counter=$((counter + 1))
    echo "[heartbeat] fd-notify-demo 运行中, counter=$counter"
    sleep 5
done
