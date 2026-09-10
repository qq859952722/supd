# 扩展概述

`operation-global-ext` 版本 `1.0.0`，是一个**全局扩展**，用于演示操作中心（Operations Center）的**操作注册（全局阶段）**能力：通过 `actions[].operations` 注册多个操作按钮，在执行中读取操作参数（`SUPD_OPERATION_PARAMS`）并通过 `::notify::` 协议输出通知。

# 触发方式与 Actions

操作由操作中心触发（不是 `on_demand` 按钮直接触发）。扩展提供两个 action，每个 action 各带 `operations`：

| action id | label | button_style | 注册的操作 ID |
|---|---|---|---|
| `check-update` | 检查更新 | `default` | `check-software` |
| `update-software` | 安装更新 | `danger` | `update-software` |

- **全局阶段**：两阶段执行的第一步。全局扩展的注册操作会先按稳定顺序**全部终态后再进入服务阶段**；全局阶段每个操作**只执行一次**（不会被复制到每个服务）。
- `concurrency` 采用默认 `replace`；执行超时 30s。

# 运行逻辑

脚本由操作中心触发，`SUPD_OPERATION` 区分当前操作（`check-software` / `update-software`），`SUPD_OPERATION_PHASE` 恒为 `global`。脚本读取 `SUPD_OPERATION_PARAMS`（操作参数紧凑 JSON，缺省 `{}`）并向 stdout 打印 `::notify::` 协议通知（info 开始 / warning 发现 / success 完成）。

# 配置与环境变量

读取以下 `SUPD_*` 操作上下文变量（操作 Run 时注入）：

| 变量 | 说明 |
|---|---|
| `SUPD_OPERATION` | 当前操作 ID |
| `SUPD_OPERATION_PARAMS` | 操作参数 JSON 对象字符串（≤8KB，缺省 `{}`） |
| `SUPD_OPERATION_PHASE` | 操作阶段（此处恒为 `global`） |
| `SUPD_OPERATION_EXECUTION_ID` | 本次操作执行 Execution UUID |
| `SUPD_NOTIFICATION_TOPIC_ID` | 本操作 Execution 关联的通知 Topic UUIDv7 |

另含通用 `SUPD_EVENT` / `SUPD_TRIGGER_SOURCE` / `SUPD_TRIGGER_USER` / `SUPD_RUN_ID` / `SUPD_EXTENSION_NAME` / `SUPD_ACTION` 等。

# 开发与外部资源

无外部依赖，仅需 Bash、`sleep`。无上游项目或下载链接。

# 部署与特别注意事项

- 这是**全局扩展**，放入 `<baseDir>/extensions/operation-global-ext/`（目录名须与 `name` 一致）。
- 操作通知仅经 stdout 的 `::notify:: <level> "<content>"` 协议产生；参数含双引号时按"所见即所得"原样进入 content，超出 8192 字节或引号不闭合将被当作普通日志，不生成通知。
- **两阶段语义**：全局阶段所有注册操作执行完毕（全部终态）后，supd 才进入服务阶段，逐个服务执行响应该操作的**服务级扩展**。全局扩展不参与服务阶段。
- 同 ID 由不同全局扩展重复注册时合并为一个按钮（以稳定排序第一项为准）。

# 验证与故障排查

1. 启动 supd 后 `GET /api/operations` 应返回 `check-software`、`update-software` 两个已注册操作。
2. `POST /api/operations/{id}/run` 触发，之后查 `GET /api/operation-executions` 可见 `global` 阶段 Execution，Run 详情 `GET /api/extensions/runs/{runID}` 含 `::notify::` 输出。
3. 通知落库后 `GET /api/notifications/topics` 可见与执行关联的 Topic。

# 变更记录

- 2026-09-09：建立操作中心全局扩展示例（两阶段语义与 `::notify::` 演示）。