# supd 开发会话备忘（主索引）

> 跨会话上下文传递。Agent 新会话启动时首先阅读本文件（主索引）+ `blockers.md`。
> 详细信息按需读取 `notes/` 子目录，**不要默认全量读取**。读取协议见 `notes/README.md`。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。
> 完整历史归档（2026-07-21 ~ 2026-07-26）见 `archive/session-notes-20260727-precompress.md`。

---

## 一、项目状态

- **阶段**：维护/修复/测试阶段（57 Task 全部完成，8 阶段任务执行计划闭合）
- **质量水位**：⭐ 优秀（满分 100），1000+ 单元测试通过（Go + 前端），零竞态；staticcheck/go vet 零警告
- **当前版本**：v0.0.36（版本升级见 `version-upgrade-guide.md`）

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

---

## 八、最近会话重点（2026-07-28 多 profile 导出 + Skill 优化 + v0.0.36）

- **任务**：优化 Skill（目录结构 + tjs 流式下载 + 二进制更新扩展）；需求文档和代码支持多份打包规则文件；审计 + 运行测试 + 升级版本
- **Skill 优化**：
  - 新增 `bin/`+`data/` 目录规范（控制面/业务载荷分离、权限隔离、command 指向）
  - 发现 `tjs.open()` 返回 FileHandle 支持 `fh.write()` 自动推进位置，实现真正流式落盘（内存与文件大小无关），已在 txiki.js v26.6.0 源码+运行时双重验证
  - 新增 `downloadStream()` 替代旧版全量内存 `downloadFile()`
  - 修正 chown 建议：禁止 `chown -R <serviceDir>`，改为分别处理 bin/ 和 data/
  - 新增 `examples/10-binary-updater-ext/`（check-update/update/force-update 三 action，放宽验证，原子替换+回滚）
- **代码变更**：
  - `internal/archive/packer.go`：新增 `PackDirWithProfile` + `matchPattern` + `shouldPackEntry` + `shouldSkipDirTree`
  - `internal/config/package_profile.go`（新文件）：`LoadPackageProfile`/`ListPackageProfiles`/`ResolveExportProfile`
  - `internal/api/export_handler.go`：导出支持 `?profile=<name>` 参数；新增 `GET /export-profiles` 端点
  - `web/src/pages/ServiceDetail.tsx`：导出对话框（默认导出 + 按规则文件导出）
  - 需求规格说明 §2.12.2 重写：多 profile 命名规范、模式匹配语义、两种导出方式
- **运行测试**：export-profiles 端点返回正确 profile 列表；share/migrate profile 导出内容符合预期；不存在的 profile 返回 404
- **版本**：v0.0.36

### R-09 规格偏差处理（同日续）
- **偏差一**：`ui.show_logs`/`ui.button_style` 代码已实现但规格未记录 → 已写入需求规格说明_v1.5.md L451-454
- **偏差二**：`ui.category` 四分组（general/maintenance/monitoring/notify）代码完全未实现，无实际意义 → 已从规格移除（含表单字段 L1976、示例 L2887-2890）
- **R-07 复核**：所有 CI actions 已升 v4+，无 Node 20 弃用警告，闭环
- **验证**：go build/vet/test 通过，全项目无 category 残留
- **遗留**：R-01/R-02（边缘场景）、R-03/R-05（扩展脚本健壮性）、R-08（非可达路径）按维护期约束暂缓
