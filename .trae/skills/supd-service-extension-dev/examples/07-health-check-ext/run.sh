#!/bin/bash
set -e

# qBittorrent 健康检查扩展
# 支持 action: check | diagnose

ACTION="${SUPD_ACTION:-check}"
WEBUI_HOST="127.0.0.1"
WEBUI_PORT="9091"
WEBUI_URL="http://${WEBUI_HOST}:${WEBUI_PORT}"

if [ "$ACTION" = "diagnose" ]; then
    # 诊断模式：服务故障时排查问题
    echo "::progress:: 10 \"开始诊断 qBittorrent 故障...\""
    sleep 1

    echo "::progress:: 30 \"检查进程是否存在...\""
    if pgrep -x qbittorrent-nox > /dev/null 2>&1; then
        echo "  [OK] qbittorrent-nox 进程正在运行"
        process_ok=1
    else
        echo "  [FAIL] 未找到 qbittorrent-nox 进程"
        process_ok=0
    fi
    sleep 1

    echo "::progress:: 50 \"检查端口 ${WEBUI_PORT} 是否监听...\""
    if command -v ss > /dev/null 2>&1; then
        if ss -tlnp 2>/dev/null | grep -q ":${WEBUI_PORT}"; then
            echo "  [OK] 端口 ${WEBUI_PORT} 正在监听"
            port_ok=1
        else
            echo "  [FAIL] 端口 ${WEBUI_PORT} 未监听"
            port_ok=0
        fi
    else
        echo "  [SKIP] ss 命令不可用，跳过端口检查"
        port_ok=0
    fi
    sleep 1

    echo "::progress:: 70 \"尝试连接 WebUI...\""
    http_code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 3 "${WEBUI_URL}/" 2>/dev/null || true)
    if [ "$http_code" != "000" ]; then
        echo "  [OK] WebUI 响应 HTTP ${http_code}"
    else
        echo "  [FAIL] 无法连接 WebUI"
    fi
    sleep 1

    echo "::progress:: 90 \"检查磁盘空间...\""
    df -h . | head -2
    sleep 1

    if [ "$process_ok" = "1" ] && [ "$port_ok" = "1" ]; then
        echo "::result:: warning \"进程和端口正常但服务可能异常，建议检查日志\""
    else
        echo "::result:: error \"诊断完成：进程或端口异常，建议重启服务\""
    fi
    exit 0
fi

# 默认 check 模式
echo "::progress:: 10 \"开始健康检查...\""
sleep 1

echo "::progress:: 30 \"连接到 qBittorrent WebUI (${WEBUI_URL})...\""
http_code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "${WEBUI_URL}/" 2>/dev/null || true)
if [ "$http_code" = "000" ]; then
    echo "  [FAIL] 无法连接到 WebUI"
    sleep 1
    echo "::progress:: 100 \"健康检查失败\""
    echo "::result:: error \"qBittorrent WebUI 不可访问，连接超时\""
    exit 0
fi
echo "  [OK] WebUI 响应 HTTP ${http_code}"
sleep 1

echo "::progress:: 50 \"检查 WebUI API...\""
api_code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "${WEBUI_URL}/api/v2/app/version" 2>/dev/null || true)
if [ "$api_code" != "000" ]; then
    echo "  [OK] API 端点响应 HTTP ${api_code}"
else
    echo "  [WARN] API 端点无响应"
fi
sleep 1

echo "::progress:: 75 \"检查种子列表...\""
torrents_json=$(curl -s --connect-timeout 5 "${WEBUI_URL}/api/v2/torrents/info" 2>/dev/null || echo "")
if [ -n "$torrents_json" ] && [ "$torrents_json" != "" ]; then
    torrent_count=$(echo "$torrents_json" | grep -o '"name"' | wc -l)
    echo "  [OK] 当前种子数: ${torrent_count}"
else
    torrent_count=0
    echo "  [WARN] 无法获取种子列表（可能需要认证）"
fi
sleep 1

echo "::progress:: 90 \"汇总检查结果...\""
sleep 1

echo "::progress:: 100 \"健康检查完成\""
if [ "$http_code" != "000" ]; then
    echo "::result:: success \"qBittorrent 运行正常 | WebUI: HTTP ${http_code} | 种子数: ${torrent_count}\""
else
    echo "::result:: error \"qBittorrent WebUI 不可访问\""
fi
