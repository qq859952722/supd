#!/bin/bash
# restart-always-demo
# 默认行为：运行 3 秒后崩溃（exit 1），触发自动重启
# 环境变量 RESTART_LIVE_SECONDS 可控制运行时长（默认 3）
# 环境变量 RESTART_EXIT_CODE 可控制退出码（默认 1）

LIVE_SECONDS="${RESTART_LIVE_SECONDS:-3}"
EXIT_CODE="${RESTART_EXIT_CODE:-1}"

# fd_notify 通知就绪
echo "READY=1" >&3 2>/dev/null || echo "[WARN] fd 3 不可用"

echo "restart-always-demo 启动，运行 $LIVE_SECONDS 秒后退出（code=$EXIT_CODE）"

counter=0
end_time=$((SECONDS + LIVE_SECONDS))
while [ $SECONDS -lt $end_time ]; do
    counter=$((counter + 1))
    echo "[heartbeat] restart-always-demo 运行中, counter=$counter, remaining=$((end_time - SECONDS))s"
    sleep 1
done

echo "[exit] 运行 $LIVE_SECONDS 秒后退出，exit code=$EXIT_CODE"
exit "$EXIT_CODE"
