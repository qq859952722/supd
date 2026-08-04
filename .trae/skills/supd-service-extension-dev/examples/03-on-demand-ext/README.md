# 扩展概述

`on-demand-ext` 版本 `1.0.0`，演示 Bash 手动扩展的多 Action 配置。

# 触发方式与 Actions

通过 `on_demand` 手动触发。`greet` 输出问候，`status` 输出服务上下文并尝试健康检查；未显式配置并发策略。

# 运行逻辑

脚本按 `SUPD_ACTION` 分支；`status` 输出服务名、目录、脚本 PID 和时间，若存在 `curl` 则请求本机服务健康端点。

# 配置与环境变量

读取 `SUPD_ACTION`、`SUPD_SERVICE`、`SUPD_SERVICE_DIR`；`SERVICE_PORT` 可配置目标端口，默认 `8080`。

# 开发与外部资源

依赖 Bash、`date`，健康检查可选依赖 `curl`；无上游项目或下载链接。

# 部署与特别注意事项

作为全局扩展示例时入口为 `extensions/on-demand-ext/run.sh`，复制后目录名须与 `name` 一致。没有 `curl` 时状态查询会跳过健康检查。

# 验证与故障排查

分别触发 `greet` 和 `status`，检查输出；健康状态异常时核对 `SERVICE_PORT`、目标服务和 `curl` 可用性。

# 变更记录

- 2026-08-04：建立扩展开发维护手册。
