# supd 开发会话备忘（主索引）

> 跨会话上下文传递。Agent 新会话启动时首先阅读本文件（主索引）+ `blockers.md`。
> 详细信息按需读取 `notes/` 子目录，**不要默认全量读取**。读取协议见 `notes/README.md`。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。
> 完整历史归档（2026-07-21 ~ 2026-07-26）见 `archive/session-notes-20260727-precompress.md`。

---

## 一、项目状态

- **阶段**：维护/修复/测试阶段（57 Task 全部完成，8 阶段任务执行计划闭合）
- **质量水位**：⭐ 优秀（满分 100），1000+ 单元测试通过（Go + 前端），零竞态；staticcheck/go vet 零警告
- **当前版本**：v0.0.39（版本升级见 `version-upgrade-guide.md`）

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

- **生命周期**：`starting→up→ready`（唯一就绪路径）、`stopping→down`；自动重启不经过 down；`autostart:false` 初始为 `down`（§2.8.1）
- **环境变量**：4 层合并（os.Environ → 全局 env 文件 → 服务 env.yaml → 扩展 env.yaml）；`env.yaml` 必须含 `env:` 包装层；`enabled:false` 不注入
- **身份权限**：User 模式（user/run_as 按用户名）与 UID 模式（uid/run_as_uid 按数字）互斥；服务 user/uid 空=继承 supd；服务级扩展 run_as/run_as_uid 空=继承服务身份；全局扩展空=继承 supd；服务严格拒绝/扩展宽松警告语义差异；CredentialSpec 统一描述
- **关机**：单一 `shutdown_grace_seconds` 预算贯穿 cron stop / 扩展等待 / GracefulShutdown / HTTP Stop
- **PID1**：supd 自带 PR_SET_CHILD_SUBREAPER + SIGCHLD 回收；Docker 中禁用 `--no-pid1`；维护 PID 文件清理孤儿进程
- **前端嵌入**：`//go:embed dist` 在 `web/embed.go`，改前端后必须 `pnpm build` + `go build` 才能生效
- **watcher**：白名单只监控配置目录；黑名单 data/bin/logs/history/cache/tmp/temp/run；fsnotify 防抖 500ms
- **端口探测**：受管 PID 进程树 fd socket inode 精确匹配；Docker 部署需 `cap_add: SYS_PTRACE`

---

## 三、已知偏差（详见 `deviations.md`）

> **当前状态**：无活动偏差（所有已确认接受的偏差项已全部同步补充至 `docs/需求规格说明_v1.5.md`）。

---

## 四、关键决策

- 不引入数据库、不引入 SSE/WebSocket（长轮询是规格要求）
- 不引入 tini/dumb-init（supd 自带 PID 1 能力）
- triggers 格式用 map（所有 meta.yaml 与代码一致，规格 v1.5 §2.2.3 已采用 map 写法）
- meta.yaml 中 `service:` 字段冗余（YAML 解析器静默忽略，服务关联由目录结构决定）
- dropbear-ssh 是 supd 管理的普通服务（非 entrypoint 脚本），autostart: false

---

## 五、未闭合待办（详见 `blockers.md` 与 `tmp/remediation_plan.md`）

> 阻断项无。技术改进项汇总于 `tmp/remediation_plan.md`，本轮任务将按优先级处理。

| 编号 | 内容 | 优先级 | 状态 |
|------|------|--------|------|
| R-06 | `meta.yaml` 解析失败时扩展从列表消失（UX 缺陷）+ 回归 | 🟡 中 | ✅ 已修复（含 REG-01） |
| R-01/R-02 | 资源采集降级路径多实例匹配 + 缺 CPU/内存占比 | 🔵 低 | ✅ 已修复 |
| R-03/R-09 | `transmission-updater` downloadFile 全量缓冲 | 🔵 低 | ✅ 已修复 |
| R-05 | 脚本依赖外部命令未校验存在性 | 🔵 低 | ✅ 已修复 |
| R-04 | gopsutil context 限制（仅文档补注） | 🔵 低 | ✅ 文档已补 |
| R-07 | GitHub Actions Node.js 弃用警告（已复核无警告） | 🔵 低 | ✅ 已闭环 |
| R-08 | `buildStopConfigs` nil 保护（非可达路径） | 🔵 低 | ✅ 已修复 |
| R-09 | spec/code 偏差：`ui.show_logs`/`ui.button_style` 已写入规格；`ui.category` 四分组无意义已从规格移除 | 🔵 低 | ✅ 已修复 |

---

## 六、下次会话注意

- 改前端后必须 `pnpm build` + `go build` 重新嵌入二进制，否则看不到效果
- `NewReadinessChecker(cfg, dir, env)` 已变 3 参数；`OnFailure` 增加 `servicePID int`；`CronScheduler.Stop(ctx)` 带 context
- env.yaml 格式必须含 `env:` 包装层，直接写 `KEY: value` 会被静默忽略
- 前端所有 env 编辑器统一用 `web/src/lib/env-yaml` 共享工具
- 服务与扩展的非 root 语义差异需保持（服务严格拒绝、扩展宽松警告）
- Docker 镜像需重新构建才能包含 Dockerfile 变更（dropbear/env.yaml 加载等）
- 监控 yaml v4 稳定版发布（M-03），发布后升级 go.mod

---

## 七、会话历史索引

> 按需读取对应文件，不要默认全量浏览。搜索特定主题用 `rg` 在 `notes/` 中查找。
> 2026-07-27 之前的完整索引见 `archive/session-notes-20260727-precompress.md`。

| 日期 | 主题 | 摘要 | 详情文件 |
|------|------|------|----------|
| 2026-07-25 | E2E 验证 + v0.0.22 发布 | 修复 API 状态取值键名 (`state`→`status`)；4 场景 E2E 全通过 | [notes/2026-07-25.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-25.md) |
| 2026-07-25 | 代码审计 + 端口探测简化 + auto-create-users + v0.0.28 | 端口探测降级清理；auto-create-users 默认 pre_start 启用 | [notes/2026-07-25.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-25.md) |
| 2026-07-26 | auto-create-users find/UID-GID 迁移修复 + v0.0.29 | UID/GID 非数字校验、BusyBox 兼容、属主/属组迁移失败传播 | [notes/2026-07-26.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-26.md) |
| 2026-07-26 | autostart:false 初始状态修复 + v0.0.30 | Bootstrap 阶段将 autostart:false 服务状态归位至 down | [notes/2026-07-26.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-26.md) |
| 2026-07-26 | Docker 用户命令与启动提示 + v0.0.31 | shadow 工具集；启动提示 ALLID 与 env YAML 写法 | [notes/2026-07-26.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-26.md) |
| 2026-07-26 | GHCR latest 抢占修复 + v0.0.32/v0.0.33 | 架构构建不再写共享 latest-${arch}；仅最高稳定版本可标记 Latest | [notes/2026-07-26.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-26.md) |
| 2026-07-26 | 服务详情页元数据展示 + v0.0.34 | 服务详情页补充 name/version/description 展示与 i18n | [notes/2026-07-26.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-26.md) |
| 2026-07-27 | 任务执行计划全面审计与测试 | R-06 修复 + REG-01 回归修复（reload_classifier nil panic）+ 66 用例测试 + 一致性检查 + 综合测试服务准备 | [notes/2026-07-27.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-27.md) |
| 2026-07-27 | R-01/R-02、R-03/R-05、R-08 技术债闭环 | NSpid 精确映射与完整资源指标；curl 流式落盘与按需依赖校验；停止配置 nil 防御 | [notes/2026-07-27.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-27.md) |
| 2026-07-28 | 代码审计 + 运行状态测试 + v0.0.35 发布 | 13 项运行测试全通过（资源 API/停止配置/tjs 扩展）；R-01～R-09 全部闭环 | [notes/2026-07-28.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-28.md) |
| 2026-07-28 | 多 profile 导出 + Skill 优化 + v0.0.36 | 服务打包支持多份 package.\<name\>.yaml 规则文件；bin/+data/ 目录规范；tjs.open 流式下载；二进制更新扩展示例 | [notes/2026-07-28.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-28.md) |
| 2026-07-28 | WASM 工具推荐 + Debian 镜像变体 | WASI 兼容 WASM 工具调研与 Skill 文档更新；新增 Dockerfile.debian（bookworm-slim）；CI 支持双变体构建（Alpine+Debian） | [notes/2026-07-28.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-28.md) |
| 2026-07-28 | 镜像构建问题修复 | Debian 删除无效 shadow 包；手动 Alpine job 改为 alpine:3.20 内编译 musl tjs 并检查 ld-musl | [notes/2026-07-28.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-28.md) |
| 2026-07-28 | WASI 成品落地与调用修复 | Skill 内置 zstd/bsdtar WASI 成品；tjs v26.6.0 真实验证；修正 preopens、退出码和 transmission-updater 调用 | [notes/2026-07-28.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-28.md) |
| 2026-07-28 | 审计修复 + 运行状态测试 + v0.0.37 | 修复 10-binary-updater-ext 4 处缺陷（action/env、Buffer、exit_status、async getArch）+ transmission-updater exit_status；28 项运行测试全通过（D/A/B/C/E 五组） | [notes/2026-07-28.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-28.md) |
| 2026-07-28 | 扩展传参机制重构 + 删除 Action.Args | 删除 Action.Args 死代码（后端12文件+前端+规格+Skill文档）；新增运行时参数编辑抽屉（Drawer + EnvParamsDrawer），支持「保存」持久化/「运行」仅本次生效（TempEnv） | [notes/2026-07-28.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-28.md) |
| 2026-07-28 | 扩展参数运行测试 + Skill README 规范 + v0.0.38 | 修复 CLI 契约、相对 env_path、服务级未知 action 回退；TempEnv/保存/CLI E2E 与完整回归通过；Skill 强制 8 节 README | [notes/2026-07-28.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-28.md) |
| 2026-07-28 | supd 启动信息摘要（Startup Banner） | 两段式打印（Bootstrap后静态摘要 + HTTP绑定后实际监听地址+可访问URL枚举）；改造 `api.Server.Start` 为 net.Listen+Serve+addrReady 回调；双通道输出（stdout+slog）；20 个测试 | [notes/2026-07-28.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-28.md) |
| 2026-07-29 | 启动摘要运行测试 + post_ready 异步修复 + v0.0.39 | post_ready 改异步执行避免阻塞启动摘要；端到端验证启动摘要/扩展生命周期/API/优雅关闭全通过；v0.0.39 推送 GitHub | [notes/2026-07-29.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-29.md) |
| 2026-07-29 | 审计复核与整改规划 | 逐项核验审计报告 29 F 项 + 1 矛盾项；纠偏误报与不宜采用建议；输出 `tmp/审计报告复核与整改方案.md`（P0-P3 顺序 + 8 待决策） | [notes/2026-07-29.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-29.md) |
| 2026-07-29 | 审计整改方案逐项落地与交叉验证 | 生成 32 个整改方案文件（tmp/fix）覆盖 30 复核结论 + 5 漏项；交叉验证覆盖/唯一性/一致性全通过；7 待决策项待用户确认 | [notes/2026-07-29.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-29.md) |
| 2026-07-29 | 审计整改 A～H 全量落地与验证 | 32 项方案按批次完成；diagnostic 字段白名单脱敏；HTTP Start/Stop 竞态修复；Go 全量/race 与前端 60 项测试全通过 | [notes/2026-07-29.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-29.md) |
| 2026-07-29 | 运行状态测试 + serialize 队列满记录修复（F2-001） | A～K 组运行测试全通过；发现并修复 serialize queue full 的 failed 记录因 StartedAt 零值被 lazyCleanup 误删；dispatcher.go 补全 result 元数据；skill 更新并发策略详解 | [notes/2026-07-29.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-29.md) |

---

## 八、最近会话重点（2026-07-29 运行状态测试与 serialize 队列满记录修复 F2-001）

### 本次完成

1. **运行状态测试**：按 `tmp/runtime_test_plan_v2.md` 执行 A～K 组全量测试，覆盖依赖恢复、生命周期锁、启动摘要、原子文件写入、诊断脱敏、serialize FIFO、关机协调器，全部通过。
2. **F2-001 修复**：serialize 队列满时 `applySerialize` 返回的 `RunResult` 缺少 `StartedAt`（零值），导致 `TaskHistory.lazyCleanupLocked` 误判为超期记录立即删除，failed 记录无法持久化。修复在 `dispatcher.go executeWithConcurrency` 中统一补全 result 缺失字段（ExtensionName/ActionID/TriggerType/StartedAt/FinishedAt），覆盖所有触发路径。
3. **测试与验证**：新增 `TestExecuteOnDemand_SerializeQueueFull_ResultHasMetadata` 回归测试（85s）；重建二进制后运行时验证 queue full 的 failed 记录正确出现在 runs 历史。
4. **skill 更新**：`02_extension_spec.md` 新增 §3.5 并发策略行为详解；`SKILL.md` 补充 serialize 队列上限 16。
5. **完整回归**：go build/vet/test（含新测试）、pnpm build 全通过。

### 关键技术点

- `RunExtension` 是异步执行模式：预记录 `TaskRunning` 后立即返回 `run_id`，扩展在后台 goroutine 执行。queue full 等失败发生在 goroutine 中，不反映在 HTTP 响应，需查 runs 历史。
- `TaskHistory.Add` 按 RunID 覆盖存储后立即调用 `lazyCleanupLocked`；若 result.StartedAt 为零值会被判定为超期记录删除——所有 RecordRun 的 result 必须有非零 StartedAt。
- serialize 队列上限 16 是 pendingRuns 的上限：第 1 个直接执行，第 2-17 个进队列，第 18 个才触发 queue full。

### 遗留事项

- 无阻断项。
- 代码已全部验证通过，待提交与发版。

---

## 附：2026-07-28 审计修复 + 运行状态测试 + v0.0.37

### 审计修复（5 处）

| 编号 | 文件 | 修复 |
|------|------|------|
| F1 | `examples/10-binary-updater-ext/run.js` | `tjs.args[2]` → `tjs.env.SUPD_ACTION`（args[2] 取到的是脚本路径而非 action） |
| F2 | `examples/10-binary-updater-ext/run.js` | `Buffer.concat` → `TextDecoder` 累加（tjs v26.6.0 无 Buffer 全局） |
| F3 | `examples/10-binary-updater-ext/run.js` | `status !== 0` → `status.exit_status !== 0`（proc.wait 返回 `{exit_status, term_signal}`） |
| F4 | `examples/10-binary-updater-ext/run.js` | `getArch()` 改 async + 读 `/proc/cpuinfo` 支持 aarch64；调用处加 `await` |
| F5 | `dev_workspace/transmission/extensions/transmission-updater/run.js` | `status.exitCode` → `status.exit_status`（同 F3） |

### 运行状态测试（28 项全通过）

测试方案：`tmp/audit_fix_test_plan.md`（5 组）

- **D 组**（回归）：go build/vet/test、pnpm build 16.4s、zstd 压缩往返（24000→34 B）、bsdtar 解包 .tar/.tar.zst ✅
- **A 组**（10-binary-updater-ext 单元，11 项）：未设 action → "未知 action" +exit 1；check-update 缺 SERVICE_DIR → 报错；不可达 API → ::result:: failure；getArch 在 x86_64 正确；Buffer 全局不存在；proc.wait 返回 `{exit_status:7}` ✅
- **B 组**（transmission-updater 单元，4 项）：check-update 输出 ::progress:: + ::result:: 协议；实际调用 GitHub API 返回 v4.1.3；runCmd 执行 `sh -c 'exit 3'` 返回 exitCode=3 ✅
- **C 组**（端到端，3 项）：supd API 触发 tjs-test-ext success；触发 transmission-updater-test 返回 v4.1.3 warning；触发 binary-updater-test 验证 F1/F2 修复在 supd 注入环境变量下正确生效 ✅
- **E 组**（协议检查，6 项）：binary-updater-test 输出 `::progress::N\|msg` + `::result::{json}`；transmission-updater-test 输出 `::progress:: N "msg"` + `::result:: status "msg"`；日志非空 ✅

### 关键技术点

- **tjs v26.6.0 的 proc.wait() 返回值结构**：`{exit_status: number|null, term_signal: number|null}`，**不是** Node.js 风格的 `exitCode`。误用字段名会导致 `undefined ?? 0 === 0`，将所有失败误判为成功。
- **tjs v26.6.0 无 Buffer 全局**：`Buffer.concat`/`Buffer.from` 等不可用，必须用 `TextDecoder` 累加字符串或 `Uint8Array` 直接拼接。
- **supd 扩展 action 注入**：通过 `SUPD_ACTION` 环境变量（非 CLI 参数），`SUPD_SERVICE_DIR` 自动注入给服务级扩展。
- **bsdtar WASM 限制**：WASI 平台不支持 `archive_read_disk`，bsdtar.wasm 仅能**列出/解包**归档，不能创建归档。supd 扩展使用场景（解压远端下载的 .tar.xz/.zip）正好匹配。
- **测试用临时扩展已清理**：transmission-updater-test（全局）和 binary-updater-test（tcp-echo 服务级）已从 test_workdir 移除。

### R-09 规格偏差处理（同日续）
- **偏差一**：`ui.show_logs`/`ui.button_style` 代码已实现但规格未记录 → 已写入需求规格说明_v1.5.md L451-454
- **偏差二**：`ui.category` 四分组（general/maintenance/monitoring/notify）代码完全未实现，无实际意义 → 已从规格移除（含表单字段 L1976、示例 L2887-2890）
- **R-07 复核**：所有 CI actions 已升 v4+，无 Node 20 弃用警告，闭环
- **验证**：go build/vet/test 通过，全项目无 category 残留
- **遗留**：R-01/R-02（边缘场景）、R-03/R-05（扩展脚本健壮性）、R-08（非可达路径）按维护期约束暂缓
