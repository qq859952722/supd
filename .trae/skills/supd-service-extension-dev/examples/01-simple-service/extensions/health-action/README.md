# 扩展概述

`health-action` 版本 `1.0.0`，用于查询所属服务的 HTTP 健康状态。

# 触发方式与 Actions

通过 `on_demand` 手动触发，仅提供 `check` action；并发策略为 `replace`。

# 运行逻辑

Bash 脚本使用 `curl` 请求 `http://${APP_HOST}:${APP_PORT}/health`，成功时输出 success 结果，失败时输出 error 并返回非零状态。

# 配置与环境变量

读取 `APP_HOST`（默认 `127.0.0.1`）和 `APP_PORT`（默认 `9001`）。

# 开发与外部资源

依赖 Bash 和 `curl`；无上游项目或下载链接。

# 部署与特别注意事项

这是服务级扩展，应置于所属服务的 `extensions/health-action/`，入口为扩展目录下的 `run.sh`。

# 验证与故障排查

触发 `check`，确认目标服务已启动且 `/health` 可访问；失败时核对主机、端口、`curl` 和服务日志。

# 变更记录

- 2026-08-04：建立扩展开发维护手册。
