# MEMORY.md — supd 项目长期记忆

## 审计任务（tmp/audit_plan.md）
- 审计方案：`tmp/audit_plan.md`（v1.1，含 A–Q 类 + 打分体系）。唯一可写路径：`tmp/`；严禁改 .go/.tsx/.ts/.css/go.mod/go.sum/package.json/docs/。
- 打分：P 类 0.5 分（P-01/02/03 各 ≈0.17），Q 类 0.5 分（Q-01/02 各 0.25）。扣减：🔴=0、🟠=-50~80%、🟡=-15~30%、🔵=-0~10%。
- 报告文件约定：tmp/audit_results_P_ux.md、tmp/audit_results_Q_docs.md、tmp/审计报告.md（最终汇总，需全类完成后才完整）。
- 已完成：P 类与 Q 类报告（2026-07-24）。A–O 类尚未审计。

## 工具踩坑（跨会话必读）
- **search_content 工具在本项目持续失效**（返回 0 命中，含实际存在的模式如括号/".go"）。改用 `execute_command` 执行 `grep -rn ... --include="*.go"` 做内容检索，可靠。
- **mv 事故**：`mv a b c d/` 多源会全部移入末目录。一律一次一个源一个目标。

## 关键代码事实
- `EventPublisher.Publish(eventType string, payload any)` 签名（payload 是 `any`，非 `map[string]any`）。core 包用 `sm.publisher.Publish(...)`。
- 14 种事件类型全部实际发布（grep .Publish 确认）；system_resource_warning 仅磁盘（run.go:715）。
- 错误码 22 个，后端 DefaultMessages 中英混杂（P-01-001 待修）；CLI/前端均有纯中文覆盖。
