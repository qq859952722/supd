# 扩展概述

`tjs-demo` 版本 `1.0.0`，演示 tjs 的环境访问、HTTP fetch、文件读写、路径模块和 stdout 协议。

# 触发方式与 Actions

通过 `on_demand` 手动触发 `run` action；并发策略为 `replace`。

# 运行逻辑

脚本输出 tjs 版本、工作目录和服务目录，请求 GitHub API 获取 txiki.js 仓库信息；随后写入并读回 `/tmp/tjs-demo-output.txt`，演示 `tjs:path` 后输出成功结果。网络请求失败不会中止演示。

# 配置与环境变量

读取 `SUPD_ACTION` 和可选的 `SUPD_SERVICE_DIR`，无需自定义环境变量。

# 开发与外部资源

运行时为 `tjs`；外部 API 为 `https://api.github.com/repos/saghul/txiki.js`，开发参考为 Skill 内 `references/06_tjs_runtime_guide.md`，无下载链接。

# 部署与特别注意事项

部署前须在 supd runtimes 中配置可用的 `tjs`。运行环境需允许访问 GitHub API，并允许写入 `/tmp`；输出文件名固定，重复执行会覆盖。

# 验证与故障排查

触发 `run`，确认进度、文件读回信息和 success 结果。失败时检查 tjs runtime 和 `/tmp` 权限；仅 GitHub 请求失败时脚本会记录信息后继续。

# 变更记录

- 2026-08-04：建立扩展开发维护手册。
