# 服务名称与版本

- 服务名称：`web-demo`
- 版本：`1.0.0`
- 用途：提供带 HTTP 就绪检测的简单 Python Web 服务示例。

# 目录结构与权限边界

- `service.yaml`、`env.yaml` 和 `package.*.yaml` 保存控制面配置，由管理员或 supd 管理。
- `bin/run.py` 为示例程序；实际可执行程序应放在 `bin/`，服务用户仅可读取和执行。
- `data/` 用于服务运行数据，服务用户可读写；不要授予服务用户修改服务配置或扩展脚本的权限。
- `extensions/health-action/` 是服务级扩展，由目录位置自动关联本服务。

# 启动方式与就绪检测

将目录部署为服务目录后由 supd 按 `service.yaml` 的 `python3 ./bin/run.py` 启动。服务通过 `http://127.0.0.1:9001/health` 的 HTTP 200 响应确认就绪。

# 配置与环境变量

`env.yaml` 定义 `APP_HOST` 和 `APP_PORT`，供服务及服务级扩展读取。每个变量均使用 `env.KEY.value` 对象结构；若需要敏感变量，只记录变量名称和用途，例如 `API_TOKEN` 用于访问外部 API，不在本文档写入其值。

# 服务级扩展与 Actions

服务级扩展 `health-action` 提供 `check` action，通过 `APP_HOST` 和 `APP_PORT` 请求 `/health`。扩展关联由 `extensions/health-action/` 的目录位置决定。

# 数据持久化与升级更新

示例不持久化业务数据。实际服务应将可保留配置和状态置于 `data/`；升级时替换 `bin/` 中的版本化载荷并保留数据。默认 profile 排除全部 `data/`；`migrate` profile 保留未被显式排除的配置数据，并排除状态、缓存和证书。

# 常用运维操作

使用 supd CLI 操作：`supd start web-demo`、`supd stop web-demo`、`supd restart web-demo`。通过 `supd logs web-demo` 查看服务日志。

# 安全与备份注意事项

按最小权限运行服务，不在服务目录或本文档存放密码、token、私钥和运行期敏感数据。升级前备份 `data/` 中需保留的配置和状态。
