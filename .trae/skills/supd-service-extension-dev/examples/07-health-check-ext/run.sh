#!/bin/bash
# health-check-ext: 通用 HTTP 服务健康检查扩展
# 支持两种 action：check（快速检查）、diagnose（深度诊断）
# 通过环境变量 SERVICE_PORT 配置目标服务端口（默认 8080）
# 通过环境变量 HEALTH_PATH 配置健康检查路径（默认 /health）

ACTION="${SUPD_ACTION:-check}"
PORT="${SERVICE_PORT:-8080}"
HEALTH_PATH="${HEALTH_PATH:-/health}"
BASE_URL="http://127.0.0.1:${PORT}"

if [ "$ACTION" = "diagnose" ]; then
    # 深度诊断模式
    echo "::progress:: 10 \"开始深度诊断服务 (port=${PORT})...\""
    sleep 1

    echo "::progress:: 30 \"检查进程是否存在...\""
    PROC_NAME="${SUPD_SERVICE:-unknown-service}"
    if pgrep -f "${PROC_NAME}" > /dev/null 2>&1; then
        echo "  [OK] 服务进程 '${PROC_NAME}' 正在运行"
        PROC_OK=1
    else
        echo "  [WARN] 未找到服务进程 '${PROC_NAME}'（可能进程名不匹配）"
        PROC_OK=0
    fi
    sleep 1

    echo "::progress:: 50 \"检查端口 ${PORT} 是否监听...\""
    if command -v ss > /dev/null 2>&1; then
        if ss -tlnp 2>/dev/null | grep -q ":${PORT}"; then
            echo "  [OK] 端口 ${PORT} 正在监听"
            PORT_OK=1
        else
            echo "  [FAIL] 端口 ${PORT} 未监听"
            PORT_OK=0
        fi
    else
        echo "  [SKIP] ss 命令不可用，跳过端口检查"
        PORT_OK=0
    fi
    sleep 1

    echo "::progress:: 75 \"尝试连接健康检查端点...\""
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "${BASE_URL}${HEALTH_PATH}" 2>/dev/null || true)
    if [ "${HTTP_CODE}" != "000" ]; then
        echo "  [OK] 健康检查端点响应 HTTP ${HTTP_CODE}"
        HTTP_OK=1
    else
        echo "  [FAIL] 无法连接健康检查端点 ${BASE_URL}${HEALTH_PATH}"
        HTTP_OK=0
    fi
    sleep 1

    echo "::progress:: 90 \"汇总诊断结果...\""
    sleep 1

    if [ "$PORT_OK" = "1" ] && [ "$HTTP_OK" = "1" ]; then
        echo "::result:: success \"诊断完成：服务运行正常\""
    elif [ "$PORT_OK" = "1" ] && [ "$HTTP_OK" = "0" ]; then
        echo "::result:: warning \"端口监听正常但健康接口异常，建议检查应用日志\""
    else
        echo "::result:: error \"诊断完成：端口未监听或服务无响应，建议重启服务\""
    fi
    exit 0
fi

# 默认 check 模式：快速健康检查
echo "::progress:: 20 \"开始快速健康检查 (${BASE_URL}${HEALTH_PATH})...\""
sleep 1

echo "::progress:: 60 \"发送 HTTP GET 请求...\""
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "${BASE_URL}${HEALTH_PATH}" 2>/dev/null || true)

echo "::progress:: 90 \"解析响应...\""
sleep 1

echo "::progress:: 100 \"健康检查完成\""
if [ "${HTTP_CODE}" = "200" ]; then
    echo "::result:: success \"服务健康，HTTP ${HTTP_CODE}\""
elif [ "${HTTP_CODE}" != "000" ]; then
    echo "::result:: warning \"服务响应 HTTP ${HTTP_CODE}，状态异常\""
else
    echo "::result:: error \"服务无响应，连接超时\""
fi
