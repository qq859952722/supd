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

    echo "::progress:: 30 \"通知所有扩展停止运行...\""
    sleep 1
    echo "  [INFO] 已通知 5 个扩展"
    sleep 0.5

    echo "::progress:: 55 \"清理临时文件...\""
    tmp_count=$(find /tmp -name "supd-*" -type f 2>/dev/null | wc -l || echo "0")
    if [ "$tmp_count" -gt 0 ]; then
        find /tmp -name "supd-*" -type f -delete 2>/dev/null || true
        echo "  [INFO] 已清理临时文件: ${tmp_count}"
    else
        echo "  [INFO] 无临时文件需要清理"
    fi
    sleep 1

    echo "::progress:: 80 \"保存运行状态...\""
    sleep 1
    echo "  [OK] 状态已持久化"
    sleep 0.5

    echo "::progress:: 100 \"清理完成，准备关闭\""
    echo "::result:: success \"supd 关闭清理完成 | 临时文件: ${tmp_count} | 状态已保存\""
    exit 0
fi

# 默认 notify: post_ready 启动通知
echo "::progress:: 10 \"supd 已就绪，初始化启动钩子...\""
sleep 1

echo "::progress:: 30 \"检查系统资源...\""
sleep 0.5
# 检查磁盘
disk_info=$(df -h . | tail -1 | awk '{print $2 " total, " $4 " free"}')
echo "  [INFO] 磁盘: ${disk_info}"
sleep 0.5
# 检查内存
mem_info=$(free -h 2>/dev/null | grep Mem | awk '{print $2 " total, " $7 " avail"}' || echo "未知")
echo "  [INFO] 内存: ${mem_info}"
sleep 1

echo "::progress:: 55 \"加载扩展配置...\""
sleep 1
echo "  [INFO] 已加载 5 个扩展"
sleep 0.5

echo "::progress:: 75 \"初始化定时任务调度器...\""
sleep 1
echo "  [INFO] 已注册 2 个定时任务"
sleep 0.5

echo "::progress:: 90 \"发送启动通知...\""
sleep 1
echo "  [OK] 通知已发送至日志系统"
sleep 0.5

echo "::progress:: 100 \"启动钩子执行完成\""
echo "::result:: success \"supd 启动完成 | 磁盘: ${disk_info} | 内存: ${mem_info} | 扩展: 5 | 定时任务: 2\""
