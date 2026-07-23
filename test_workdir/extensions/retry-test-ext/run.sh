#!/bin/bash
# retry-test-ext: 总是失败，用于验证 retry_on_failure 重试链
echo "retry-test-ext: failing on purpose (exit 1)"
exit 1
