#!/bin/bash
# demo-lifecycle: web-demo 服务级 service_lifecycle 扩展
# 覆盖 4 个 lifecycle 事件：pre_start / post_ready / on_failure / pre_stop
# 通过 SUPD_ACTION / SUPD_SERVICE / SUPD_SERVICE_PID / SUPD_SERVICE_EXIT_CODE 等环境变量观察上下文

ACTION="${SUPD_ACTION:-on_ready}"
SERVICE="${SUPD_SERVICE:-unknown}"
SERVICE_PID="${SUPD_SERVICE_PID:-0}"
EXIT_CODE="${SUPD_SERVICE_EXIT_CODE:-0}"
SIGNAL="${SUPD_SERVICE_SIGNAL:-0}"
RESTART_COUNT="${SUPD_SERVICE_RESTART_COUNT:-0}"

case "$ACTION" in
    on_pre_start)
        echo "::progress:: 30 \"${SERVICE} 启动前准备（PID 尚未创建）...\""
        sleep 0.3
        echo "  [INFO] 服务 $SERVICE 即将启动，PID=${SERVICE_PID}（应为 0）"
        echo "  [INFO] 执行启动前准备任务（如创建目录、清理缓存）"
        sleep 0.3
        echo "::progress:: 100 \"准备完成\""
        echo "::result:: success \"${SERVICE} pre_start 钩子完成\""
        ;;
    on_ready)
        echo "::progress:: 30 \"${SERVICE} 已就绪（PID=${SERVICE_PID}），执行初始化...\""
        sleep 0.3
        echo "  [INFO] 服务 $SERVICE 进入 ready 状态"
        echo "  [INFO] 执行服务级初始化任务"
        sleep 0.3
        echo "::progress:: 100 \"初始化完成\""
        echo "::result:: success \"${SERVICE} 就绪钩子完成\""
        ;;
    on_failure)
        echo "::progress:: 30 \"${SERVICE} 失败（exit=${EXIT_CODE} signal=${SIGNAL} restart=${RESTART_COUNT}）\""
        sleep 0.3
        echo "  [INFO] 服务 $SERVICE 异常退出，exit_code=${EXIT_CODE}, signal=${SIGNAL}"
        echo "  [INFO] 执行失败诊断与告警（如发送通知）"
        sleep 0.3
        echo "::progress:: 100 \"失败处理完成\""
        echo "::result:: warning \"${SERVICE} on_failure 钩子完成（exit=${EXIT_CODE}）\""
        ;;
    on_stop)
        echo "::progress:: 30 \"${SERVICE} 准备停止（PID=${SERVICE_PID}），执行清理...\""
        sleep 0.3
        echo "  [INFO] 服务 $SERVICE 即将停止"
        echo "  [INFO] 执行服务级清理任务"
        sleep 0.3
        echo "::progress:: 100 \"清理完成\""
        echo "::result:: success \"${SERVICE} 停止钩子完成\""
        ;;
    *)
        echo "Unknown action: $ACTION" >&2
        exit 1
        ;;
esac
