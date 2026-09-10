# supd 开发会话备忘（主索引）

> 跨会话上下文传递。Agent 新会话启动时首先阅读本文件（主索引）+ `blockers.md`。
> 历史日志已压缩归档至 `archive/notes-20260721-20260901-source.md`（**仅保留 supd 仓库源码/文档调整**；对 188/190 服务器的远程运维操作不记录于开发日志）。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。核心机制备忘见 `notes/core-mechanisms.md`。

---

## 一、项目状态

- **阶段**：维护/修复/测试阶段（57 Task 全部完成，8 阶段任务执行计划闭合）
- **质量水位**：⭐ 优秀，1000+ 单元测试通过（Go + 前端），零竞态；go vet 零警告
- **当前版本**：v0.1.1（操作中心 + 通知中心全面审计修复；版本升级见 `version-upgrade-guide.md`）

### 验证命令（每次改动后必跑）
```bash
go build ./... && go vet ./... && go test ./... -count=1   # 后端
cd web && pnpm build                                        # 前端（改前端后必须 go build 重新嵌入二进制）
SUPD_LOG_DIR=/tmp/supd-logs ./supd --workdir test_workdir run  # 服务启动（测试用）
```

---

## 二、核心机制摘要

> 详细备忘见 `notes/core-mechanisms.md`（涉及底层机制时按需读取）

- **生命周期**：`starting→up→ready`（唯一就绪路径）、`stopping→down`；自动重启不经过 down；`autostart:false` 初始为 `down`（§2.8.1）
- **环境变量**：4 层合并（os.Environ → 全局 env 文件 → 服务 env.yaml → 扩展 env.yaml）；`env.yaml` 必须含 `env:` 包装层；`enabled:false` 不注入
- **身份权限**：User 模式与 UID 模式互斥；服务 user/uid 空=继承 supd；服务级扩展空=继承服务身份；全局扩展空=继承 supd；服务严格拒绝/扩展宽松警告
- **关机**：单一 `shutdown_grace_seconds` 预算贯穿 cron stop / 扩展等待 / GracefulShutdown / HTTP Stop
- **PID1**：supd 自带 PR_SET_CHILD_SUBREAPER + SIGCHLD 回收；Docker 中禁用 `--no-pid1`；维护 PID 文件清理孤儿进程
- **前端嵌入**：`//go:embed dist` 在 `web/embed.go`，改前端后必须 `pnpm build` + `go build` 才能生效
- **watcher**：白名单只监控配置目录；黑名单 data/bin/logs/history/cache/tmp/temp/run；fsnotify 防抖 500ms
- **端口探测**：受管 PID 进程树 fd socket inode 精确匹配；Docker 部署需 `cap_add: SYS_PTRACE`
- **路径解析**：全局 env_files/extension_dirs/runtimes 基于 `<baseDir>`；服务 command[0]/workdir/script readiness check[0] 基于服务根；**扩展相对 entry 与进程 CWD 基于扩展自身目录（meta.yaml 所在目录）**；裸命令保留 PATH 查找；`BuildRegistryAt(baseDir, ...)` 统一解析 runtime 路径

---

## 三、已知偏差与待办

> **当前状态**：无活动偏差，无阻断。R-01～R-09 技术改进项已全部闭环（详见 `deviations.md` 与压缩归档）。

---

## 四、关键决策

- 不引入数据库、不引入 SSE/WebSocket（长轮询是规格要求）
- 不引入 tini/dumb-init（supd 自带 PID 1 能力）
- triggers 格式用 map（规格 v1.5 §2.2.3）
- meta.yaml 中 `service:` 字段冗余（服务关联由目录结构决定）
- 开发日志只记录 supd 仓库源码/文档调整，远程服务器运维操作不入日志（用户决定 2026-09-01）
- dropbear-ssh 是 supd 管理的普通服务（非 entrypoint 脚本），autostart: false

---

## 五、下次会话注意

- 改前端后必须 `pnpm build` + `go build` 重新嵌入二进制，否则看不到效果
- `NewReadinessChecker(cfg, dir, env)` 为 3 参数；`OnFailure` 含 `servicePID int`；`CronScheduler.Stop(ctx)` 带 context
- env.yaml 必须含 `env:` 包装层，直接写 `KEY: value` 会被静默忽略
- 前端所有 env 编辑器统一用 `web/src/lib/env-yaml` 共享工具
- 服务与扩展的非 root 语义差异需保持（服务严格拒绝、扩展宽松警告）
- Docker 镜像需重新构建才能包含 Dockerfile 变更
- 监控 yaml v4 稳定版发布后升级 go.mod
- tjs `proc.wait()` 返回 `{exit_status, term_signal}`（**不是** `exitCode`）；tjs 无 Buffer 全局；大文件下载必须流式读取（`arrayBuffer()` 会死锁）

---

## 六、会话历史索引

> 全部历史会话的源码调整详情见压缩归档 [archive/notes-20260721-20260901-source.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/archive/notes-20260721-20260901-source.md)（按日期分段，含各版本变更与技术要点）。

| 阶段 | 主题 |
|------|------|
| 07-21 ~ 07-26（v0.0.1~v0.0.33） | Docker/tjs/CI 集成；script readiness/user 字段/服务 env.yaml/身份系统等重大修复；Skill 重构与 references 体系；supervisor 重构（M-04-001/TD-003）；测试覆盖率闭环；GHCR latest 回退修复 |
| 07-27 ~ 07-31（v0.0.34~v0.0.40） | R-01~R-09 技术改进闭环；规格一致性审计 A~H 批次整改；F2-001 serialize 队列满；clear-failed 改 down；热重载矩阵修正 |
| 08-02 ~ 08-05（v0.0.41~v0.0.46） | Skill 目录契约完善；v0.0.42 多服务同名扩展竞态；v0.0.43/44/45 路径统一三部曲；LogViewer 时间戳修复 |
| 08-31 ~ 09-01（v0.0.47~v0.0.54） | 扩展 CWD/entry 解析根回归修复；Dockerfile 镜像层增量更新；tjs Release 构建缓存全链路（v0.0.50~54）；Skill 底包 libc 双向门禁 |
| 09-08 | 操作中心与通知中心设计 | 完成 stdout-only、持久化通知、独立页面方案及三轮审计修订，注册方式改为全局扩展 action.operations，尚未实施 | [notes/2026-09-08.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-08.md) |
| 09-09 | 最终方案定稿（v4） | SQLite 持久化定稿；代码核实确认 DB 路径/TriggerUser/服务阶段并发/runResults 治理等 8 项；用户确认 8 Tab+顶栏铃铛、仅红点；全部待决事项关闭 | [notes/2026-09-09.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-09.md) |
| 09-09 | v5 三角色审计定稿 | v4 断言逐项代码复核全部为真；补执行历史保留（30 天/200 条）、Topic 有序关闭消竞态等 10 项细化；用户确认红点范围/混排列表/同页两区 | [notes/2026-09-09.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-09.md) |
| 09-09 | 节点 10 联调验收与发布收尾 | 操作+通知中心全链路 E2E（O1~O10+N1~N10+异常路径）通过，SQLite 落库证据；示例 11/12 交付、validate_dev.py operations 解析修复；质量门禁 5 命令全绿；§十三 8 项闭合（API 实测 90 端点）；Skill/规格"规划中→已实现"回填；release note 草稿（WAL/体积）| [notes/2026-09-09.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-09.md) |
| 09-10 | 操作中心与通知中心全面审计修复 | 修复长轮询超限 503/SERVICE_BUSY、服务扩展跨服务回退、Topic 按 closed_at 保留；补充作用域回归测试；build/vet/test/pnpm build 全部通过 | [notes/2026-09-10.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-10.md) |
| 09-10 | 实施收尾 + v0.1.0 发布 | 全部 10 节点实施完成；补充操作服务阶段 SUPD_SERVICE/SUPD_SERVICE_DIR 注入（RunGateway 按服务作用域解析）；清理 .workbuddy/skills 遗留副本；README/变更记录升 v0.1.0；本地提交 f234553 + tag v0.1.0（远程推送因网络不可达未完成，待网络恢复）| [notes/2026-09-09.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-09.md) |
| 09-10 | 第二轮全面审计（后端/前端/Skill） | 修复 12 项（🔴2：上轮幂等修复被误改为无锁直写 map、前端幂等键复用吞掉重触发；🟠3：retention 软删 Topic 永不清理、铃铛红点滞留、长轮询 toast 风暴；🟡7）；Skill 修复示例 11 printf 与 validate_dev.py 三处；验证 5 命令全绿 | [notes/2026-09-10.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-10.md) |
| 09-10 | 第四轮全面审计 + 运行状态测试 | 修复 15 项（🟠4：danger 确认丢参数、幂等键 stale 复用、MarkRead TOCTOU→原子 MarkReadToLast、responders null；🟡6/🔵5）；第五阶段运行状态测试 15 项全 PASS（API/浏览器/工具/生命周期）；build/vet/test/-race/pnpm build 全绿 | [notes/2026-09-10.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-10.md) |
| 09-10 | 第五轮全面审计与全矩阵实测 | 修复 pruneExecutions 排序致逆向淘汰最新执行、显式级联删除 operation_run、前端 URL query 双向联动与判空兜底；15 项全矩阵运行状态测试全绿；服务持续运行 | [notes/2026-09-10.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-10.md) |
| 09-10 | 第六轮全面审计与实例运行状态实测 | 修复操作历史表格失败展示误显"成功"（Issue 1）、通知中心 URL 参数残留（Issue 2）、OperationsTagInput 重复 key 警告（Issue 3）；制定运行状态测试方案并实机验收 7 项全 PASS；测试服务保持运行 | [notes/2026-09-10.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-10.md) |
| 09-10 | 最终审计 + 测试 + v0.1.1 发布 | 全量质量门禁通过（build/vet/test-race/pnpm build/version 注入/validate_dev.py 12 示例 0 err 0 warn）；抽查关键修复断言属实；README/升级指南/session-notes 升 v0.1.1；提交 + tag v0.1.1 | [notes/2026-09-10.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-10.md) |

---

## 七、最近会话重点（2026-09-10：最终审计 + 测试 + v0.1.1 发布）

- **最终质量门禁（全绿）**：
  - `go build ./...` ✅、`go vet ./...` ✅（零警告）、`go test ./... -count=1` ✅（14 个包，含 extension 134s/core 42s/api 8s）。
  - `go test -race ./internal/api ./internal/store ./internal/extension ./internal/notification ./internal/stream ./internal/logging` ✅（零竞态）。
  - `cd web && pnpm build` ✅（TS 严格编译 + Vite 构建，生成 Operations/Notifications/octagon-alert chunk）。
  - 版本注入：`go build -ldflags "-X main.version=0.1.1"` → `supd 0.1.1` ✅。
  - `validate_dev.py`：skill 下 12 个示例逐一校验，0 error 0 warning（含示例 11/12 operations 解析）。
- **最终审计抽查**：确认关键修复已在代码中落地——`MarkReadToLast` 原子推进命令（commands.go:393）、幂等 `idempotentPut`（互斥锁+清理）、`pruneExecutions` 改 `ORDER BY created_at DESC`（retention.go:95）、execution 查询 `ORDER BY created_at DESC`、responders 空值序列化。
- **版本升级 v0.1.1（PATCH，本次所有缺陷修复）**：README 3 处版本号、`version-upgrade-guide.md` 变更记录追加 v0.1.1 行。
- **发布动作**：提交全部变更 + annotated tag `v0.1.1` + `git push origin main` 与 `git push origin v0.1.1`（含此前待推送的 v0.1.0 提交与 tag）。
- 详细证据见 `docs/devlog/notes/2026-09-10.md`「最终审计与 v0.1.1 发布」章节。

### 更早会话重点：第六轮全面审计、3处缺陷修复与实例运行状态实测

- **审计与 3 处缺陷修复**：
  - 🟠 **Issue 1（操作历史表格失败状态误显"成功"）**：`web/src/pages/Operations.tsx` 执行历史表格仅判断 `ex.finished_at` 存在即展示绿色"成功"徽章（`finishedSuccess`），未检查 runs 中的失败状态。修复：引入 `hasFailed` 检查，当 finished 且存在失败 run 时正确呈现 `finishedFailed`（失败）红色 danger 徽章及 `XCircle` 图标。
  - 🟡 **Issue 2（通知中心 URL Query 参数残留）**：`web/src/pages/Notifications.tsx` 在用户折叠详情、删除单项或清空全部时未同步清除 URL 栏的 `?topic=<id>` 参数，导致刷新或后退仍旧定位旧/已删主题。修复：在 `handleExpand` 折叠、`handleDelete` 及 `handleClearAll` 中加入 `if (urlTopic) navigate('/notifications', { replace: true })`。
  - 🟡 **Issue 3（扩展配置 OperationsTagInput React Key 冲突）**：`web/src/components/extension/OperationsTagInput.tsx` 使用 `key={op}` 导致重复标签出现 key warning。修复：改为 `${op}-${index}` 复合 key 并按 index 过滤删除。
- **实例运行状态实测（7 项全部 PASS，`tmp/运行状态测试方案.md` 实测与 SQLite 证据）**：
  - **TC-01（DB & WAL）**：SQLite WAL 模式及 4 张核心表结构完整就绪。
  - **TC-02（并发幂等）**：30 并发高压触发相同 `Idempotency-Key`，全部返回 HTTP 200 且返回同一 `execution_id`，SQLite 中严格仅插入 1 条记录。
  - **TC-03（失败状态修复实测）**：触发 `fail-demo` 操作返回 exit code 2，后端记录 `state: "failed"`，前端表格准确展示红色"失败"徽标。
  - **TC-04（通知行协议与原子游标）**：行协议生成通知，调用 `/read` 推进原子游标至 `read_seq == last_seq == 2`，未读红点归零。
  - **TC-07（Skill 工具脚本）**：`validate_dev.py` 严格校验通过。
- **质量门禁与验证**：
  - `go build ./...`、`go vet ./...`、`go test ./... -count=1`（14 个包全部 PASS）。
  - `go test -race ./internal/api ./internal/store ./internal/extension`（高并发竞争检测全部 PASS，0 race）。
  - `cd web && pnpm build`（TypeScript 严格编译与 Vite 构建通过，0 错误）。
  - `go build -o supd ./cmd/supd`（嵌入最新构建产物）。
- **测试服务**：保持运行于 `http://localhost:8080`（工作目录 `test_workdir`），用户可直接通过浏览器访问检验效果。

### 更早会话重点：第五轮全面审计、核心缺陷修复与全矩阵运行状态实测

- **审计与修复**：
  - 🔴 **R5-01（严重逻辑缺陷）**：`internal/store/retention.go` 的 `pruneExecutions` 使用 `ORDER BY created_at ASC` 导致超出 200 条时错误淘汰最新的执行记录而保留最老的历史。修复为 `ORDER BY created_at DESC`，确保 FIFO 保留最新的 200 条执行；补充回归单测 `TestRetentionPruneOldestExecutions`。
  - 🟠 **R5-02（级联删除健全性）**：`commands.go` 中的 `DeleteExecution` 与 `DeleteAllExecutions` 在同一事务中显式增加 `DELETE FROM operation_run`，防止外键约束异常时产生孤儿运行记录。
  - 🟡 **R5-03（前端 URL 动态同步）**：`Operations.tsx` 与 `Notifications.tsx` 针对 URL query（`?execution=` 和 `?topic=`）增加 `useEffect` 动态监听，支持 SPA 内未卸载跳转并自动弹开详情抽屉；关闭抽屉或删除记录时同步清理 URL 参数。
  - 🟡 **R5-04（前端判空兜底）**：`OperationsTagInput.tsx` 增加 `value = []` 和 `list = value ?? []`，杜绝未定义情况下的渲染异常。
  - 🔵 **R5-05（Skill 引用修正）**：`05_env_spec.md` 修正对扩展规格章节的过时引用。
- **全矩阵运行状态实测（15项全部 PASS，真实运行实例与 SQLite 证据）**：
  - **R-01 200条保留容量淘汰方向实测**：写入 210 条执行记录（10 条历史 + 200 条最新），启动 retention 后老记录 10 条全部清理，最新 200 条完好无损，总数恰好 200。
  - **R-02 软删实测**：DELETE `/api/notifications/topics/{id}` 返回 200，SQLite 核实 `deleted_at` 准确置位且物理保留；GET 返回 404 `FILE_NOT_FOUND`。
  - **R-06 键值对参数全链路实测**：触发 `check-software` 携带 `params: {"target":"nginx"}`，通知 Topic 记录落地并准确展示参数。
  - **R-07/R-08 Danger 操作与参数防丢实测**：触发危险操作 `update-software`（`target=redis, force=true`），弹窗确认后 4 条通知全数入库，参数未丢失。
  - **R-09 手动删除与清空实测**：删除单个 execution 级联删除 `operation_run`，关联 Topic 保留；清空 executions 后所有 execution 归零，所有 Topic 完好保留。
  - **R-10 并发幂等测试**：30 并发发送携带相同 `Idempotency-Key` 的运行请求，全部返回 200 且返回同一 ExecutionID，SQLite 中仅创建 1 条执行记录。
  - **R-11 执行中重启的中断标记测试**：触发 6s 慢操作 `slow-demo`，执行中强杀 supd，重启后 `RecoverInterruptedExecutions` 准确将 `interrupted_at` 置为启动时间戳，API 返回 `state: interrupted`。
  - **R-12 长轮询 503 保护实测**：发起 5 个长轮询挂起请求，同一客户端第 6 个请求立即被拒并返回 503 `SERVICE_BUSY`。
  - **R-13/R-14 Skill 示例与工具校验**：`validate_dev.py` 对示例 11 与示例 12 进行全量静态与规则校验，0 error 0 warning；负例测试准确拦截 6 项非法配置。
  - **R-15 API 端点数核验**：通过 chi 路由表反射核实当前活跃端点总数为 92 个，与规格及 Skill 完全一致。
- **验证**：`go build ./...`、`go vet ./...`、`go test ./... -count=1`、`pnpm build` 全部通过。
- **测试服务**：保持运行于 `http://localhost:8080`，可直接通过浏览器或 curl 验证。

### 更早会话重点：第四轮全面审计 + 第五阶段运行状态测试

- **审计范围**：按 `tmp/audit_plan.md` 对操作中心+通知中心全部变更（含第三轮功能改进新代码）做第四轮审计；后端核心链路逐文件亲读，store 层/前端/Skill 由 3 个并行审计员逐文件通读；规格基准 v1.6。
- **发现并修复 R4-01～R4-15（🟠4/🟡6/🔵5）**，最重要 4 项 🟠：danger 确认按钮绕过 buildParams 致 KV 参数静默丢弃；幂等键 state 闭包捕获旧值复用（重触发被吞，R2-02 回归路径）；MarkRead get-then-mark TOCTOU（收起瞬间新通知到达红点不消）→ 新增 `store.MarkReadToLast` 原子命令 + `TestMarkReadToLastAtomic`；GET /api/operations/{id} 无响应者 responders null。
- **第五阶段运行状态测试 15 项全 PASS**（方案+证据见 `tmp/运行状态测试方案.md` 第五节）：API 层（30 并发幂等/MarkRead 原子 sqlite 实证/responders []/非法 notify 行 warning 日志）、浏览器（danger 带参 KV `params: {"target":"nginx"}` 落库、连续触发不吞、未读保留/收起消失/卸载兜底、JSON textarea placeholder、通知→执行对称跳转）、工具（示例 12 脱离 supd 运行、validate 0 错误、92 端点核对）、生命周期（执行中 SIGTERM → interrupted_at、重启 retention 无错）。
- **验证**：build/vet/test 全量/-race/pnpm build 全绿；二进制重编译嵌入。
- **运行态补充实测（R1~R9）**再发现并修复 **R8**：执行详情抽屉 `['operation-execution']` 不在 changes 失效列表（`UpdateRunState` 推进 GlobalSeq 时抽屉不随新写更新）→ `useNotificationChanges.ts` 补失效前缀；另删死代码键 `paramsPlaceholder`。详见 `notes/2026-09-10.md` 附节。
- 服务运行中（http://localhost:8080，后台 job `job-0eb80b16674d47dd886a702f503bb0b5`）。工作区含全部改动未提交；v0.1.0 远程推送仍待网络恢复。
- 教训：xargs `-I{}` 会替换 `-d '{}'` 请求体（测试命令坑，用 `-I@`）；新功能"最后一公里"（确认弹窗路径/闭包捕获/两步持久化竞态）是单测盲区高发区。

### 更早会话重点：审计修复 + 运行状态测试双层验证

- **背景**：按 `tmp/audit_plan.md` 全面审计操作/通知中心（代码+规格 v1.6+Skill）并修复；随后按用户要求对全部修复做**真实实例运行状态测试**。
- **代码审计修复（R2-01～R2-16）**：🔴2（上轮幂等修复被误改为无锁直写 map 可致进程崩溃；前端幂等键复用吞掉重触发）、🟠4（retention 软删 Topic 永不清理、铃铛红点滞留、长轮询 toast 风暴、示例 11 printf 失效+`${VAR:-{}}` bash 展开坑）、🟡7、🔵记录不修 2。
- **运行状态测试（`tmp/运行状态测试方案.md`，12 项全 PASS）又发现 3 个单测盲区缺陷并修复**：
  - **R2-19**：幂等 get/put 分离 TOCTOU——30 并发同 key 产生 21 个 Execution（第一轮"已修复"实际未落地）→ 查重+创建同一临界区 + 并发单测，重测唯一 execution=1。
  - **R2-17**：`warnings:null` 导致操作中心整页崩溃（浏览器实测）→ 后端 nil→`[]` + 前端 `?? []`。
  - **R2-18**：空库 topics 返回 null，铃铛 `.some()` 崩溃（**全新安装首开必现**）→ 空列表统一返回 `[]` + 前端判空。
- **测试覆盖面**：幂等并发/重放、Topic 删除+DB 软删核对、changes epoch+第 6 连接 503、缺省参数通知、执行中 SIGTERM→`interrupted_at` 落库（6s slow-demo 窗口）、重启后 retention 无错、连续触发不吞、红点即时消除、停机 70s 0 toast、重启恢复 OK、validate_dev.py 负例全捕获。
- **验证**：build/vet/test（含新增 `TestOperationRunIdempotencyConcurrent`）/pnpm build 全绿；二进制重编译嵌入；测试服务运行中（http://localhost:8080，test_workdir 含 slow-demo 测试操作）。
- **教训**："修复已落地"必须以运行实例行为为证据；null 序列化崩溃是 Go+TS 单测盲区。
- **遗留**：工作区含全部修复未提交；v0.1.0 远程推送待网络恢复。

### 更早会话重点：第二轮全面审计（后端/前端/Skill）

- **背景**：用户要求按 `tmp/audit_plan.md` 对操作中心+通知中心全部变更做全面审计（含规格 v1.6 一致性与 Skill 有效性），发现问题自行修复。上一轮（本日早些时候）已修复核心逻辑 3 项；本轮扩展至 API 层、前端 UI/UX、前后端一致性、Skill。
- **🔴 修复 2 项**：
  1. `operation_provider.go` 幂等写入被上轮修复误改为**无锁直写** `p.idem` map（并发请求可 fatal crash、进程退出），恢复 `idempotentPut`（互斥锁 + 过期清理）。教训：修复落地后需核对 diff 与声明一致。
  2. Operations 页幂等键永不清理，10 分钟 TTL 内重触发被后端幂等缓存命中而**静默吞掉**（新参数被忽略仍提示"已触发"）；改为 mutation `onSettled` 清除。
- **🟠 修复 3 项**：retention 软删除 Topic 及其通知**永不物理清理**（数据无限增长）→ 新增 `deleted_at` 超 30 天清理 + 回归测试 `TestRetentionDeletedTopicPurge`；已读/删除后铃铛红点滞留（`['notification-topics-bell']` 前缀不匹配、changes 不唤醒）→ Notifications 页 `invalidateAll()`；长轮询未走 silent 路径 → 宕机时 toast 风暴 → `apiLongPoll` 改 silent。
- **🟡 修复 7 项**：changes 注释 429→503 同步；retention 失败静默改 slog.Warn；validate_dev.py 提取正则过窄（`Check-Op`/`my_op` 静默漏检）+ 跨 action 重复/flow 形式漏报；02_extension_spec §5→§4 引用失效 + `::notify::` 转义规则范围矛盾；Operations/Notifications 补 loading/error 三态与专用文案；Registrants 补去重。
- **Skill 修复**：示例 11 run.sh printf 缺闭合引号（`::notify::` 演示失效）+ `set -u` 缺省保护；06_tjs_runtime_guide 加 supd.db 边界警示。
- **记录不修（🔵）**：run_gateway fired map 缓慢增长（家用速率极低）；GetExecution 吞错当 404（接口无 error 通道）。
- **验证**：`go build`/`go vet` 零警告、`go test ./... -count=1`、`go test -race ./internal/api ./internal/store ./internal/extension`、`pnpm build`、validate_dev.py 负例全捕获全部通过。
- **遗留**：工作区含两轮审计修复未提交；v0.1.0 远程推送仍待网络恢复。

### 更早会话重点：实施收尾 + v0.1.0 发布

- **背景**：承接 09-09 节点 10 完成态，用户批准"执行吧"后完成收尾与发布。
- **操作中心+通知中心 10 节点全部实施完成**（节点 01~10）：规格 v1.6/约束同步 → Phase 0（tracker 服务维度、stdout 排水+notify 协议、统一 RunGateway、runResults 有界）→ SQLite 存储层 → 操作注册+两阶段 Runner → 通知路由+API → 前端 8 Tab+操作/通知中心+铃铛红点 → 联调发布。质量门禁全绿（build/vet/test/-race/pnpm build），E2E O1~O10+N1~N10 通过。
- **本次收尾**：
  1. 补充操作服务阶段 `SUPD_SERVICE` 注入（`run_context.go`：OperationID 非空且 ServiceName 非空时注入，无 PID）；`RunGateway.SubmitRun` 对服务阶段 Run 按 `spec.ServiceName` 精确解析扩展（避免多服务同名扩展解析串线，使 SUPD_SERVICE_DIR 正确注入）；新增 `TestBuildSupdEnvServiceLifecycleUnchanged` 回归。
  2. 清理 `.workbuddy/skills/` 遗留副本（7 月过期、含旧"禁止引入数据库"表述、缺新示例 11/12，AGENTS 未引用）；保留 `expert-history.json` 与 `memory/`。
  3. 版本升级至 **v0.1.0**（MINOR，操作中心+通知中心）：README 两处版本号、`version-upgrade-guide.md` 变更记录追加；`-ldflags` 注入验证 `supd 0.1.0` 通过。
  4. 提交 `f234553` + 本地 tag `v0.1.0`；**远程推送因 github.com 不可达未完成**（`git ls-remote` 超时），待网络恢复后执行 `git push origin main && git push origin v0.1.0` 触发 CI 构建。
- 详细证据见 `docs/devlog/notes/2026-09-09.md`「09-10 实施收尾与发布」章节。

### 更早会话重点：节点 10 联调验收与发布收尾（2026-09-09）

- 执行开发计划**节点 10**（操作中心+通知中心联调/端到端验收/发布收尾），前置节点 01~09 全部完成。
- **10-1 示例**：新增 `examples/11-operation-global-ext/`（全局扩展，`actions[].operations` 注册多操作、演示 `SUPD_OPERATION_PARAMS` 与 `::notify::`）+ `examples/12-operation-responder-ext/`（服务扩展，operations 响应绑定）；`validate_dev.py` 修复 operations 内嵌列表被误判为 action 的切分 bug 并补充 operations ID/重复校验，旧示例回归通过。
- **10-2 E2E 矩阵**：真实启动 supd 于临时 workdir `/tmp/supd-e2e`，落库 SQLite（`<baseDir>/data/supd.db`，`sqlite3` 核实 notification_topic/notification/operation_execution/operation_run = 13/31/13/22）。O1~O10、N1~N10 全部实测通过（来源不可伪造、stderr 不生成通知、超长行不堵塞、changes epoch、并发不串、重启中断 `interrupted_at`、已读游标持久化、replace canceled）；N11/异常路径3(磁盘满)/Docker 双镜像/kill 强杀恢复等依赖环境项如实记录"由代码+单测确认/依赖发布环境"。
- **10-3 质量门禁**：`go build/vet/test/-race/pnpm build` 5 命令全绿（go test 含 extension 134s/core 41s/store；-race 零竞态；pnpm build 生成 Operations/Notifications/octagon-alert chunk）。
- **10-4 §十三 8 项闭合**：解除数据库禁令四处允许性表述就绪；operations/::notify::/5 个 SUPD_* 均有规格章节+测试在；App.tsx 实测 8 Tab、NotificationBell 仅红点；`server.go setupRoutes()` 实际注册 **90** 端点（新增 12 = 操作 5 + 通知 7，基线 78）；deviations DEV-009/010 在案。Skill（02_extension_spec/05_env_spec/SKILL.md）与规格 v1.6 "规划中→已实现" 回填完成。
- **10-5 发布收尾（仅文档）**：Dockerfile 确认 `/etc/supd` VOLUME 覆盖 `<baseDir>/data`（supd.db+WAL 持久化）；release note 草稿要点（WAL 备份策略、二进制体积增至约33MB）写入 2026-09-09 归档；**未**执行 git tag/push/GHCR，版本号待用户确认（基线 v0.0.54）。
- **发现项待决策**：操作服务阶段 `SUPD_SERVICE/SUPD_SERVICE_DIR` 不注入（示例已用默认值保护，是否补充注入属行为增强）；`.workbuddy/skills/` 历史技能副本（含旧数据库禁令）处置。
- 详细证据与修改文件清单见 `docs/devlog/notes/2026-09-09.md`「节点 10」章节。

### 更早会话重点：操作中心与通知中心 v5 审计定稿（2026-09-09）
- 操作注册方式已简化：不再使用独立全局配置；全局扩展 action 的 `operations` 字符串数组自动注册操作，服务扩展 action 使用同属性作为响应绑定。
- 操作严格分两阶段执行：全局扩展 action 按稳定顺序先执行，全部终态后再执行服务扩展；服务与扩展通信由脚本通过缓存文件自行控制。
- 通知内容仅接受受管服务/扩展 stdout 的 `::notify::`、`::notify-to::`，无 HTTP/UDS/共享内存/文件写入口；采用 UUIDv7 Topic、Topic seq、JSONL 持久化和已读游标。
- 三轮审计后定为“有条件通过”：实施前须完成并发 service 作用域、stdout 持续排水、统一 Run 生命周期、持久化一致性、Topic 异步生命周期和 runResults 无界增长等 Phase 0 修复。
- 当前仅完成设计和审计，尚未修改需求规格、Skill 或源码。

### 更早会话重点：Skill 文档同步（2026-09-01）

- `SKILL.md` 底包 libc 门禁改为双向（Alpine↔Debian）；`01_service_spec.md` 补充 musl 二进制运行于 Debian/glibc 底包的启用路径。

### 更早会话重点：tjs 固定 Release 构建缓存（v0.0.51–v0.0.54）

- 自动发布和手动镜像 workflow 共用固定 `tjs-cache` prerelease；Actions Cache 未命中时下载对应 Release asset，仍未命中才编译。
- 资产按 Alpine/Debian、amd64/arm64、`TJS_VERSION` 和 `TJS_CACHE_SCHEMA` 区分；按版本保留最近 5 个版本；同一并发组避免资产竞争；`push: false` 不写 Release。
- v0.0.53 修复 `gh release upload source#label` 未重命名资产的问题（上传真实命名文件 + 存在性校验）；v0.0.54 实测命中复用成功，并用 `tjs_version=v26.5.0` + `push=false` 手动 run 验证了"版本变更→未命中→真实编译→不上传污染缓存"全链路。

### 更早会话重点：扩展 CWD/entry 解析根回归修复

- v0.0.44 将扩展 CWD/相对 entry 解析根从扩展自身目录误改为服务根/baseDir，导致远程实例全部扩展启动失败；`buildWorkDir`/`RunExtension`/导出导入校验已恢复以扩展自身目录为根，规格/Skill/示例/测试同步。
