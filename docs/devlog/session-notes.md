# supd 开发会话备忘（主索引）

> 跨会话上下文传递。Agent 新会话启动时首先阅读本文件（主索引）+ `blockers.md`。
> 详细信息按需读取 `notes/` 子目录，**不要默认全量读取**。读取协议见 `notes/README.md`。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。
> 完整历史归档（2026-07-21 ~ 2026-07-26）见 `archive/session-notes-20260727-precompress.md`。

---

## 一、项目状态

- **阶段**：维护/修复/测试阶段（57 Task 全部完成，8 阶段任务执行计划闭合）
- **质量水位**：⭐ 优秀（满分 100），1000+ 单元测试通过（Go + 前端），零竞态；staticcheck/go vet 零警告
- **当前版本**：v0.0.43（版本升级见 `version-upgrade-guide.md`；本地 commit 已完成，待推 GitHub）

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
| 2026-07-30 | clear-failed 状态重置由 pending 改为 down | 诊断远程实例 code-server 卡 pending 根因（clear-failed 不触发启动 + 无上游依赖无法被依赖协调器唤醒）；ClearFailedState 改为 ResetTo(StateDown)，避免无依赖服务永久卡 pending | [notes/2026-07-30.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-30.md) |
| 2026-07-30 | 审计+运行状态测试+错误码修复+skill 更新+v0.0.41 发版 | 审计新增代码（符合规格 §2.8.1）；A～F 六组运行状态测试全通过；E 组发现并修复"非 failed 状态调用 clear-failed 误返 500"（改返 400 INVALID_REQUEST）；skill 同步更新 clear-failed 行为与 API；发版 v0.0.41 | [notes/2026-07-30.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-30.md) |
| 2026-08-02 | Skill 完善与远程服务优化 + code-server 修复 | Skill 目录契约/校验/打包修复（详见同日早段）；远程 192.168.31.188:7979 code-server 启动修复（versioned wrapper 缺失 + 入口错误 + glibc node fcntl64 缺失）；root 空白密码解锁 SSH 调试；12 个服务 README 编写；11 ready / 1 down / 0 failed | [notes/2026-08-02.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-02.md) |
| 2026-08-02 | tracker-updater v1.3.0 测速分组 + 两个下载扩展 uid 1000 | ngosang 源加入（持续更新）；BEP-15 UDP CONNECT 测速（tjs UDPSocket）；3 源合并 563→测速存活 325（含 147 UDP）→49 优质独立 tier+1 垫底=50 tier；transmission-updater 路径修复+chown 非致命；pre-start-fixperms 分层 chown（bin/+web/→uid1000）；两扩展 run_as_uid 1000 验证通过 | [notes/2026-08-02.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-02.md) |
| 2026-08-03 | 修复多服务同名扩展查找竞态 + v0.0.42 发版 | `GetExtension(name)` 在多服务同名扩展时随机匹配（Go map 随机序）→ 偶发 404 + Update/Delete/SaveEnv 静默数据损坏/丢失；新增 `GetExtensionForService(service, name)` 精确查找；5 个 provider 方法 + 2 个 handler 改用；7 新测试（含 80 请求 handler 压测 + 数据不串扰回归）；go test/race/pnpm build 全通过；推 v0.0.42 | [notes/2026-08-03.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-03.md) |
| 2026-08-04 | 远程服务目录结构优化 + SSH 空密码连接固化 | smartdns rules 迁入 config/rules/ + 8 服务绝对路径相对化（含 transmission/env.yaml）；新建 `remote_ssh.sh` 封装脚本（SSH_ASKPASS 注入空密码，不改 ssh 配置）；skill 文档补 §3.1 SSH 连接 + §3.2 smartdns 路径基准差异；清理 s-ui 残留备份扩展目录；12 服务 ready，update-gfw-china 扩展验证通过 | [notes/2026-08-04.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-04.md) |
| 2026-08-04 | workdir 相对路径支持 + v0.0.43 发版 + 远程验证 | 新增 `core.ResolveWorkdir` 统一解析函数；移除 workdir 绝对路径校验；统一 6 处 workdir 构建逻辑（含审计新发现 resource_handler 隐蔽缺陷）；新增 7 个测试用例；本地端到端测试通过；构建 v0.0.43 部署远程容器重启；adguardhome `workdir: .` 热重载+重启验证 CWD 正确解析 | [notes/2026-08-04.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-04.md) |

---

## 八、最近会话重点（2026-08-04 workdir 相对路径支持 + v0.0.43 发版 + 远程验证）

### 本次完成

1. **代码修改：支持相对路径 workdir**：
   - 新增 `core.ResolveWorkdir` 统一解析函数（空=服务根、绝对=清理、相对=Join+Clean 基于服务根目录）
   - 移除 `service_validate.go` 中 workdir 必须绝对路径的校验
   - 统一 6 处 workdir 构建逻辑为调用 `ResolveWorkdir`（此前每处手动写 `if workdir == "" { ... }`）
   - **审计新发现第 6 处**：`resource_handler.go` 磁盘占用统计路径直接用 `info.Config.Workdir` 未解析相对路径，本次修复
2. **规格与前端同步**：规格 workdir 字段描述更新；前端表单 placeholder 与 hint 更新
3. **测试覆盖**：新增 `workdir_test.go` 7 个用例（空/绝对/相对/`./`/`.`/嵌套/逃逸/AdguardHome 场景）；更新 service_test.go 期望相对路径通过验证
4. **本地端到端测试**：创建 `workdir-rel-test` 服务（`workdir: subdir`），启动后 CWD 正确解析为 `service_root/subdir`，服务 ready
5. **v0.0.43 构建与部署**：构建 linux/amd64 二进制（25MB），SFTP 上传，原子替换远程 `/usr/local/bin/supd`，`kill -TERM 1` 容器重启加载新版本
6. **远程 adguardhome 验证**：添加 `workdir: .`（相对路径，旧版本会拒绝），watcher 热重载成功接受，restart 后进程 CWD = `/etc/supd/services/adguardhome`（服务根），服务 2 秒内 ready

### 关键技术点

- **ResolveWorkdir 设计**：统一入口消除 6 处重复模式；相对路径用 `filepath.Join(root, workdir)` + `filepath.Clean` 清理
- **resource_handler 隐蔽缺陷**：磁盘统计路径此前的 workdir 构建与启动路径不一致（直接用未解析的 `info.Config.Workdir`），相对 workdir 会导致 `syscall.Statfs` 路径错误
- **容器重启策略**：supd 作为 PID 1 在 Docker 容器中，`kill -TERM 1` 触发优雅关机后容器自动重启
- **dropbear-ssh autostart:false**：容器重启后需通过 API 手动启动 dropbear-ssh 恢复 SSH 访问
- **热重载验证**：watcher 检测 service.yaml 变更后热重载，新版本验证器接受相对 workdir，服务无需重启即生效（CWD 在下次重启时应用）

### 遗留事项

- v0.0.43 尚未推送到 GitHub（本地 commit 已完成，待用户确认是否发版）
- adguardhome service.yaml 中 `workdir: .` 为测试添加，可保留（功能等价于未设 workdir）或移除

---

## 附：2026-08-04 远程服务目录结构优化 + SSH 空密码连接固化（同日早段）

### 本次完成

1. **smartdns 目录结构优化**：
   - `rules/`（顶层）→ `config/rules/`，规则文件归入 config 命名空间
   - smartdns.conf 路径相对化：`domain-set -file rules/xxx.txt`（基准=config/）、`plugin ../bin/smartdns_ui.so`（基准=config/）、`cache-file config/smartdns.cache`（基准=服务根）
   - 清理路径基准错误产生的空目录 `config/config/`
   - 验证：smartdns restart 后 ready，update-gfw-china 扩展 6s 更新 110878+26079+4189/1605 条
2. **全量绝对路径扫描与修复**：通过 SSH 全面扫描 12 个服务的 service.yaml / env.yaml / config/ / 扩展脚本，修复所有实际问题：
   - 8 个服务的 service.yaml + config 文件绝对路径相对化（smartdns/dnscrypt-proxy/code-server/openlist/backrest/filebrowser/adguardhome + 之前已修复的）
   - `transmission/env.yaml`：`TRANSMISSION_HOME`/`TRANSMISSION_WEB_HOME` 绝对路径 → 相对路径（`config`/`web`），重启验证 ready
   - 清理 `s-ui/extensions/fix-sui-perm.bak.20260726-145818/` 残留备份目录（会被 supd 发现为扩展）
   - VSCode 自管理元数据（extensions.json 等）和 filebrowser.db 二进制数据库不改（前者 VSCode 自管理，后者被 `--root data` 命令行参数覆盖）
   - `${SUPD_SERVICE_DIR:-/etc/supd/...}` fallback 模式（shell + JS）可接受，supd 运行时注入环境变量覆盖
3. **SSH 空密码连接方式固化**：
   - 新建 `scripts/remote_ssh.sh`：自动创建 askpass + 自动启动 dropbear-ssh + 支持交互/命令/SFTP 上传下载
   - 设计原则：不修改 dropbear-ssh 服务配置，仅依赖现有 `-B` 空密码模式；通过 `SSH_ASKPASS` 注入空密码，不依赖 sshpass
   - `ensure_dropbear` 改用 grep 替代 python3（远程容器无 python3）
   - skill 文档 `04_online_dev_guide.md` §3.1 新增 SSH 客户端空密码连接章节（前置条件 + 封装脚本 + 手动命令 + 运维示例 + 注意事项）
4. **smartdns 路径基准差异文档化**：skill §3.2 新增路径基准对照表（`cache-file` 基准=CWD vs `domain-set/ip-set/plugin/conf-file` 基准=配置文件目录）+ 推荐目录结构 + 常见错误

### 关键技术点（同日早段）

- **smartdns 路径基准双重性**：`cache-file` 基准 = 进程 CWD（service.yaml 未设 workdir 时 = 服务根目录）；`domain-set -file`/`ip-set -file`/`plugin`/`conf-file` 基准 = 配置文件所在目录（config/）
- **SSH 空密码注入**：`SSH_ASKPASS=<script> SSH_ASKPASS_REQUIRE=force setsid -w ssh ...` 通过 askpass 脚本输出空密码，绕过交互式密码提示
- **dropbear -B 模式**：允许空密码登录，但要求 `/etc/shadow` 中密码字段为空（`NP` 状态）
- **容器重启恢复**：`/etc/shadow` 在 overlay 文件系统，容器重启后 root 密码恢复，需重新 `passwd -d root`
- **环境变量相对路径**：supd 启动服务时 CWD=服务根目录（service.yaml 未设 workdir），故 env.yaml 中的 `TRANSMISSION_HOME=config` 等相对路径会被进程正确解析为 `<服务根>/config/`

---

## 附：2026-08-02 tracker-updater v1.3.0 测速分组 + 两个下载扩展 uid 1000

> 历史详情见 [notes/2026-08-02.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-02.md)

### 本次完成

1. **tracker-updater v1.2.0 → v1.3.0**：ngosang 源加入；BEP-15 UDP CONNECT 测速；3 源合并 563→存活 325→49 优质 tier+1 垫底=50 tier
2. **两个下载扩展 uid 1000**：tracker-updater + transmission-updater v1.1.0（路径修复+chown 非致命）run_as_uid 1000
3. **pre-start-fixperms v1.1.0**：分层 chown（config/→nobody, bin/+web/→uid1000）

### 遗留事项（已在 2026-08-03 解决）

- ~~service-scoped 扩展触发端点多服务同名 404 问题~~ → **2026-08-03 已彻底修复**

---

## 附：2026-08-02 Skill 配置结构与工具完善（同日早段）

> 历史详情见 [notes/2026-08-02.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-02.md)

### 本次完成

1. **统一目录契约**：补齐 `<baseDir>`、服务、全局扩展、服务级扩展与 runtime 的发现规则，并明确 watcher 和运行期写入边界。
2. **规格纠偏**：修正扩展 `entry`/`runtime`/timeout、服务与扩展 env 来源、package profile 名称及三级回退语义。
3. **开发工具修复**：`validate_dev.py` 的所有 FAIL 现在返回非零，并递归校验服务级扩展；`pack_dev.py` 对齐 Go profile 解析和非法名称拒绝规则。
4. **示例完善**：简单服务补充 env、default/migrate profile 和服务级扩展；复杂服务补齐实际入口 `bin/myapp`。
5. **源码印证**：对照 discovery、watcher、config、dispatcher、package profile、packer、CLI init/run；服务 version 三段数字来自需求规格 §2.3.2，当前 Go 服务校验仅检查非空，Skill 已显式说明并补齐门禁。
6. **验证**：正例、目录名负例、非法 profile、默认/命名 profile 打包、Python 编译、diff check、Go build/vet/test 全部通过。

### 遗留事项

- Go `ValidateService` 尚未调用已定义的 `versionRegex`；本次仅修复 Skill，未改动 Go 源码。

---

## 附：2026-07-30 审计+运行状态测试+错误码修复+skill 更新+v0.0.41

> 历史详情见 [notes/2026-07-30.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-30.md)

- 诊断远程实例 code-server 卡 pending 根因（clear-failed 重置为 pending 后无上游依赖无法被唤醒）
- ClearFailedState 改为 ResetTo(StateDown)（规格 §2.8.1 允许 down/pending）
- E 组发现并修复"非 failed 状态调用 clear-failed 误返 500"（改返 400 INVALID_REQUEST）
- 发版 v0.0.41

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
