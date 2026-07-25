# supd 扩展配置与规范指南 (meta.yaml & run.sh)

本参考文档包含 `meta.yaml` 的完整字段定义、4 种触发器类型配置、14 个 `SUPD_*` 环境变量、stdout 通讯协议规范及检查清单。

---

## 1. 扩展目录结构与位置

- **全局扩展**：`<baseDir>/extensions/<ext-name>/`（跨服务通用）
- **服务级扩展**：`<baseDir>/services/<svc>/extensions/<ext-name>/`（绑定特定服务）

```
<ext-name>/
├── meta.yaml          # 必需：扩展配置与触发器元数据
├── run.sh             # 必需：入口脚本（需 chmod +x）
├── env.yaml           # 可选：扩展专属环境变量（需包含 env: 包装层）
└── <辅助资源/代码>     # 随扩展一同执行或打包
```

---

## 2. meta.yaml 完整字段参考

**必填字段**：`name`、`version`、`runtime`、`entry`、`timeout_seconds`

| 字段 | 类型 | 默认值 | 说明与约束 |
|---|---|---|---|
| `name` | string | 必填 | 扩展名称，必须匹配 `^[a-z][a-z0-9-]*$` 且与目录名一致 |
| `version` | string | 必填 | 扩展版本号，如 `"1.0.0"` |
| `description` | string | `""` | 扩展功能描述 |
| `enabled` | bool | `true` | 是否启用该扩展 |
| `runtime` | string | 必填 | 运行时环境（如 `bash`, `python`, `node`, `tjs` 等） |
| `entry` | string | 必填 | 入口脚本相对路径（如 `run.sh` 或 `main.py`），须具备执行权限 |
| `timeout_seconds` | int | `600` | 单次运行超时时限（硬上限 1800 秒） |
| `run_as` | string | `""` | 运行身份（User 模式）：`root` / `<用户名>` / 空（服务级扩展继承服务身份，全局扩展继承 supd 用户）。与 `run_as_uid` 互斥 |
| `run_as_uid` | int | `0` | 直接指定 uid（UID 模式，与 `run_as` 互斥，不查 /etc/passwd，适用于 NAS 固定 uid 服务）；`0`=未设置 |
| `run_as_gid` | int | `0` | 直接指定 gid（UID 模式下可选，`0`=等于 `run_as_uid`） |
| `run_as_groups` | list[int] | `[]` | 补充组 gid 列表（UID 模式下可选） |
| `concurrency` | string | `"replace"` | 并发控制策略：`replace` / `serialize` / `parallel` / `debounce:Ns` |
| `ui.show_logs` | bool | `true` | 前端 UI 是否实时展示日志 |
| `ui.button_style` | string | `"default"` | 前端按钮样式：`primary` / `default` / `danger` |
| `ui.icon` | string | `""` | 前端图标名称 |
| `actions` | list[struct] | `[]` | 扩展导出的 Action 动作列表（`id`, `label`, `button_style`, `args`） |
| `triggers` | struct | nil | 触发器配置 |

> **注意**：`actions[].icon` 字段为规格要求但代码暂未实现（详见偏差台账 DEV-012），配置时可省略该字段。`actions[].id` 必须唯一。

> **身份配置说明**（§2.2.13）：
> - **User 模式**（`run_as`）：通过用户名查找，值为 `root` / `<用户名>` / 空。
> - **UID 模式**（`run_as_uid`/`run_as_gid`/`run_as_groups`）：直接指定数字，不查 `/etc/passwd`。`run_as_gid=0` 表示等于 `run_as_uid`。
> - **互斥**：`run_as` 与 `run_as_uid` 不能同时指定，配置校验报错。
> - **继承规则**：`run_as`/`run_as_uid` 为空时，服务级扩展继承所属服务的身份，全局扩展继承 supd 启动用户。
> - **非 root 语义（宽松警告）**：supd 非 root 启动时，`run_as`/`run_as_uid` 指定其他用户仅**记录警告并降级为当前用户**运行（区别于服务的严格拒绝）。

---

## 3. 4 种触发器类型配置示例

```yaml
# 1. on_demand — 手动触发（UI 按钮或 API 调用）
triggers:
  on_demand: true

# 2. on_schedule — cron 定时任务 (标准 5 段表达式)
triggers:
  on_schedule:
    - cron: "0 */5 * * *"
      action: ping

# 3. service_lifecycle — 服务生命周期事件
# 事件类型限定为: pre_start | post_ready | on_failure | pre_stop
triggers:
  service_lifecycle:
    - event: post_ready
      action: on-ready
    - event: on_failure
      action: on-fail
    - event: pre_stop
      action: on-stop

# 4. supd_lifecycle — supd 系统生命周期事件
# 事件类型限定为: pre_start | post_ready | pre_shutdown
triggers:
  supd_lifecycle:
    - event: pre_start
      action: on-startup
    - event: post_ready
      action: on-ready
    - event: pre_shutdown
      action: on-shutdown
```

> **提示**：服务级扩展自动由目录位置关联到所属服务，`meta.yaml` 中无需也不解析 `service` 字段。

---

## 4. 14 个 `SUPD_*` 环境变量

扩展进程启动时，系统将自动注入以下 14 个环境变量：

| 环境变量名 | 类型 | 说明与含义 |
|---|---|---|
| `SUPD_EVENT` | string | 触发事件类型（如 `post_ready`, `on_demand`, `on_schedule` 等） |
| `SUPD_TRIGGER_SOURCE` | string | 触发源标识 |
| `SUPD_TRIGGER_TIME` | string | 触发时间戳 (RFC3339) |
| `SUPD_TRIGGER_USER` | string | 触发用户（如 API 认证用户或 `system`） |
| `SUPD_RUN_ID` | string | 本次扩展运行的唯一任务 ID |
| `SUPD_EXTENSION_NAME` | string | 当前扩展名称 |
| `SUPD_ACTION` | string | 当前执行的 Action ID |
| `SUPD_PHASE` | string | 执行阶段标识 |
| `SUPD_SERVICE` | string | 所关联的服务名称（服务级扩展或生命周期触发） |
| `SUPD_SERVICE_PID` | string | 关联服务的当前进程 PID（`on_failure` 事件时为失败前的 PID） |
| `SUPD_SERVICE_DIR` | string | 关联服务的工作目录绝对路径 |
| `SUPD_SERVICE_EXIT_CODE` | string | 关联服务退出码（`on_failure` 时有效） |
| `SUPD_SERVICE_SIGNAL` | string | 关联服务信号 |
| `SUPD_SERVICE_RESTART_COUNT` | string | 关联服务的当前已重启次数 |

---

## 5. stdout 通讯协议与终态判定

扩展脚本输出到标准输出 (stdout) 时，supd 实时捕获并解析协议行：

```bash
#!/bin/bash
# 1. 进度上报 (0 - 100)
echo '::progress:: 50 "正在处理中..."'

# 2. 结果上报 (success / warning / error)
echo '::result:: success "数据同步完成"'

# 3. 普通标准输出日志
echo '正常打印执行日志'
```

### 任务终态判定优先级：
`timeout` / `killed` / `canceled` > `::result::` 协议判定 > `exit code` 退出码（0 成功，非 0 失败）。

---

## 6. meta.yaml 检查清单

- [ ] `name` 匹配 `^[a-z][a-z0-9-]*$` 且与所在目录名完全一致
- [ ] `entry` 脚本路径正确且已具备可执行权限（`chmod +x run.sh`）
- [ ] `timeout_seconds` ≤ 1800（硬上限）
- [ ] 触发器 `service_lifecycle.event` 仅使用 `pre_start`/`post_ready`/`on_failure`/`pre_stop`
- [ ] 触发器 `supd_lifecycle.event` 仅使用 `pre_start`/`post_ready`/`pre_shutdown`
- [ ] `actions[].id` 唯一且符合命名规范 `^[a-z][a-z0-9-]*$`
- [ ] `button_style` 属于 `primary`/`default`/`danger` 三者之一
- [ ] `on_schedule` 的 `cron` 表达式格式正确（标准 5 段）
- [ ] `concurrency` 策略符合规范（`debounce:Ns` 中 N 为秒数，不支持毫秒 `ms`）
- [ ] 若包含 `env.yaml`，必须使用 `env:` 包装层格式
- [ ] `run_as`（User 模式）与 `run_as_uid`（UID 模式）不能同时指定（互斥校验）
- [ ] UID 模式下 `run_as_uid` > 0、`run_as_gid` >= 0（0=等于 uid）、`run_as_groups` 元素均 > 0
