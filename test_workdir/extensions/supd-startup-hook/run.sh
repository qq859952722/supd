#!/bin/bash
set -e

# supd 生命周期钩子扩展
# 覆盖 3 个 supd_lifecycle 事件：
#   - pre_start: supd 启动前执行（在此阶段 supd 主进程尚未启动）
#   - post_ready: supd 就绪后执行
#   - pre_shutdown: supd 关闭前执行

ACTION="${SUPD_ACTION:-notify}"

case "$ACTION" in
    on_pre_start)
        # pre_start: supd 启动前准备
        echo "::progress:: 30 \"supd 启动前准备...\""
        sleep 0.3
        echo "  [INFO] supd 即将启动，执行启动前准备（如创建日志目录）"
        mkdir -p /tmp/supd-logs 2>/dev/null || true
        sleep 0.3
        echo "::progress:: 100 \"准备完成\""
        echo "::result:: success \"supd pre_start 钩子完成\""
        ;;
    cleanup)
        # pre_shutdown: 优雅清理
        echo "::progress:: 10 \"收到 supd 关闭信号，开始清理...\""
        sleep 1

        echo "::progress:: 50 \"清理临时文件...\""
        tmp_count=$(find /tmp -name "supd-*" -type f 2>/dev/null | wc -l || echo "0")
        echo "  [INFO] 发现临时文件: ${tmp_count}"
        sleep 1

        echo "::progress:: 100 \"清理完成，准备关闭\""
        echo "::result:: success \"supd 关闭清理完成 | 临时文件: ${tmp_count}\""
        ;;
    notify|*)
        # 默认 post_ready: 启动通知
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
        ;;
esac
