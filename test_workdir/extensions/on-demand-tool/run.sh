#!/bin/bash
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
        # 进度演示：每秒 10% 进度
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
