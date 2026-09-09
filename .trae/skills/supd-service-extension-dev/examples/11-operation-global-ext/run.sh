#!/bin/bash
# operation-global-ext: 操作中心全局扩展示例
# 由操作中心触发，演示：
#   1) 通过 SUPD_OPERATION 区分当前执行的操作；
#   2) 读取操作参数 SUPD_OPERATION_PARAMS（缺省 "{}"）；
#   3) 使用 ::notify:: 协议输出通知（进程 stdout 唯一入口）。
# SUPD_OPERATION_PHASE 恒为 global；全局阶段每次操作只执行一次。
set -u

echo "== operation-global-ext =="
echo "Operation:       ${SUPD_OPERATION}"
echo "Phase:           ${SUPD_OPERATION_PHASE}"
echo "Execution ID:    ${SUPD_OPERATION_EXECUTION_ID}"
echo "NotificationTopic: ${SUPD_NOTIFICATION_TOPIC_ID}"

# 演示读取操作参数。注意 os.Environ 原样注入，参数为紧凑 JSON 字符串。
# 参数含双引号时按"所见即所得"语义原样进入 content，超出协议行长/引号不闭合会被当作普通日志。
printf '::notify:: info "操作 [%s] 已开始"\n' "$SUPD_OPERATION"
printf '::notify:: info "params: %s\n' "$SUPD_OPERATION_PARAMS"

case "$SUPD_OPERATION" in
    check-software)
        # 演示 info / warning / success 三种等级通知
        echo "检查软件更新..."
        sleep 0.3
        printf '::notify:: warning "发现 2 个可用更新"\n'
        printf '::notify:: success "检查完成"\n'
        ;;
    update-software)
        echo "安装更新..."
        sleep 0.3
        printf '::notify:: warning "开始安装更新"\n'
        printf '::notify:: success "更新完成"\n'
        ;;
    *)
        echo "Unknown operation: $SUPD_OPERATION"
        exit 1
        ;;
esac

exit 0