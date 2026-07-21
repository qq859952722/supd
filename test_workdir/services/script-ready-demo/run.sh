#!/bin/bash
# script-ready-demo 主进程
# 启动时创建 ready 标记文件（延迟 3 秒，模拟慢启动）
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
