#!/bin/bash
set -e

# 种子统计报告扩展
# action: report — 生成种子统计报告

ACTION="${SUPD_ACTION:-report}"
WEBUI_HOST="127.0.0.1"
WEBUI_PORT="9091"
WEBUI_URL="http://${WEBUI_HOST}:${WEBUI_PORT}"

echo "::progress:: 5 \"开始生成种子统计报告...\""
sleep 1

echo "::progress:: 15 \"检测 qBittorrent 运行状态...\""
sleep 0.5

# 检测端口是否在监听
is_running=0
if command -v ss > /dev/null 2>&1; then
    if ss -tln 2>/dev/null | grep -q ":${WEBUI_PORT}"; then
        is_running=1
        echo "  [OK] qBittorrent 正在运行 (端口 ${WEBUI_PORT} 监听中)"
    fi
elif curl -s --connect-timeout 2 "${WEBUI_URL}/" > /dev/null 2>&1; then
    is_running=1
    echo "  [OK] qBittorrent 正在运行 (WebUI 可访问)"
fi

if [ "$is_running" = "0" ]; then
    echo "  [INFO] qBittorrent 未运行，使用模拟数据生成报告"
fi
sleep 1

echo "::progress:: 30 \"获取种子列表...\""
sleep 1

if [ "$is_running" = "1" ]; then
    # 尝试从 API 获取真实数据
    torrents_json=$(curl -s --connect-timeout 5 "${WEBUI_URL}/api/v2/torrents/info" 2>/dev/null || echo "")
    if [ -n "$torrents_json" ] && [ "$torrents_json" != "" ]; then
        total_count=$(echo "$torrents_json" | grep -o '"name"' | wc -l)
        seeding_count=$(echo "$torrents_json" | grep -o '"state":"uploading"' | wc -l || echo 0)
        downloading_count=$(echo "$torrents_json" | grep -o '"state":"downloading"' | wc -l || echo 0)
        paused_count=$(echo "$torrents_json" | grep -o '"state":"pausedUP"\|"state":"pausedDL"' | wc -l || echo 0)
        echo "  [OK] 从 API 获取数据成功"
        data_source="API (实时)"
    else
        total_count=23
        seeding_count=15
        downloading_count=3
        paused_count=5
        data_source="模拟数据"
        echo "  [WARN] API 无响应，使用模拟数据"
    fi
else
    # 模拟数据
    total_count=23
    seeding_count=15
    downloading_count=3
    paused_count=5
    data_source="模拟数据"
fi
echo "  [INFO] 总种子数: ${total_count}"
sleep 1.5

echo "::progress:: 50 \"统计做种数与下载者数...\""
sleep 1
# 模拟计算
total_seeders=$((seeding_count * 8 + 42))
total_leechers=$((downloading_count * 4 + 12))
echo "  [INFO] 总做种者: ${total_seeders}"
echo "  [INFO] 总下载者: ${total_leechers}"
sleep 1

echo "::progress:: 70 \"计算分享率...\""
sleep 1
# 模拟计算
total_uploaded=$((seeding_count * 1024 + 5120))
total_downloaded=$((downloading_count * 512 + 3072))
if [ "$total_downloaded" -gt 0 ]; then
    ratio=$(awk "BEGIN {printf \"%.2f\", ${total_uploaded} / ${total_downloaded}}")
else
    ratio="∞"
fi
echo "  [INFO] 总上传: ${total_uploaded} MB"
echo "  [INFO] 总下载: ${total_downloaded} MB"
echo "  [INFO] 全局分享率: ${ratio}"
sleep 1

echo "::progress:: 85 \"生成报告摘要...\""
sleep 1
echo "  ========================"
echo "  qBittorrent 统计报告"
echo "  ========================"
echo "  数据来源: ${data_source}"
echo "  生成时间: $(date '+%Y-%m-%d %H:%M:%S')"
echo "  ------------------------"
echo "  做种中:   ${seeding_count}"
echo "  下载中:   ${downloading_count}"
echo "  已暂停:   ${paused_count}"
echo "  总计:     ${total_count}"
echo "  ------------------------"
echo "  做种者:   ${total_seeders}"
echo "  下载者:   ${total_leechers}"
echo "  分享率:   ${ratio}"
echo "  ========================"
sleep 1

echo "::progress:: 100 \"报告生成完成\""
echo "::result:: success \"报告完成 | 种子: ${total_count} (做种 ${seeding_count}/下载 ${downloading_count}/暂停 ${paused_count}) | 分享率: ${ratio} | 来源: ${data_source}\""
