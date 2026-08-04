# 扩展概述

`supd-startup-hook` 版本 `1.0.0`，演示 supd 启动通知和关闭清理钩子。

# 触发方式与 Actions

supd `post_ready` 触发 `notify`，`pre_shutdown` 触发 `cleanup`；并发策略为 `parallel`。

# 运行逻辑

`notify` 读取磁盘和内存信息并输出进度与结果；`cleanup` 查找并删除 `/tmp/supd-*` 普通文件，随后输出模拟的通知、状态保存和汇总信息。

# 配置与环境变量

读取 `SUPD_ACTION`，未提供时执行 `notify`；无需自定义环境变量。

# 开发与外部资源

依赖 Bash，以及 `sleep`、`df`、`free`、`find`、`wc`、`awk` 等系统命令；无上游项目或下载链接。

# 部署与特别注意事项

作为全局扩展部署，入口为 `extensions/supd-lifecycle-ext/run.sh`。`cleanup` 会删除匹配 `/tmp/supd-*` 的普通文件，使用前应确认该范围符合部署环境要求；脚本中的扩展数和定时任务数是演示输出，不是动态探测结果。

# 验证与故障排查

在测试环境触发 `notify` 并观察资源信息；关闭 supd 前验证 `cleanup`。命令缺失或权限不足时检查扩展日志和 `/tmp` 权限。

# 变更记录

- 2026-08-04：建立扩展开发维护手册。
