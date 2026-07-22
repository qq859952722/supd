---
name: "supd-service-extension-dev"
description: "supd 服务与扩展的开发、修改、打包、导入一条龙流程指南。当用户要求创建新服务/扩展、为已有服务添加扩展、修改已有扩展配置或脚本、打包或导入服务/扩展，或在 supd 项目中编写/调整 service.yaml/meta.yaml/run.sh 时调用。"
---

# supd 服务/扩展一条龙开发

本 skill 覆盖 supd 项目中**服务**与**扩展**的完整生命周期：开发 → 修改 → 打包 → 导入 → 验证。详细参考手册见 `docs/服务扩展开发指南.md`，业务规则权威来源见 `docs/需求规格说明_v1.5.md`。

## 何时使用

- 用户要求**开发新服务**（新增 `<baseDir>/services/<name>/service.yaml`）
- 用户要求**开发新扩展**（on_demand / on_schedule / service_lifecycle / supd_lifecycle）
- 用户要求**为已有服务添加扩展**（服务已存在，需新增扩展而不重启服务）
- 用户要求**修改已有扩展**（调整触发器、actions、脚本、超时、并发策略、禁用/启用等）
- 用户要求**修改已有服务配置**（端口、命令、依赖、停止策略等）
- 用户要求**打包**服务或扩展为 tar.gz
- 用户要求**导入**服务或扩展包
- 用户在 supd 项目中提及 service.yaml / meta.yaml / run.sh 的编写或调整

## 前置必读

每次开始前必须读取以下文件恢复上下文：

1. `docs/devlog/session-notes.md` — 当前工作状态
2. `docs/devlog/blockers.md` — 未解决的阻断问题
3. `AGENTS.md` — 项目约束（枚举锁定、数值锁定、禁止引入 DB/SSE 等）
4. `docs/服务扩展开发指南.md` — 完整字段与流程参考（按需查阅对应章节）

## 一条龙流程

### 阶段 1：需求确认

开发前必须与用户确认：

1. 开发对象：**服务** 还是 **扩展**（或两者）
2. 扩展触发器类型（4 选 1）：
   - `on_demand` — 手动触发
   - `on_schedule` — cron 定时
   - `service_lifecycle` — 服务生命周期钩子（pre_ready/post_ready/on_failure/pre_stop）
   - `supd_lifecycle` — supd 启动/退出钩子
3. 并发策略（4 选 1）：`replace` / `serialize` / `parallel` / `debounce:Ns`
4. 运行时：`bash` / `python` / `node` / 其他
5. 是否需要 UI：`show_logs`、`button_style`、`actions`
6. 是否需要打包分发
7. 服务级扩展还是全局扩展

### 阶段 2：开发服务

> 编写 service.yaml 时可参考 `examples/01-simple-service/`（http_check）或 `examples/02-complex-service/`（tcp_check）。

#### 2.1 创建目录结构

```
<baseDir>/services/<service-name>/
├── service.yaml          # 必需
├── <启动脚本或二进制>     # 由 command 指定
└── extensions/           # 可选：服务级扩展
    └── <ext-name>/
        ├── meta.yaml
        └── run.sh
```

#### 2.2 编写 service.yaml

**必填字段**：`name`、`version`、`command`

> `runtime` 为可选字段：设置时将 runtime 别名解析为绝对路径并前置到 command（如 `runtime: python` + `command: [run.py]` → `[/usr/bin/python3, run.py]`）；省略时 command 数组本身即为完整命令。

**完整字段速查**（详见指南第 3 章）：

| 字段 | 说明 | 约束 |
|---|---|---|
| `name` | 服务名，与目录名一致 | — |
| `version` | 版本号 | 字符串 |
| `description` | 描述 | — |
| `icon` | 图标名 | 使用 IconPicker 库 |
| `autostart` | supd 启动时是否自启 | bool |
| `command` | 启动命令（字符串数组） | `[python3, run.py]` 或 `[bash, run.sh]` |
| `runtime` | 运行时（可选） | 设置时前置到 command，省略则 command 本身为完整命令 |
| `user` | 运行用户 | — |
| `group` | 运行组 | — |
| `workdir` | 工作目录 | 默认服务目录 |
| `depends_on` | 依赖服务 | 启动顺序 |
| `tags` | 标签数组 | — |
| `readiness` | 就绪检测 | 4 种类型 |
| `restart` | 重启策略 | policy/delay/max_attempts |
| `stop` | 停止策略 | grace 10s / timeout 60s |
| `logging` | 日志配置 | dir/max_size/rotate |
| `signals` | 自定义信号映射 | — |
| `package` | 打包配置 | include/exclude |

#### 2.3 service.yaml 检查清单

- [ ] `name` 与目录名完全一致
- [ ] `command` 路径正确（相对服务目录）
- [ ] `runtime` 与 command 匹配
- [ ] `readiness` 配置的端口与实际监听端口**一致**（最常见错误）
- [ ] `readiness` 类型在 `fd_notify`/`tcp_check`/`http_check`/`script` 内
- [ ] `stop.timeout_seconds` ≥ `grace_seconds`
- [ ] `depends_on` 引用的服务存在
- [ ] `logging.dir` 可写
- [ ] 数值不超规格上限（见"关键约束"）

#### 2.4 服务状态机

7 种状态（锁定不可新增）：
`pending` → `starting` → `up` → `ready` → `stopping` → `down`
                                            ↘ `failed` ↗

**热重载**：service.yaml 修改后 fsnotify 触发（500ms 防抖），元数据变更即时生效，命令变更标记"待重启"下次 start 生效。**热重载不自动重启运行中的服务**。

### 阶段 3：开发扩展

> 编写 meta.yaml 和 run.sh 时按触发器类型参考对应示例：`examples/03-on-demand-ext/` ~ `examples/08-stats-report-ext/`。带 stdout 协议输出参考 `examples/07-health-check-ext/`。

#### 3.1 选择扩展位置

- **全局扩展**：`<baseDir>/extensions/<ext-name>/`
- **服务级扩展**：`<baseDir>/services/<svc>/extensions/<ext-name>/`

#### 3.2 编写 meta.yaml

**必填**：`name`、`version`、`runtime`、`entry`、`timeout_seconds`

**完整字段速查**（详见指南第 4 章）：

| 字段 | 说明 | 约束 |
|---|---|---|
| `name` | 扩展名 | 与目录名一致 |
| `version` | 版本 | 字符串 |
| `description` | 描述 | — |
| `enabled` | 是否启用 | bool |
| `runtime` | 运行时 | bash/python/node/... |
| `entry` | 入口脚本路径 | 需 `chmod +x` |
| `timeout_seconds` | 超时 | 默认 600，硬上限 1800 |
| `run_as` | 运行身份 | `root` / `<用户名>` / 空（继承，见场景 G） |
| `concurrency` | 并发策略 | 4 种 |
| `ui.show_logs` | 是否显示日志 | bool |
| `ui.button_style` | 按钮样式 | primary/default/danger |
| `ui.icon` | 扩展图标 | — |
| `actions` | 动作列表 | id/label/button_style/args（**注意：actions[].icon 字段规格要求但实现未支持，详见 DEV-012**）|
| `triggers` | 触发器配置 | 4 种类型 |

#### 3.3 触发器配置示例

```yaml
# on_demand — 手动触发
triggers:
  on_demand: true

# on_schedule — cron 定时
triggers:
  on_schedule:
    - cron: "0 */5 * * *"
      action: ping

# service_lifecycle — 服务生命周期
triggers:
  service_lifecycle:
    - event: post_ready
      action: on-ready
    - event: on_failure
      action: on-fail
    - event: pre_stop
      action: on-stop

# supd_lifecycle — supd 生命周期
triggers:
  supd_lifecycle:
    - event: pre_start
      action: on-startup
    - event: post_ready
      action: on-ready
    - event: pre_shutdown
      action: on-shutdown
```

> 说明：服务关联由目录结构决定（`services/<svc>/extensions/<ext>/`），meta.yaml 中无需 `service` 字段（不被解析）。

#### 3.4 编写 run.sh

**接收上下文**（14 个 SUPD_* 环境变量，按场景注入）：

| 变量 | 说明 |
|---|---|
| `SUPD_EVENT` | 触发事件类型 |
| `SUPD_TRIGGER_SOURCE` | 触发源 |
| `SUPD_TRIGGER_TIME` | 触发时间 |
| `SUPD_TRIGGER_USER` | 触发用户 |
| `SUPD_RUN_ID` | 本次运行 ID |
| `SUPD_EXTENSION_NAME` | 扩展名 |
| `SUPD_ACTION` | action id |
| `SUPD_PHASE` | 阶段 |
| `SUPD_SERVICE` | 服务名（服务级扩展） |
| `SUPD_SERVICE_PID` | 服务 PID |
| `SUPD_SERVICE_DIR` | 服务目录 |
| `SUPD_SERVICE_EXIT_CODE` | 服务退出码 |
| `SUPD_SERVICE_SIGNAL` | 服务信号 |
| `SUPD_SERVICE_RESTART_COUNT` | 服务重启次数 |

**stdout 协议**（实时解析）：

```bash
# 进度上报（0-100）
echo '::progress:: 50 "处理中..."'

# 结果上报（success/warning/error）
echo '::result:: success "全部完成"'

# 普通日志（无前缀）
echo '这是一条普通日志'
```

**状态判定优先级**：`timeout/killed/canceled` > `::result:: 协议` > `exit code`

**退出码**：0 成功，非 0 失败

#### 3.5 扩展配置检查清单

- [ ] `name` 与目录名一致
- [ ] `entry` 路径正确且**有执行权限**（`chmod +x run.sh`）
- [ ] `runtime` 与 entry 匹配
- [ ] `timeout_seconds` ≤ 1800（硬上限）
- [ ] 触发器 `event` 值在允许列表内
- [ ] `actions.id` 唯一
- [ ] `button_style` 在 `primary`/`default`/`danger` 内
- [ ] `on_schedule` 必须配置 `cron` 表达式
- [ ] `concurrency` 格式正确（`debounce:Ns`，N∈[1,3600]，**不支持 Nms**）
- [ ] 端口等硬编码值与 service.yaml 一致（避免 8081/9091 这类不一致 bug）

### 阶段 4：修改与扩展已有服务/扩展

本阶段覆盖三类高频场景：**为已有服务添加扩展**、**修改已有扩展**、**修改已有服务配置**。

#### 4.1 修改原则（来自 AGENTS.md）

1. **最小化修改**：只改必要代码，不重构无关部分
2. **对照规格**：业务逻辑疑问查 `docs/需求规格说明_v1.5.md`
3. **保留 REQ 注释追溯**：struct 字段、枚举、API、CLI 需对应 REQ
4. **验证**：`go build ./...` + 相关测试（修改 Go 代码时）
5. **更新 session-notes.md**：记录修改内容
6. **热重载友好**：优先利用 fsnotify 热重载，避免重启 supd 影响运行中的服务

#### 4.2 为已有服务添加扩展

**目标**：服务 `<svc>` 已存在并运行，需要为其新增一个扩展（不重启服务）。

> 服务级扩展示例参考 `examples/05-service-lifecycle-ext/` 或 `examples/07-health-check-ext/`（含 `service` 字段配置）。

**完整流程**：

1. **确认扩展归属与位置**

   | 类型 | 位置 | 适用场景 |
   |---|---|---|
   | 服务级扩展 | `<baseDir>/services/<svc>/extensions/<ext-name>/` | 强绑定该服务，使用其环境变量、跟随服务打包 |
   | 全局扩展 | `<baseDir>/extensions/<ext-name>/` | 跨服务通用，独立打包 |

   **优先选择服务级扩展**（除非确需跨服务复用），这样会随服务一起打包导出。

2. **创建扩展目录与文件**

   ```
   <baseDir>/services/<svc>/extensions/<ext-name>/
   ├── meta.yaml
   └── run.sh   (chmod +x)
   ```

3. **编写 meta.yaml（服务级扩展关键字段）**

   ```yaml
   name: <ext-name>            # 必须与目录名一致
   version: "1.0.0"
   description: "..."
   enabled: true
   runtime: bash
   entry: ./run.sh             # 相对扩展目录
   timeout_seconds: 60
   ui:
     show_logs: true
     button_style: default
   actions:
     - id: <action-id>
       label: "动作名"
   triggers:
     on_demand: true           # 按需选择 4 种触发器之一
   ```

   **关键检查**：
   - [ ] `name` 与目录名一致
   - [ ] `entry` 路径正确且有执行权限（`chmod +x run.sh`）
   - [ ] 若是 `service_lifecycle` 触发器，`event` 在 `pre_start`/`post_ready`/`on_failure`/`pre_stop` 内
   - [ ] 若是 `on_schedule`，cron 表达式格式正确

4. **编写 run.sh**

   - 通过 `SUPD_SERVICE`、`SUPD_SERVICE_PID`、`SUPD_SERVICE_DIR` 等环境变量获取服务上下文
   - 若需访问服务端口/路径，**从 service.yaml 读取实际值**，不要硬编码（避免端口变更时不一致）
   - 推荐做法：在 run.sh 开头从环境变量或配置文件动态获取
     ```bash
     #!/bin/bash
     SERVICE_DIR="${SUPD_SERVICE_DIR:-.}"
     # 从 service.yaml 读取 readiness 端口（需 yq），或通过 SUPD_SERVICE_PORT 等约定变量
     ```

5. **热重载自动加载**

   - 新增扩展目录会被 fsnotify 检测（500ms 防抖）
   - **无需重启 supd**，扩展自动注册
   - **无需重启目标服务**，运行中的服务不受影响
   - `service_lifecycle` 扩展：下次服务进入对应生命周期事件时触发
   - `on_demand` 扩展：立即可在 UI 或 API 触发
   - `on_schedule` 扩展：按 cron 表达式下次触发时间执行

6. **验证**

   - UI 检查：服务详情页或扩展列表页应出现新扩展
   - API 验证：`GET /api/services/<svc>/extensions` 应返回新扩展
   - 触发测试：`POST /api/extensions/<ext-name>/run` 手动触发
   - 日志检查：`GET /api/extensions/<ext-name>/logs`

#### 4.3 修改已有扩展

**目标**：扩展已存在，需要调整其配置、脚本或行为。

**热重载行为对照表**（关键）：

| 修改对象 | 生效方式 | 是否影响运行中的任务 |
|---|---|---|
| `meta.yaml` 元数据（description/ui/icon） | fsnotify 热重载，即时生效 | 否 |
| `meta.yaml` triggers 配置 | 热重载，即时生效 | 否（已运行任务继续） |
| `meta.yaml` actions 定义 | 热重载，即时生效 | 否 |
| `meta.yaml` concurrency 策略 | 热重载，即时生效 | 否（已运行任务按旧策略完成） |
| `meta.yaml` timeout_seconds | 热重载，即时生效 | 否（已运行任务按旧超时） |
| `meta.yaml` enabled（禁用/启用） | 热重载，即时生效 | 否（已运行任务继续） |
| `meta.yaml` run_as | 热重载，下次执行生效 | 否 |
| `run.sh` 脚本内容 | **下次执行时生效** | 否（已运行任务用旧脚本） |
| `entry` 路径 | 热重载，下次执行生效 | 否 |
| `runtime` | 热重载，下次执行生效 | 否 |

**常见修改场景与操作**：

**场景 A：调整触发器（如 on_demand 改为 on_schedule，或新增生命周期钩子）**

```yaml
# 修改前
triggers:
  on_demand: true

# 修改后
triggers:
  on_schedule:
    - cron: "0 */5 * * *"
      action: ping
```

- 热重载即时生效，无需重启
- 检查 `actions` 中是否定义了对应 action id
- 若改为 `service_lifecycle`，确保扩展位于 `services/<svc>/extensions/` 目录下（服务关联由目录决定）

**场景 B：新增/修改 actions（增加按钮、修改标签）**

```yaml
actions:
  - id: ping
    label: "Ping"
  - id: cleanup          # 新增
    label: "清理缓存"
```

- 热重载后 UI 自动刷新出现新按钮
- `id` 不能与已有 action 重复
- `button_style` 在 `primary`/`default`/`danger` 内

**场景 C：修改 run.sh 脚本逻辑**

- 直接编辑 `run.sh`，**下次执行时生效**
- 已在运行的任务**不受影响**（继续用旧脚本执行完毕）
- 修改后建议手动触发一次验证：`POST /api/extensions/<ext-name>/run`
- 检查 stdout 协议输出格式正确（`::progress::` / `::result::`）

**场景 D：临时禁用扩展**

```yaml
enabled: false   # 改为 true 重新启用
```

- 热重载即时生效
- 禁用后不会触发（包括 cron 和生命周期事件）
- 已在运行的任务会执行完毕
- UI 上扩展会标记为禁用状态

**场景 E：调整超时**

```yaml
timeout_seconds: 120   # ≤ 1800 硬上限
```

- 热重载即时生效，仅影响新执行的任务
- 已运行任务按旧超时执行

**场景 F：修改并发策略**

```yaml
concurrency: serialize   # 或 parallel / replace / debounce:5s
```

- 热重载即时生效
- 已运行任务按旧策略完成
- `debounce:Ns` 格式必须正确（N∈[1,3600]，**不支持 Nms**）

**场景 G：修改 run_as（运行身份）**

```yaml
run_as: root   # 或 <用户名>，或省略
```

- 热重载，下次执行生效
- `root`：以 root 身份运行（需 supd 以 root 启动）
- `<用户名>`：以指定用户身份运行（需该用户存在于容器中，否则启动失败并提示创建方法）
- 省略（空值）：
  - 全局扩展 → 继承 supd 启动用户
  - 服务级扩展 → 继承所属服务的 `user` 字段值（服务 user 也为空时回退到 supd 用户）

**场景 H：升级版本号**

```yaml
version: "1.1.0"   # 从 1.0.0 升级
```

- 修改后如需打包分发，版本号必须更新以避免覆盖旧包
- 不影响运行行为，仅影响打包输出文件名

**场景 I：修改 entry 脚本路径或新增依赖文件**

- 修改 `entry` 指向新脚本，确保新脚本有执行权限
- 新增的依赖文件（如配置、库）放在扩展目录内，随扩展打包
- 热重载后下次执行生效

**修改扩展检查清单**：

- [ ] `name` 与目录名仍一致（若重命名目录需同步改 name）
- [ ] `entry` 路径正确且有执行权限
- [ ] `timeout_seconds` ≤ 1800
- [ ] 触发器 `event` 值在允许列表内
- [ ] `actions.id` 唯一
- [ ] `button_style` 在 `primary`/`default`/`danger` 内
- [ ] `service_lifecycle` 扩展的 `service` 字段已填写
- [ ] `concurrency` 格式正确
- [ ] run.sh 中无硬编码端口/路径（或与 service.yaml 一致）
- [ ] run.sh 的 stdout 协议格式正确
- [ ] 若修改了 entry 路径，新脚本有 `chmod +x`

#### 4.4 修改已有服务配置

**目标**：服务已存在，需要调整 service.yaml 或其启动行为。

**热重载行为对照表**：

| 修改对象 | 生效方式 | 是否影响运行中的服务 |
|---|---|---|
| 元数据（description/icon/tags） | 热重载，即时生效 | 否 |
| `depends_on` | 热重载，即时生效 | 否 |
| `readiness` 配置 | 热重载，**下次启动生效** | 否（运行中服务不重新检测） |
| `command`/`args`/`runtime` | 热重载，**标记"待重启"** | 否（下次 start 生效） |
| `stop` 策略 | 热重载，下次停止生效 | 否 |
| `logging` 配置 | 热重载，即时生效 | 否 |
| `user`/`group` | 热重载，**下次启动生效** | 否 |
| `signals` | 热重载，下次信号生效 | 否 |
| `autostart` | 热重载，即时生效 | 否（影响下次 supd 启动） |

**关键约束**：**热重载不会自动重启运行中的服务**，命令类变更标记为"待重启"，需要用户手动 stop→start 或 restart 生效。这是规格要求，避免影响运行中的服务。

**常见修改场景**：

- **修改服务端口**：同步改 `service.yaml` readiness.port + **所有引用该端口的扩展脚本**（最易遗漏）
- **修改启动命令**：改 `command` + `args`，检查 `signals`，重启服务生效
- **修改依赖关系**：改 `depends_on`，即时生效，但不会自动重启已启动的服务
- **修改停止策略**：改 `stop.grace_seconds`（默认 10）/ `stop.timeout_seconds`（默认 60），下次停止生效
- **修改重启策略**：改 `restart.policy`/`restart.delay`/`restart.max_attempts`

#### 4.5 修改场景同步点矩阵

| 修改对象 | 必须同步检查的位置 |
|---|---|
| 服务端口 | `service.yaml` readiness.port + **所有扩展脚本中的端口变量**（最常见 bug 源） |
| 启动命令 | `command` + `args` + `signals` + 依赖该命令的扩展 |
| 触发器 | `triggers` 配置完整性 + 对应 `actions` 定义 |
| 超时 | `timeout_seconds` ≤ 1800，且与业务实际耗时匹配 |
| 运行时 | `runtime` + `entry` 脚本 shebang 一致 |
| 服务名 | 目录名 + `service.yaml.name`（服务级扩展由目录结构关联，无需 meta.yaml 字段） |
| 扩展名 | 目录名 + `meta.yaml.name` + triggers 中引用的 action |
| entry 路径 | `meta.yaml.entry` + 实际文件存在 + 有执行权限 |
| run_as | `meta.yaml.run_as` + supd 有对应权限 |

#### 4.6 禁止行为

- 禁止趁机重构（修复 bug 时不得改动无关代码）
- 禁止添加未要求的功能
- 禁止伪造测试结果
- 禁止新增枚举成员或调整锁定数值
- 禁止为应用热重载而强制重启运行中的服务（违反规格）
- 禁止修改扩展时遗漏同步 service.yaml 中的端口/路径引用

### 阶段 5：打包（导出）

#### 5.1 服务打包

```
GET /api/services/{name}/export
```

- 输出：`<service-name>.tar.gz`（浏览器下载）
- 包含：`service.yaml` + `env.yaml` + `extensions/` + 资源文件
- 通过 `service.yaml` 的 `package.include` / `package.exclude` / `package.default` 字段过滤
- CLI 等价：`supd --workdir <baseDir> export <name>`

#### 5.2 扩展打包

```
GET /api/extensions/{name}/export
```

- 输出：`<ext-name>.tar.gz`（浏览器下载）
- 包含：`meta.yaml` + `env.yaml` + `entry` + 资源文件
- CLI 等价：`supd --workdir <baseDir> export <name>`

#### 5.3 打包前检查

- [ ] `version` 已更新（避免覆盖旧版本）
- [ ] 无调试日志输出（`echo "debug: ..."`）
- [ ] `entry` 脚本有执行权限
- [ ] 资源文件完整（图标、配置、依赖脚本）
- [ ] 配置字段无拼写错误
- [ ] 不包含临时文件、缓存、日志
- [ ] 服务 `package` 段配置合理（include/exclude/default）

### 阶段 6：导入（双次上传无状态模式）

> 实现为无状态双次上传：用户上传包后端立即解压预览（不持久化），用户确认后再次上传同一包执行实际导入。

#### 6.1 两步确认流程

```
# 步骤 1：上传包，返回差异预览（不持久化）
POST /api/services/import
Content-Type: multipart/form-data
file: <tar.gz>

# 响应：{archive_version, local_version, exists_local, ...}

# 步骤 2：用户确认后，再次上传同一包执行实际导入（备份现有目录 → 解压覆盖 → 失败自动回滚）
POST /api/services/import/confirm
Content-Type: multipart/form-data
file: <tar.gz>
```

扩展导入同理：`POST /api/extensions/import` → `POST /api/extensions/import/confirm`

**关键点**：
- 无 session_id，两步都上传完整包
- 步骤 2 执行前会备份现有目录到 `<name>.bak.<timestamp>/`
- 步骤 2 失败时自动回滚（恢复备份）
- 包内顶层目录会被自动剥离（如 `myapp-v1.0.0/service.yaml` → `service.yaml`）

#### 6.2 导入检查

- [ ] 预览差异符合预期（archive_version/local_version/exists_local）
- [ ] 配置无冲突（端口、路径、服务名）
- [ ] 不覆盖未备份的重要配置
- [ ] 导入后 `POST /api/reload` 触发热重载（如未自动触发）

### 阶段 7：验证

#### 7.1 编译验证

```bash
# 后端
go build ./...
go vet ./...
go test ./... -count=1

# 前端
cd web && pnpm build
```

#### 7.2 运行时验证

```bash
# 启动 supd
SUPD_LOG_DIR=/tmp/supd-logs ./supd --workdir <baseDir> run

# 查看服务状态
./supd --workdir <baseDir> service list
./supd --workdir <baseDir> service status <name>

# 查看日志
tail -f /tmp/supd-logs/supd.log
```

#### 7.3 扩展验证

- 通过 UI 或 API 触发：`POST /api/extensions/{name}/run`（全局扩展）或 `POST /api/services/{service}/extensions/{ext}/run`（服务级扩展）
- 检查返回的 `run_id`
- 查询运行结果：`GET /api/extensions/runs/{run_id}`（注意路径是 `/api/extensions/runs/`，不是 `/api/extensions/{name}/runs/`）
- 查询运行历史：`GET /api/extensions/runs?extension_name={name}&state=success`（支持 `extension_name`/`state`/`limit` 过滤）
- 查看运行日志：`GET /api/extensions/runs/{run_id}/logs`
- 取消运行：`POST /api/extensions/runs/{run_id}/cancel`

#### 7.4 验证清单

- [ ] 服务能正常启动并进入 `ready` 状态
- [ ] 服务端口正确识别（HTTP 端口可点击）
- [ ] 扩展手动触发能执行
- [ ] 定时扩展按 cron 执行
- [ ] 生命周期扩展在对应事件触发
- [ ] stdout 协议正确解析（progress/result）
- [ ] 日志文件正确写入 `/tmp/supd-logs/`
- [ ] PID 文件正确写入 `<baseDir>/.supd/pids/`

## 关键约束（不可违反）

### 枚举锁定（禁止新增成员）

| 枚举 | 固定值 | 数量 |
|---|---|---|
| 服务状态 | pending/starting/up/ready/stopping/down/failed | 7 |
| 任务状态 | pending/running/success/failed/timeout/canceled/killed | 7 |
| 触发器类型 | on_demand/on_schedule/service_lifecycle/supd_lifecycle | 4 |
| 并发策略 | replace/serialize/parallel/debounce:Ns | 4 |
| Readiness 类型 | fd_notify/tcp_check/http_check/script | 4 |
| 认证模式 | none/local_skip/always_token | 3 |
| 事件类型 | 14 种（见规格 §2.9.7） | 14 |
| button_style | primary/default/danger | 3 |
| 错误码 | 22 个（见规格 §5.4） | 22 |

### 数值锁定（不可调整）

| 参数 | 值 |
|---|---|
| fsnotify 防抖 | 500ms |
| 日志搜索上限 | 1000 行 |
| 扩展运行日志上限 | 10MB |
| 任务历史保留 | 7 天（内存） |
| 事件环形缓冲 | 200 条 |
| 长轮询并发上限 | 全局 50 / 单客户端 5 |
| stop 默认 grace | 10 秒 |
| stop 默认 timeout | 60 秒 |
| 扩展默认 timeout | 600 秒 |
| 扩展硬上限 | 1800 秒 |
| 上传大小限制 | 100MB |
| 编辑器多标签上限 | 8 个 |
| 文件历史版本 | 50 个 |

### 架构约束

- **禁止引入数据库**（SQLite/Bolt/Badger 等）
- **禁止引入 SSE/WebSocket**（长轮询是规格要求）
- **禁止添加需求规格说明书中不存在的配置字段**
- **业务逻辑疑问以 `docs/需求规格说明_v1.5.md` 为唯一权威来源**

## 何时停止并请求人工介入

遇到以下情况**必须停止**，记录到 `docs/devlog/blockers.md`：

- 修复方案涉及业务规则变更，而规格说明书中该规则不明确
- 修复方案会影响多个模块，且影响范围难以评估
- 发现的问题与规格说明书存在矛盾
- 同一问题修复失败 3 次以上

## 在线开发（SSH + API 混合方案）

当 supd 部署到 NAS 后，可通过 SSH + API 混合方案在线开发、测试、部署服务与扩展，无需本地 Go 源码或编译环境。

### 架构

```
开发者 / 智能 IDE（Trae / Cursor / VS Code）
    ├── SSH/SFTP ──→ NAS 上的 supd 容器（端口 2222，文件编辑）
    └── HTTP API ──→ NAS 上的 supd:7979（服务/扩展控制 + 文件操作）
```

**两条通道分工**：
- **SSH/SFTP**：智能 IDE Remote-SSH 连接，直接编辑工作目录中的文件，享受 IDE 的语法高亮、自动补全、YAML 校验
- **HTTP API**：服务/扩展的创建、启动、停止、触发、日志查看、打包导入等控制操作

### SSH 连接（Dropbear）

容器内置 Dropbear SSH 服务器（含 SFTP 模块），端口 2222。Dropbear 作为 supd 管理的**普通服务**运行（`services/dropbear-ssh/`），由 `supd init` 自动生成。

**关键特性**：
- `autostart: false` — 默认不自动启动，用户按需通过 Web UI/API 启动（避免暴露登录入口）
- 认证模式由 `services/dropbear-ssh/env.yaml` 中的 `SSH_PUBLIC_KEY` 控制（supd 自动注入到服务进程环境，规格 §2.2.4）
- 启动脚本 `run.sh` 根据环境变量自动选择认证模式，无需额外扩展

| 参数 | 值 |
|---|---|
| 主机 | NAS IP |
| 端口 | 2222（`DROPBEAR_PORT` 可配置） |
| 用户 | `supd`（工作目录 `/etc/supd`）或 `root` |
| 认证 | `SSH_PUBLIC_KEY` 非空 → 公钥认证；为空 → 空白密码免认证（仅内网可信场景） |

**配置认证模式**（编辑 `services/dropbear-ssh/env.yaml`）：

```yaml
env:
  # 公钥认证模式（推荐）：填入公钥内容
  SSH_PUBLIC_KEY:
    value: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... your@email"
    hint: "SSH 公钥内容，留空=空白密码免认证，填入=公钥认证"

  # 空白密码免认证模式（默认，仅内网可信场景）：留空
  # SSH_PUBLIC_KEY:
  #   value: ""
```

修改 env.yaml 后需重启 dropbear-ssh 服务生效（REQ-F-027）。

**启动与连接**：

```bash
# 启动 dropbear-ssh 服务（通过 API）
curl -X POST http://<NAS-IP>:7979/api/services/dropbear-ssh/start

# SSH 连接
ssh -p 2222 supd@<NAS-IP>

# SFTP 文件传输
sftp -P 2222 supd@<NAS-IP>

# SCP 上传单个文件
scp -P 2222 run.sh supd@<NAS-IP>:/etc/supd/services/myapp/

# SCP 上传整个目录
scp -r -P 2222 ./myapp supd@<NAS-IP>:/etc/supd/services/
```

> Dropbear 体积约 500KB，支持公钥认证和 SFTP 子系统，不含 SCP 协议（SCP 通过 SFTP 兼容层实现）。
>
> **run_as: root** — dropbear 需要 root 权限写 host key、切换登录用户、设置空白密码。非 root 运行 supd 时此服务启动失败。

### API 在线开发能力

supd 提供 76 个 API 端点，覆盖完整的在线开发闭环。以下列出在线开发常用的端点（认证方式见 config.yaml 的 `auth_mode`）。

#### 文件操作 API（替代 SSH 编辑文件）

| 操作 | 方法 | 路径 | 说明 |
|---|---|---|---|
| 浏览目录树 | GET | `/api/files/tree?path=services/` | 返回 JSON 目录树 |
| 读文件 | GET | `/api/files?path=services/myapp/service.yaml` | 返回文件内容 |
| 写文件 | PUT | `/api/files` | body: `{path, content}`，自动保留历史版本 |
| 创建文件 | POST | `/api/files` | body: `{path, content}` |
| 删除文件 | DELETE | `/api/files?path=...` | |
| 移动/重命名 | POST | `/api/files/move` | body: `{old_path, new_path}` |
| 上传文件 | POST | `/api/files/upload` | multipart/form-data，100MB 上限 |
| 校验 YAML | POST | `/api/files/validate` | 检查 service.yaml/meta.yaml 语法 |
| 文件历史 | GET | `/api/files/history?path=...` | 返回最近 50 个版本 |
| 回滚文件 | POST | `/api/files/rollback` | body: `{path, version}` |

#### 服务管理 API

| 操作 | 方法 | 路径 |
|---|---|---|
| 列出服务 | GET | `/api/services` |
| 创建服务 | POST | `/api/services` |
| 查看服务详情 | GET | `/api/services/{name}` |
| 更新服务配置 | PUT | `/api/services/{name}` |
| 删除服务 | DELETE | `/api/services/{name}` |
| 启动服务 | POST | `/api/services/{name}/start` |
| 停止服务 | POST | `/api/services/{name}/stop` |
| 重启服务 | POST | `/api/services/{name}/restart` |
| 查看服务日志 | GET | `/api/services/{name}/logs` |
| 搜索日志 | GET | `/api/services/{name}/logs/search?q=...` |
| 查看端口/进程 | GET | `/api/services/{name}/resources` |
| 导出服务包 | GET | `/api/services/{name}/export` |
| 导入服务包 | POST | `/api/services/import` |

#### 扩展管理 API

| 操作 | 方法 | 路径 |
|---|---|---|
| 列出全局扩展 | GET | `/api/extensions` |
| 创建全局扩展 | POST | `/api/extensions` |
| 列出服务级扩展 | GET | `/api/services/{svc}/extensions` |
| 创建服务级扩展 | POST | `/api/services/{svc}/extensions` |
| 更新扩展 | PUT | `/api/extensions/{name}` |
| 删除扩展 | DELETE | `/api/extensions/{name}` |
| 触发扩展 | POST | `/api/extensions/{name}/run` |
| 查看运行结果 | GET | `/api/extensions/runs/{run_id}` |
| 查看运行日志 | GET | `/api/extensions/runs/{run_id}/logs` |
| 导出扩展包 | GET | `/api/extensions/{name}/export` |

#### 配置与系统 API

| 操作 | 方法 | 路径 |
|---|---|---|
| 读取配置 | GET | `/api/settings` |
| 更新配置 | PUT | `/api/settings` |
| 手动热重载 | POST | `/api/reload` |
| 系统状态 | GET | `/api/system/status` |
| 运行时列表 | GET | `/api/runtimes` |

### 在线开发工作流

以"在 NAS 上创建一个新服务并添加 on_demand 扩展"为例：

```bash
# 0. 设置 API 地址和认证
API="http://<NAS-IP>:7979/api"
TOKEN="<auth_token>"  # local_skip 模式下本地免认证

# 1. 通过 API 创建服务（等价于编写 service.yaml）
curl -X POST "$API/services" -H "Content-Type: application/json" -d '{
  "name": "myapp",
  "version": "1.0.0",
  "command": ["python3", "run.py"],
  "readiness": {"type": "http_check", "url": "http://127.0.0.1:9001/health", "expected_status": 200}
}'

# 2. 通过文件 API 上传启动脚本
curl -X POST "$API/files/upload?path=services/myapp/run.py" \
  -F "file=@./run.py"

# 3. 启动服务
curl -X POST "$API/services/myapp/start"

# 4. 查看日志（确认就绪）
curl "$API/services/myapp/logs?tail=50"

# 5. 创建 on_demand 扩展
curl -X POST "$API/services/myapp/extensions" -H "Content-Type: application/json" -d '{
  "name": "health-check",
  "version": "1.0.0",
  "runtime": "bash",
  "entry": "run.sh",
  "timeout_seconds": 30,
  "triggers": {"on_demand": true},
  "actions": [{"id": "check", "label": "健康检查"}]
}'

# 6. 上传扩展脚本
curl -X POST "$API/files/upload?path=services/myapp/extensions/health-check/run.sh" \
  -F "file=@./health-check.sh"

# 7. 触发扩展测试
curl -X POST "$API/extensions/health-check/run" -d '{"action":"check"}'

# 8. 查看扩展运行结果
curl "$API/extensions/runs/$(curl -s -X POST "$API/extensions/health-check/run" | jq -r .run_id)"
```

> 也可用 SSH/SFTP 替代步骤 2/6 的文件上传，直接编辑远程文件：
> ```bash
> scp -P 2222 ./run.py supd@<NAS-IP>:/etc/supd/services/myapp/
> ```
> 文件写入后 fsnotify 自动热重载（500ms 防抖），无需手动触发 `/api/reload`。

### 智能 IDE 集成

#### Trae / Cursor / VS Code Remote-SSH

1. **配置 SSH 连接**（`~/.ssh/config`）：
   ```
   Host supd-nas
       HostName <NAS-IP>
       Port 2222
       User supd
       IdentityFile ~/.ssh/supd_key
   ```

2. **连接远程工作目录**：Remote-SSH 打开 `/etc/supd/`

3. **直接编辑文件**：IDE 语法高亮 + 自动补全 + YAML 校验，保存后 fsnotify 自动热重载

4. **终端中控制 supd**：在 IDE 集成终端中调用 curl 命令（见上方工作流）

5. **查看日志**：IDE 终端 `curl $API/services/myapp/logs?tail=50` 或 SSH `tail -f /var/log/supd/supd.log`

#### 纯 API 模式（无 SSH）

不使用 SSH 时，可通过文件 API 完成所有操作：
- `GET /api/files/tree` 浏览目录
- `GET /api/files` 读取文件内容
- `PUT /api/files` 写入文件（自动保留历史版本）
- `POST /api/files/validate` 校验 YAML 语法
- `POST /api/files/upload` 上传二进制文件

此模式下智能 IDE 通过 HTTP API 读写文件，但无法享受 IDE 的本地文件索引和全项目搜索。

### 在线开发注意事项

1. **热重载优先**：修改 service.yaml/meta.yaml/run.sh 后，fsnotify 自动热重载（500ms 防抖），无需重启 supd
2. **运行中服务不受影响**：命令类变更标记"待重启"，需手动 `POST /api/services/{name}/restart`
3. **文件历史自动保留**：通过 API 或 SSH 修改文件时自动保留最近 50 个版本，可回滚
4. **YAML 校验**：写入前可调用 `POST /api/files/validate` 检查语法
5. **认证**：`local_skip` 模式下本地/局域网免认证；外部访问需在请求头携带 `Authorization: Bearer <token>`
6. **端口冲突**：SSH 端口 2222 与 API 端口 7979 独立，互不影响

## 完整示例参考（按需查阅）

示例文件存放在本 skill 目录下的 `examples/`，均为 test_workdir 中实际运行验证过的配置。**不要一次性全部读取**，按当前任务需要查阅对应示例即可。

| 需要参考时 | 查阅目录 | 覆盖特性 |
|---|---|---|
| 编写简单服务（http_check） | `examples/01-simple-service/` | http_check readiness、Python HTTP 服务、stop/logging |
| 编写复杂服务（tcp_check） | `examples/02-complex-service/` | tcp_check readiness、autostart、command 数组、tags |
| 编写 on_demand 扩展 | `examples/03-on-demand-ext/` | 手动触发、多 action、action args、button_style |
| 编写 on_schedule 扩展 | `examples/04-scheduled-ext/` | cron 定时触发、单 action |
| 编写 service_lifecycle 扩展 | `examples/05-service-lifecycle-ext/` | post_ready/on_failure/pre_stop 钩子 |
| 编写 supd_lifecycle 扩展 | `examples/06-supd-lifecycle-ext/` | post_ready/pre_shutdown、parallel 并发、stdout 协议 |
| 编写带 stdout 协议的扩展 | `examples/07-health-check-ext/` | 混合触发、多 action、::progress::/::result:: 协议 |
| 编写定时+手动混合扩展 | `examples/08-stats-report-ext/` | on_schedule+on_demand、完整协议输出、API 调用 |

示例使用说明见 `examples/README.md`。所有示例的 `entry` 已改为相对路径 `./run.sh`，可直接复制使用。

## 详细参考

- `docs/服务扩展开发指南.md` — 完整字段说明、执行流程、API/CLI 参考（1021 行）
- `docs/需求规格说明_v1.5.md` — 业务规则唯一权威来源
- `AGENTS.md` — 项目 AI 协作协议
- `docs/devlog/session-notes.md` — 当前工作状态
- `docs/devlog/blockers.md` — 未解决阻断问题
- `docs/devlog/deviations.md` — 已确认偏差
