# 扩展概述

`binary-updater` 版本 `1.0.0`，演示 tjs 服务级二进制版本检测、流式下载、备份和原子替换。

# 触发方式与 Actions

通过 `on_demand` 手动触发。`check-update` 只比较版本，`update` 仅在远端版本更高时安装，`force-update` 强制下载安装；并发策略为 `serialize`。

# 运行逻辑

脚本运行 `${SUPD_SERVICE_DIR}/bin/${BINARY_NAME} --version` 获取本地版本，并请求 `RELEASES_API` 读取 `tag_name` 或 `version`。下载 URL 由 `DOWNLOAD_URL_TEMPLATE` 将 `{version}` 和 `{arch}` 替换后生成；文件流式写入 tjs 临时目录，尝试用系统 `tar` 解压，失败则按直接二进制处理。安装前把旧文件改名为 `.bak`，再原子移动新文件，替换失败时尝试恢复备份。

# 配置与环境变量

必须有 `SUPD_SERVICE_DIR`。可配置 `RELEASES_API`、`BINARY_NAME`、`DOWNLOAD_URL_TEMPLATE`、`CHECKSUM_URL`、`MAX_BYTES`；默认 API 和下载模板使用 `example.com` 占位地址，部署前必须替换。架构生成值为 `aarch64` 或回退的 `x86_64`。

# 开发与外部资源

依赖 `tjs` runtime 和系统 `tar`。实际上游、发布 API 与下载链接：无，示例默认 URL 均为占位值。`CHECKSUM_URL` 仅控制进入示例校验分支，脚本未实现真实 SHA-256 计算。

# 部署与特别注意事项

应作为服务级扩展部署，配置以 `root` 运行并写入所属服务 `bin/`。上线前必须替换占位 API/URL，确认归档成员名等于 `BINARY_NAME`、目标架构正确，并先检查下载二进制的 libc 兼容性。默认最大下载大小为 200MB。

# 验证与故障排查

先用 `check-update` 验证本地版本输出和发布 API，再在测试服务执行 `update`，检查 `.bak`、新文件权限和版本。失败时检查 `SUPD_SERVICE_DIR`、网络、API 响应、模板替换、`tar`、目录权限及扩展日志；不要把脚本中的 SHA-256 提示视为真实校验结果。

# 变更记录

- 2026-08-04：建立扩展开发维护手册。
