#!/bin/bash
set -e
# stats-report-ext: 定时+手动混合扩展示例
# 完整演示 ::progress:: / ::result:: stdout 协议用法
# 通过环境变量 SERVICE_PORT 配置关联服务端口（默认 8080）

ACTION="${SUPD_ACTION:-report}"
PORT="${SERVICE_PORT:-8080}"
BASE_URL="http://127.0.0.1:${PORT}"

echo "::progress:: 5 \"开始生成服务统计报告...\""
sleep 1

echo "::progress:: 20 \"检测服务运行状态 (port=${PORT})...\""
IS_RUNNING=0
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 3 "${BASE_URL}/" 2>/dev/null || true)
if [ "${HTTP_CODE}" != "000" ]; then
    IS_RUNNING=1
    echo "  [OK] 服务响应 HTTP ${HTTP_CODE}"
else
    echo "  [WARN] 服务无响应，将使用模拟数据"
fi
sleep 1

echo "::progress:: 40 \"收集运行时指标...\""
sleep 1

# 系统级指标（通用，不依赖特定服务）
UPTIME=$(uptime -p 2>/dev/null || echo "unknown")
MEM_INFO=$(free -m 2>/dev/null | awk '/^Mem:/{printf "used=%dMB total=%dMB", $3, $2}' || echo "unavailable")
DISK_INFO=$(df -h . 2>/dev/null | awk 'NR==2{printf "used=%s avail=%s", $3, $4}' || echo "unavailable")

echo "  系统运行时长: ${UPTIME}"
echo "  内存: ${MEM_INFO}"
echo "  磁盘: ${DISK_INFO}"
sleep 1

echo "::progress:: 70 \"汇总服务状态信息...\""
sleep 1

# 关联服务上下文信息（来自 SUPD_* 注入变量）
SVC_NAME="${SUPD_SERVICE:-unknown-service}"
SVC_PID="${SUPD_SERVICE_PID:-(not set)}"
TRIGGER_TIME="${SUPD_TRIGGER_TIME:-unknown}"
TRIGGER_SOURCE="${SUPD_TRIGGER_SOURCE:-unknown}"

echo "  触发服务: ${SVC_NAME}"
echo "  服务 PID: ${SVC_PID}"
echo "  触发时间: ${TRIGGER_TIME}"
echo "  触发来源: ${TRIGGER_SOURCE}"
sleep 1

echo "::progress:: 90 \"生成报告摘要...\""
sleep 0.5
echo "  ========================"
echo "  服务统计报告"
echo "  ========================"
echo "  服务名:    ${SVC_NAME}"
echo "  状态:      $([ "$IS_RUNNING" = "1" ] && echo "运行中 (HTTP ${HTTP_CODE})" || echo "离线/无响应")"
echo "  运行时长:  ${UPTIME}"
echo "  内存:      ${MEM_INFO}"
echo "  磁盘:      ${DISK_INFO}"
echo "  生成时间:  $(date '+%Y-%m-%d %H:%M:%S')"
echo "  ========================"

echo "::progress:: 100 \"报告生成完成\""
if [ "$IS_RUNNING" = "1" ]; then
    echo "::result:: success \"报告完成 | 服务: ${SVC_NAME} | 状态: 运行中 HTTP ${HTTP_CODE}\""
else
    echo "::result:: warning \"报告完成 | 服务: ${SVC_NAME} | 状态: 无响应，请检查服务\""
fi
