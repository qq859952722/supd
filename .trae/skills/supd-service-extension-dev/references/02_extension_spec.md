# supd 扩展配置与规范指南 (meta.yaml & run.sh)

本参考文档包含 `meta.yaml` 的完整字段定义、4 种触发器类型配置、14 个 `SUPD_*` 环境变量、stdout 通讯协议规范及检查清单。

---

## 1. 扩展目录结构与位置

- **全局扩展**：`<baseDir>/extensions/<ext-name>/`（跨服务通用）
- **服务级扩展**：`<baseDir>/services/<svc>/extensions/<ext-name>/`（绑定特定服务）

```
<ext-name>/
├── meta.yaml          # 必需：扩展配置与触发器元数据
├── <entry>            # 必需：meta.yaml entry 指向的相对路径，如 run.sh/run.js
├── env.yaml           # 可选：扩展专属环境变量（需包含 env: 包装层）
├── data/              # 可选：需随扩展分发的资源；扩展打包不会默认排除
└── <辅助资源/代码>     # 可选：随扩展一同执行或打包
```

---

## 2. meta.yaml 完整字段参考

**原始 YAML 必填字段**：`name`、`version`、`entry`。`timeout_seconds` 省略或为 `0` 时加载器填充 `600`；`runtime` 可选，省略时直接执行 `entry`。

> **`version` 格式校验**：必须匹配正则 `^[0-9]+\.[0-9]+\.[0-9]+$`（三段数字，与服务相同）。
>
> **`entry` 路径安全校验**：禁止包含 `..`、shell 元字符（`;` `|` `&` `$` `` ` `` `(` `)` `{` `}` `\n` `\r`）以及冗余路径分隔符；`filepath.Clean(entry) == entry` 必须成立。

| 字段 | 类型 | 默认值 | 说明与约束 |
|---|---|---|---|
| `name` | string | 必填 | 扩展名称，必须匹配 `^[a-z][a-z0-9-]*$` 且与目录名一致 |
| `version` | string | 必填 | 扩展版本号，必须匹配 `^[0-9]+\.[0-9]+\.[0-9]+$`（如 `"1.0.0"`） |
| `description` | string | `""` | 扩展功能描述 |
| `enabled` | bool | `true` | 是否启用该扩展 |
| `runtime` | string | `""` | 可选运行时别名（如 `bash`, `sh`, `python3`, `node`, `tjs`）；非空时由解释器执行 entry |
| `entry` | string | 必填 | 入口文件相对路径（如 `run.sh` 或 `run.js`）；`runtime` 为空时须具备执行权限，非空时只要求文件可读；路径受安全校验（见上） |
| `timeout_seconds` | int | `600` | 单次运行超时时限；省略/`0` 时加载器填充 600，生效值须 > 0 且不超过 `settings.extension_hard_limit_seconds`（默认 1800） |
| `run_as` | string | `""` | 运行身份（User 模式）：`root` / `<用户名>` / 空（服务级扩展继承服务身份，全局扩展继承 supd 用户）。与 `run_as_uid` 互斥 |
| `run_as_uid` | int | `0` | 直接指定 uid（UID 模式，与 `run_as` 互斥，不查 /etc/passwd，适用于 NAS 固定 uid 服务）；`0`=未设置 |
| `run_as_gid` | int | `0` | 直接指定 gid（UID 模式下可选，`0`=等于 `run_as_uid`） |
| `run_as_groups` | list[int] | `[]` | 补充组 gid 列表（UID 模式下可选） |
| `concurrency` | string | `"replace"` | 并发控制策略：`replace` / `serialize` / `parallel` / `debounce:Ns`（N 为 1-3600 的整数秒） |
| `ui.show_logs` | bool | `true` | 前端 UI 是否实时展示日志 |
| `ui.button_style` | string | `"default"` | 前端按钮样式：`primary` / `default` / `danger` |
| `ui.icon` | string | `""` | 前端图标名称 |
| `actions` | list[struct] | `[]` | 扩展导出的 Action 动作列表，含 `id`（必填、唯一）、`label`（必填）、`button_style`（可选，默认继承 `ui.button_style`）。**有 actions 时 `triggers.on_demand` 默认 true** |
| `triggers` | struct | nil | 触发器配置 |

> **actions 字段约束**：
> - `actions[].id` 必填，且在整个 actions 列表中唯一（重复 id 校验报错）。
> - `actions[].label` 必填（不能为空字符串）。
> - `actions[].button_style` 可选，默认继承 `ui.button_style` 的值；空字符串允许（默认值填充前）。
> - `actions[].icon` 字段**在代码中不存在**（YAML 写入会被静默忽略）。如需为单个 action 配置不同图标，请在 `ui.icon` 中设置扩展级图标。

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

# 2. on_schedule — cron 定时任务 (标准 5 段：分 时 日 月 周)
#    cron 表达式由 robfig/cron ParseStandard 校验，配置阶段即拦截非法表达式
triggers:
  on_schedule:
    - cron: "0 */5 * * *"
      action: ping
      # 可选：失败重试配置（规格 §2.2.3 / REQ-D-004）
      retry_on_failure:
        max_retries: 3          # 失败后最大重试次数（每次重试生成新 run_id）
        interval_minutes: 5     # 重试间隔（分钟）

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

> **触发器 action 引用校验**：`on_schedule[].action`、`service_lifecycle[].action`、`supd_lifecycle[].action` 必须引用 `actions[]` 中已定义的 `id`；未定义则校验报错。
>
> **on_demand 默认值**：当 `actions` 非空且未显式设置 `triggers.on_demand` 时，`on_demand` 默认为 `true`。
>
> **提示**：服务级扩展自动由目录位置关联到所属服务，`meta.yaml` 中无需也不解析 `service` 字段（YAML 解析器静默忽略）。

---

## 3.5 并发策略行为详解 (concurrency)

`concurrency` 字段控制同一 action 多次触发时的执行行为（§2.2.7）：

| 策略 | 行为 | 适用场景 |
|---|---|---|
| `replace` | 取消前任务（SIGTERM → 5s → SIGKILL），启动新任务 | 默认值；适合"只关心最新结果"的场景 |
| `serialize` | 排队执行，前任务终态后执行下一任务；FIFO 队列上限 16 | 串行化操作，避免并发冲突（如配置写入） |
| `parallel` | 并行执行，不限数量 | 无副作用的并行任务 |
| `debounce:Ns` | trailing debounce，N 秒内无新触发后执行最后一次 | 合并高频触发（N 为 1-3600 整数，单位秒） |

### serialize 队列满行为（§2.2.7）

- 队列固定最多 **16 个待执行任务**（`maxSerializeQueue`）。
- 超过上限的新触发立即以 `failed` 结束，`result_msg="serialize queue full"`。
- 该 `failed` 记录会写入任务历史（runs），包含完整的 `extension_name`/`action_id`/`started_at` 等元数据，可通过 `GET /api/extensions/runs` 查询。
- 扩展开发者无需特殊处理；队列满是 supd 的自动限流，防止高频触发下 pending 无限堆积。

> **on_demand 异步执行模式**：`POST /api/extensions/{name}/run` 立即返回 `state=running` 和 `run_id`，扩展在后台 goroutine 中执行。最终结果（success/failed）通过 `GET /api/extensions/runs/{runID}` 或 `GET /api/extensions/runs` 查询。

---

## 4. 14 个 `SUPD_*` 环境变量

扩展进程启动时，系统按场景自动注入以下 14 个环境变量（并非所有变量在所有场景都注入，见下表"适用场景"）：

| 环境变量名 | 类型 | 适用场景 | 说明与含义 |
|---|---|---|---|
| `SUPD_EVENT` | string | 所有扩展 | 触发事件类型（`on_demand` / `on_schedule` / `service_lifecycle` / `supd_lifecycle`） |
| `SUPD_TRIGGER_SOURCE` | string | 所有扩展 | 触发源标识（`webui` / `cli` / `schedule` / `service_lifecycle` / `supd_lifecycle`） |
| `SUPD_TRIGGER_TIME` | string | 所有扩展 | 触发时间戳，RFC3339 格式，**固定 CST +08:00 时区**（如 `2026-07-30T15:30:00+08:00`） |
| `SUPD_TRIGGER_USER` | string | 所有扩展 | 触发用户（API 认证用户或 `system`） |
| `SUPD_RUN_ID` | string | 所有扩展 | 本次扩展运行的唯一任务 ID（任务历史主键） |
| `SUPD_EXTENSION_NAME` | string | 所有扩展 | 当前扩展名称 |
| `SUPD_ACTION` | string | 所有扩展 | 当前执行的 Action ID；扩展通过此变量区分动作分支（不再用命令行参数） |
| `SUPD_PHASE` | string | 仅 lifecycle 触发 | 执行阶段：`pre_start` / `post_ready` / `on_failure` / `pre_stop` / `pre_shutdown` |
| `SUPD_SERVICE` | string | 仅 service_lifecycle | 触发生命周期事件的服务名；全局扩展在 service_lifecycle 触发时也注入 |
| `SUPD_SERVICE_PID` | string | 仅 service_lifecycle | 关联服务的当前进程 PID；`pre_start` 阶段为空字符串（进程尚未启动），`on_failure` 时为退出前 PID |
| `SUPD_SERVICE_DIR` | string | 仅服务级扩展 | 关联服务的工作目录绝对路径；当 ServiceName 和 ServiceDir 均非空时注入 |
| `SUPD_SERVICE_EXIT_CODE` | int | 仅 on_failure | 关联服务退出码（数字） |
| `SUPD_SERVICE_SIGNAL` | int | 仅 on_failure | 关联服务退出信号（数字，0 表示正常退出而非信号致死） |
| `SUPD_SERVICE_RESTART_COUNT` | int | 仅 on_failure | 关联服务的当前已重启次数（数字） |

> **额外变量 `SUPD_SCRIPT_TMP`**：supd 扩展执行器管理的 `script_tmp/<service>+<ext>` 或 `script_tmp/global+<ext>` 子目录绝对路径，供扩展脚本写入临时文件；扩展应优先使用此目录而非 `/tmp`，不得把运行期临时文件写回扩展代码目录。
>
> **注入顺序**：`os.Environ()` → 4 层 env.yaml 合并 → `SUPD_*` 上下文变量（后者可覆盖同名系统变量）。

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

### 协议格式规范

- **`::progress:: <0-100> "<message>"`**：百分比必须为 0-100 整数，消息必须用双引号包裹。
- **`::result:: <success|warning|error> "<message>"`**：状态严格三选一，消息必须用双引号包裹。
- **`::result::` 多次输出**：以最后一次为准（一个 run 内允许多次输出 result，最终态用最后一条）。
- **消息内双引号转义**：消息内容中的双引号需转义为 `\"`（如 `::result:: success "say \"hello\""`）。
- **行长限制**：单行超过 **8KB**（8192 字节）会被截断并按普通日志处理，不再尝试解析协议。
- **未识别的 `::xxx::` 前缀**：按普通日志处理。
- **stderr**：全部按普通日志处理，不解析协议。

### 任务终态判定优先级

`timeout` / `killed` / `canceled` > `::result::` 协议判定 > `exit code` 退出码（0 成功，非 0 失败）。

> `::result:: warning` 视为成功完成（带告警），不计入失败统计，不触发 `retry_on_failure`。

---

## 6. meta.yaml 检查清单

- [ ] `name` 匹配 `^[a-z][a-z0-9-]*$` 且与所在目录名完全一致
- [ ] `version` 匹配 `^[0-9]+\.[0-9]+\.[0-9]+$`（三段数字，如 `1.0.0`）
- [ ] `entry` 路径正确且文件存在（无 `..`、无 shell 元字符、无冗余分隔符）；`runtime` 为空时已具备执行权限，非空时由解释器执行
- [ ] `timeout_seconds` > 0 且不超过 config.yaml 生效的硬上限（默认 1800）
- [ ] 需要解释器时配置合适的 `runtime` 别名（`bash`/`sh`/`python3`/`node`/`tjs` 或自定义）；直接执行入口时可省略
- [ ] 触发器 `service_lifecycle.event` 仅使用 `pre_start`/`post_ready`/`on_failure`/`pre_stop`
- [ ] 触发器 `supd_lifecycle.event` 仅使用 `pre_start`/`post_ready`/`pre_shutdown`
- [ ] `actions[].id` 唯一（重复 id 校验报错）；`actions[].label` 必填（非空字符串）
- [ ] `actions[].button_style` ∈ `{primary, default, danger}`（如未填则继承 `ui.button_style`）
- [ ] `ui.button_style` ∈ `{primary, default, danger}`
- [ ] `on_schedule` 的 `cron` 表达式格式正确（标准 5 段：分 时 日 月 周；配置阶段校验）
- [ ] `on_schedule[].action`、`service_lifecycle[].action`、`supd_lifecycle[].action` 均引用 `actions[]` 中已定义的 `id`
- [ ] `concurrency` 策略符合规范（`debounce:Ns` 中 N 为 1-3600 的整数秒，不支持毫秒 `ms`）
- [ ] 若包含 `env.yaml`，必须使用 `env:` 包装层格式
- [ ] `run_as`（User 模式）与 `run_as_uid`（UID 模式）不能同时指定（互斥校验）
- [ ] UID 模式下 `run_as_uid` > 0、`run_as_gid` >= 0（0=等于 uid）、`run_as_groups` 元素均 > 0
