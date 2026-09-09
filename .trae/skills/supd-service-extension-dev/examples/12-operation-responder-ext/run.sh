#!/bin/bash
# operation-responder-ext: 操作中心服务响应扩展示例
# 在操作的服务阶段执行，SUPD_OPERATION_PHASE 为 service，且按所属服务各执行一次。
# 全局扩展负责注册与发起操作；本扩展负责在服务上下文中"响应"该操作。
#
# 注意：SUPD_SERVICE / SUPD_SERVICE_DIR 仅在 service_lifecycle 触发时注入，
# 操作服务阶段（EventType=on_demand）可能未注入，故用默认值保护（见 05_env_spec.md）。
set -u

SVC="${SUPD_SERVICE:-unknown}"
SVC_DIR="${SUPD_SERVICE_DIR:-}"

echo "== operation-responder-ext (service responder) =="
echo "Service:         $SVC"
echo "Service Dir:     ${SVC_DIR:-(not set)}"
echo "Operation:       ${SUPD_OPERATION}"
echo "Phase:           ${SUPD_OPERATION_PHASE}"
echo "Execution ID:    ${SUPD_OPERATION_EXECUTION_ID}"

# 模拟服务交互（示例中仅回显服务目录下的进程信息，避免硬编码端口）
if [ -n "$SVC_DIR" ] && [ -d "$SVC_DIR" ]; then
    if [ -f "$SVC_DIR/.supd/pid" ]; then
        echo "service pid file: $(cat "$SVC_DIR/.supd/pid" 2>/dev/null || echo unknown)"
    else
        echo "service pid file not present; simulating interaction"
    fi
fi

printf '::notify:: info "服务 [%s] 正在响应检查更新"\n' "$SVC"
printf '::notify:: success "服务 [%s] 检查完成，无更新需安装"\n' "$SVC"

exit 0