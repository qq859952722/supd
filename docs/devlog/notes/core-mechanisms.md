# 核心机制备忘（长期参考）

> 本文件记录 supd 核心机制的实现细节与坑点，供开发时按需查阅。
> 主索引：`../session-notes.md`。业务规则权威来源：`../../需求规格说明_v1.5.md`。

---

## 一、生命周期与状态机

- 服务状态：`pending/starting/up/ready/stopping/down/failed`（7 种，禁止新增）
- 唯一就绪路径：`starting → up → ready`（readiness 通过后）
- 停止路径：`stopping → down`
- `sync.RWMutex` 保护 `SetDiscovery`
- 自动重启**不经过** `down` 状态（直接 starting→up）
- 优雅停机包含 `pre_stop`，总时长由 `stop.timeout_seconds` 控制
- **热重载更新 RestartEngine**（规格 §2.4.3 "立即生效"）：`applyReload` 重新加载 Config 后调用 `svcOperator.UpdateRestartEngines(cfg, discovery)`，对每个 engine 用 `BuildRestartEngine` 构建临时 engine 再 `SyncConfigFrom` 原地更新配置字段（保留 retries/lastStartTime）。`superviseService` 是递归 goroutine 传递固定 engine 引用，必须原地更新而非替换引用。maxRetries 从 0 改为 N 时，已累积的重试计数 ≥ N → 下次 `MaxRetriesReached` 即 true，重试中的服务停止

## 二、扩展与调度

- `dispatcher` 管理并发，`RecordRun` 记历史
- 扩展脚本必须读 `$SUPD_ACTION` 环境变量获取当前 action
- 扩展工作目录：`filepath.Dir(extEntry.ConfigPath)`（扩展自身目录）；`script_tmp` 仍创建供临时文件
- 并发策略：`replace/serialize/parallel/debounce:Ns`（4 种）
  - serialize 语义："last pending wins"——pendingRun 单指针，新触发覆盖旧 pending（A-04-001）
- 任务历史保留 7 天（内存），环形缓冲 200 条
- **on_schedule 失败重试**（`retry_on_failure`，规格 §2.2.3）：
  - 仅 `on_schedule` 触发器支持重试；其他触发器（on_demand/service_lifecycle/supd_lifecycle）失败不重试
  - YAML 配置：`triggers.on_schedule[].retry_on_failure: { max_retries, interval_minutes }`
  - 数据流：config.`RetryOnFailureConfig` → `ToRetryConfig` → extension.`RetryConfig`（nil/MaxRetries≤0 不重试；interval≤0 默认 1 分钟）
  - 连接架构：`CronScheduler` 持有 `cronTrigger` + `retryConfigs map[string]*RetryConfig`；`SetCronTrigger` 注入；`AddJob` 接收 `retryCfg` 并在 `jobFunc` 失败后调 `cronTrigger.HandleResult`
  - `HandleResult` 语义：成功重置计数；失败递增计数，未达上限则 `time.AfterFunc(interval)` 延时重试；达上限删除计数
  - `RemoveJob`/`ClearAllJobs` 需同步清理 `retryConfigs`
- **服务重启 vs 扩展重试差异**：服务用 `RestartEngine`（指数退避，`max_retries=0`=无限）；扩展用 `CronTrigger`（固定间隔，`max_retries>0` 才生效）

## 三、环境变量层级（规格 §2.2.4）

4 层合并（后者覆盖前者）：
1. `os.Environ()`（底层）
2. 全局 env 文件（按 `cfg.EnvFiles` 顺序）
3. 服务 env（`services/<svc>/env.yaml`）
4. 扩展 env（`extensions/<name>/env.yaml` 或 `services/<svc>/extensions/<ext>/env.yaml`）

**关键约束**：
- `env.yaml` 格式必须包含 `env:` 包装层：`env: { KEY: { value, enabled?, hint? } }`
- 直接写 `KEY: value` 会被 `config.LoadEnv` 静默忽略
- `enabled: false` 的变量不注入子进程
- 敏感词（PASSWORD/PWD/SECRET/TOKEN/KEY）前端用 password 渲染
- 服务进程启动加载 env.yaml：`internal/core/service_env.go` 的 `BuildServiceProcessEnv`
- script readiness 检查继承服务环境变量（规格 §2.2.3）
- API 启动/重启的服务也加载 env.yaml（`service_operator.go`）

## 四、身份与权限（规格 §2.2.13）

- 服务 `user` 为空 → 继承 supd 启动用户
- 服务级扩展未指定 `run_as` → 继承服务的 `user`
- 全局扩展未指定 `run_as` → 继承 supd 启动用户
- 用户不存在 → 报错拒绝启动（详细错误消息含原因和解决方法）
- `run_as` 只接受 `root` 或具体用户名；`service`/`none` 是无效值；空值=继承
- **语义差异**：服务（严格）非 root 切换其他用户→拒绝；扩展（宽松）→警告+以当前用户运行
- **全局扩展 service_lifecycle 触发**：`TriggerContext.ServiceUser` 始终为空（不继承服务 user），即使被服务生命周期触发
- `internal/identity` 是共享叶子包（core 和 extension 都可 import，避免循环依赖）
- 非 root 环境下目标 UID=当前 UID 时返回 nil credential，避免 setuid EPERM

## 五、关机流程

- 单一 `shutdown_grace_seconds` 预算贯穿：cron stop → 扩展等待 → GracefulShutdown → HTTP Stop
- HTTP Shutdown 派生 5s 子 context
- `cronScheduler.Stop(ctx)` 用 select 竞争 `stopCtx.Done()`（job 完成）与 `ctx.Done()`（超时）
- stop 默认 grace 10s，默认 timeout 60s

## 六、PID 1 与子进程回收

- supd 自带 `PR_SET_CHILD_SUBREAPER` + SIGCHLD 回收 + 10s ticker 兜底
- Docker 中禁用 `--no-pid1`（会导致僵尸进程无法回收）
- SIGCHLD buffer=1 合理（Go 官方文档），`reapZombies` 内部 `Wait4(-1, WNOHANG)` 循环清空
- 不主动调用 PR_SET_CHILD_SUBREAPER（PID 1 内核自动接收孤儿子进程）
- ZombieReaper 支持 `Stop()` 方法，`run.go` 使用 `defer reaper.Stop()`
- supd 维护 PID 文件 `<baseDir>/.supd/pids/<service>.pid`，启动时扫描清理孤儿进程
- supd 子进程在独立进程组，无 PR_SET_PDEATHSIG 时被 SIGKILL 会变孤儿

## 七、文件监控（watcher）

- 白名单：只监控配置文件所在目录（`services/<name>/`、`services/<name>/extensions/<ext>/`、`extensions/<name>/` 等）
- 黑名单（`shouldSkipDir`）：`data/bin/logs/history/cache/tmp/temp/run` 及隐藏目录
- fsnotify 防抖 500ms
- fd 耗尽检测：连续 addWatch 失败超过阈值（5 次）发出 slog.Warn（A-08-001）
- 若新增服务/扩展目录结构变化，需检查 `shouldWatchDir` 是否覆盖新路径模式

## 八、导入流程

- 双次上传无状态模式（预览 + 确认），按目标目录加锁
- 备份-解包-回滚全过程原子
- 修复 R-001 后，导入响应包含 reload 结果（services/global_extensions/scan_errors）

## 九、API 坑点

- 文件 path 在 URL query，body 为 `{"content":"..."}`
- move 目标参数 `destination`
- 建服务/扩展必含 `version` 字段
- 扩展名正则：`^[a-z][a-z0-9-]*$`
- 任务历史：`/api/extensions/runs`
- 长轮询并发上限：全局 50 / 单客户端 5
- cron 表达式必须 5 段（robfig/cron/v3 标准），6 段会解析失败
- 服务配置无 `args` 字段，command 本身是字符串数组
- `restart.policy` 有效值 `always/on-failure/never`（`no` 无效）

## 十、前端嵌入机制

- `//go:embed dist` 在 `web/embed.go`
- **必须 `go build` 才能将最新前端嵌入二进制**
- 修改前端后流程：`pnpm build` → `go build` → 重启 supd → 浏览器硬刷新

## 十一、前端架构要点

- 所有 env 编辑器统一使用 `web/src/lib/env-yaml` 共享工具（parseEnvYaml/serializeEnvYaml/isSensitiveKey/entriesToEnvFileJson）
- `serializeEnvYaml` 跳过空 key（`if (!e.key) continue`）——空行新增需用独立 editEntries 状态，不能用 serialize→parse 往返
- React `useState(entries)` 仅首次渲染初始化，异步数据加载后需 `useEffect` 同步
- React Hooks 规则：所有 hooks 必须在条件 return 之前无条件调用
- 用 `useMemo` 包裹计算确保引用稳定，避免 useEffect 在每次渲染都重置 editEntries
- 下拉列表必须用自定义 div 面板（非原生 `<option>`）确保暗色模式对比度
- 弹窗头部必须含操作按钮（如关闭按钮旁）
- Action 按钮弹窗必须是非阻塞浮动通知，非全屏 modal
- HTTP 端口检测：前端用 `no-cors` fetch resolve
- 文件树单一 `logs` 节点指向 `/tmp/supd-logs`（无重复 `_logs` 节点）

## 十二、Docker 部署要点

- Dockerfile 镜像默认不含 bash/python3/node，扩展脚本依赖需 `apk add`
- Docker 部署用 `docker stop -t 30` 与 `shutdown_grace_seconds` 对齐
- `--no-pid1` 仅适用于 systemd 场景
- Docker 镜像集成 Dropbear（含 SFTP），dropbear-ssh 是 supd 管理的普通服务（autostart: false）
- dropbear host key 由 `-R` 参数运行时动态生成（每容器独立），不要在 Dockerfile 预生成
- dropbear 认证模式由 env.yaml 的 `SSH_PUBLIC_KEY` 控制：留空=免认证，填入=公钥认证
- tjs 运行时（txiki.js）集成到 Docker 镜像，双架构原生编译（amd64 + arm64）
- Docker 工具集约 12.6MB（26 个 Alpine 包），20MB 预算
- `/etc/supd/runtimes` 必须在 PATH 中

## 十三、CI/CD 要点

- CI 流水线：lint → test → build → image → release，任一步骤失败中止
- GitHub Actions 必须用 `runs.using: node24`（避免 Node.js 20 弃用警告）
- pnpm 版本固定 `pnpm@10`（Node 20 不支持 pnpm 11 的 node:sqlite）
- pnpm overrides 在 `package.json` 的 `pnpm.overrides`（非 workspace）
- Dockerfile pnpm 安装用 `npx -y pnpm@10`（规避 corepack 兼容性）
- 版本号通过 git tag + CI ldflags 注入，源码默认值 `dev`
- 唯一需手动改版本号的文件：`README.md`（2 处），详见 `version-upgrade-guide.md`

## 十四、测试数据

- `test_workdir/services/env-test/` — 服务 env 测试服务（含 hint 数据）
- `test_workdir/extensions/env-test-ext/` — 扩展 env 测试扩展
- `test_workdir/services/transmission/` — binary-updater 首次运行前 `bin/` 不存在（run.sh 已 mkdir -p）
- `supd init` 生成 5 示例服务（web-demo/tcp-echo/signals-demo/script-ready-demo/dropbear-ssh）+ 5 示例扩展
