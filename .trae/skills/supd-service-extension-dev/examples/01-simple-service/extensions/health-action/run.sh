#!/bin/bash
set -eu

host="${APP_HOST:-127.0.0.1}"
port="${APP_PORT:-9001}"

if curl --fail --silent --show-error "http://${host}:${port}/health" >/dev/null; then
    echo '::result:: success "服务健康检查通过"'
else
    echo '::result:: error "服务健康检查失败"'
    exit 1
fi
