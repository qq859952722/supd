# 扩展概述

`service-lifecycle-ext` 版本 `1.0.0`，演示服务生命周期事件的 Bash 钩子。

# 触发方式与 Actions

`post_ready` 触发 `on-ready`，`on_failure` 触发 `on-fail`，`pre_stop` 触发 `on-stop`；未显式配置并发策略。

# 运行逻辑

脚本按 `SUPD_ACTION` 输出服务就绪、失败或停止信息，并附带可用的 PID、退出码和时间。

# 配置与环境变量

读取 `SUPD_ACTION`、`SUPD_SERVICE`、`SUPD_SERVICE_PID` 和 `SUPD_SERVICE_EXIT_CODE`，无需自定义配置。

# 开发与外部资源

依赖 Bash 和 `date`；无上游项目或下载链接。

# 部署与特别注意事项

该类扩展需部署为服务级扩展才能获得所属服务上下文；入口相对服务根路径须与实际目录一致。

# 验证与故障排查

依次让所属服务进入 ready、failed 和 stopping，检查对应 action 日志；上下文缺失时核对扩展目录位置和触发事件。

# 变更记录

- 2026-08-04：建立扩展开发维护手册。
