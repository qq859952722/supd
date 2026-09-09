# supd 开发会话备忘（主索引）

> 跨会话上下文传递。Agent 新会话启动时首先阅读本文件（主索引）+ `blockers.md`。
> 历史日志已压缩归档至 `archive/notes-20260721-20260901-source.md`（**仅保留 supd 仓库源码/文档调整**；对 188/190 服务器的远程运维操作不记录于开发日志）。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。核心机制备忘见 `notes/core-mechanisms.md`。

---

## 一、项目状态

- **阶段**：维护/修复/测试阶段（57 Task 全部完成，8 阶段任务执行计划闭合）
- **质量水位**：⭐ 优秀，1000+ 单元测试通过（Go + 前端），零竞态；go vet 零警告
- **当前版本**：v0.1.0（操作中心 + 通知中心；版本升级见 `version-upgrade-guide.md`）

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
| 09-10 | 实施收尾 + v0.1.0 发布 | 全部 10 节点实施完成；补充操作服务阶段 SUPD_SERVICE/SUPD_SERVICE_DIR 注入（RunGateway 按服务作用域解析）；清理 .workbuddy/skills 遗留副本；README/变更记录升 v0.1.0；本地提交 f234553 + tag v0.1.0（远程推送因网络不可达未完成，待网络恢复）| [notes/2026-09-09.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-09.md) |

---

## 七、最近会话重点（2026-09-10：实施收尾 + v0.1.0 发布）

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
