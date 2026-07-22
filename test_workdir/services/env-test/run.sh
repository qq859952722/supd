#!/bin/bash
# 将环境变量写入文件以验证 env.yaml 加载
{
  echo "=== ENV TEST $(date) ==="
  echo "ENV_TEST_VAR=${ENV_TEST_VAR:-<not set>}"
  echo "ENV_DISABLED_VAR=${ENV_DISABLED_VAR:-<not set>}"
  echo "ENV_PASSWORD=${ENV_PASSWORD:-<not set>}"
  echo "ENV_SPECIAL=${ENV_SPECIAL:-<not set>}"
  echo "GLOBAL_TEST_VAR=${GLOBAL_TEST_VAR:-<not set>}"
} >> /tmp/env-test-output.txt 2>&1
