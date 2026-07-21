#!/bin/bash
# demo-action: web-demo 服务级 on_demand 扩展
# action: greet [name] — 打招呼
# action: status — 通过 HTTP 查询 web-demo 健康状态

ACTION="${SUPD_ACTION:-greet}"
ARG1="${1:-}"

case "$ACTION" in
    greet)
        echo "::progress:: 50 \"正在打招呼...\""
        sleep 0.3
        echo "Hello, ${ARG1:-World}! Greetings from demo-action."
        echo "::result:: success \"已向 ${ARG1:-World} 打招呼\""
        ;;
    status)
        echo "::progress:: 30 \"查询 web-demo 健康...\""
        sleep 0.3
        if command -v curl &>/dev/null; then
            health_resp=$(curl -s -w "\nHTTP_CODE=%{http_code}" http://127.0.0.1:9001/health 2>/dev/null)
            http_code=$(echo "$health_resp" | grep "HTTP_CODE=" | cut -d= -f2)
            body=$(echo "$health_resp" | grep -v "HTTP_CODE=")
            echo "  HTTP $http_code: $body"
            echo "::progress:: 80 \"查询完成\""
            sleep 0.2
            if [ "$http_code" = "200" ]; then
                echo "::result:: success \"web-demo 健康 (HTTP $http_code)\""
            else
                echo "::result:: warning \"web-demo 异常 (HTTP $http_code)\""
            fi
        else
            echo "::result:: error \"curl 不可用\""
            exit 1
        fi
        ;;
    *)
        echo "Unknown action: $ACTION" >&2
        exit 1
        ;;
esac
