# 服务概述

`web-demo` 版本 `1.0.0`，是使用 Python 标准库提供 HTTP 接口的简单服务示例。

# 运行逻辑

`bin/run.py` 在 `0.0.0.0:9001` 监听请求；`/health` 返回 HTTP 200 和 JSON，其余路径返回普通文本。

# 目录结构与权限边界

`service.yaml`、`env.yaml` 和 `package.*.yaml` 由管理员维护；`bin/run.py` 只读执行；运行数据应写入可读写的 `data/`。`extensions/health-action/` 是服务级扩展。

# 启动方式与就绪检测

supd 按 `python3 ./bin/run.py` 启动。服务不自动启动；启动后以 `http://127.0.0.1:9001/health` 返回 HTTP 200 作为就绪条件。

# 配置与环境变量

`env.yaml` 声明 `APP_HOST=127.0.0.1`、`APP_PORT=9001`，供服务级扩展使用。当前 `run.py` 固定监听 `0.0.0.0:9001`，修改端口时须同步脚本、就绪 URL 和环境变量。

# 开发与上游资源

程序仅使用 Python 标准库，无上游项目、下载链接或外部开发资源。

# 服务级扩展与 Actions

`health-action` 由 `on_demand` 触发，提供 `check` action，使用 `curl` 请求由 `APP_HOST`、`APP_PORT` 组成的 `/health` 地址。

# 数据持久化与升级更新

示例不持久化业务数据。实际数据应放入 `data/`；升级仅替换 `bin/` 载荷并保留数据。默认 profile 排除全部 `data/`，`migrate` profile 排除状态、缓存和证书等内容。

# 部署与特别注意事项

部署目录名须与 `name: web-demo` 一致，并确保 Python 3 与扩展所需的 `curl` 可用。不要把运行数据写入 `bin/`。

# 常用运维操作

使用 `supd start web-demo`、`supd stop web-demo`、`supd restart web-demo` 管理服务，使用 `supd logs web-demo` 查看日志。

# 安全与备份注意事项

按最小权限运行，不在 README、配置或日志中保存密码、token、私钥。升级前备份 `data/` 中需要保留的内容。

# 变更记录

- 2026-08-04：按维护手册规范补充运行逻辑、开发资源、部署注意和变更记录。
