#!/usr/bin/env bash
# pre-start-fixperms — Transmission
# 以 root 运行（run_as: root），在服务主进程(nobody)启动前修复权限：
#   1) 修复配置目录 $SVCDIR/config（-g config）及工作目录本身的属主 → nobody
#   2) 修复 bin/ 与 web/ 目录属主 → uid:gid 1000（transmission-updater 扩展写入管理）
#   3) 创建并修复下载目录（/Downloads/Tdown、/Downloads/inTdown）及目录以下的权限 → nobody
set -uo pipefail

SVCDIR="${SUPD_SERVICE_DIR:-/etc/supd/services/transmission}"
DOWNLOAD_DIRS=("/Downloads/Tdown" "/Downloads/inTdown")

echo "::progress:: 10 \"修复 Transmission 配置目录权限 ($SVCDIR/config) ...\""
mkdir -p "$SVCDIR/config"
chown -R nobody:nobody "$SVCDIR/config"
chmod -R u=rwX,g=rwX,o=rX "$SVCDIR/config"
# 工作目录本身也改为 nobody，确保主进程可将 settings.json 写回
chown nobody:nobody "$SVCDIR"
chmod u=rwX,g=rwX,o=rX "$SVCDIR"

echo "::progress:: 30 \"修复 bin/ 与 web/ 目录权限（uid 1000 管理，nobody 可读执行）...\""
# bin/ 和 web/ 由 transmission-updater 扩展（run_as_uid: 1000）写入管理
# nobody 主进程通过 o=rX 权限读取/执行二进制与 WebUI 静态资源
mkdir -p "$SVCDIR/bin" "$SVCDIR/web"
chown -R 1000:1000 "$SVCDIR/bin" "$SVCDIR/web" 2>/dev/null || true
chmod -R u=rwX,g=rwX,o=rX "$SVCDIR/bin" "$SVCDIR/web" 2>/dev/null || true
echo "  已修复: $SVCDIR/bin (1000:1000), $SVCDIR/web (1000:1000)"

echo "::progress:: 60 \"创建并修复下载目录权限...\""
for d in "${DOWNLOAD_DIRS[@]}"; do
  mkdir -p "$d"
  chown -R nobody:nobody "$d"
  chmod -R u=rwX,g=rwX,o=rX "$d"
  echo "  已修复: $d"
done

echo "::progress:: 100 \"权限修复完成\""
echo "::result:: success \"Transmission 权限已修复：config/→nobody, bin/+web/→uid1000, 下载目录→nobody\""

