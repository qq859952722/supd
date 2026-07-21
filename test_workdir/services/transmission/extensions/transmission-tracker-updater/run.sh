#!/bin/bash
# transmission-tracker-updater: 通过 Transmission RPC 更新所有种子的 tracker 列表
# Tracker 来源: https://github.com/adysec/tracker (trackers_best.txt, 全协议优选)
# 协议支持: udp:// http:// https:// wss:// (Transmission 原生支持)
# 更新方式: 调用 torrent-set 的 trackerList 参数, 替换每个种子的整个 tracker 列表
# action 通过 SUPD_ACTION 环境变量传入 (update | check)

set -euo pipefail

# ============== 配置 (可通过环境变量覆盖) ==============
RPC_HOST="${TRANSMISSION_RPC_HOST:-127.0.0.1}"
RPC_PORT="${TRANSMISSION_RPC_PORT:-9091}"
RPC_USER="${TRANSMISSION_RPC_USER:-}"
RPC_PASS="${TRANSMISSION_RPC_PASS:-}"
RPC_URL="http://${RPC_HOST}:${RPC_PORT}/transmission/rpc"

# Tracker 列表源: 主源(GitHub raw) + 备源(adysec CDN)
TRACKER_URL="${TRANSMISSION_TRACKER_URL:-https://raw.githubusercontent.com/adysec/tracker/main/trackers_best.txt}"
TRACKER_URL_BACKUP="https://tracker.adysec.com/trackers_best.txt"

ACTION="${SUPD_ACTION:-update}"

# ============== HTTP 工具函数 ==============
# 鉴权标志 (配置了用户名密码时启用)
HAS_AUTH=0
if [ -n "${RPC_USER}" ] && [ -n "${RPC_PASS}" ]; then
    HAS_AUTH=1
fi

# 获取 Transmission 的 X-Transmission-Session-Id (CSRF token)
# 首次请求会返回 409, 响应头中包含 session-id
get_session_id() {
    local headers session_id
    if command -v curl &>/dev/null; then
        if [ "${HAS_AUTH}" -eq 1 ]; then
            headers=$(curl -s -o /dev/null -D - -u "${RPC_USER}:${RPC_PASS}" "${RPC_URL}" 2>/dev/null || true)
        else
            headers=$(curl -s -o /dev/null -D - "${RPC_URL}" 2>/dev/null || true)
        fi
    elif command -v wget &>/dev/null; then
        if [ "${HAS_AUTH}" -eq 1 ]; then
            headers=$(wget -S -qO /dev/null --user="${RPC_USER}" --password="${RPC_PASS}" "${RPC_URL}" 2>&1 || true)
        else
            headers=$(wget -S -qO /dev/null "${RPC_URL}" 2>&1 || true)
        fi
    else
        return 1
    fi
    # 响应头大小写不敏感匹配
    session_id=$(echo "${headers}" | grep -i "^X-Transmission-Session-Id:" | head -1 | awk '{print $2}' | tr -d '\r\n')
    echo "${session_id}"
}

# 调用 Transmission RPC
# 参数: $1=session_id, $2=method, $3=arguments(JSON 字符串)
rpc_call() {
    local session_id="$1"
    local method="$2"
    local arguments="$3"
    [ -z "${arguments}" ] && arguments="{}"
    local body="{\"method\":\"${method}\",\"arguments\":${arguments}}"
    if command -v curl &>/dev/null; then
        if [ "${HAS_AUTH}" -eq 1 ]; then
            curl -fsSL -u "${RPC_USER}:${RPC_PASS}" \
                 -H "X-Transmission-Session-Id: ${session_id}" \
                 -H "Content-Type: application/json" \
                 -X POST -d "${body}" "${RPC_URL}" 2>/dev/null
        else
            curl -fsSL -H "X-Transmission-Session-Id: ${session_id}" \
                 -H "Content-Type: application/json" \
                 -X POST -d "${body}" "${RPC_URL}" 2>/dev/null
        fi
    elif command -v wget &>/dev/null; then
        if [ "${HAS_AUTH}" -eq 1 ]; then
            wget -qO- --user="${RPC_USER}" --password="${RPC_PASS}" \
                 --header="X-Transmission-Session-Id: ${session_id}" \
                 --header="Content-Type: application/json" \
                 --post-data="${body}" "${RPC_URL}" 2>/dev/null
        else
            wget -qO- --header="X-Transmission-Session-Id: ${session_id}" \
                 --header="Content-Type: application/json" \
                 --post-data="${body}" "${RPC_URL}" 2>/dev/null
        fi
    else
        return 1
    fi
}

# 获取 tracker 列表 (主源失败时使用备源)
fetch_trackers() {
    local out="$1"
    if command -v curl &>/dev/null; then
        if curl -fsSL -o "${out}" "${TRACKER_URL}" 2>/dev/null && [ -s "${out}" ]; then
            return 0
        fi
        echo "主源失败, 尝试备源..."
        if curl -fsSL -o "${out}" "${TRACKER_URL_BACKUP}" 2>/dev/null && [ -s "${out}" ]; then
            return 0
        fi
    elif command -v wget &>/dev/null; then
        if wget -q -O "${out}" "${TRACKER_URL}" 2>/dev/null && [ -s "${out}" ]; then
            return 0
        fi
        echo "主源失败, 尝试备源..."
        if wget -q -O "${out}" "${TRACKER_URL_BACKUP}" 2>/dev/null && [ -s "${out}" ]; then
            return 0
        fi
    fi
    return 1
}

# 显示 tracker 协议分布
show_protocol_stats() {
    local file="$1"
    local total udp http https wss other
    total=$(grep -cv '^[[:space:]]*$' "${file}" 2>/dev/null || echo 0)
    udp=$(grep -c '^udp://' "${file}" 2>/dev/null || echo 0)
    http=$(grep -c '^http://' "${file}" 2>/dev/null || echo 0)
    https=$(grep -c '^https://' "${file}" 2>/dev/null || echo 0)
    wss=$(grep -c '^wss://' "${file}" 2>/dev/null || echo 0)
    other=$(( total - udp - http - https - wss ))
    echo "Tracker 总数: ${total}"
    echo "协议分布:"
    echo "  udp://   : ${udp}"
    echo "  http://  : ${http}"
    echo "  https:// : ${https}"
    echo "  wss://   : ${wss}"
    if [ "${other}" -gt 0 ]; then
        echo "  其他     : ${other}"
    fi
}

# ============== 主逻辑 ==============
case "${ACTION}" in
    check)
        echo "=== Transmission Tracker 状态检查 ==="
        echo "RPC 地址: ${RPC_URL}"
        echo "Tracker 源: ${TRACKER_URL}"
        echo "鉴权: $([ "${HAS_AUTH}" -eq 1 ] && echo "已配置" || echo "未配置")"
        echo ""

        # 1. 检查 Transmission 连接
        echo "正在连接 Transmission RPC..."
        SESSION_ID=$(get_session_id)
        if [ -z "${SESSION_ID}" ]; then
            echo "无法获取 Session ID (Transmission 服务未启动或 RPC 不可达)"
            echo "::result:: warning \"Transmission 未运行, 无法获取状态\""
            exit 0
        fi
        echo "Session ID: ${SESSION_ID}"
        echo ""

        # 2. 获取种子数量
        RESPONSE=$(rpc_call "${SESSION_ID}" "torrent-get" '{"fields":["id"]}')
        if [ -z "${RESPONSE}" ]; then
            echo "::result:: error \"RPC 调用失败\""
            exit 1
        fi
        TORRENT_COUNT=$(echo "${RESPONSE}" | grep -o '"id":[0-9]*' | wc -l | tr -d ' ')
        echo "种子数量: ${TORRENT_COUNT}"
        echo ""

        # 3. 获取 tracker 列表信息
        TEMP_FILE=$(mktemp)
        echo "正在获取 Tracker 列表..."
        if ! fetch_trackers "${TEMP_FILE}"; then
            rm -f "${TEMP_FILE}"
            echo "::result:: error \"获取 Tracker 列表失败 (主备源均不可用)\""
            exit 1
        fi
        echo ""
        show_protocol_stats "${TEMP_FILE}"
        rm -f "${TEMP_FILE}"

        echo ""
        echo "::result:: success \"种子数: ${TORRENT_COUNT}, Tracker 源正常\""
        ;;

    update)
        echo "=== Transmission Tracker 列表更新 ==="
        echo "RPC 地址: ${RPC_URL}"
        echo "Tracker 源: ${TRACKER_URL}"
        echo "鉴权: $([ "${HAS_AUTH}" -eq 1 ] && echo "已配置" || echo "未配置")"
        echo ""

        # 1. 获取 tracker 列表
        TEMP_FILE=$(mktemp)
        echo "::progress:: 10 \"正在获取 Tracker 列表...\""
        if ! fetch_trackers "${TEMP_FILE}"; then
            rm -f "${TEMP_FILE}"
            echo "::result:: error \"获取 Tracker 列表失败 (主备源均不可用)\""
            exit 1
        fi

        # 过滤空行和注释行
        grep -v '^[[:space:]]*$' "${TEMP_FILE}" | grep -v '^#' > "${TEMP_FILE}.clean" || true
        mv "${TEMP_FILE}.clean" "${TEMP_FILE}"

        TRACKER_COUNT=$(wc -l < "${TEMP_FILE}" | tr -d ' ')
        if [ "${TRACKER_COUNT}" -eq 0 ]; then
            rm -f "${TEMP_FILE}"
            echo "::result:: error \"Tracker 列表为空\""
            exit 1
        fi

        echo "获取到 ${TRACKER_COUNT} 个 Tracker:"
        show_protocol_stats "${TEMP_FILE}"
        echo ""

        # 2. 连接 Transmission RPC
        echo "::progress:: 30 \"正在连接 Transmission RPC...\""
        SESSION_ID=$(get_session_id)
        if [ -z "${SESSION_ID}" ]; then
            rm -f "${TEMP_FILE}"
            echo "::result:: error \"无法连接 Transmission RPC (服务未启动或 RPC 不可达)\""
            exit 1
        fi
        echo "Session ID: ${SESSION_ID}"
        echo ""

        # 3. 获取所有种子 ID
        echo "::progress:: 50 \"正在获取种子列表...\""
        RESPONSE=$(rpc_call "${SESSION_ID}" "torrent-get" '{"fields":["id"]}')
        if [ -z "${RESPONSE}" ]; then
            rm -f "${TEMP_FILE}"
            echo "::result:: error \"获取种子列表失败\""
            exit 1
        fi

        # 提取所有 id (格式: "id":123)
        TORRENT_IDS=$(echo "${RESPONSE}" | grep -o '"id":[0-9]*' | sed 's/"id"://' | tr '\n' ',' | sed 's/,$//')
        TORRENT_COUNT=$(echo "${TORRENT_IDS}" | tr ',' '\n' | grep -c '[0-9]' || echo 0)

        if [ -z "${TORRENT_IDS}" ] || [ "${TORRENT_COUNT}" -eq 0 ]; then
            rm -f "${TEMP_FILE}"
            echo "无种子任务, 无需更新 Tracker"
            echo "::result:: warning \"无种子任务, 无需更新 Tracker\""
            exit 0
        fi
        echo "找到 ${TORRENT_COUNT} 个种子"
        echo ""

        # 4. 构建 trackerList 字符串 (JSON 转义)
        # trackerList 格式: 每行一个 URL, 用 \n 分隔 (JSON 字符串)
        # 需要转义: \ -> \\, " -> \", 换行 -> \n
        TRACKER_LIST_JSON=$(sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' "${TEMP_FILE}" | awk 'BEGIN{ORS="\\n"} {print}' | sed 's/\\n$//')

        rm -f "${TEMP_FILE}"

        # 5. 调用 torrent-set 一次性更新所有种子
        # trackerList 参数替换每个种子的整个 tracker 列表
        echo "::progress:: 70 \"正在更新 ${TORRENT_COUNT} 个种子的 Tracker 列表...\""
        ARGS="{\"ids\":[${TORRENT_IDS}],\"trackerList\":\"${TRACKER_LIST_JSON}\"}"
        RESULT=$(rpc_call "${SESSION_ID}" "torrent-set" "${ARGS}")

        echo "::progress:: 90 \"正在验证结果...\""
        if [ -n "${RESULT}" ] && echo "${RESULT}" | grep -q '"result":"success"'; then
            echo "=== 更新完成 ==="
            echo "成功更新种子数: ${TORRENT_COUNT}"
            echo "Tracker 列表条目: ${TRACKER_COUNT}"
            echo ""
            echo "说明: 已替换每个种子的整个 tracker 列表为 adysec/tracker 的最新优选列表"
            echo "::progress:: 100 \"完成\""
            echo "::result:: success \"已为 ${TORRENT_COUNT} 个种子更新 ${TRACKER_COUNT} 个 Tracker\""
        else
            echo "RPC 响应: ${RESULT:-空响应}"
            echo "::result:: error \"更新失败, 请检查 Transmission 日志\""
            exit 1
        fi
        ;;

    *)
        echo "::result:: error \"未知操作: ${ACTION} (支持: update | check)\""
        exit 1
        ;;
esac
