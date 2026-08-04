# 扩展概述

`health-check-ext` 版本 `1.0.0`，提供 HTTP 快速健康检查和故障诊断。

# 触发方式与 Actions

支持 `on_demand`；服务 `post_ready` 触发 `check`，`on_failure` 触发 `diagnose`。Actions 为 `check`、`diagnose`，并发策略为 `replace`。

# 运行逻辑

`check` 请求健康端点并按 HTTP 状态输出结果；`diagnose` 使用 `pgrep`、可选的 `ss` 和 `curl` 检查进程、端口与 HTTP 响应，通过 stdout 协议报告进度和结论。

# 配置与环境变量

`SERVICE_PORT` 默认 `8080`，`HEALTH_PATH` 默认 `/health`；还读取 `SUPD_ACTION` 和 `SUPD_SERVICE`。

# 开发与外部资源

依赖 Bash、`curl`、`pgrep`，端口诊断可选依赖 `ss`；无上游项目或下载链接。

# 部署与特别注意事项

生命周期触发需要部署为目标服务的服务级扩展。进程诊断按服务名模糊匹配，结果仅供排查；`ss` 不可用时端口检查会标记跳过。

# 验证与故障排查

手动触发 `check` 和 `diagnose`，并分别在服务正常与不可达时检查结果。异常时核对端口、路径、服务名及 `curl`、`pgrep`、`ss` 可用性。

# 变更记录

- 2026-08-04：建立扩展开发维护手册。
