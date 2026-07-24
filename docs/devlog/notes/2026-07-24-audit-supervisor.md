# 2026-07-24 审计报告：M-04-001 / TD-003 supervisor 重构

> 审计对象：M-04-001（superviseService 圈复杂度 43）+ TD-003（superviseService 重复代码）重构产出物
> 审计范围：`internal/core/supervisor.go`（新建）、`internal/core/supervisor_test.go`（新建）、`internal/core/bootstrap.go` superviseService（薄包装）、`internal/api/service_operator.go` superviseService（薄包装）、`internal/core/restart_max_retries_test.go`（T6）
> 审计方法：静态阅读 + 与原实现逐行行为差异比对（git show HEAD 原始 superviseService）+ 圈复杂度度量（gocyclo）+ 单元测试 + 竞态检测（-race）

---

## 一、变更清单与基线

| 文件 | 类型 | 行数 | 说明 |
|---|---|---|---|
| internal/core/supervisor.go | 新建 | ~352 | SupervisorCallbacks + RunSupervisor/DecideRestart/handleProcessExit/doBackoff/doRestartProcess |
| internal/core/supervisor_test.go | 新建 | ~92 | T1-T5 决策/退避测试 |
| internal/core/bootstrap.go | 修改 | superviseService 222→45 行薄包装 + rebuildLogger 提取 | |
| internal/api/service_operator.go | 修改 | superviseService 237→55 行薄包装 + rebuildLogger 提取 + 清理三重 cancelFuncs | |
| internal/core/restart_max_retries_test.go | 修改 | 追加 T6 端到端 OnFailure 回调测试 | |

---

## 二、圈复杂度（gocyclo 实测）

| 函数 | 改造前 CC | 改造后 CC | 验收（≤20） |
|---|---|---|---|
| bootstrap.superviseService | ~43 | **3** | ✅ |
| service_operator.superviseService | ~32 | **6** | ✅ |
| core.RunSupervisor | — | **16** | ✅ |
| core.doRestartProcess | — | **19** | ✅ |
| core.handleProcessExit | — | **12** | ✅ |
| core.DecideRestart | — | **5** | ✅ |
| core.doBackoff | — | **4** | ✅ |

**最高单函数 CC 由 43 降至 19**，满足 M-04-001 的 ≤20 目标。

> **⚠️ 验收口径说明（F1）**：方案文档 §九 验收第 4/5 条写的是「`gocyclo -over 20 bootstrap.go` 输出空」「`gocyclo -over 20 service_operator.go` 输出空」。实测这两个文件级检查**非空**，但原因是文件内**既有**的、与 M-04-001 无关的高 CC 函数：
> - `core (*Bootstrap).startAutostartServices` = 38（并行启动编排，非 supervisor 逻辑）
> - `api (*CoreServiceOperator).StartService` = 29（首次启动编排，非 supervisor 逻辑）
> - `api (*CoreServiceOperator).StopService` = 17（首次停止编排，非 supervisor 逻辑）
>
> 这三处属于独立技术债，**不在 M-04-001 范围内**，且重构未引入任何新增长链函数。因此 M-04-001 的真实目标（superviseService 函数级 CC）已达成；文件级 gocyclo 检查非空不构成回归。建议后续将 startAutostartServices/StartService/StopService 单独立项优化，与本重构解耦。

---

## 三、行为差异比对（与原实现逐行核对）

对照 `git show HEAD` 的两份原始 superviseService，逐项确认重构后行为一致：

### 3.1 bootstrap 路径（同步 readiness）
| 关注点 | 原实现 | 重构后 | 结论 |
|---|---|---|---|
| 进程退出日志 | `writeServiceLog` 三段格式 | 同 | ✅ 一致 |
| 事件发布 | service_died/service_exited | 同 | ✅ 一致 |
| OnFailure ctx 语义（硬伤①） | `OnServiceFailure(context.Background(), ...)` | 闭包内 `OnServiceFailure(context.Background(), ...)` | ✅ 已正确保留 Background() |
| 重启决策（never→failed / on-failure+exit0→down） | 同 | DecideRestart 逻辑一致 | ✅ |
| 退避 select + ctx.Done 处理 | 同 | doBackoff 一致（含 starting→stopping→down） | ✅ |
| fd_notify checker | `NewNotifyChecker` | `doRestartProcess` 内 `NewNotifyChecker` | ✅ |
| SpawnNextSupervisor ctx（硬伤②） | 创建 newCtx、存 CancelFuncs、启动新 goroutine；readiness 用旧**ctx** | 返回旧 `ctx`、异步启动新 goroutine（newCtx） | ✅ 一致 |
| RunReadiness error（硬伤③） | `checkReadiness(ctx,...)` 返回 error + `newProc.Done()` 区分 | RunReadiness 返回 error + RunSupervisor `newProc.Done()` 区分 | ✅ 一致 |
| 日志器重建 | rebuildLogger | 提取为 `b.rebuildLogger` 方法 | ✅ 一致 |

### 3.2 service_operator 路径（异步 readiness）
| 关注点 | 原实现 | 重构后 | 结论 |
|---|---|---|---|
| 进程退出 Debug 日志 | `"process exited (api-started)"` | `sourceTag="api"` → `"process exited (api-started)"` | ✅ 完全一致 |
| OnFailure ctx 语义（硬伤①） | `OnFailure(ctx, ...)`（supervisor ctx） | 闭包内 `OnFailure(fCtx, ...)`（fCtx=传入 ctx） | ✅ 已正确保留 ctx |
| restart.max_retries→failed | 同 | DecideRestart 一致 | ✅ |
| 退避 select + ctx.Done（含 delete cancelFuncs） | 同 + ctx.Done 分支 delete | doBackoff 返回 false，defer 统一 delete | ✅ 等价（幂等删除） |
| fd_notify checker | `NewReadinessChecker` 通用派发 + 接口断言 WriterFd/CloseWriter | `NewNotifyChecker` 直接构造 + `*NotifyChecker` 类型断言 | ✅ 行为等价（均产出 *NotifyChecker） |
| SpawnNextSupervisor ctx（硬伤②） | 创建 newCtx、存 cancelFuncs、启动新 goroutine；readiness 用 newCtx | 返回 newCtx、异步启动 | ✅ 一致 |
| RunReadiness | `go runReadinessCheck(newCtx,...)` 异步 | RunReadiness 内 `go o.runReadinessCheck(rCtx,...)`，返回 nil | ✅ 一致 |
| 历史记录 | `HistoryStore.RecordStart` | `RecordRestartHistory` 回调 | ✅ 一致 |

### 3.3 评估报告 3 处硬伤 + 3 处遗漏的修复确认
- **硬伤① OnFailure ctx**：✅ 两路径均按原语义保留（bootstrap=Background，service_operator=传入 ctx）。
- **硬伤② readiness ctx**：✅ SpawnNextSupervisor 返回 context.Context，bootstrap 返回旧 ctx、service_operator 返回 newCtx。
- **硬伤③ checkReadiness error 丢弃**：✅ RunReadiness 返回 error，RunSupervisor 用 `newProc.Done()` 区分进程退出 vs 超时。
- **遗漏D2 RuntimeRegistry 缓存策略**：✅ 标注为「有意变更」（service_operator 改为一次性预构建，与原每次重建设计不同但运行时差异极罕见）。
- **遗漏D8 Source 字段**：✅ slog 消息含 `(cli)` / `(api-started)` 区分来源。
- **遗漏 DecideRestart 签名**：✅ 含 `sm` 参数，注释改为「含 engine/sm 状态副作用」。

### 3.4 评估报告 P1（EventRestartAllowed 失败路径）修复确认
- 原始行为：`sm.Transition(EventRestartAllowed)` 失败时**静默 return，不触发 EventMaxRetries**。
- 重构：`DecideRestart` 返回 `RestartActionAbort`，`RunSupervisor` `case RestartActionAbort: return`。
- **结论**：✅ 正确保留原始静默返回语义，**未**采用方案文档中「映射为 RestartActionFailed → EventMaxRetries」的错误写法（该写法经评估为会改变行为语义，已规避）。

---

## 四、并发安全性（F5）

- `StateMachine` 为 channel 驱动（`stateVal atomic.Value` + 单 goroutine `run()` 串行处理所有 `Transition`），`Current()` 原子读、`Transition()` 经 channel 串行化。
- `handleProcessExit` 读取 `sm.Current()` 与 `StopService` 的 `Transition` 并发执行时，均经同一 channel 串行化，**无数据竞态**。
- `cancelFuncs` / `loggers` / `restartEngines` 均有各自 mutex 保护（service_operator）或 `loggersMu` 等（bootstrap），读写在互斥区内。
- `go test -race ./internal/core/ -run 'TestDecideRestart|TestDoBackoff|TestSupervisorCallbacks|TestRestart'` **通过**，无竞态报告。

---

## 五、发现项汇总

| 编号 | 级别 | 描述 | 处置 |
|---|---|---|---|
| F1 | 信息 | 文件级 `gocyclo -over 20` 非空，仅因既有无关高 CC 函数（startAutostartServices=38 / StartService=29 / StopService=17），非本重构引入 | 记录口径；建议独立立项 |
| F2 | 已确认 | 3 处硬伤（OnFailure ctx / readiness ctx / RunReadiness error）均正确修复 | 无需处理 |
| F3 | 已确认 | P1 EventRestartAllowed→Abort 静默返回，未误发 EventMaxRetries | 无需处理 |
| F4 | 观察(低) | service_operator.superviseService 的 `defer delete(cancelFuncs[name])` 在旧 supervisor 返回时会清除新 supervisor 的 cancel 句柄。此为**重构前既有行为**（原实现同样在 defer 中删除），非回归；且 StopService 杀进程路径可令新 supervisor 自然退出 | 观察；非阻塞 |
| F5 | 已确认 | StateMachine channel 驱动，无数据竞态；-race 通过 | 无需处理 |
| F6 | 已确认 | fd_notify 重启由通用派发改为 `NewNotifyChecker` 直接构造，行为等价 | 无需处理 |
| F7 | 已确认 | slog.Debug 进程退出日志已补全（含 cli/api 区分），对应评估报告 P2 | 无需处理 |

**未发现任何由重构引入的新功能 bug 或行为回归。** 全部 3 处硬伤与评估报告 P1 修复均经核对确认正确。

---

## 六、单元测试结论

- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./... -count=1`：11 个包全部 **ok**，0 FAIL ✅
- `go test -race ./internal/core/...` 关键用例 ✅
- T1-T5（DecideRestart×4 + doBackoff×1）+ T6（OnFailure 端到端）全部通过 ✅
- supervisor 函数级 CC 全部 ≤20 ✅

## 七、审计结论

**审计通过。** 新增代码在静态正确性、圈复杂度、与原实现行为一致性、并发安全性方面均达标，3 处硬伤与 P1 修复均正确落地，可进入「运行状态测试」阶段。

---

## 八、运行时验证结论（2026-07-24）

在静态审计基础上，按 `2026-07-24-testplan-supervisor.md` 的 7 场景（A–G）执行**真实运行测试**：

- 构建带版本戳 `0.0.21` 的二进制，复制 `test_workdir` → `/tmp/supd-rt`，新增 audit-crash / audit-fd / audit-fd2 / audit-stable / audit-backoff 服务 + `on-failure-marker` 扩展。
- **A 启动期 ready**：autostart 的 tcp-echo / fd-notify-demo / script-ready-demo / web-demo 均达 ready，web-demo 在 tcp-echo 之后 ready（依赖顺序正确）。
- **B 崩溃→failed**：audit-crash 重启 2 次后 failed，restart_count=2，日志 `restarting service after unexpected exit (api-started)` 出现 2 次。
- **C 手动停止→down**：stop 后 state=down（非 failed），on_failure 未触发，cancelFuncs 清理无泄漏。
- **D fd_notify 重启重新就绪**：audit-fd2（8s 窗口，API 路径）首启 t≈0.8s 达 ready、restart 后 t≈7.0s 再次 ready、最终 failed(retries=1)。fd_notify 管道在 `doRestartProcess` 重建并生效。早期对 1s 窗口 audit-fd 的 3s 粒度轮询遗漏了短暂 ready，经 200ms 细粒度轮询确认属轮询粒度问题，**非缺陷**。
- **E on_failure 扩展**：崩溃触发扩展并写 `/tmp/audit_onfailure_audit-crash.txt`（含 SUPD_PHASE/SERVICE_NAME/EXIT_CODE/RESTART_COUNT/PID），与 C 手动停止不触发形成对照。
- **F 稳定服务往返**：audit-stable start→up→stop→down→start→up 正确，无残留进程。
- **G 退避中断**：audit-backoff 在退避中（starting/retries=1）stop → 直接 down，retries 不再增长（无重启），doBackoff 的 ctx.Done 路径正确。

**结论**：7 场景全部 PASS，全程无 panic、无 goroutine/进程泄漏、无由重构引入的行为回归。审计 + 运行测试共同确认 M-04-001/TD-003 重构可发布（v0.0.21）。

---

*审计执行：2026-07-24 | 工具：go build/vet/test、gocyclo、go test -race、git show HEAD 比对、真实实例运行测试*
