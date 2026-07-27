#!/bin/bash
# concurrency-demo-ext: serialize 策略演示
# 多次触发会被排队，每次执行约 2 秒，可在前端 runs 列表观察串行排队

echo "::progress:: 0 \"开始执行（serialize 演示）\""
sleep 1
echo "::progress:: 50 \"执行中（已等待 1s）\""
sleep 1
echo "::progress:: 100 \"执行完成\""
echo "::result:: success \"serialize 演示完成（共耗时约 2s）\""
