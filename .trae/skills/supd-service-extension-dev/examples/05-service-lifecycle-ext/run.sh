#!/bin/bash
# demo-lifecycle: service lifecycle hooks for web-demo
ACTION="${SUPD_ACTION:-on-ready}"

case "$ACTION" in
    on-ready)
        echo "[demo-lifecycle] web-demo is ready! $(date)"
        ;;
    on-fail)
        echo "[demo-lifecycle] web-demo failed! $(date)"
        ;;
    on-stop)
        echo "[demo-lifecycle] web-demo is stopping... $(date)"
        ;;
    *)
        echo "[demo-lifecycle] Unknown action: $ACTION"
        ;;
esac
