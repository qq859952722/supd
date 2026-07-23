#!/bin/bash
# service-lifecycle-ext: 服务生命周期钩子示例
# 通过 SUPD_SERVICE / SUPD_SERVICE_PID / SUPD_SERVICE_EXIT_CODE 获取服务上下文
ACTION="${SUPD_ACTION:-on-ready}"
SVC="${SUPD_SERVICE:-unknown}"

case "$ACTION" in
    on-ready)
        echo "[lifecycle] Service '${SVC}' is ready! PID=${SUPD_SERVICE_PID:-?} Time=$(date)"
        ;;
    on-fail)
        echo "[lifecycle] Service '${SVC}' failed! ExitCode=${SUPD_SERVICE_EXIT_CODE:-?} Time=$(date)"
        ;;
    on-stop)
        echo "[lifecycle] Service '${SVC}' is stopping... Time=$(date)"
        ;;
    *)
        echo "[lifecycle] Unknown action: $ACTION"
        ;;
esac
