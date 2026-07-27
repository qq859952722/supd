#!/bin/bash
# debounce-demo-ext: debounce:5s 策略演示
# 5 秒内多次触发会被合并为最后一次执行

echo "::progress:: 50 \"debounce 触发执行\""
sleep 1
echo "::result:: success \"debounce 演示完成（仅最后一次触发执行了）\""
