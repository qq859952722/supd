#!/bin/sh
# remote_ssh.sh — supd 远程实例 SSH 空密码连接封装
#
# 用法：
#   ./remote_ssh.sh '<远程命令>'        # 执行单条远程命令
#   ./remote_ssh.sh                    # 交互式 shell
#   ./remote_ssh.sh -f <本地> <远程>    # SFTP 上传文件
#   ./remote_ssh.sh -g <远程> <本地>    # SFTP 下载文件
#
# 环境变量（可选，覆盖默认值）：
#   REMOTE_HOST  远程主机（默认 192.168.31.188）
#   REMOTE_PORT  SSH 端口（默认 2222）
#   REMOTE_USER  登录用户（默认 root）
#   API_BASE     supd HTTP API 基址（默认 http://$REMOTE_HOST:7979）
#
# 前置条件：
#   1. dropbear-ssh 服务存在且配置了 -B（空密码模式）—— supd init 默认生成，无需改配置
#   2. root 密码已清空（容器内 passwd -d root）
#      - 首次或容器重启后需执行一次：docker exec <容器> passwd -d root
#      - 本脚本不修改 ssh 服务配置，仅依赖现有 -B 模式
#
# 设计原则：
#   - 不修改 dropbear-ssh 的 service.yaml / env.yaml
#   - dropbear-ssh 默认 autostart:false，本脚本通过 API 自动启动
#   - SSH 空密码通过 SSH_ASKPASS 注入，不依赖 sshpass

set -eu

REMOTE_HOST="${REMOTE_HOST:-192.168.31.188}"
REMOTE_PORT="${REMOTE_PORT:-2222}"
REMOTE_USER="${REMOTE_USER:-root}"
API_BASE="${API_BASE:-http://${REMOTE_HOST}:7979}"

# 创建临时 askpass 脚本（输出空密码）
ASKPASS="$(mktemp -t supd-askpass.XXXXXX 2>/dev/null || mktemp)"
cleanup() { rm -f "$ASKPASS"; }
trap cleanup EXIT INT TERM
printf '#!/bin/sh\necho ""\n' > "$ASKPASS"
chmod +x "$ASKPASS"

SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o PreferredAuthentications=password -o PubkeyAuthentication=no -o NumberOfPasswordPrompts=1"

# 确保 dropbear-ssh 服务已就绪
# 用 grep -o 提取 status 字段，避免依赖 python3/jq
ensure_dropbear() {
  status=$(curl -s --max-time 5 "${API_BASE}/api/services/dropbear-ssh" 2>/dev/null \
           | grep -o '"status":"[^"]*"' 2>/dev/null | cut -d'"' -f4 || echo "")
  if [ "$status" = "ready" ]; then
    return 0
  fi
  echo "→ dropbear-ssh 未就绪（status=$status），通过 API 启动..." >&2
  curl -s --max-time 10 -X POST "${API_BASE}/api/services/dropbear-ssh/start" >/dev/null 2>&1 || true
  for i in 1 2 3 4 5 6 7 8; do
    sleep 1
    status=$(curl -s --max-time 5 "${API_BASE}/api/services/dropbear-ssh" 2>/dev/null \
             | grep -o '"status":"[^"]*"' 2>/dev/null | cut -d'"' -f4 || echo "")
    if [ "$status" = "ready" ]; then
      echo "✓ dropbear-ssh 已就绪" >&2
      return 0
    fi
  done
  echo "✗ dropbear-ssh 启动超时，请检查" >&2
  return 1
}

# SSH 执行（askpass 注入空密码）
ssh_exec() {
  SSH_ASKPASS="$ASKPASS" SSH_ASKPASS_REQUIRE=force setsid -w ssh $SSH_OPTS -p "$REMOTE_PORT" "${REMOTE_USER}@${REMOTE_HOST}" "$@"
}

# SFTP 文件传输
sftp_put() {
  local_file="$1"; remote_path="$2"
  SSH_ASKPASS="$ASKPASS" SSH_ASKPASS_REQUIRE=force setsid -w sftp $SSH_OPTS -P "$REMOTE_PORT" \
    "${REMOTE_USER}@${REMOTE_HOST}" <<EOF
put "$local_file" "$remote_path"
EOF
}

sftp_get() {
  remote_path="$1"; local_file="$2"
  SSH_ASKPASS="$ASKPASS" SSH_ASKPASS_REQUIRE=force setsid -w sftp $SSH_OPTS -P "$REMOTE_PORT" \
    "${REMOTE_USER}@${REMOTE_HOST}" <<EOF
get "$remote_path" "$local_file"
EOF
}

ensure_dropbear

case "${1:-}" in
  -f|--put)
    shift
    sftp_put "$@"
    ;;
  -g|--get)
    shift
    sftp_get "$@"
    ;;
  "")
    # 交互式 shell
    ssh_exec
    ;;
  *)
    # 执行远程命令
    ssh_exec "$@"
    ;;
esac
