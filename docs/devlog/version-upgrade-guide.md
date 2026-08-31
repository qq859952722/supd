# supd 版本升级指南

> 本文档记录 supd 的版本管理架构与升级流程。后续发版时只需按本指南执行，无需重新分析代码。

---

## 一、版本管理架构

supd 的版本号通过 **ldflags 编译时注入**，源码中不硬编码版本号。

### 注入链路

```
git tag v0.0.3
    │
    ├── CI（release.yml）从 tag 名提取版本 → -ldflags "-X main.version=v0.0.3"
    │       └── 同时注入 Dockerfile ARG VERSION=v0.0.3
    │
    ├── Makefile 通过 git describe 自动推导 → -ldflags "-X main.version=$(VERSION)"
    │
    └── 源码默认值 version = "dev"（未被 ldflags 覆盖时显示）
```

### 关键文件（无需修改）

| 文件 | 作用 | 默认值 | 是否需改 |
|------|------|--------|----------|
| `cmd/supd/main.go:14` | 版本变量定义 | `version = "dev"` | ❌ 永远不改 |
| `internal/cli/version.go:14` | CLI 包版本变量 | `Version = "dev"` | ❌ 永远不改 |
| `internal/cli/version.go:34` | `SetVersionInfo(v, bt)` | 由 main 调用注入 | ❌ 永远不改 |
| `Dockerfile:17` | Docker 构建 ARG | `ARG VERSION=dev` | ❌ 永远不改 |
| `Makefile:5` | 本地构建版本推导 | `git describe --tags` | ❌ 永远不改 |
| `.github/workflows/release.yml` | CI 版本提取 | 从 `${GITHUB_REF_NAME}` 提取 | ❌ 永远不改 |

### 唯一需手动修改的文件

| 文件 | 行 | 内容 | 说明 |
|------|----|------|------|
| `README.md` | ~246 | `docker pull ghcr.io/.../supd:vX.Y.Z` | Docker 镜像拉取示例 |
| `README.md` | ~305 | `| 当前版本 | \`vX.Y.Z\` |` | 项目状态表 |

> **测试文件 `internal/api/adapters_test.go` 中的 `"0.0.2-test"` 是测试夹具字符串（验证版本透传），不是版本引用，不需要修改。**

---

## 二、升级流程（每次发版执行）

### Step 1: 更新 README.md

```bash
# 将旧版本号替换为新版本号（仅 2 处）
# 例如 v0.0.2 → v0.0.3
sed -i 's/supd:v0\.0\.2/supd:v0.0.3/' README.md
sed -i 's/当前版本 | `v0\.0\.2`/当前版本 | `v0.0.3`/' README.md
```

或手动编辑 `README.md`：
1. `docker pull ghcr.io/qq859952722/supd:v0.0.3`（Docker 镜像示例）
2. `| 当前版本 | \`v0.0.3\` |`（项目状态表）

### Step 2: 验证构建

```bash
go build ./...
go vet ./...
go test ./... -count=1

# 验证版本注入（本地）
go build -ldflags "-X main.version=0.0.3" -o /tmp/supd-test ./cmd/supd/
/tmp/supd-test version
# 应输出: supd 0.0.3
rm /tmp/supd-test
```

### Step 3: 提交变更

```bash
git add README.md
git commit -m "release: vX.Y.Z — <简述本次变更>"
```

### Step 4: 创建并推送 tag（触发 CI 构建）

```bash
# 创建 annotated tag
git tag -a vX.Y.Z -m "supd vX.Y.Z"

# 推送到远程（自动触发 release.yml 工作流）
git push origin vX.Y.Z
```

### Step 5: 确认 CI 构建

```bash
# 查看 workflow 运行状态
gh run list --limit 3

# 或监控特定 run
gh run watch
```

CI 会自动完成：
1. 编译双架构 tjs（amd64 + arm64）
2. 构建双平台二进制（linux-amd64 + linux-arm64）
3. 构建双架构 Docker 镜像并推送到 GHCR
4. 创建多架构 manifest（`vX.Y.Z` + `latest`）
5. 生成 GitHub Release（含二进制 + checksums + 自动变更记录）

### Step 6: 更新开发日志

在 `docs/devlog/session-notes.md` 末尾追加版本升级记录。

---

## 三、预发布版本

预发布版本（如 `v0.0.3-beta`、`v1.0.0-rc.1`）流程相同：

```bash
git tag -a v0.0.3-beta -m "supd v0.0.3-beta"
git push origin v0.0.3-beta
```

CI 会自动识别预发布（tag 含 `-`），GitHub Release 标记为 prerelease，**不更新 `latest` Docker tag**。

---

## 四、版本号规范

遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)：

| 版本类型 | 格式 | 何时使用 | 示例 |
|----------|------|----------|------|
| 正式版 | `vMAJOR.MINOR.PATCH` | 正式发布 | `v0.0.3` |
| 预发布 | `vMAJOR.MINOR.PATCH-LABEL` | 测试/候选 | `v0.0.3-beta`、`v1.0.0-rc.1` |

- **PATCH**（0.0.X）：bug 修复、小改进，向后兼容
- **MINOR**（0.X.0）：新功能，向后兼容
- **MAJOR**（X.0.0）：破坏性变更

> 当前处于 0.x.x 阶段，任何变更都可能不向后兼容，但仍遵循 semver 格式。

---

## 五、常见问题

### Q: 为什么不在源码中写死版本号？
A: ldflags 注入是 Go 社区标准实践。源码默认值为 `"dev"`，CI/Docker/Makefile 通过 ldflags 覆盖。好处是：单一真相源（git tag），无需修改源码。

### Q: 忘记改 README 就推了 tag 怎么办？
A: README 版本号仅影响文档展示，不影响构建产物。可以在下个 commit 补改，或创建新 tag。

### Q: 如何本地验证版本注入？
A: `go build -ldflags "-X main.version=0.0.3" -o /tmp/supd ./cmd/supd/ && /tmp/supd version`

### Q: CI 构建失败怎么办？
A: `gh run view <run-id> --log-failed` 查看失败日志，修复后重新推送 tag（需先删除旧 tag）：
```bash
git tag -d vX.Y.Z                    # 删除本地 tag
git push origin :refs/tags/vX.Y.Z    # 删除远程 tag
# 修复后重新创建并推送
git tag -a vX.Y.Z -m "supd vX.Y.Z"
git push origin vX.Y.Z
```

---

## 六、变更记录

| 日期 | 版本 | 变更 |
|------|------|------|
| 2026-07-22 | v0.0.5 | 修复 Docker 首次启动时 config.yaml 缺失导致退出（run 命令自动检测并调用 init） |
| 2026-07-22 | v0.0.4 | 3 项规格偏差修复（script readiness env 继承 / on_failure SUPD_SERVICE_PID 注入 / cronScheduler.Stop 超时）+ Dropbear SSH 集成 + 服务进程 env.yaml 加载修复 |
| 2026-07-22 | v0.0.3 | tjs 运行时接入默认配置 + Docker 工具集 + auto-create-users 扩展 + user 字段接入进程启动修复 |
| 2026-07-21 | v0.0.2 | 端口迁移 7979、init 示例、script readiness 修复 |
| 2026-07-21 | v0.0.1 | 首次发布 |
| 2026-07-24 | v0.0.20 | transmission-updater run.js 7 项 bug 修复（含 3 项 tjs 运行时发现：readDir DirHandle / isDirectory getter / --version stderr）+ 新增代码审计与运行测试全通过 |
| 2026-07-24 | v0.0.21 | M-04-001/TD-003 supervisor 重构闭环：抽取共享 `internal/core/supervisor.go`（SupervisorCallbacks 依赖注入）+ 审计（圈复杂度最高单函数 43→19）+ 7 场景真实运行测试全通过；修复 3 处 ctx/error 硬伤与 P1（EventRestartAllowed→Abort 静默返回）；无新增 bug |
| 2026-07-26 | v0.0.29 | auto-create-users UID-GID 迁移修复：find -prune 语义显式化、UID/GID 输入校验、BusyBox adduser -G 组名参数、UID/组属主分别迁移及失败传播、扩展超时 300s |
| 2026-07-26 | v0.0.30 | 修复 `autostart: false` 服务初始状态未置为 `down` 导致首页持续显示"过渡中"的 BUG（对齐规格 §2.8.1） |
| 2026-07-26 | v0.0.31 | Docker 镜像增加 shadow 标准用户管理工具，优化 auto-create-users 扩展优先使用 usermod/groupmod，并在启动时向控制台与日志输出 ALLID 和全局 env 示例 |
| 2026-07-26 | v0.0.32 | 修复 GHCR `latest` 发布竞争：仅仓库最高稳定语义版本可更新共享标签，旧标签补推不再覆盖最新镜像 |
| 2026-07-26 | v0.0.33 | 修复 GitHub Latest Release 发布竞争：创建 Release 时禁止自动抢占，仅仓库最高稳定语义版本可显式标记为 Latest |
| 2026-07-26 | v0.0.34 | 服务详情页基本信息与元数据补充（显示名称、版本号与描述，补充 i18n 支持） |
| 2026-07-28 | v0.0.35 | R-01/R-02 资源采集降级路径修复（NSpid 精确映射 + 完整 CPU/内存指标）；R-03/R-05 transmission-updater 改用 curl 流式落盘 + 按需依赖校验 + 严格架构识别；R-08 buildStopConfigs nil 防御 |
| 2026-07-28 | v0.0.36 | 服务打包支持多 profile 规则文件（`package.<name>.yaml`，命名规范区分迁移/共享等场景）；前端导出对话框支持默认导出与按规则文件导出；Skill 优化：新增 `bin/`+`data/` 目录规范、`tjs.open` 流式下载代码段、二进制更新扩展示例（check-update/update/force-update） |
| 2026-07-28 | v0.0.37 | 审计修复 `10-binary-updater-ext` 与 `transmission-updater` 运行时缺陷：`tjs.args[2]`→`tjs.env.SUPD_ACTION`、`Buffer.concat`→`TextDecoder` 累加、`proc.wait()` 返回值改用 `exit_status` 字段、`getArch()` 改为 async 并支持 aarch64；28 项运行状态测试全通过（D/A/B/C/E 五组） |
| 2026-07-28 | v0.0.38 | 扩展运行时参数重构：删除 Action.Args，统一 SUPD_ACTION；新增临时 env 参数编辑与保存语义；修复 CLI 请求字段、服务级 env_path 和未知 action 静默回退；Skill 强制服务 README 规范 |
| 2026-07-29 | v0.0.39 | supd 启动信息摘要（Startup Banner）：两段式打印（Bootstrap 后静态摘要 + HTTP 绑定后实际监听地址+可访问 URL 枚举）；改造 `api.Server.Start` 为 `net.Listen`+`Serve`+`addrReady` 回调；双通道输出（stdout+slog）；IPv4/IPv6 双栈地址枚举；post_ready 扩展改为异步执行避免阻塞启动摘要 |
| 2026-07-30 | v0.0.40 | 审计整改 A～H 批次全量落地：修复 serialize 队列满的 failed 记录因 `StartedAt` 零值被 `TaskHistory.lazyCleanupLocked` 误删（F2-001，dispatcher 统一补全 result 元数据）；diagnostic 字段白名单脱敏；HTTP Server Start/Stop 数据竞态修复；Bootstrap.Cleanup 资源回收；skill 更新（02_extension_spec 并发策略详解、SKILL.md serialize 队列上限 16） |
| 2026-07-30 | v0.0.41 | `clear-failed` 状态重置由 `pending` 改为 `down`（规格 §2.8.1 允许 down/pending，选择 down 避免无上游依赖服务永久卡在 pending"等待中"假象）；`handleClearFailedService` 用 `errors.As` 识别 `*ServiceError`，非 failed 状态返回 `400 INVALID_REQUEST`（原误返 500）；skill 同步更新（01_service_spec §4.1 状态机 clear-failed 说明、04_online_dev_guide §3.2 补全 clear-failed/force-stop/signal API） |
| 2026-08-03 | v0.0.42 | 修复多服务同名扩展查找竞态：`GetExtension(name)` 遍历 `Discovery.Services`（Go map 随机序）多服务同名扩展时返回项不确定，导致 `handleRunServiceExtension`/`handleGetServiceExtension` 偶发 404，且 `UpdateExtension`/`DeleteExtension`/`SaveExtensionEnv`/`RunExtension`/`GetExtensionStatus` 可能静默误操作到错误服务的扩展（写错 meta.yaml / 删错目录 / 写错 env）。接口新增 `GetExtensionForService(service, name)` 按服务作用域精确查找；5 个 provider 方法 + 2 个 handler 改用；service="" 退化为 GetExtension 语义（导入预览兼容）；7 个新测试含 80 请求 handler 压测 + 数据不串扰回归；go test/race/pnpm build 全通过 |
| 2026-08-31 | v0.0.47 | 修复扩展工作目录与相对 entry 解析根被 v0.0.44 错误改为 baseDir/服务根的问题；同步运行时、导入导出校验、规格、Skill、示例与测试；完成全链路审计，Go build/vet/test、race、版本注入和示例校验全部通过 |
| 2026-08-31 | v0.0.48 | 优化 Alpine/Debian 镜像层结构：使用 `COPY --link --chmod` 分离 supd/tjs 二进制层，更新 supd 时复用基础系统与运行时层；兼容现有 GitHub Actions Buildx workflow，未执行本地构建 |
| 2026-08-31 | v0.0.49 | 增强正式发布和手动构建 workflow 的 tjs 编译缓存 key，绑定缓存 schema、基础镜像/libc、架构和 `TJS_VERSION`，避免构建环境变化时误复用旧产物；未执行本地构建 |
| 2026-08-31 | v0.0.50 | 新增固定 `tjs-cache` prerelease 资产缓存：按 Alpine/Debian、amd64/arm64、`TJS_VERSION` 和 schema 复用 tjs，未命中时编译并上传；清理任务按 `TJS_VERSION` 保留最近 5 个版本；未执行本地构建 |
| 2026-08-31 | v0.0.51 | 完善固定 `tjs-cache` Release 复用链路：Actions Cache 未命中时下载 Release asset，缓存命中但 Release 缺少资产时补上传；自动发布与手动构建共用并发保护和五版本清理；未执行本地构建 |
| 2026-08-31 | v0.0.52 | 修复 `build-push.yml` 的 `build-alpine` 和 `build-debian` 重复声明 job-level `if` 导致 GitHub Actions 调度前解析失败、workflow 列表显示异常的问题；未执行本地构建 |
| 2026-08-31 | v0.0.53 | 修复 tjs 固定 Release 资产未按目标名称上传，导致缓存始终无法命中的问题；改为上传实际命名资产并增加上传后校验，同时清理旧的无平台信息 `tjs` 资产；未执行本地构建 |



