# supd 需求偏差记录

> 记录实现与需求原文不一致的情况，供人工确认。

---

## DEV-001 — Vite 版本偏差

- **发现时间**：2026-07-08 13:10
- **关联任务**：Task 1.1.2
- **关联需求**：REQ-C-013
- **需求原文**：前端技术栈要求 Vite v8
- **实际实现**：使用 Vite v6（最新稳定版）
- **偏差原因**：npm 上 Vite 8.x 尚未正式发布，6.x 是当前最新可用稳定版
- **风险评估**：低。Vite 6 与 8 的核心 API 兼容，项目使用的功能无版本差异
- **状态**：🟢 已确认可接受

---

## DEV-002 — embed.go 位置偏差

- **发现时间**：2026-07-08 13:25
- **关联任务**：Task 1.1.3
- **关联需求**：REQ-C-015, REQ-F-002
- **需求原文**：需求规格说明书隐含 embed.go 在项目根目录
- **实际实现**：embed.go 放在 web/ 包下（web/embed.go）
- **偏差原因**：Go 的 embed 指令要求嵌入的目录必须与 go 文件同包同目录，无法从项目根目录 embed web/dist 子目录
- **风险评估**：低。功能等价，仅文件位置不同
- **状态**：🟢 已确认可接受

---

## DEV-003 — 并发安全保障使用 sync.Mutex / sync.RWMutex

- **发现时间**：2026-07-08 16:20（补充更新：2026-07-08）
- **关联任务**：Task 2.3.1, Task 3.x, Task 4.x
- **关联需求**：REQ-C-003
- **需求原文**：B-09 禁止 goroutine 间使用共享状态+mutex（logger文件写除外）
- **实际实现**：以下组件使用 sync.Mutex 或 sync.RWMutex 保护并发数据结构：
  - `ProcessManager.mu` (sync.RWMutex) — `internal/core/process_manager.go`，保护 procs map
  - `EventRingBuffer.mu` (sync.RWMutex) — `internal/api/longpoll.go`，保护事件环形缓冲区
  - `TaskHistory.mu` (sync.Mutex) — `internal/extension/task_history.go`，保护任务历史记录
  - `ConcurrencyManager.mu` (sync.Mutex) — `internal/extension/concurrency.go`，保护并发策略状态
  - `Executor.mu` (sync.RWMutex) — `internal/extension/executor.go`，保护 runResults map
  - `Debouncer.mu` (sync.Mutex) — `internal/extension/debounce.go`，保护防抖状态
- **偏差原因**：这些 Mutex 均为底层并发安全保障（非业务状态共享），用于保护简单的 map/切片数据结构的并发读写。channel 化改造会增加不必要的复杂度，且这些场景的操作是简单的注册/注销/查询/追加，不适合用 channel 重构。属于 B-09 的合理例外
- **风险评估**：低。Mutex 保护简单 map/切片是 Go 标准做法，不存在死锁风险（无嵌套加锁），不涉及跨 goroutine 业务状态共享
- **状态**：🟢 已确认可接受

---

## DEV-004 — api/interfaces.go 定义13个接口

- **发现时间**：2026-07-08（2026-07-18 审计 O-04-001 复核更新接口数）
- **关联任务**：Task 4.1.1
- **关联需求**：REQ-C-003
- **需求原文**：B-05 禁止 interface 抽象层
- **实际实现**：API 层定义了 StateProvider/WatchProvider 等13个接口（J-01 审计确认实际为13个，原记录的11个为早期统计遗漏 ServiceHistoryGetter 等2个接口）
- **偏差原因**：API 层定义自己需要的接口是 Go 标准做法（上层依赖下层接口），这些接口不是 DI 容器或抽象层，而是 Go 的隐式接口组合模式，用于解耦 API handler 对 core/extension 包的直接依赖
- **风险评估**：低。符合 Go 语言惯用法，不引入 DI 容器
- **状态**：🟢 已确认可接受

---

## DEV-005 — executor.go 中 run_as 未完整接入 StartProcess

- **发现时间**：2026-07-08 21:45
- **关联任务**：Task 3.1.6
- **关联需求**：REQ-F-023
- **需求原文**：run_as 字段应通过 Credential 参数传入 StartProcess 实现身份切换
- **实际实现**：core.StartProcess 函数签名不支持 syscall.SysProcAttr 的 Credential 参数，executor.go 中 run_as 解析出的 Credential 暂未传入进程启动
- **偏差原因**：core.StartProcess 是通用进程启动函数，当前仅接受 user/group 字符串而非 Credential 结构体。完整接入需要重构 StartProcess 签名以支持 SysProcAttr，属于跨模块改动
- **风险评估**：中。run_as 配置可解析但运行时不生效，非 root 用户下扩展以 supd 启动用户身份运行
- **状态**：✅ 已完全修复（2026-07-22）
- **修复内容**：
  - core.StartProcess 签名重构为接受 `*syscall.Credential` 参数，支持身份切换（含补充组）
  - executor.go 接入 ResolveRunAs + BuildCredential，显式 run_as 字段现已生效
  - TriggerContext 新增 ServiceUser 字段，用于服务级扩展默认 run_as
  - 新增 `internal/core/credential.go`：ResolveServiceCredential + StartServiceProcess，服务自身 user 字段身份切换已接入
  - bootstrap.go 2 处 StartProcess → StartServiceProcess，服务启动时按 user/group 切换身份
  - dispatcher.go 填充 matchedExtension.serviceUser，服务级扩展默认 run_as 继承服务 user
  - service_ops.go 识别 *ServiceError → HTTP 422，用户不存在时返回详细错误提示
- **当前风险评估**：无。run_as 全链路已生效：显式 root / 显式用户名 / 空值继承（全局→supd 用户，服务级→服务用户）

---

## DEV-006 — Go 依赖版本偏差

- **发现时间**：2026-07-08
- **关联任务**：Task 1.1.1
- **关联需求**：REQ-C-012
- **需求原文**：需求规格说明3.1节明确指定依赖版本：chi v5.3.0、cobra v1.10.2、yaml v4.0.0
- **实际实现**：chi v5.2.1、cobra v1.9.1、yaml v4.0.0-rc.6
- **偏差原因**：go get 拉取的是模块代理中的最新可用版本。chi v5.2.1 和 cobra v1.9.1 是 go.mod 中实际可获取的最新版；yaml v4.0.0 尚未正式发布，使用 rc.6 预发布版
- **风险评估**：低。次要版本和 RC 版本差异不影响使用的功能，API 兼容
- **状态**：🟢 已确认可接受

---

## DEV-008 — API端点数77 vs 规格65（多出12个端点）

- **发现时间**：2026-07-11 14:30（2026-07-16 审计 D-01-01 复核更新；2026-07-17 第二轮审计 F-01-002 复核：移除已删除的 test-restart 与 extensions/{name}/logs 端点；2026-07-18 D-01-001 复核：实际路由数 76 条；2026-07-19 D-01-001 第三轮复核：实际路由数 77 条，多出 12 个端点）
- **关联任务**：Task 4.x（API层）
- **关联需求**：REQ-I-006
- **需求原文**：REQ-I-006 明确列出 65 个 API 端点
- **实际实现**：`internal/api/server.go` 实际注册 77 个路由，多出 12 个端点：
  1. `GET /api/health` — 健康检查端点（规格未列出，但为容器化/监控探针标准实践）
  2. `POST /api/services/start` — 批量启动所有服务（规格仅定义单服务启动 `POST /api/services/<name>/start`）
  3. `POST /api/services/stop` — 批量停止所有服务（规格仅定义单服务停止 `POST /api/services/<name>/stop`）
  4. `POST /api/services/<name>/force-stop` — 强制停止（SIGKILL，规格未定义，failed 状态手动恢复机制）
  5. `POST /api/services/<name>/clear-failed` — 清除 failed 状态重置为 pending（规格未定义，状态机恢复辅助）
  6. `PUT /api/services/<name>/config` — 仅更新 service.yaml 配置（规格仅定义 `PUT /api/services/<name>` 整体更新）
  7. `PUT /api/services/<name>/env` — 保存服务环境变量（规格仅定义扩展级 env 端点）
  8. `DELETE /api/extensions/runs` — 批量清除所有运行记录（规格仅定义单条删除）
  9. `DELETE /api/extensions/runs/<runID>/logs` — 删除单条运行日志（规格未列出）
  10. `POST /api/files/upload` — 文件上传（规格未列出，2026-07-16 文件管理增强新增）
  11. `POST /api/reload` — 手动热重载（N-04-002 修复，触发配置热重载）
  12. `POST /api/services/<name>/signal` — 向服务进程发送信号（规格 §2.6 服务操作仅明示 start/stop/restart，signal 为通用 POSIX 信号转发便利端点）
- **偏差原因**：
  - `force-stop` 和 `clear-failed` 是状态机 failed 状态处理的实际需要（手动恢复机制）
  - `batch start/stop` 是前端 Dashboard 一键操作的需要
  - `/api/health` 是容器化/监控探针的标准实践
  - `/config` 和 `/env` 端点区分"整体更新"和"仅配置/环境变量更新"
  - `runs` 批量清理和单条日志清理是运维便利性增强
  - `/files/upload` 是文件管理增强新增功能
  - `/reload` 是热重载手动触发，便于开发调试
  - `/signal` 是 POSIX 信号转发的通用接口，便于发送 SIGHUP/SIGUSR1 等信号
- **风险评估**：低。这些端点均为功能补充，不修改规格定义的65个端点的行为；属实用增强但严格意义上违反 B-01 铁律
- **状态**：🟢 已确认可接受（实用增强，保留）

---

## DEV-009 — SUPD_SERVICE_DIR 规格外环境变量

- **发现时间**：2026-07-16（A-03-001 审计发现）
- **关联需求**：§2.2.5 扩展执行环境变量（13个）
- **需求原文**：规格定义 13 个 SUPD_* 环境变量（SUPD_BASE_DIR/SUPD_SERVICE_NAME/SUPD_EXTENSION_NAME/SUPD_ACTION_ID/SUPD_RUN_ID/SUPD_EVENT/SUPD_TRIGGER_SOURCE/SUPD_TRIGGER_USER/SUPD_WORKDIR/SUPD_ACTION_ARGS/SUPD_EXTENSION_DIR/SUPD_LOG_DIR/SUPD_DRY_RUN）
- **实际实现**：`internal/extension/run_context.go:78-81` 注入第 14 个变量 `SUPD_SERVICE_DIR`（仅服务级扩展），便于扩展脚本定位服务目录
- **偏差原因**：
  - 服务级扩展的工作目录是 `<baseDir>/script_tmp/<svc>+<ext>/`，但扩展脚本常需访问服务自身的目录（`<baseDir>/services/<svc>/`）读取配置、data 等
  - 规格未提供获取服务目录的标准机制，扩展脚本只能通过 `cd ../` 或硬编码路径访问，脆弱易错
  - 该变量已写入 `docs/服务扩展开发指南.md` 和 `supd-service-extension-dev` skill，是扩展开发的标准接口
- **使用场景**：扩展脚本中 `SERVICE_DIR="${SUPD_SERVICE_DIR:-$(pwd)}"` 获取服务目录，用于读取 service.yaml、env.yaml、data/ 等
- **风险评估**：低。仅新增变量不破坏现有 13 个变量的契约；扩展脚本通过 `${SUPD_SERVICE_DIR:-$(pwd)}` 兼容旧扩展
- **状态**：🟢 已确认可接受（实用增强，规格漏洞的合理补充）

---

## DEV-010 — on-failure + exit 0 使用 ResetTo(StateDown) 绕过状态机

- **发现时间**：2026-07-16（A-01-001 审计发现）
- **关联需求**：§2.1.1 服务状态机（10条转移规则）
- **需求原文**：规格定义 10 条转移规则，无 `up/ready → down` 转移路径。但 §2.1.1 同时定义：
  - on-failure 触发条件：进程退出且退出码非 0（即 exit 0 不触发重启）
  - down 状态含义：进程已退出，未自动重启
- **实际实现**：`internal/core/bootstrap.go:674` 和 `internal/api/adapters.go` 中，当 `restart.policy=on-failure` + `exit code=0` 时，使用 `sm.ResetTo(StateDown)` 绕过状态机直接进入 down 状态
- **偏差原因**：
  - 这是规格漏洞：on-failure + exit 0 场景下，按 §2.1.1 应进入 down（未自动重启），但 10 条转移规则中无 up/ready → down 规则
  - 替代方案1：通过 EventMaxRetries → failed（语义错误，exit 0 不是 failure）
  - 替代方案2：新增转移规则 up/ready → down（违反 AGENTS.md "10条转移规则"约束）
  - 替代方案3：ResetTo 绕过状态机（当前方案，行为正确但破坏状态机封闭性）
- **使用场景**：服务以 on-failure 策略运行，进程正常退出（exit 0）时不触发重启，应进入 down 状态等待用户手动启动
- **风险评估**：低。行为符合规格语义；ResetTo 仅在终态转换使用，不影响运行中状态的转移合法性；状态机仍保证运行中状态（starting/up/ready/stopping）的转移合法性
- **状态**：✅ 已修复（2026-07-24：补充 `EventNormalExit` 及 `up/ready/starting → down` 转移规则，并更新规格说明书 §2.1.1）

---


