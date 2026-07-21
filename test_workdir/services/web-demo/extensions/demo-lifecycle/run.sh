#!/bin/bash
# demo-lifecycle: web-demo 服务级 service_lifecycle 扩展
# action: on_ready — web-demo 进入 ready 状态时触发
# action: on_stop — web-demo 收到停止信号前触发

ACTION="${SUPD_ACTION:-on_ready}"
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
