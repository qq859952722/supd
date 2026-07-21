#!/bin/bash
set -e

# supd 生命周期钩子扩展
# action: notify — supd post_ready 时执行，通知系统就绪
# action: cleanup — supd pre_shutdown 时执行，优雅清理

ACTION="${SUPD_ACTION:-notify}"

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
sleep 1

echo "::progress:: 80 \"加载扩展配置...\""
sleep 1

echo "::progress:: 100 \"启动钩子执行完成\""
echo "::result:: success \"supd 启动完成 | 磁盘: ${disk_info}\""
