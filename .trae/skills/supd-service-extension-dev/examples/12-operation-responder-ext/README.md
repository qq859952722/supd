# 扩展概述

`operation-responder-ext` 版本 `1.0.0`，是一个**服务级扩展**，用于演示操作中心（Operations Center）的**服务阶段响应**能力：通过 `actions[].operations` 绑定响应某个已注册操作，在服务上下文中执行各自一次并通过 `::notify::` 汇报结果。

# 触发方式与 Actions

服务扩展**不生成操作按钮**（按钮由全局扩展注册）。本扩展提供一个 action，其 `operations` 数组绑定监听操作 `check-software`：

| action id | label | button_style | 绑定的操作 ID |
|---|---|---|---|
| `respond-check` | 响应检查更新 | `default` | `check-software` |

- **服务阶段**：两阶段执行的第二步。当某操作进入服务阶段时，supd 会遍历所有服务，为每个"绑定响应该操作的服务扩展"按其 `operations` 匹配执行一次。
- 与触发方式无关，这里 `concurrency: replace`（连续触发时保留最新一次）。

# 运行逻辑

脚本在服务阶段执行，`SUPD_OPERATION_PHASE` 为 `service`，且 `SUPD_SERVICE` / `SUPD_SERVICE_DIR` 已注入。脚本尝试读取所属服务 PID 文件模拟"服务交互"，随后向 stdout 打印 `::notify::` 通知（info 开始 / success 完成）。

# 配置与环境变量

读取以下变量：

| 变量 | 说明 |
|---|---|
| `SUPD_OPERATION` | 当前操作 ID（本示例为 `check-software`） |
| `SUPD_OPERATION_PHASE` | 操作阶段（此处为 `service`） |
| `SUPD_OPERATION_EXECUTION_ID` | 本次操作执行 Execution UUID（与全局阶段共享同一 Execution） |
| `SUPD_NOTIFICATION_TOPIC_ID` | 本操作 Execution 关联的通知 Topic UUIDv7 |
| `SUPD_SERVICE` | 所属服务名 |
| `SUPD_SERVICE_DIR` | 所属服务工作目录绝对路径 |

# 开发与外部资源

无外部依赖，仅需 Bash、`cat`。无上游项目或下载链接。

# 部署与特别注意事项

- 这是**服务级扩展**，放入所属服务目录 `<baseDir>/services/<svc>/extensions/operation-responder-ext/`（目录名须与 `name` 一致）；关联由目录位置决定，无需 `service:` 字段。
- **两阶段语义**：服务阶段在全局阶段全部终态后才执行；每个服务只响应执行一次（多个服务各自一次，不互相复用）。
- 若绑定的操作 ID 未在任何全局扩展注册，supd 会记录 warning 并忽略该响应，不执行。

# 验证与故障排查

1. 触发对应操作 `POST /api/operations/check-software/run`。
2. 执行完成后 `GET /api/operation-executions/{id}` 应含 `service` 阶段 Run，`GET /api/extensions/runs/{runID}` 含 `::notify::` 输出。
3. 通知落库后 `GET /api/notifications/topics` 可见对应 Topic。

# 变更记录

- 2026-09-09：建立操作中心服务响应扩展示例（两阶段语义与服务阶段响应演示）。