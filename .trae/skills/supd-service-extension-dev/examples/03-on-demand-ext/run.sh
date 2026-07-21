#!/bin/bash
# demo-action: on-demand action for web-demo
ACTION="${SUPD_ACTION:-greet}"
ARG1="${1:-}"

case "$ACTION" in
    greet)
        echo "Hello, ${ARG1:-World}! Greetings from demo-action."
        ;;
    status)
        echo "=== web-demo status ==="
        if command -v curl &>/dev/null; then
            curl -s http://127.0.0.1:9001/health 2>/dev/null || echo "health check failed"
        else
            echo "curl not available"
        fi
        echo "PID: $$"
        echo "Timestamp: $(date)"
        ;;
    *)
        echo "Unknown action: $ACTION"
        exit 1
        ;;
esac
