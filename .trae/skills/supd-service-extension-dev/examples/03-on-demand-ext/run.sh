#!/bin/bash
# demo-action: 通用 on-demand 手动扩展示例
# 通过 SUPD_ACTION 区分动作；通过 SERVICE_PORT 配置关联服务的端口（默认 8080）
ACTION="${SUPD_ACTION:-greet}"
ARG1="${1:-}"
PORT="${SERVICE_PORT:-8080}"

case "$ACTION" in
    greet)
        echo "Hello, ${ARG1:-World}! Greetings from demo-action."
        ;;
    status)
        echo "=== Service Status ==="
        echo "Service: ${SUPD_SERVICE:-unknown}"
        echo "Service Dir: ${SUPD_SERVICE_DIR:-(not set)}"
        echo "PID: $$"
        echo "Timestamp: $(date)"
        if command -v curl > /dev/null 2>&1; then
            # 端口通过环境变量注入，避免硬编码
            http_code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 3 "http://127.0.0.1:${PORT}/health" 2>/dev/null || true)
            echo "Health Check (port ${PORT}): HTTP ${http_code}"
        else
            echo "curl not available, skipping health check"
        fi
        ;;
    *)
        echo "Unknown action: $ACTION"
        exit 1
        ;;
esac
