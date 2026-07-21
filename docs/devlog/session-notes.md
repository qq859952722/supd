# supd 开发会话备忘（压缩版）

> 跨会话上下文传递。Agent 新会话启动时首先阅读此文件。
> 原始详版见 `session-notes.full.md`（归档）。维护阶段：全部 57 Task 完成，聚焦修复/测试。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。

---

## 一、项目状态与规范
- 阶段：维护/修复/测试全面完成，8 阶段任务执行计划全部闭合（Phase 8 最终审计完成）。
- 验证命令（每次改动后必跑）：
  - 后端：`go build/vet/test -buildvcs=false -count=1 ./...`
  - 前端：`cd web && pnpm build`（及 `pnpm tsc --noEmit`）
  - 测试启动：`SUPD_LOG_DIR=/tmp/supd-logs ./supd --workdir test_workdir run`
- 质量水位（2026-07-21 Phase 8 终态 + v2.1 核查）：
  - 测试函数 913 个（Phase 8 新增 parseHexIP 测试 4 个）；api 包覆盖率 41.9%（从 41.5% 提升）
  - `go test -race -count=3` 零竞态（Phase 8 实际重跑验证）
  - staticcheck/go vet 零警告
  - 17 类审计最终评分 **97.4375 / 100（⭐ 优秀）**，无 🔴 严重 / 🟠 重要遗留
  - Phase 8 修复 8 项（I-03-002/A-02-001/A-04-001/A-08-001/M-02-001/N-01~N-05/L-01-001 部分修复/M-05-001 bundle 拆分）

## 二、核心机制备忘
- 生命周期：`starting→up→ready`（唯一路径）、`stopping→down`；`sync.RWMutex` 保护 `SetDiscovery`。
- 扩展/调度：`dispatcher` 管理并发，`RecordRun` 记历史；扩展脚本必须读 `$SUPD_ACTION` 环境变量。
- API 坑点：文件 path 在 URL query，body 为 `{"content":"..."}`，move 目标参数 `destination`；建服务/扩展必含 `version` 字段；扩展名 `^[a-z][a-z0-9-]*$`；任务历史 `/api/extensions/runs`。
- 关机：单一 `shutdown_grace_seconds` 预算贯穿 cron stop / 扩展等待 / GracefulShutdown / HTTP Stop；HTTP Shutdown 派生 5s 子 context。
- PID1：supd 自带 PR_SET_CHILD_SUBREAPER + SIGCHLD 回收 + 10s ticker 兜底；Docker 中禁用 `--no-pid1`。
- watcher 白名单：只监控配置文件所在目录（services/<name>/、services/<name>/extensions/<ext>/、extensions/<name>/ 等），不监控 data/bin/logs/history/cache/tmp/temp/run 及隐藏目录。
- 扩展工作目录：`filepath.Dir(extEntry.ConfigPath)`（扩展自身目录）；`script_tmp` 仍创建供临时文件。
- 导入流程：双次上传无状态模式（预览 + 确认），按目标目录加锁，备份-解包-回滚全过程原子。
- serialize 策略：pendingRun 单指针，新触发覆盖旧 pending（"last pending wins" 语义，A-04-001 修复说明已添加）。
- ResetIfNeeded：在服务退出决策点调用（bootstrap.go:706），与规格"立即重置"语义等价（A-02-001 修复说明已添加）。
- watcher fd 耗尽检测：连续 addWatch 失败超过阈值（5 次）时发出 slog.Warn（A-08-001 修复）。

## 三、未闭合待办（Phase 8 剩余偏差，详见 `tmp/审计报告.md` v2.1 和 `blockers.md`）

经 Phase 8 最终审计 + v2.1 技术债核查，剩余扣分 0.72 分（评分 97.4375），均为技术债务或测试覆盖率问题：

**剩余扣分项（合理偏差/技术债务，4 项）**：
- L-01-001（-0.150）：api 包覆盖率 41.9%（部分修复），继续提升需大量测试代码
- L-04-001（-0.250）：缺失高价值端到端集成测试代码（N 类已手动验证通过）
- M-03-001（-0.160）：yaml v4 rc 版本（需等社区发布稳定版）
- M-04-001（-0.160）：superviseService 圈复杂度 43（gocyclo 实测，修复需重构，违反"最小化修改"原则）

**Phase 8 已修复项（8 项，恢复 1.9925 分）**：
- ✅ I-03-002：upload.go 5 处 file.Close() 添加 slog.Warn 错误日志
- ✅ A-02-001：restart.go 添加 ResetIfNeeded 语义澄清注释
- ✅ A-04-001：concurrency.go 添加 serialize "last pending wins" 语义说明
- ✅ A-08-001：watcher.go 添加连续 addWatch 失败阈值检测（fd 耗尽预警）
- ✅ M-02-001：实际运行 race 测试 -count=3，零竞态
- ✅ N-01~N-05：实际运行端到端测试（核心API/生命周期/扩展/热重载/长轮询）全部通过
- ✅ L-01-001（部分）：补充 port_collector_test.go（parseHexIP IPv4/IPv6/错误分支测试）
- ✅ M-05-001：前端 bundle 代码分割（605KB 单 bundle → index 151KB + vendor 57KB = 208KB gzip 首次加载）

**技术债修复状态核查（v2.1 新增）**：
- ✅ TD-001：executor.go 圈复杂度 53 → 已修复（gocyclo 实测最高 22）
- ✅ TD-002：adapters.go 2135 行 → 已修复（当前 9 行，superviseService 已移出）
- 🟡 TD-003：superviseService 重复 → 部分修复（移到 service_operator.go，但 bootstrap.go 仍有重复）
- ✅ TD-004：CLI 错误中文化 → 已修复（internal/cli/errors.go 存在）
- ❌ TD-005：useLongPolling hook → 未修复（web/src/hooks/ 未找到）
- ✅ TD-006：service.ts 共享类型 → 已修复（web/src/types/service.ts 23 行）
- ✅ TD-007：getErrorMessage 工具函数 → 已修复（web/src/lib/error-utils.ts 存在）

**规格侧建议（SPEC-001~SPEC-005）**：退避公式 / triggers 格式 / 状态机扩展 / serialize 队列上限 / symlink 说明——建议规格 v1.6 补充。

**Phase 4 新增偏差（DEV-012）**：`actions[].icon`/`enabled` 字段规格要求但 Action struct 未实现（详见 deviations.md）。

## 四、关键决策（合并历史）
- 不引入数据库、不引入 SSE/WebSocket（长轮询是规格要求）
- 不引入 tini/dumb-init（supd 自带 PID 1 能力）
- trackerList 替换整表 vs trackerAdd 追加：选择 `trackerList`（语义匹配）
- triggers 格式差异（DEV-011）：实现使用 map 格式，所有 meta.yaml 与代码一致
- meta.yaml 中 `service:` 字段冗余（YAML 解析器静默忽略），服务关联由目录结构决定
- SIGCHLD buffer=1 合理（Go 官方文档明确），reapZombies 内部 Wait4(-1, WNOHANG) 循环清空
- 不主动调用 PR_SET_CHILD_SUBREAPER（PID 1 内核自动接收孤儿子进程）
- Phase 8 决策：接受 97.4375 分作为最终结果，剩余扣分项记录为合理偏差（详见 blockers.md）

## 五、已知偏差（详见 `deviations.md`）
- DEV-003：ProcessManager/EventRingBuffer/TaskHistory 使用 sync.Mutex（合理例外）
- DEV-004：api/interfaces.go 定义 13 个接口（符合 Go 惯用法）
- DEV-005：服务级扩展默认 run_as 未完整接入（显式 run_as 已生效）
- DEV-008：API 端点 77 vs 规格 65（多出 12 个实用端点，已确认保留）
- DEV-009：`SUPD_SERVICE_DIR` 是规格外变量（其余 13 个 SUPD_* 与规格一致）
- DEV-011：triggers 格式差异（实现 map vs 规格 list）
- DEV-012：`actions[].icon`/`enabled` 字段规格要求但 Action struct 未实现

## 六、下次会话注意
- Phase 8 已完成 + v2.1 技术债核查完成，审计报告 v2.1 在 `tmp/审计报告.md`，blockers.md 记录了未达 98 分的原因
- 修复 R-001 后，导入响应应包含 reload 结果（services/global_extensions/scan_errors）
- watcher 白名单已修复，qbittorrent 等服务运行时数据目录不再被监控
- ZombieReaper 现支持 `Stop()` 方法，run.go 使用 `defer reaper.Stop()`
- Dockerfile 镜像默认不含 bash/python3/node，扩展脚本依赖需 `apk add`
- Docker 部署用 `docker stop -t 30` 与 `shutdown_grace_seconds` 对齐
- `--no-pid1` 仅适用于 systemd 场景，Docker 中禁用会导致僵尸进程无法回收
- transmission 服务位于 `test_workdir/services/transmission/`，binary-updater 首次运行前 `bin/` 不存在（run.sh 已 `mkdir -p`）
- 若新增服务/扩展目录结构变化，需检查 `shouldWatchDir` 是否覆盖新路径模式
- `shouldSkipDir` 黑名单（data/bin/logs/history/cache/tmp/temp/run）覆盖常见运行时目录，新目录名需补充
- 后续补充测试时优先覆盖 api 包错误分支（L-01 剩余）和端到端集成测试代码（L-04）
- 监控 yaml v4 稳定版发布（M-03），发布后升级 go.mod

## 七、会话结束规范（来自 AGENTS.md）
每次结束更新本文件：本次完成 / 遗留 / 下次注意。禁止伪造、禁止记录未实际完成内容。

---

## 八、2026-07-21 Docker 容器测试与 tjs 运行时集成（本轮完成）

### 本次完成
1. **tjs 运行时集成**：
   - 将 txiki.js 静态二进制集成到 [Dockerfile](file:///home/qq/Documents/trae_projects/supd/Dockerfile) （`/usr/local/bin/tjs-bin`）
   - 添加 `/usr/local/bin/tjs` 包装脚本（支持直接运行 `tjs <script.js>` 或 `tjs run <script.js>`）
   - 建立 `/etc/supd/runtimes/tjs` 软链接，使 supd `watch.Discovery` 自动扫描注册 `tjs` 别名 (`source="scan"`, `available=true`)
2. **Docker 完整功能与优雅退出测试**：
   - 成功构建并启动 `supd:test` 容器（PID 1 模式，端口 8089 映射）
   - 运行时 API 查询：`GET /api/runtimes` 成功返回包含 `tjs` 的已注册运行时列表
   - 服务启停验证：
     - `POST /api/services/web-demo/start` → 返回 `202 accepted`，状态变为 `up` (PID 30, Mem 5.6MB)
     - `POST /api/services/web-demo/stop` → 返回 `202 accepted`，状态顺利变回 `down`
   - 扩展执行验证：
     - 运行 `tjs-test-ext` (使用 `runtime: tjs`) → 执行成功（12ms），`state: success`，输出 `::progress::` / `::result::`
     - 运行 `on-demand-tool` (bash 扩展) → 执行成功（1.0s），`state: success`
   - 容器优雅退出：`docker stop -t 10` 拦截 `SIGTERM` 并在 2s 内完成平滑关机
3. **GitHub Actions 与双架构 tjs 集成**：
   - 设计并实现手动构建与推送工作流：[.github/workflows/build-push.yml](file:///.github/workflows/build-push.yml)。
   - 设计并实现自动 Release 触发工作流：[.github/workflows/release.yml](file:///.github/workflows/release.yml)。
   - **双架构 tjs 原生编译**：为规避 QEMU 模拟编译带来的性能惩罚，上述工作流统一使用 GitHub 托管的 `ubuntu-latest` (amd64) 与 `ubuntu-24.04-arm` (arm64) 原生 runner 编译静态 tjs 二进制，并通过 `actions/cache` 对编译产物进行缓存加速。
   - **多阶段 Docker 镜像打包**：重写了 [Dockerfile](file:///Dockerfile)，不再依赖宿主机预编译产物，使用多阶段打包，将编译出的 `tjs` 注入 `amd64` 和 `arm64` 两架构的运行时，实现双平台均自带轻量 JavaScript (tjs) 运行时。
   - **多平台 Manifest 联合推送**：工作流最后通过 `docker buildx imagetools` 自动合并生成多架构联合 Manifest，推送至 GitHub 容器托管服务 (GHCR)。

---

## 九、2026-07-21 v0.0.1 发布与 README 完善（本轮完成）

### 本次完成
1. **v0.0.1 Release 全流程**：经 9 次 workflow 修复，最终成功发布。
   - Release: https://github.com/qq859952722/supd/releases/tag/v0.0.1
   - Docker 镜像：`ghcr.io/qq859952722/supd:v0.0.1` 与 `:latest`（amd64 + arm64，含 tjs）
   - 二进制产物：`supd-linux-amd64.tar.gz` (6.2MB) + `supd-linux-arm64.tar.gz` (5.8MB) + `checksums.txt`
2. **CI 关键修复**（详见提交日志）：
   - pnpm 版本：`pnpm@latest` → `pnpm@10`（Node 20 不支持 pnpm 11 的 `node:sqlite`）
   - pnpm overrides：从 `pnpm-workspace.yaml` 移至 `package.json` 的 `pnpm.overrides`（pnpm 10 不读 workspace overrides）
   - Dockerfile pnpm 安装：`corepack` → `npx -y pnpm@10`（规避 corepack 兼容性）
   - 新建 `.dockerignore`：构建上下文 508MB → 30MB
   - 简化平台支持：仅保留 linux/amd64 + linux/arm64（移除 darwin / armv7）
   - checksums.txt 覆盖 bug：matrix 各实例分别生成 → release job 统一生成
3. **README 完善**（[README.md](file:///home/qq/Documents/trae_projects/supd/README.md)）：
   - 「安装」章节：新增三种安装方式（Docker 推荐 / 预编译二进制 / 源码编译）
   - 「CLI 命令参考」：补充 `version` / `ext show` / `ext status` / `export` / `import` / `token` / `runtimes` / `validate` 命令
   - 「Docker 部署」：更新为优先从 GHCR 拉取镜像（含 tjs 运行时说明），本地构建作为备选
   - 新增「项目状态」章节（版本、平台支持、技术栈）
   - 新增「相关文档」章节（链接到需求规格说明书与开发指南）
   - 新增「参与贡献」章节（验证命令与规格对照提醒）

### 遗留未完成
- 工作区有未提交的 README 修改（待用户决定是否提交）
- GitHub Actions Node.js 20 弃用警告（建议后续升级 actions/setup-node@v5）
- TD-003：`superviseService` 在 `bootstrap.go` 中仍有重复代码
- L-01-001：api 包测试覆盖率 41.9%（剩余错误分支未覆盖）
- L-04-001：缺少端到端集成测试代码

### 下次会话注意
- README 修改未提交，若用户要求提交，按 `docs/需求规格说明_v1.5.md` 校对后再 commit
- 升级 actions 版本时需注意 Node.js 22+ 要求（参考 pnpm 11 兼容性教训）
- TD-005 `useLongPolling` hook 抽取属于中等收益/中风险，按需评估

---

## 十、2026-07-21 工作区过程性文件清理（本轮完成）

### 本次完成
按用户要求「清理非源代码、文档的过程性、缓存性的文件及文件夹，把后续相关文件及路径添加到 git 排除目录」，清理如下：

**1. 从 git 跟踪移除并删除磁盘文件（共约 41MB）：**
- 根目录：`config.test` (5.1MB) / `extension.test` (7.9MB) / `coverage.out` (360KB) — Go 测试二进制与覆盖率输出
- `test_workdir/services/transmission/bin/transmission-daemon` (28MB) — 通过扩展下载的运行时二进制
- `test_workdir/services/transmission/data/` (40KB) — transmission 运行时数据
- `test_workdir/runtimes/tjs` — 容器内 `/usr/local/bin/tjs` 的软链接（本地无意义）
- `web/tsconfig.tsbuildinfo` — TypeScript 增量构建缓存
- `webdist/` — 空目录（历史遗留）

**2. [.gitignore](file:///home/qq/Documents/trae_projects/supd/.gitignore) 更新：**
- 新增 web 缓存规则：`web/.pnpm-store/` / `web/.vite/` / `web/*.tsbuildinfo`
- 新增 test_workdir 通用规则：`test_workdir/services/*/bin/` / `test_workdir/services/*/data/` / `test_workdir/runtimes/` / `test_workdir/script_tmp/`
- 删除已被通用规则覆盖的 `test_workdir/services/qbittorrent/data/` 与 `test_workdir/services/qbittorrent/bin/qbittorrent-nox` 单条规则

### 验证结果
- `go build ./...` 通过
- `go vet ./...` 通过
- `git status` 显示 11 个变更条目（10 个删除 + 1 个 .gitignore 修改），无意外文件
- `git check-ignore` 验证所有新增规则生效
- 未跟踪文件数 0（所有过程性文件均被 .gitignore 正确覆盖）

### 保留的文件（源代码/配置/文档级别）
- `test_workdir/config.yaml` / `env/` / `extensions/*/meta.yaml + run.sh` / `services/*/service.yaml + run.sh` — 测试用例配置
- `tmp/audit_plan.md` — 用户明确要求保留的审计计划文档
- `docs/` 全部文档

### 遗留未完成
- 清理变更尚未 git commit（待用户确认）
- README 修改（上一轮）也未提交

---

## 十一、2026-07-21 node_modules 等历史遗留跟踪文件深度清理（本轮完成）

### 触发原因
用户发现 `web/node_modules/` 被跟踪，要求排查根因与其他类似情况。

### 排查结果（`git ls-files -i -c --exclude-standard`）
共发现 **5225 个**「被 .gitignore 排除但仍被跟踪」的文件：
- `web/node_modules/` — **5213 个**前端依赖文件
- `web/dist/` — 9 个前端构建产物
- `web/pnpm-lock.yaml` — 1 个（lockfile 被错误排除）
- `cmd/supd/main.go` — 1 个（被 `supd` 规则误伤）
- `tmp/audit_plan.md` — 1 个（被 `tmp/` 规则误伤，但本应保留）

### 根因
初始提交 `b875bdd "1"` 时直接 `git add .`，将 `web/node_modules/` 与 `web/dist/` 一起提交。后续添加的 .gitignore 规则只对未跟踪文件生效，对已跟踪文件无效，导致这些垃圾文件一直留在版本控制中。

### 本次完成

**1. 从 git 跟踪移除（磁盘文件保留）：**
- `web/node_modules/`（5213 个文件，229MB）
- `web/dist/`（9 个文件，9.9MB）

**2. [.gitignore](file:///home/qq/Documents/trae_projects/supd/.gitignore) 规则修正：**
- `supd` → `/supd`（仅匹配根目录二进制，不再误伤 `cmd/supd/` 源代码目录）
- `tmp/` → `tmp/*` + `!tmp/audit_plan.md`（显式保留审计计划文档）
- 删除 `pnpm-lock.yaml` 和 `web/pnpm-lock.yaml` 两条错误规则（lockfile 必须纳入版本控制）

### 验证结果
- ✅ `go build ./...` 通过
- ✅ `go vet ./...` 通过
- ✅ `git ls-files -i -c --exclude-standard` 输出为空（无遗留冲突）
- ✅ `cmd/supd/main.go` 不再被 .gitignore 误判
- ✅ `tmp/audit_plan.md` 被显式保留
- ✅ 磁盘上 `web/node_modules/` 与 `web/dist/` 仍存在（开发环境可用）
- 被跟踪文件数：**5562 → 330**（减少 94%）

### git status 概况
- 5232 个 D（deleted from index，磁盘文件保留）
- 2 个 M（.gitignore + README.md）
- 加上上一轮的 11 个变更，总计待提交变更约 5245 条

### 下次会话注意
- 提交时建议分两个 commit：① 清理 node_modules/dist 等历史遗留跟踪 ② README 与上一轮清理
- 后续 `pnpm install` 会自动重建 `web/node_modules/`，`pnpm build` 会重建 `web/dist/`，均不影响开发
- `web/pnpm-lock.yaml` 现已正确纳入版本控制，后续 pnpm 依赖变更会正常被 git 跟踪

---

*最近一次更新：2026-07-21（node_modules 历史遗留跟踪问题彻底修复，被跟踪文件数 5562→330）*
