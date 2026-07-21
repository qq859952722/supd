#!/bin/bash
# readiness 检查脚本
# 退出码 0 = 就绪，非 0 = 未就绪
# 检查 /tmp/script-ready-demo.ready 文件是否存在

if [ -f /tmp/script-ready-demo.ready ]; then
    exit 0
else
    exit 1
fi
