#!/bin/bash
# transmission-binary-updater: 检查并更新 transmission-daemon 静态编译二进制
# 来源: https://github.com/qq859952722/transmission-builder
# 二进制安装到 ${SUPD_SERVICE_DIR}/bin/transmission-daemon
# action 通过 SUPD_ACTION 环境变量传入 (check | update)

set -euo pipefail

ACTION="${SUPD_ACTION:-check}"
SERVICE_DIR="${SUPD_SERVICE_DIR:-$(pwd)}"
BIN_DIR="${SERVICE_DIR}/bin"
BIN_PATH="${BIN_DIR}/transmission-daemon"
ARCH=$(uname -m)

REPO="qq859952722/transmission-builder"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"

# 架构映射: 系统架构 -> release asset 后缀
case "${ARCH}" in
    x86_64)          TARGET_ARCH="amd64" ;;
    aarch64|arm64)   TARGET_ARCH="arm64" ;;
    *)
        echo "::result:: error \"不支持的架构: ${ARCH} (仅支持 amd64/arm64)\""
        exit 1
        ;;
esac

# HTTP GET (curl 优先, wget 兜底)
http_get() {
    local url="$1"
    local out="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL -o "${out}" "${url}" 2>/dev/null
    elif command -v wget &>/dev/null; then
        wget -q -O "${out}" "${url}" 2>/dev/null
    else
        return 1
    fi
}

http_get_stdout() {
    local url="$1"
    if command -v curl &>/dev/null; then
        curl -fsSL "${url}" 2>/dev/null
    elif command -v wget &>/dev/null; then
        wget -qO- "${url}" 2>/dev/null
    else
        return 1
    fi
}

# 获取本地已安装版本
get_local_version() {
    if [ -x "${BIN_PATH}" ]; then
        "${BIN_PATH}" --version 2>&1 | head -1 || echo "unknown"
    else
        echo "未安装"
    fi
}

# 查询 GitHub 最新 release, 输出 "tag|version|asset_name|download_url"
get_latest_release() {
    local response tag version asset_name download_url
    response=$(http_get_stdout "${GITHUB_API}") || return 1
    # 解析 tag_name (兼容 "key":"val" 与 "key": "val" 两种 JSON 格式)
    tag=$(echo "${response}" | grep -oE '"tag_name":[[:space:]]*"[^"]*"' | head -1 | sed -E 's/.*"tag_name":[[:space:]]*"([^"]*)".*/\1/')
    if [ -z "${tag}" ]; then
        return 1
    fi
    # tag 格式: transmission-static-4.1.3 -> version 4.1.3
    version=$(echo "${tag}" | sed 's/transmission-static-//')
    asset_name="transmission-daemon-${version}-${TARGET_ARCH}.tar.xz"
    download_url="https://github.com/${REPO}/releases/download/${tag}/${asset_name}"
    echo "${tag}|${version}|${asset_name}|${download_url}"
}

case "${ACTION}" in
    check)
        echo "=== Transmission-daemon 版本检查 ==="
        echo "架构: ${ARCH} (target: ${TARGET_ARCH})"
        echo "本地版本: $(get_local_version)"
        echo "二进制路径: ${BIN_PATH}"
        echo ""
        echo "正在查询 GitHub 最新版本..."
        if ! LATEST_INFO=$(get_latest_release); then
            echo "::result:: error \"查询 GitHub API 失败 (可能是网络问题或 API 限流)\""
            exit 1
        fi
        LATEST_TAG=$(echo "${LATEST_INFO}" | cut -d'|' -f1)
        LATEST_VERSION=$(echo "${LATEST_INFO}" | cut -d'|' -f2)
        LATEST_ASSET=$(echo "${LATEST_INFO}" | cut -d'|' -f3)
        LATEST_URL=$(echo "${LATEST_INFO}" | cut -d'|' -f4)
        echo "最新版本: ${LATEST_VERSION} (tag: ${LATEST_TAG})"
        echo "资产名称: ${LATEST_ASSET}"
        echo "下载地址: ${LATEST_URL}"
        echo ""
        if [ ! -x "${BIN_PATH}" ]; then
            echo "::result:: warning \"未安装, 最新版本 ${LATEST_VERSION} 可用, 点击 '下载更新' 安装\""
        elif get_local_version | grep -q "${LATEST_VERSION}"; then
            echo "::result:: success \"已是最新版本 ${LATEST_VERSION}\""
        else
            echo "::result:: warning \"有新版本可用: ${LATEST_VERSION}, 点击 '下载更新' 升级\""
        fi
        ;;

    update)
        echo "=== Transmission-daemon 二进制更新 ==="
        echo "架构: ${ARCH} (target: ${TARGET_ARCH})"
        LOCAL_VERSION=$(get_local_version)
        echo "当前本地版本: ${LOCAL_VERSION}"
        echo ""

        echo "正在查询 GitHub 最新版本..."
        if ! LATEST_INFO=$(get_latest_release); then
            echo "::result:: error \"查询 GitHub API 失败 (可能是网络问题或 API 限流)\""
            exit 1
        fi
        LATEST_TAG=$(echo "${LATEST_INFO}" | cut -d'|' -f1)
        LATEST_VERSION=$(echo "${LATEST_INFO}" | cut -d'|' -f2)
        LATEST_ASSET=$(echo "${LATEST_INFO}" | cut -d'|' -f3)
        LATEST_URL=$(echo "${LATEST_INFO}" | cut -d'|' -f4)
        echo "最新版本: ${LATEST_VERSION} (tag: ${LATEST_TAG})"
        echo ""

        # 若已是最新版本且二进制存在, 跳过
        if [ -x "${BIN_PATH}" ] && echo "${LOCAL_VERSION}" | grep -q "${LATEST_VERSION}"; then
            echo "已是最新版本, 无需更新"
            echo "::result:: success \"已是最新版本 ${LATEST_VERSION}\""
            exit 0
        fi

        mkdir -p "${BIN_DIR}"
        TEMP_FILE="${BIN_DIR}/${LATEST_ASSET}.tmp"

        echo "::progress:: 10 \"正在下载 ${LATEST_ASSET}...\""
        if ! http_get "${LATEST_URL}" "${TEMP_FILE}"; then
            rm -f "${TEMP_FILE}"
            echo "::result:: error \"下载失败: ${LATEST_URL}\""
            exit 1
        fi

        if [ ! -s "${TEMP_FILE}" ]; then
            rm -f "${TEMP_FILE}"
            echo "::result:: error \"下载失败: 文件为空\""
            exit 1
        fi

        FILE_SIZE=$(stat -c%s "${TEMP_FILE}" 2>/dev/null || stat -f%z "${TEMP_FILE}" 2>/dev/null || echo "0")
        SIZE_MB=$(( FILE_SIZE / 1024 / 1024 ))
        echo "下载完成, 文件大小: ${SIZE_MB}MB"
        echo "::progress:: 50 \"下载完成, 正在解压...\""

        # 解压 tar.xz
        TEMP_DIR="${BIN_DIR}/.extract.tmp.$$"
        mkdir -p "${TEMP_DIR}"
        if ! tar -xJf "${TEMP_FILE}" -C "${TEMP_DIR}" 2>/dev/null; then
            # 兜底: 手动 xz + tar
            if command -v xz &>/dev/null; then
                if ! xz -d -c "${TEMP_FILE}" | tar -x -C "${TEMP_DIR}" 2>/dev/null; then
                    rm -rf "${TEMP_DIR}" "${TEMP_FILE}"
                    echo "::result:: error \"解压失败: tar.xz 文件损坏或缺少 xz 支持\""
                    exit 1
                fi
            else
                rm -rf "${TEMP_DIR}" "${TEMP_FILE}"
                echo "::result:: error \"解压失败: 需要 tar 支持 xz 或安装 xz 工具\""
                exit 1
            fi
        fi

        # 查找解压出的 transmission-daemon 二进制
        # release 中二进制名可能带版本/架构后缀 (如 transmission-daemon-4.1.3-amd64), 排除校验文件
        EXTRACTED_BIN=$(find "${TEMP_DIR}" -type f -name "transmission-daemon*" ! -name "*.b2sum" ! -name "*.sha*" ! -name "*.md5" ! -name "*.txt" | head -1)
        if [ -z "${EXTRACTED_BIN}" ] || [ ! -f "${EXTRACTED_BIN}" ]; then
            rm -rf "${TEMP_DIR}" "${TEMP_FILE}"
            echo "::result:: error \"解压失败: 未找到 transmission-daemon 二进制\""
            exit 1
        fi

        echo "::progress:: 80 \"正在安装二进制...\""
        chmod +x "${EXTRACTED_BIN}"
        mv -f "${EXTRACTED_BIN}" "${BIN_PATH}"
        rm -rf "${TEMP_DIR}" "${TEMP_FILE}"

        INSTALLED_VERSION=$(get_local_version)
        echo "更新完成! 版本: ${INSTALLED_VERSION}"
        echo "路径: ${BIN_PATH}"
        echo "::progress:: 100 \"安装完成\""
        echo "::result:: success \"已更新到 ${INSTALLED_VERSION}, 大小 ${SIZE_MB}MB\""
        ;;

    *)
        echo "::result:: error \"未知操作: ${ACTION} (支持: check | update)\""
        exit 1
        ;;
esac
