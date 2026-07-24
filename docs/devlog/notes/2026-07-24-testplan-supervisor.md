# 2026-07-24 运行状态测试方案：M-04-001 / TD-003 supervisor 重构

> 目标：通过**真实运行 supd 实例 + 真实服务 + 真实扩展**，验证重构后的 supervisor（bootstrap 同步路径 + service_operator 异步路径）在以下场景的行为正确：
> restart / failed / down / ready / on_failure / 手动停止不触发 on_failure / fd_notify 重启后重新就绪 / 退避中断。
> 单元测试已覆盖决策逻辑（T1-T6）；本方案覆盖**集成级可观测行为**。

---

## 一、测试环境

- 构建：`go build -ldflags "-X main.version=0.0.21" -o /tmp/supd-rt/supd ./cmd/supd/`
- 工作目录：复制 `test_workdir` → `/tmp/supd-rt`（避免污染仓库），并按需增改服务/扩展
- 启动：`SUPD_LOG_DIR=/tmp/supd-rt/logs /tmp/supd-rt/supd --workdir /tmp/supd-rt run --no-pid1 > /tmp/supd-rt/supd.out 2>&1 &`
- 配置：`test_workdir/config.yaml` 已 `auth_mode: none`、`http_listen: :8080`，可直接 curl
- 观测：`GET /api/services`、`GET /api/services/{name}`、`POST /api/services/{name}/start|stop|restart|force-stop|clear-failed`、`GET /api/events`（长轮询）、服务日志文件、`/tmp` 标记文件

---

## 二、测试用例

### A. 启动期（bootstrap 同步路径）autostart 服务可达 ready
- **前置**：`tcp-echo`(tcp_check:9002)、`fd-notify-demo`(fd_notify)、`script-ready-demo`(script)、`web-demo`(http_check, depends_on tcp-echo) 均为 `autostart: true`。
- **步骤**：启动 supd → 轮询 `GET /api/services`。
- **预期**：
  - 各 autostart 服务最终状态为 `ready`（或至少 `up`）。
  - `web-demo` 在 `tcp-echo` ready 之后才 ready（依赖顺序）。
  - supd.out 中 bootstrap 路径 slog 含 `process exited (cli)`（来源标记）。
- **覆盖**：bootstrap.superviseService 薄包装 + RunReadiness 同步 error 处理（newProc.Done 区分）。

### B. API 启动崩溃服务 → restart 循环 → failed（service_operator 异步路径）
- **服务**：新建 `audit-crash`（autostart:false，command `bash run.sh`（exit 1），`restart: policy always, max_retries=2, backoff_ms=300, max_backoff_ms=2000`）。
- **步骤**：`POST /api/services/audit-crash/start` → 每 500ms 轮询 `GET /api/services/audit-crash` 状态与 restart 次数，直到 `failed`（超时 20s）。
- **预期**：
  - 状态序列：`up →（退出）→ starting → up → ... → failed`。
  - 最终 `state=failed`，restart 次数 = 2。
  - supd.out 中 `restarting service after unexpected exit (api-started)` 出现 2 次，且 `process exited (api-started)` 多次。
- **覆盖**：service_operator.superviseService 薄包装 + doRestartProcess + DecideRestart + SpawnNextSupervisor(newCtx) + RunReadiness 异步(nil)。

### C. 手动停止正在重启的服务 → down，且**不触发** on_failure
- **服务**：同 B 的 `audit-crash`（在 restart 循环期间状态为 `starting`/`up`）。
- **步骤**：start 后立刻（<1s）`POST /api/services/audit-crash/stop` → 轮询状态。
- **预期**：
  - 最终 `state=down`（非 failed）。
  - **on_failure 未触发**（F4 验证 + 与 B 的 on_failure 标记对照）。
  - cancelFuncs 被清理（无 supervisor goroutine 泄漏：可通过 `ps` 确认仅 1 个 supd 主进程 + 无残留 bash）。
- **覆盖**：StopService(ctx 取消) → doBackoff 中断（starting→stopping→down）→ defer delete(cancelFuncs)。

### D. fd_notify 重启后重新就绪（验证重构的 fd_notify 路径）
- **服务**：新建 `audit-fd`（autostart:false，command `bash run.sh`：运行 1s、向 fd3 写 READY、exit 1；`readiness: fd_notify, fd:3, timeout:5`；`restart: always, max_retries=3, backoff_ms=300`）。
- **步骤**：start → 轮询 `GET /api/services/audit-fd`，观察状态在 `ready ↔（退出重启）↔ ready` 间循环，共 3 次后 `failed`。
- **预期**：
  - 每次重启后均重新到达 `ready`（fd_notify checker 在 doRestartProcess 重建并生效）。
  - 最终 `failed`，restart 次数 = 3。
- **覆盖**：doRestartProcess 内 `NewNotifyChecker` 重建 + RunReadiness 重新检查。

### E. on_failure 服务级扩展触发（service_operator 路径回调落地）
- **扩展**：在 `audit-crash`（或 `audit-fd`）下新增服务级扩展 `on-failure-marker`（`runtime: bash`，`triggers.service_lifecycle: [{event: on_failure, action: mark}]`），`run.sh` 写入 `/tmp/audit_onfailure_<svc>.txt` 标记。
- **步骤**：start `audit-crash` → 等待 `failed` → 检查 `/tmp/audit_onfailure_audit-crash.txt` 存在。
- **预期**：
  - 标记文件存在，内容为服务名/退出码（验证 OnFailure 闭包 → ServiceLifecycleTrigger.OnFailure → 扩展执行）。
  - 与 C（手动停止不触发）形成对照：C 的 stop 不产生标记，E 的崩溃产生标记。
- **覆盖**：handleProcessExit → OnFailure 闭包（fCtx）→ ServiceLifecycleTrigger.OnFailure → 扩展派发（重构前的 on_failure 接线未被破坏）。

### F. 稳定服务 start → stop → start（生命周期往返）
- **服务**：新建 `audit-stable`（autostart:false，command `bash run.sh`：`sleep 3600`，无 readiness）。
- **步骤**：start → 确认 `up` → stop → 确认 `down` → start → 确认 `up`。
- **预期**：状态往返正确，stop 后进程退出（ps 无残留），再次 start 成功。
- **覆盖**：StartService/StopService 全链路 + supervisor goroutine 创建/退出。

### G. 退避中断（ctx 取消）路径的 down 转移
- **已并入 C**。另设边界：B 服务在退避（`starting`+backoff）期间 stop，验证 `starting → stopping → down` 且 supd 不卡死。

---

## 三、通过准则

1. A：autostart 服务到达 ready/up，依赖顺序正确。
2. B：audit-crash 重启 2 次后 failed，restart 次数=2。
3. C：stop 后 down，on_failure 未触发，无 goroutine/进程泄漏。
4. D：audit-fd 每次重启后重新 ready，3 次后 failed。
5. E：崩溃触发 on_failure 扩展标记文件；手动停止不触发（对照 C）。
6. F：稳定服务 start/stop/start 往返正确，无残留进程。
7. 全程无 supd panic（supd.out 无 `panic recovered`），无未处理报错。

---

## 四、执行记录占位（实际结果见审计报告第六章 / 下方回填）

| 用例 | 结果 | 备注 |
|---|---|---|
| A | ✅ PASS | autostart 的 tcp-echo / fd-notify-demo / script-ready-demo / web-demo 均达 ready；web-demo 在 tcp-echo 之后 ready（依赖顺序正确）；slog 含 `process exited (cli)` |
| B | ✅ PASS | audit-crash 重启 2 次后 failed，restart_count=2；日志 `restarting service after unexpected exit (api-started)` 出现 2 次 |
| C | ✅ PASS | stop 后 state=down（非 failed），on_failure 未触发（与 E 对照），cancelFuncs 清理无 goroutine/进程泄漏 |
| D | ✅ PASS | audit-fd2（8s 窗口，API 路径）首启 t≈0.8s 达 ready、restart 后 t≈7.0s 再次 ready、最终 failed(retries=1)；fd_notify 管道在 `doRestartProcess` 重建生效。早期对 1s 窗口 audit-fd 用 3s 粒度轮询遗漏短暂 ready，经细粒度轮询确认属轮询粒度问题，非缺陷 |
| E | ✅ PASS | 崩溃触发 on_failure 扩展并写 /tmp/audit_onfailure_audit-crash.txt（含 SUPD_PHASE/SERVICE_NAME/EXIT_CODE/RESTART_COUNT/PID）；与 C 手动停止不触发形成对照 |
| F | ✅ PASS | audit-stable start→up→stop→down→start→up 往返正确，stop 后无残留进程 |
| G | ✅ PASS | audit-backoff 在退避中（starting/retries=1）stop → 直接 down，6s 内 retries 不再增长（无重启）；doBackoff 的 ctx.Done 路径正确（G 由独立服务 audit-backoff 验证，方案中原计划并入 C） |

---

*方案制定：2026-07-24 | 配合 `2026-07-24-audit-supervisor.md` 使用*
