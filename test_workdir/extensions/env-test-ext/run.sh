#!/bin/bash
# env-test-ext: 验证扩展 env.yaml 环境变量注入
# 将所有 EXT_TEST_* 环境变量写入文件以验证注入情况

{
  echo "=== EXT ENV TEST $(date) ==="
  echo "EXT_TEST_VAR=${EXT_TEST_VAR:-<not set>}"
  echo "EXT_DISABLED_VAR=${EXT_DISABLED_VAR:-<not set>}"
  echo "EXT_PASSWORD=${EXT_PASSWORD:-<not set>}"
  echo "EXT_SPECIAL=${EXT_SPECIAL:-<not set>}"
  echo "GLOBAL_TEST_VAR=${GLOBAL_TEST_VAR:-<not set>}"
} >> /tmp/ext-env-test-output.txt 2>&1

echo "::progress:: 50 \"环境变量已写入文件\""
echo "::result:: success \"扩展环境变量测试完成，查看 /tmp/ext-env-test-output.txt\""
