# supd 开发会话备忘（主索引）

> 跨会话上下文传递。Agent 新会话启动时首先阅读本文件（主索引）+ `blockers.md`。
> 详细信息按需读取 `notes/` 子目录，**不要默认全量读取**。读取协议见 `notes/README.md`。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。

---

## 一、项目状态

- **阶段**：维护/修复/测试阶段（57 Task 全部完成，8 阶段任务执行计划闭合）
- **质量水位**：17 类审计评分 **97.84 / 100（⭐ 优秀）**；1000+ 单元测试通过（Go 948+ + 前端 60）；零竞态；staticcheck/go vet 零警告
- **当前版本**：v0.0.21（版本升级见 `version-upgrade-guide.md`）

### 验证命令（每次改动后必跑）
```bash
# 后端
go build ./... && go vet ./... && go test ./... -count=1
# 前端（改前端后必须 go build 重新嵌入二进制）
cd web && pnpm build
# 服务启动（测试用）
SUPD_LOG_DIR=/tmp/supd-logs ./supd --workdir test_workdir run
```

---

## 二、核心机制摘要

> 详细备忘见 `notes/core-mechanisms.md`（涉及底层机制时按需读取）

- **生命周期**：`starting→up→ready`（唯一就绪路径）、`stopping→down`；自动重启不经过 down
- **环境变量**：4 层合并（os.Environ → 全局 env 文件 → 服务 env.yaml → 扩展 env.yaml）；`env.yaml` 必须含 `env:` 包装层；`enabled:false` 不注入
- **身份权限**：User 模式（user/run_as 按用户名）与 UID 模式（uid/run_as_uid 按数字）互斥；服务 user/uid 空=继承 supd；服务级扩展 run_as/run_as_uid 空=继承服务身份；全局扩展空=继承 supd；服务严格拒绝/扩展宽松警告语义差异；CredentialSpec 统一描述
- **关机**：单一 `shutdown_grace_seconds` 预算贯穿 cron stop / 扩展等待 / GracefulShutdown / HTTP Stop
- **PID1**：supd 自带 PR_SET_CHILD_SUBREAPER + SIGCHLD 回收；Docker 中禁用 `--no-pid1`；维护 PID 文件清理孤儿进程
- **前端嵌入**：`//go:embed dist` 在 `web/embed.go`，改前端后必须 `pnpm build` + `go build` 才能生效
- **watcher**：白名单只监控配置目录；黑名单 data/bin/logs/history/cache/tmp/temp/run；fsnotify 防抖 500ms

---

## 三、已知偏差（详见 `deviations.md`）

| 编号 | 内容 | 状态 |
|------|------|------|
| DEV-003 | ProcessManager 等用 sync.Mutex | 合理例外 |
| DEV-004 | api/interfaces.go 定义 13 个接口 | Go 惯用法 |
| DEV-005 | 服务级扩展 run_as 继承 | ✅ 已完全修复 |
| DEV-008 | API 端点 77 vs 规格 65 | 已确认保留 |
| DEV-009 | `SUPD_SERVICE_DIR` 规格外变量 | 已确认 |


---

## 四、关键决策

- 不引入数据库、不引入 SSE/WebSocket（长轮询是规格要求）
- 不引入 tini/dumb-init（supd 自带 PID 1 能力）
- triggers 格式用 map（所有 meta.yaml 与代码一致，规格 v1.5 §2.2.3 已采用 map 写法）
- meta.yaml 中 `service:` 字段冗余（YAML 解析器静默忽略，服务关联由目录结构决定）
- dropbear-ssh 是 supd 管理的普通服务（非 entrypoint 脚本），autostart: false
- 接受 97.44 分作为审计最终结果，剩余扣分项为合理偏差

---

## 五、未闭合待办（详见 `blockers.md`）

| 编号 | 扣分 | 内容 | 状态 |
|------|------|------|------|
| L-01-001 | -0.150 | api 包覆盖率 | ✅ 已闭环：43.3%→68.0%（≥65% 达标） |
| L-04-001 | -0.250 | 前端测试代码 | ✅ 已闭环：Vitest 框架 + 60 例（P2+P3）util/hook 单测，lib 纯逻辑 util 覆盖 100%/92.85%/90.16%/87.27% |

技术债：无活债务（M-04-001/TD-003 已关闭；L-01-001/L-04-001 覆盖率债已闭环）

---

## 六、下次会话注意

- 改前端后必须 `pnpm build` + `go build` 重新嵌入二进制，否则看不到效果
- `NewReadinessChecker(cfg, dir, env)` 已变 3 参数；`OnFailure` 增加 `servicePID int`；`CronScheduler.Stop(ctx)` 带 context
- env.yaml 格式必须含 `env:` 包装层，直接写 `KEY: value` 会被静默忽略
- 前端所有 env 编辑器统一用 `web/src/lib/env-yaml` 共享工具
- 服务与扩展的非 root 语义差异需保持（服务严格拒绝、扩展宽松警告）
- Docker 镜像需重新构建才能包含 Dockerfile 变更（dropbear/env.yaml 加载等）
- 后续补充测试优先覆盖 api 包错误分支（L-01）和端到端集成测试（L-04）
- 监控 yaml v4 稳定版发布（M-03），发布后升级 go.mod

---

## 七、会话历史索引

> 按需读取对应文件，不要默认全量浏览。搜索特定主题用 `rg` 在 `notes/` 中查找。

| 日期 | 主题 | 摘要 | 详情文件 |
|------|------|------|----------|
| 2026-07-21 | Docker/tjs/发布/清理 | tjs 集成、v0.0.1 发布、工作区清理、仓库重建、readiness bug、user 字段接入 | [notes/2026-07-21.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-21.md) |
| 2026-07-22 | env/Dropbear/规格偏差 | tjs 默认配置、Dropbear SSH、env.yaml 加载 BUG、3 项规格偏差修复、前端 env 修复、v0.0.6 | [notes/2026-07-22.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-22.md) |
| 2026-07-23 | 审计/env/仪表盘/retry/热重载/访问日志/tjs工作流/qbittorrent | 全面审计（97.44 分）、env 编辑器统一、仪表盘服务资源汇总、扩展 retry_on_failure 补全、热重载 RestartEngine 不更新 BUG 修复、HTTP 访问日志改用 slog + --log-level CLI BUG 修复、v0.0.9；晚：v0.0.12 镜像 tjs 集成验证全通过、action 字段名（action 非 action_id）、tjs fetch arrayBuffer 大文件卡死坑点（改流式读取）、qbittorrent 服务部署成功（ready）；更晚：扩展列表/删除 bug 修复（discovery 过滤 .bak + 前端 timeout 校验）、下载日志 formatBytes 优化、代码审计 + 运行状态测试、v0.0.14；编辑扩展保存后 Discovery 缓存不刷新修复、v0.0.15；时区设置 v0.0.16；服务/扩展执行身份 UID 模式（CredentialSpec + 互斥校验 + 前端模式切换）、v0.0.17；UID 模式代码审计 + 负数校验修复 + 9 项运行状态测试全通过、v0.0.18；CI 缓存 GHA→registry 修复（layer blob not_found）+ skill 文档 UID 模式更新 + v0.0.18 重发 | [notes/2026-07-23.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-23.md) |
| 2026-07-24 | Transmission 服务/扩展开发 + Skill 文档更新 + 端口探测 BUG 修复 | transmission：直接二进制启动+user:nobody+两个tjs扩展；skill §8/§9更新；端口探测Yama LSM降级修复（UID匹配） | [notes/2026-07-24.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24.md) |
| 2026-07-24 | 新增代码审计 + 运行测试 + v0.0.20 发布 | transmission-updater run.js 7 项 bug 修复（含 3 项运行时发现）、审计+测试全通过、版本升级 v0.0.20 | [notes/2026-07-24-audit.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24-audit.md) / [notes/2026-07-24-testplan.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24-testplan.md) |
| 2026-07-24 | 待办梳理 + 三项优化 | 文档待办全面核实（retry_on_failure已实现、args格式规格禁止）；DEV-007/011/012 撤销偏差并转为规格内容（history直接路径、triggers map、actions 无 icon/enabled）；tracker-updater全局Tracker+动态rpc-port；进程树DFS递归；CPU首次采样200ms补偿；运行状态测试（Go+node mock RPC）全通过 | [notes/2026-07-24.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24.md) |
| 2026-07-24 | M-04-001/TD-003 重构实施 | superviseService CC43+重复 → 抽取共享 supervisor.go（SupervisorCallbacks依赖注入）；修复3处ctx/error硬伤（OnFailure闭包内决定ctx/SpawnNextSupervisor返回ctx/RunReadiness返回error）；3处遗漏补全（RuntimeRegistry有意变更/Source字段/DecideRestart签名）；新增RestartActionAbort；bootstrap CC3 + service_operator CC6；T1-T6测试全通过 | [notes/2026-07-24.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24.md) |
| 2026-07-24 | M-04-001/TD-003 审计+运行测试全通过 + v0.0.21 发布 | 审计（git show HEAD 逐行比对 + gocyclo + -race + T1-T6）通过；7 场景 A–G 真实运行测试全通过（无 panic/泄漏/回归）；fd_notify 重启重新就绪经 200ms 细粒度轮询确认；版本升级 v0.0.21 并推送 origin/main | [notes/2026-07-24-audit-supervisor.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24-audit-supervisor.md) / [notes/2026-07-24-testplan-supervisor.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24-testplan-supervisor.md) |
| 2026-07-24 | DEV-010 修复 | 补充 EventNormalExit 及 up/ready/starting → down 转移规则（规则 11）；用 Transition(EventNormalExit) 替代 ResetTo(StateDown)；更新需求规格 v1.5 §2.1.1 与 deviations.md 标记已修复 | [notes/2026-07-24.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24.md) |
| 2026-07-24 | 测试覆盖率提升计划执行（P0/P1/P2） | api 43.3%→68.0%、cli 50%→66.1%、前端 Vitest 框架+38 例 util 单测（lib 纯逻辑 util 全 100%/92%/67%）；L-01-001/L-04-001 闭环，质量水位 97.84；全部 hermetic，未改生产代码 | [notes/2026-07-24.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24.md) |
| 2026-07-24 | 测试覆盖率提升计划（P3 收尾） | 前端主链路轻量集成：api-client 67%→90.16%（REST 封装+handleResponse 全分支）、http-probe 0%→87.27%（jsdom hook 测试）；前端 60 例全过、lib 总覆盖 91%；CI 接入 coverage.yml（上传 go/web artifact，非阻断）+ Makefile cover；修复 P2 遗留 tsconfig 将测试纳入生产构建致 build 失败 | [notes/2026-07-24.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24.md) |
| 2026-07-24 | 测试覆盖率提升计划（P4 核心包攻坚） | state_machine/pidfile/cron_scheduler/trigger_cron/dispatcher/service_operator 补测，35 例 hermetic（全部未改生产代码）；后端总 73.6%→74.9%（+1.3pp），core/extension/api 包分别 +2.9/+3.9/+0.6pp；task_manager 已充分覆盖跳过 | [notes/2026-07-24.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-24.md) |

---

## 八、最近会话重点（2026-07-24 测试覆盖率提升计划执行）

**覆盖率提升（对应审计扣分 L-01-001 / L-04-001）**：
- **P0 api**：43.3% → **68.0%**（目标 ≥65% 达成）。新增 4 个 hermetic 测试文件（extension/settings/task/service_ops handler 覆盖），全部 `NewServer(cfg)` + fake provider + httptest 驱动，未改生产代码。
- **P1 cli**：50% → **66.1%**。新增 `run_coverage_test.go`（`validateLogDir`/`buildStopConfigs` 纯函数）+ `commands_coverage_test.go`（经 `getAPIClient` var 注入 httptest 服务端，覆盖各命令成功路径）。
- **P2 前端**：绿地 → Vitest 4（node 环境）框架 + 38 例 util 单测。`error-utils/utils/i18n/query-client` 100%、`env-yaml` 92.85%、`api-client` 67.21%；`http-probe`(React hook) 留 P3 可选。
- **P3 前端主链路轻量集成**：`api-client` **67.21% → 90.16%**（扩至 22 例，覆盖 `apiGet/Post/Put/Delete` 封装 + `handleResponse` 全部分支：204/4xx/5xx/非标准错误体/silent/网络错误/AbortError）；`http-probe` **0% → 87.27%**（新增 9 例，jsdom + `@testing-library/react` `renderHook` 覆盖 `useHTTPProbe` hook + `sortAndLimitPorts`/`invalidatePortCache`）。前端 lib 总覆盖率 **91.03%**。
- **P3 CI 接入**：`.github/workflows/coverage.yml`（push/PR 触发，`go test -coverprofile` 上传 `go-coverage` + `vitest --coverage` 上传 `web-coverage` artifact，**非阻断、暂不设备阈值**）；`Makefile` 新增 `cover`/`cover-web`；`.gitignore` 补充 `cover.html`/`web/coverage/`。
- **P3 构建修正**：修复 P2 遗留 `pnpm build` 实际失败——`web/tsconfig.json` 的 `tsc -b` 将 `*.test.ts` 纳入严格类型检查（`noUncheckedIndexedAccess` + `ApiError.code` 必填）致测试类型错误；修正为 `exclude` 测试文件出生产构建（标准做法，测试仍由 vitest 执行）。修正后 `pnpm build` 真通过。
- **闭环**：L-01-001（-0.150）/ L-04-001（-0.250）回收 → 质量水位 **97.84 / 100**。`cmd/supd` 决策性不覆盖。
- **验证全绿**：`go build/vet/test ./...` ✅；`pnpm test` **60 例** ✅；`pnpm build` ✅；`make cover` 后端总覆盖 **73.6%** ✅。
- **风险记录**：`buildStopConfigs` 对 `result.Discovery==nil` 无保护，生产路径 Discovery 总由 bootstrap 赋值，非可达；按维护期约束未改生产代码。
- **未提交**：本轮新增测试文件均为 untracked；v0.0.21（commit `5a82e07` + tag）仍本地未推送，按用户"暂不推送"决策保留。

**P4（Go 核心包攻坚，2026-07-24 收尾）**：
- 攻"可 hermetic 测 + 高价值"核心域：6 个新增测试文件共 **35 例**（`core/state_machine`、`core/pidfile`、`extension/cron_scheduler`、`extension/trigger_cron`、`extension/dispatcher`、`api/service_operator`）。
- **真实增量**（移走 P4 测试文件测基线、再还原比对 statement coverage）：后端总 **73.6% → 74.9%**（+1.3pp）；包级 `core` 81.6→**84.6%**、`extension` 76.4→**80.4%**、`api` 68.0→**68.6%**；逐文件最高 `pidfile` +40.7pp（23.7→64.4）、`cron_scheduler` +26.4pp（40.2→66.7）、`state_machine` +19.1pp（74.5→93.6）。
- `task_manager.go` 因 `task_manager_test.go` 已充分覆盖，按"已充分覆盖不 blanket uplift"护栏**跳过**；`state_test.go` 已极完备，P4 仅补 publisher 分支不重写。
- 验证全绿：`go build/vet/test ./...` ✅（core 51.9s / extension 34.3s / api 4.7s 无 FAIL）；全部 hermetic，未改生产代码。
- **未提交（P4）**：6 个新增 Go 测试文件均为 untracked；v0.0.21 仍本地未推送（用户"暂不推送"决策）。
