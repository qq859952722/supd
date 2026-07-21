# supd 项目全面审计方案

> **文档性质**：可执行审计任务清单，每项任务均指定审计角色、具体方法、验收标准。
> **严格要求**：执行审计时，每一项均须逐文件阅读实际代码，不得跳过、不得推断、不得抽样代替全量。
> **审计范围**：`internal/`（所有 Go 包）+ `web/src/`（所有前端代码）+ `cmd/`
> **规格基准**：`docs/需求规格说明_v1.5.md`

---

## 审计总览

| 类别 | 审计域 | 任务数 |
|---|---|---|
| A | 后端逻辑正确性 | 8项 |
| B | 并发安全与竞态 | 5项 |
| C | 错误处理完整性 | 5项 |
| D | API 层审计 | 6项 |
| E | 前端 UI/UX 质量 | 8项 |
| F | 前后端一致性 | 6项 |
| G | 性能与资源管理 | 6项 |
| H | 安全性审计 | 5项 |
| I | 死代码与冗余 | 4项 |
| J | 过度设计识别 | 3项 |
| K | 配置与校验完整性 | 4项 |
| L | 测试覆盖质量 | 4项 |
| **合计** | | **64项** |

---
## 第 A 类：后端逻辑正确性

> **角色设定**：资深 Go 后端工程师，专注业务逻辑精确性，对照需求规格说明书逐条核验，不接受"大体正确"的结论。
> **工作方式**：阅读每个文件的每个函数，对照规格说明书对应章节，记录每处偏差。

---

### A-01：状态机转移规则完整性校验

**文件**：`internal/core/state_machine.go`、`internal/core/state.go`、`internal/core/state_test.go`

**审计方法**：
1. 列出规格说明书 §2.1.1 定义的全部合法转移路径（共10条），制成表格
2. 对照代码中所有转移函数，逐条确认每条转移是否实现
3. 反向检查：代码中是否存在规格未定义的转移路径（规格外转移 = bug）
4. 检查非法转移调用时的返回值：是明确 error 还是静默忽略
5. 检查 `failed` 状态的三条进入路径（启动失败/运行中崩溃/重启次数耗尽）是否均有对应测试用例

**验收标准**：
- 10条合法转移全部有代码对应，并逐一标注代码位置
- 不存在规格外转移（若有则列为 🔴 严重问题）
- 所有非法转移返回明确错误
- `failed` 状态的三条进入路径均有独立测试用例

---

### A-02：重启策略实现正确性

**文件**：`internal/core/restart.go`、`internal/core/restart_test.go`、`internal/core/bootstrap.go`

**审计方法**：
1. 对照规格 §2.1.3 的三种策略（always/on-failure/never），逐一确认 restart.go 中的分支实现
2. 检查指数退避算法：初始等待、倍增系数、最大等待上限是否与规格数值一致
3. 检查 `max_restarts` 计数逻辑：重启计数是否在进程正常运行一段时间后重置（规格如有定义）
4. 检查 `max_restarts` 耗尽后的状态迁移路径：是否准确进入 `failed`
5. 检查 `never` 策略在进程退出时不触发任何重启，仅更新状态

**验收标准**：
- 三种策略行为与规格完全一致，逐条验证并记录
- 退避上限数值与规格完全匹配
- 重启耗尽→ `failed` 路径有端到端测试用例

---

### A-03：扩展执行流程完整性（14个环境变量、stdout协议、dry_run）

**文件**：`internal/extension/executor.go`、`internal/extension/run_context.go`、`internal/extension/protocol.go`

**审计方法**：
1. 对照规格 §2.2 列出扩展执行的完整步骤，在 `executor.go` 中逐步追踪代码实现
2. 逐个核对 14 个 `SUPD_*` 环境变量（变量名完整列表来自规格），不允许遗漏或拼写错误
3. 检查 `::progress::` 和 `::result::` 两种 stdout 协议格式的解析完整性，及边界情况处理（格式错误时如何处理）
4. 检查 `dry_run=true` 时：进程不被 fork、环境变量不被注入、日志记录是否仍生成
5. 检查扩展进程退出后的清理顺序：日志刷新 → stdout goroutine 退出 → 任务状态更新 → 资源释放

**验收标准**：
- 14个环境变量制成核对表，逐一确认存在且值正确
- stdout 协议的格式错误有容错处理
- dry_run 不触发进程启动，有测试覆盖

---

### A-04：并发策略四种模式行为正确性

**文件**：`internal/extension/concurrency.go`、`internal/extension/debounce.go`、`internal/extension/concurrency_test.go`

**审计方法**：
1. **replace**：确认旧任务收到 Kill 信号后新任务才启动；旧任务 Kill 失败时的处理
2. **serialize**：确认是队列等待（非丢弃），队列上限（若规格有定义则验证），等待中任务的状态
3. **parallel**：确认确实无并发数限制，并发任务数超大时的资源风险评估
4. **debounce:Ns**：N 值解析（负数/非数字/零的处理），每次新触发是否重置计时器
5. 所有策略：任务被中止/取消时状态是否正确标记为 `canceled`（而非 `failed`）

**验收标准**：
- 四种策略各有覆盖"正常触发"+"边界触发"+"取消触发"的测试用例
- debounce N 值非法时有明确错误或降级行为

---

### A-05：触发器调度优先级与串行顺序

**文件**：`internal/extension/dispatcher.go`、`internal/extension/trigger_lifecycle.go`、`internal/extension/trigger_cron.go`、`internal/extension/trigger_on_demand.go`、`internal/extension/trigger_supd_lifecycle.go`

**审计方法**：
1. 对照规格 §2.2.3：以服务为粒度、先全局后服务级、字母序串行，在 dispatcher.go 中找到对应实现代码
2. `service_lifecycle` 触发器：监听的状态事件（on_start/on_stop/on_ready等）是否与规格完全一致
3. `supd_lifecycle` 触发器：pre_start/post_ready/pre_shutdown 三个钩子——在 bootstrap.go 和 shutdown.go 中逐一找到调用点
4. cron 触发器：表达式解析库（是5字段还是6字段cron？是否支持秒？）时区使用服务器时区还是UTC？
5. 多触发器同时触发同一扩展时，并发策略的应用是否隔离（不同触发器实例不共享并发状态）

**验收标准**：
- 字母序串行有测试用例（含多服务多扩展场景）
- 三个 supd_lifecycle 钩子均能在代码中追踪到调用链
- cron 时区行为有明确说明（注释或文档）

---

### A-06：配置热重载六类分类逻辑

**文件**：`internal/watch/reload.go`、`internal/watch/reload_classifier.go`、`internal/watch/reload_test.go`

**审计方法**：
1. 对照规格 §2.4 列出六种变更类型，在 `reload_classifier.go` 中找到每种类型的分类条件
2. 检查"无变化"判断：是比较文件 mtime 还是内容哈希？mtime 在某些情况下会误判
3. 检查多文件同时变化（500ms 防抖窗口内）的合并处理逻辑
4. 检查重载期间服务状态的并发处理：正在启动的服务收到重载信号时的行为
5. 检查热重载导致的配置错误：是否回滚到旧配置？是否有用户可见的错误提示？

**验收标准**：
- 六类变更每类有独立测试用例，覆盖分类条件的边界情况
- 防抖 500ms 有测试验证（参数与 AGENTS.md 数值锁定表一致）
- 配置错误时的回滚行为有明确实现或文档

---

### A-07：停止流程7步与超时保障

**文件**：`internal/core/stop.go`、`internal/core/shutdown.go`、`internal/core/stop_test.go`、`internal/core/shutdown_test.go`

**审计方法**：
1. 对照规格 §2.1.4 的停止步骤，在 `stop.go` 中逐步追踪代码（pre_stop→SIGTERM→grace→SIGKILL→zombie回收→状态→日志）
2. 检查 `timeout_seconds` 总时限：grace 期 + SIGKILL 等待的总和是否受 timeout_seconds 约束（不可超出）
3. 检查默认值 grace=10s、timeout=60s 的定义位置（`defaults.go` 或 constants），确认与 AGENTS.md 数值表一致
4. 检查 pre_stop 占位（session-notes.md 标注的 TODO）：是否有清晰的 TODO 注释，占位不影响实际停止流程
5. 检查优雅退出期间的全局 shutdown：拓扑反序停止服务的实现是否使用依赖图的反向拓扑序

**验收标准**：
- 停止7步在代码中可逐步追踪（附行号）
- 超时总时限受约束，不会无限等待
- 拓扑反序停止有测试用例（含有依赖链的场景）

---

### A-08：文件发现5种规则与 fsnotify 防抖

**文件**：`internal/watch/discovery.go`、`internal/watch/discovery_test.go`、`internal/watch/watcher.go`、`internal/watch/debounce.go`

**审计方法**：
1. 对照规格 §2.4.1 的五种发现规则（glob/recursive/explicit/directory/ignore），在 discovery.go 中逐一找到实现
2. 检查 ignore 规则的优先级：ignore 是否能覆盖其他规则匹配到的文件
3. 检查符号链接处理：是否跟随 symlink？是否有循环 symlink 保护（避免无限遍历）
4. 检查 fsnotify 防抖实现（`debounce.go`）：防抖时间是否硬编码为 500ms（与数值锁定表一致）
5. 检查 fsnotify 降级（`fallback.go`）：inotify 文件描述符耗尽时的降级处理逻辑

**验收标准**：
- 5种规则各有测试用例
- fsnotify 防抖 500ms 与规格一致（代码中的数值可追踪）
- 降级场景有测试或日志记录

---

## 第 B 类：并发安全与竞态

> **角色设定**：并发系统专家，精通 Go 内存模型，使用 `go test -race` 和静态分析工具，追查每一处潜在的数据竞争、死锁、goroutine 泄漏。
> **工作方式**：对每个包运行 race detector，对所有共享状态逐一追踪访问路径。

---

### B-01：ProcessManager 并发访问审计

**文件**：`internal/core/process_manager.go`、`internal/core/process.go`

**审计方法**：
1. 列出 `ProcessManager` 中所有共享字段（procs map 等），制成"字段→锁保护→读写点"映射表
2. 对每个字段追踪所有读写点，确认均在正确粒度的锁保护下
3. 检查 RWMutex 使用：写操作用写锁，读操作用读锁，不混用（写锁用于读是性能浪费，读锁用于写是竞态）
4. 检查持有 mutex 期间是否调用了可能阻塞的操作（如 os.Kill、网络调用），这会导致其他 goroutine 饥饿
5. 运行 `go test -race ./internal/core/...` 并记录输出

**验收标准**：
- 字段映射表完整，每个字段有明确的锁保护
- `go test -race` 零竞态警告
- 持有锁期间无阻塞 I/O 调用

---

### B-02：EventRingBuffer 并发读写安全

**文件**：`internal/api/longpoll.go`

**审计方法**：
1. 梳理环形缓冲区的数据结构：存储结构、头尾指针（或写入计数）的并发访问
2. 检查写入（事件追加）和读取（客户端长轮询等待）是否有正确的 mutex 保护
3. 检查缓冲区满时的覆盖写逻辑：是否存在 TOCTOU（时间窗口）问题
4. 检查等待机制：使用 `sync.Cond` 还是 channel？Broadcast 还是 Signal？语义是否正确
5. 检查多客户端并发长轮询：各客户端的读取游标（sequence/offset）是否独立，不会互相干扰
6. 运行 `go test -race ./internal/api/...` 并记录输出

**验收标准**：
- `go test -race` 零竞态
- 200条缓冲区满时的覆盖写有并发测试用例

---

### B-03：TaskHistory 并发操作安全

**文件**：`internal/extension/task_history.go`、`internal/extension/task_manager.go`

**审计方法**：
1. 检查任务历史 slice/map 的写入（新任务记录）和读取（历史查询 API）的并发安全
2. 检查 `UpdateProgress()` 方法：并发调用时（执行 goroutine 更新进度，API 查询同时读取）是否安全
3. 检查惰性清理触发时机：并发写入两个任务时是否可能触发两次清理？清理是否幂等？
4. 检查任务状态从 `running` 转换为终态的原子性：是否存在"已进入成功路径但状态还未更新"的短暂窗口
5. 运行 `go test -race ./internal/extension/...` 并记录输出

**验收标准**：
- `go test -race` 零竞态
- UpdateProgress 并发调用有专项测试

---

### B-04：Goroutine 生命周期与泄漏检测

**文件**：所有含 `go func()` 的文件（重点：`executor.go`、`bootstrap.go`、`longpoll.go`、`cron_scheduler.go`）

**审计方法**：
1. 列出所有 `go func()` 调用点（使用 `grep -rn 'go func' internal/` 枚举）
2. 对每个调用点，明确其退出条件（context.Done / channel close / 显式 return）
3. 检查扩展执行 goroutine：进程意外死亡时，读取 stdout 的 goroutine 是否能正常退出
4. 检查长轮询 goroutine：客户端 HTTP 连接断开（request context cancel）时，等待 goroutine 是否立即清理
5. 检查 cron_scheduler：调度 goroutine 在扩展配置热重载（调度器重建）时是否正确停止旧实例
6. 在集成测试结束后检查 goroutine 数量（可用 `runtime.NumGoroutine()`）

**验收标准**：
- 每个 `go func()` 都有明确退出路径（列表记录）
- 测试结束后 goroutine 数量回到基准水平（不泄漏）

---

### B-05：ConcurrencyManager 状态一致性与 Timer.Reset 正确性

**文件**：`internal/extension/concurrency.go`、`internal/extension/debounce.go`、`internal/extension/concurrency_test.go`

**审计方法**：
1. **replace 竞态窗口**：旧任务 Kill 信号发出后、进程实际死亡前，新任务是否已启动（两个进程并存的窗口）
2. **serialize 队列安全**：等待队列用 channel 还是 slice+mutex？channel 在关闭时的安全性
3. **debounce Timer.Reset 的正确使用**：Go 标准库 `time.Timer.Reset` 有已知的并发问题，检查是否按官方推荐模式使用（先 Stop + drain，再 Reset）
4. 检查并发策略状态在扩展配置热重载时的清理：旧扩展的 ConcurrencyManager 是否正确停止
5. 检查并发策略计数/状态在服务停止（优雅退出）期间的行为

**验收标准**：
- `go test -race` 零竞态
- Timer.Reset 使用符合 Go 官方推荐模式（有代码注释或测试验证）
- 热重载时旧 ConcurrencyManager 完整清理

---

## 第 C 类：错误处理完整性

> **角色设定**：防御性编程专家，假设每个操作都可能失败，检查错误是否被正确传播、记录、转换。
> **工作方式**：对每个函数的 error 返回值追踪处理路径，`_ = err` 视为需要审查的信号。

---

### C-01：Go 错误丢弃检测

**文件**：所有 `.go` 文件

**审计方法**：
1. 运行 `grep -rn '_ =' internal/ cmd/` 列出所有显式忽略的错误，记录文件和行号
2. 对每处 `_ = someFunc()` 评估安全性，分类：
   - 安全忽略（如 `io.EOF` 在读完时的正常情况）
   - 不应忽略（如写入文件的 Close 错误）
3. 检查 `defer f.Close()` 模式：写入路径的 Close 错误不应静默丢弃（应 log 或 return）
4. 检查 goroutine 内部的错误：只 log 不上报的错误，评估是否导致功能静默失败
5. 运行 `go install github.com/kisielk/errcheck@latest && errcheck ./...` 扫描

**验收标准**：
- 每处 `_ = err` 有注释说明忽略原因
- 写入操作的 Close 错误不丢弃
- errcheck 扫描结果有详细记录（允许豁免但需标注）

---

### C-02：HTTP API 错误响应一致性

**文件**：`internal/api/errors.go`、所有 `*_handler.go`、`internal/errors/`

**审计方法**：
1. 梳理 `errors.go` 中定义的 API 错误 helper 函数，列出所有 helper
2. 检查所有 handler 文件：是否统一使用这些 helper，还是有直接 `http.Error()` 或 `json.Encode()` 的散落调用
3. 检查22个错误码（§5.4）→ HTTP 状态码的映射表是否完整（无遗漏错误码）
4. 构造触发500错误的场景，检查响应体：是否包含 Go panic 堆栈信息、内部文件路径
5. 检查 400/404/405/409/429 等常见状态码的使用是否语义正确

**验收标准**：
- 所有 handler 使用统一 error helper（无散落的 http.Error）
- 22个错误码全部有对应的HTTP状态码映射
- 500响应体不含内部信息

---

### C-03：配置解析错误质量

**文件**：`internal/config/` 所有文件

**审计方法**：
1. 检查 YAML 解析错误的上下文信息：错误消息是否包含文件名和问题行号（而非只有"yaml: line X"）
2. 检查 `yaml_safe.go` 超限错误消息：是否告知用户具体超出了哪个限制和当前值
3. 检查类型不匹配错误（如把字符串值给数字字段）：能否从错误消息定位到具体字段路径
4. 检查校验失败（service_validate、extension_validate）的错误消息：是否明确指向哪个字段违反了哪条规则
5. 检查热重载配置解析失败时的用户通知：是否写入了可见的日志或事件

**验收标准**：
- 解析错误包含文件名和具体位置
- 校验错误精确指向问题字段
- 热重载失败有用户可见的错误通知

---

### C-04：进程启动与监控错误处理

**文件**：`internal/core/process.go`、`internal/core/bootstrap.go`

**审计方法**：
1. 检查 `exec.Cmd.Start()` 失败路径：状态机是否迁移到 `failed`，相关资源（log fd等）是否清理
2. 检查进程启动成功但立即退出的场景（命令路径不存在或权限不足）：是否会误认为进程正常运行
3. 检查 Readiness 检查超时（各类型：fd_notify/tcp/http/script）：超时后进程是否被 Kill，状态是否更新
4. 检查 stdout/stderr pipe 建立失败时的错误处理（pipe 系统调用失败是罕见但合法的情况）
5. 检查进程管理员重启时的资源泄漏：log file、pipe fd 在每次重启后是否正确关闭

**验收标准**：
- 启动失败的所有路径（Start失败/立即退出/Readiness超时）都有状态机迁移
- Readiness 超时不泄漏进程资源（使用 `lsof` 或 `/proc/fd` 验证）

---

### C-05：扩展执行错误处理与任务状态完整性

**文件**：`internal/extension/executor.go`、`internal/extension/task.go`、`internal/extension/failure_handler.go`

**审计方法**：
1. 列出7种任务终态（success/failed/timeout/canceled/killed）的代码进入路径，制成表格
2. 检查扩展进程非零退出码：是否记录退出码，任务状态是否为 `failed`
3. 检查超时（默认600s/最大1800s）：进程是否被 SIGKILL，僵尸进程是否被 Wait 回收
4. 检查 replace 策略 kill 旧任务：旧任务状态是 `killed`（被系统 kill）还是 `canceled`（被策略取消）——规格是否有定义？
5. 检查 failure_handler（`failure_handler.go`）的 on_failure 配置：现有实现支持哪些策略，是否与规格一致

**验收标准**：
- 7种任务终态均有可追踪的代码路径（制成表格）
- 超时后进程不成为僵尸（验证 `ps aux | grep Z`）
- killed vs canceled 的语义区分有明确实现或注释

---

## 第 D 类：API 层审计

> **角色设定**：API 设计评审师，对照规格 §REQ-I-006 的端点清单，检查每个端点的行为正确性、入参校验、响应格式、权限控制。
> **工作方式**：对 `server.go` 中每个注册路由，逐一检查 handler 实现。

---

### D-01：路由注册完整性与多余端点审计

**文件**：`internal/api/server.go`

**审计方法**：
1. 列出 server.go 中全部注册路由（共72个），制成带方法+路径+handler名的完整清单
2. 对照规格 §REQ-I-006 的65个端点，逐一在清单中标注匹配状态
3. 对多出的7个端点（参见 deviations.md DEV-008），对每个端点评估：
   - 当前是否有前端 API 调用？（查 api-client.ts）
   - 是否与已有端点功能重叠？
   - 是否存在安全隐患（无鉴权/副作用操作/GET引起状态变化）？
4. 检查路由方法语义：GET 不得有写副作用，POST/PUT/DELETE 区分语义
5. 检查路由路径命名一致性（资源名用复数、层级结构）

**验收标准**：
- 完整72路由清单（含匹配状态标注）
- 7个额外端点每个都有明确的"保留/删除"建议及理由
- 无 GET 请求触发状态修改的路由

---

### D-02：认证与授权中间件完整性

**文件**：`internal/api/middleware.go`、`internal/api/auth_handler.go`、`internal/api/server.go`

**审计方法**：
1. 检查三种认证模式（none/local_skip/always_token）的中间件实现，逐模式追踪请求路径
2. `local_skip` 模式：如何判断"本地请求"？仅检查 IP 是否足够？是否处理 X-Forwarded-For 绕过风险？
3. 检查 server.go 的路由注册：每个受保护端点是否都在认证中间件的作用范围内（无认证绕过路径）
4. 检查 token 生成（`cli/token.go`）：使用 `crypto/rand` 还是 `math/rand`？生成长度/熵是否足够？
5. 检查 token 比较：是否使用 `subtle.ConstantTimeCompare`（常数时间比较，防时序攻击）

**验收标准**：
- 所有非公开端点均有认证中间件保护（无漏网端点）
- token 使用 `crypto/rand` 生成
- 使用常数时间比较
- local_skip 的本地判断有 IPv6 回环测试

---

### D-03：长轮询并发限制实现审计

**文件**：`internal/api/longpoll.go`、`internal/api/longpoll_test.go`

**审计方法**：
1. 找到全局50/单客户端5的计数器实现代码，检查数据类型（atomic int32 还是 mutex+int？）
2. 检查计数器原子性：并发请求到达时是否存在 TOCTOU（检查→加1之间有窗口）
3. 检查连接断开时计数器减1的路径：正常断开/超时断开/panic 恢复后是否全部减1
4. 检查超出上限的响应状态码：应为429 Too Many Requests（检查是否与规格一致）
5. 检查客户端 ID 的识别方式：通过 IP？通过 Header？识别方式是否与限制语义匹配

**验收标准**：
- 计数器使用原子操作（`sync/atomic`）或 mutex 保护，无 TOCTOU
- 所有断开路径均减计数（有专项测试）
- 超出上限返回正确状态码（与规格一致）

---

### D-04：文件操作 API 路径安全审计

**文件**：`internal/api/file_handler.go`、`internal/api/path_whitelist.go`、`internal/api/path_whitelist_test.go`

**审计方法**：
1. 列出所有接受文件路径参数的端点，检查是否全部经过 `path_whitelist.go` 校验
2. 检查 `path_whitelist.go` 实现：是否处理以下攻击向量：
   - `../` 路径穿越
   - URL 编码 `%2F`、`%2e`
   - 双点双斜杠 `....//`
   - 绝对路径 `/etc/passwd`
   - null byte `%00`
3. 检查 symlink 处理：`os.Lstat` vs `os.Stat`，指向白名单外的 symlink 是否被拒绝
4. 检查文件大小限制（100MB）的实施位置：应在 stream 读取过程中限制，而非全部读入内存后检查
5. 验证 `path_whitelist_test.go` 是否覆盖了上述所有攻击向量

**验收标准**：
- 上述全部攻击向量有对应测试用例
- 大小限制在流式读取层实施（不会 OOM）
- symlink 指向白名单外被拒绝

---

### D-05：adapters.go 数据转换正确性（35KB 最大单文件）

**文件**：`internal/api/adapters.go`

**审计方法**：
1. 列出所有 adapter 函数（将内部结构体转为 API 响应结构体），制成函数清单
2. 对每个 adapter 函数：
   - 列出内部结构体字段数
   - 列出响应结构体字段数
   - 找出有意省略的字段（需有理由）和无意遗漏的字段
3. 检查数值单位转换：time.Duration → 秒、字节数 → 人类可读，单位标注是否正确
4. 检查 nil 指针：所有可选内部字段在转换时是否有 nil guard（`if x != nil { ... }`）
5. 检查枚举值转换：内部常量到 API 字符串的 switch 语句是否有 default 分支（未知枚举值如何处理？）

**验收标准**：
- 每个 adapter 函数有字段对照表（内部字段 ↔ 响应字段，说明省略原因）
- 所有枚举转换的 switch 有 default 分支，不产生空字符串

---

### D-06：上传/导出/导入 API 完整性

**文件**：`internal/api/upload.go`、`internal/api/export_handler.go`、`internal/archive/`

**审计方法**：
1. 检查 100MB 上传限制：是否在 `http.MaxBytesReader` 层设置（在 multipart 解析之前），而非事后检查
2. 检查导出的 tar.gz 内容完整性：打包了哪些文件？路径结构是否正确（相对路径 vs 绝对路径）
3. 检查导入解压的安全性（zip slip 漏洞）：解压每个文件前是否验证目标路径在工作目录内
4. 检查导入的原子性：中途失败（磁盘满/权限不足）时是否留下部分解压的垃圾文件
5. 检查并发导入的处理：两个并发导入请求是否会相互干扰

**验收标准**：
- 100MB 限制在 MaxBytesReader 层（不是读完后再检查）
- zip slip 漏洞测试通过（包含 `../` 路径的 tar.gz 被拒绝）
- 导入失败有清理机制

---

## 第 E 类：前端 UI/UX 质量

> **角色设定**：资深前端工程师兼 UX 设计师，评估每个页面、每个交互路径、每个边界状态的用户体验。
> **工作方式**：逐文件阅读页面组件代码，评估 loading/error/empty 三态处理、交互设计、视觉反馈。

---

### E-01：页面加载态与骨架屏完整性

**文件**：所有 `pages/*.tsx`（9个页面）、`components/ui/Skeleton.tsx`

**审计方法**：
1. 对每个页面（Dashboard/Services/Extensions/CronTasks/Events/Files/Settings + 2个详情页），检查：
   - isLoading 状态：是否有骨架屏或 loading 指示器（不允许白屏）
   - isError 状态：是否有错误提示（不允许静默空白）
   - 空数据状态（data=[].length===0）：是否有 empty state（不允许空表格无任何提示）
2. 检查 ServiceDetail（67KB）、ExtensionDetail（62KB）的分区加载：子块（配置/日志/历史）是否独立加载
3. 检查轮询更新时的稳定性：数据刷新是否触发重新显示骨架屏（不应该）
4. 检查骨架屏结构：形状是否与真实内容近似，避免加载完成时的布局抖动

**验收标准**：
- 9个页面全部有 loading/error/empty 三态处理（制成检查矩阵）
- 轮询更新不触发骨架屏重现
- 大详情页的子块独立 loading

---

### E-02：错误反馈机制完整性

**文件**：所有 `pages/*.tsx`、`components/ui/Toast.tsx`、`components/ui/TaskToast.tsx`

**审计方法**：
1. 统计所有 `useMutation` 调用，对每个调用检查是否有 `onError` 回调（不允许静默失败）
2. 检查错误消息质量：是直接暴露 API 错误码还是转换为用户可理解的中文提示
3. 检查是否使用了 `lib/i18n.ts` 的翻译系统，还是直接硬编码错误字符串
4. 检查网络连接断开（服务器重启/不可达）时的处理：长轮询断开是否有用户友好提示
5. 检查 `ErrorBoundary.tsx` 的覆盖范围：是否包裹了足够多的组件层级

**验收标准**：
- 所有 useMutation 都有 onError 回调（制成清单）
- 错误提示为中文，不暴露原始 HTTP 状态码
- 网络断开有专门提示

---

### E-03：表单交互完整性

**文件**：`lib/form-engine.tsx`、`components/service/ServiceForm.tsx`、相关扩展表单、`Settings.tsx`

**审计方法**：
1. 检查每个表单字段的 zod 校验规则是否覆盖规格定义的约束（服务名长度/字符集、超时范围等）
2. 检查校验错误展示时机：是实时校验（onChange）还是提交后（onSubmit）？即时反馈更佳
3. 检查必填字段的视觉标记是否一致
4. 检查提交按钮的 disabled/loading 状态：防止重复提交
5. 检查"取消"/"重置"按钮行为：是否正确恢复到编辑前的状态
6. 检查 YAML 可视化编辑器与表单之间的数据同步（如存在双向编辑）

**验收标准**：
- 所有表单字段有对应 zod 规则（制成核对表）
- 必填字段有统一的视觉标记
- 提交按钮有防重复保护

---

### E-04：Monaco YAML 编辑器体验

**文件**：`components/editor/MonacoEditor.tsx`、`components/editor/EditorTabs.tsx`、`components/editor/DiffEditor.tsx`

**审计方法**：
1. 检查编辑器主题：深色/浅色主题切换时 Monaco 主题是否同步切换
2. 检查8标签上限：超出时的用户提示（不应静默拒绝，应有明确提示）
3. 检查"未保存"状态标记：标签是否有修改标记（·/*等）
4. 检查页面路由切换后编辑器内容是否从 store 恢复（不丢失未保存内容）
5. 检查 YAML 语法错误高亮：是否配置了 Monaco 的 YAML 语言支持和 schema 验证
6. 检查 DiffEditor 的入口：哪个操作会触发 diff 视图？

**验收标准**：
- 主题联动正常
- 8标签上限有友好提示
- 路由切换后内容保留（store 恢复）

---

### E-05：响应式布局与长文本处理

**文件**：`styles/`、所有 `pages/*.tsx`

**审计方法**：
1. 在代码中检查各页面的布局容器宽度设置：是否有 min-width 保护
2. 检查长文本（服务名/路径/描述）的 CSS 截断处理：是否有 `text-overflow: ellipsis`
3. 检查有 Tooltip 配合截断的地方：完整内容是否可通过 hover 查看
4. 检查大数据量下的表格：ServiceDetail 日志、ExtensionDetail 历史——是否有虚拟滚动或分页
5. 检查底部浮窗（BottomDrawer）在窗口较小时是否遮挡关键操作区域

**验收标准**：
- 所有长文本有截断+Tooltip
- 大数据量表格有分页或虚拟滚动
- 1280px 宽度下无横向滚动

---

### E-06：主题系统一致性

**文件**：`lib/theme.tsx`、`styles/`、所有 UI 组件

**审计方法**：
1. 检查所有 UI 组件（11个 ui/ 组件 + 各业务组件）：是否使用 CSS 变量，是否有硬编码颜色值
2. 检查 `Select.tsx`（已重写为自定义实现）的深色模式颜色是否从 CSS 变量读取
3. 检查服务状态颜色（pending/starting/up/ready/stopping/down/failed 的7种颜色）在两种主题下的对比度
4. 检查 Monaco Editor 主题设置代码：是否有主题联动逻辑
5. 检查新增的 `IconPicker` 和 `IconRenderer` 组件是否遵循主题系统

**验收标准**：
- 无硬编码颜色（列出所有发现的硬编码颜色位置）
- 服务状态颜色在深色主题下对比度 ≥ 4.5:1（WCAG AA）

---

### E-07：全局搜索（Ctrl+K）功能审计

**文件**：`components/GlobalSearch.tsx`、`App.tsx`

**审计方法**：
1. 检查快捷键注册位置：是否在根组件注册，确保所有页面均可触发
2. 检查防止快捷键与浏览器/系统快捷键冲突的处理（如 Monaco 编辑器内 Ctrl+K 的优先级）
3. 检查搜索范围：当前覆盖服务名、扩展名，是否还应覆盖文件名/配置内容（按规格确认）
4. 检查键盘导航完整性：方向键选择结果、Enter 跳转、Escape 关闭、Tab 行为
5. 检查结果高亮：匹配字符是否有高亮标记（或无？记录现状）
6. 检查搜索结果的导航跳转：是否能正确跳转到对应的服务/扩展详情页

**验收标准**：
- 快捷键在所有页面均可触发（Monaco内部场景除外有明确处理）
- 键盘导航可完成全部操作（无需鼠标）

---

### E-08：底部任务浮窗完整性

**文件**：`components/BottomDrawer.tsx`、`components/ui/TaskToast.tsx`

**审计方法**：
1. 检查运行中任务的数据来源：通过哪个 API 获取？轮询频率？
2. 检查多任务并行展示：是否有最大显示数量限制？溢出时如何处理（"还有N个"？）
3. 检查进度显示：0%→100% 的进度条更新频率是否与后端推送 `::progress::` 的频率匹配
4. 检查任务完成后消失时机：立即消失还是延迟N秒？是否有动画过渡
5. 检查任务失败时的展示：错误信息是否可见？是否有"查看详情"入口

**验收标准**：
- 多任务场景有展示策略（不会无限堆叠）
- 任务完成后有动画过渡消失
- 失败任务有错误信息展示

---

### E-09：前端全功能覆盖与操作人性化评估

**文件**：`web/src/pages/`、`web/src/components/`、`web/src/store/`



**审计方法**：

1. **功能覆盖对比**：列出后端 API 提供的所有功能（如 `dry_run`、扩展的各项触发条件、重启策略参数调整等），核对前端界面是否提供了对应的操作入口。

2. **可配置性验证**：检查表单页面是否支持所有规格说明书中的可选字段（如 `timeout_override`, `env_overrides` 等）。

3. **操作连贯性**：评估用户在执行"创建->配置->启动->查看日志"等连贯动作时，是否需要频繁跳转，是否有上下文保留机制。

4. **防错与提示**：检查涉及高危操作（如强杀进程、清空历史）时，是否有二次确认；在长时间等待操作时是否有进度反馈机制。



**验收标准**：

- 后端开放的可变参数和功能在前端均有体现或说明不支持的原因。

- 复杂表单配置项有相应的 Tooltip 或 Helper Text 引导用户。

- 危险操作统一拥有确认弹窗，长操作有按钮 Loading 状态。



---

## 第 F 类：前后端一致性

> **角色设定**：全栈集成工程师，持有前端 API 调用代码和后端 handler 代码，逐端点比对数据结构。
> **工作方式**：对每个前端 API 调用，找到对应后端 handler，逐字段比对。

---

### F-01：API 客户端请求结构一致性

**文件**：`web/src/lib/api-client.ts`、各 pages 中的 API 调用、对应后端 handler

**审计方法**：
1. 列出 `api-client.ts` 中所有的请求体类型定义（POST/PUT 请求的 body 类型）
2. 对每个请求体，找到后端对应的请求结构体（如 `json.Unmarshal` 的目标）
3. 逐字段比对（注意 camelCase vs snake_case 转换）：字段名、类型、必填/可选
4. 找出后端接受但前端从未发送的字段（功能盲区，可能是遗漏的表单字段）
5. 找出前端发送但后端不使用的字段（冗余代码）

**验收标准**：
- 每个 POST/PUT 端点有"前端字段 ↔ 后端字段"对照表
- 无遗漏的关键请求字段

---

### F-02：API 响应结构一致性

**文件**：`web/src/lib/api-client.ts` 响应类型、`internal/api/adapters.go`

**审计方法**：
1. 列出前端 `api-client.ts` 中定义的所有响应接口（TypeScript interface/type）
2. 对每个响应接口，找到后端对应的 adapter 函数返回值
3. 逐字段比对：是否有前端期望但后端不返回的字段（前端会访问 undefined）
4. 检查可选字段（TypeScript `?:`）与后端 JSON `omitempty` 的对应关系
5. 检查枚举字段：前端的 union type 值集合是否覆盖后端所有可能的枚举值

**验收标准**：
- 无前端访问 undefined 字段的潜在 runtime error（制成清单）
- 枚举字段值集合完全一致

---

### F-03：服务状态展示一致性

**文件**：前端状态相关组件、`internal/core/state.go`

**审计方法**：
1. 在后端 `state.go` 中列出所有状态常量（7种）
2. 在前端代码中搜索每种状态字符串，检查每种状态的视觉映射（颜色/图标/文本）
3. 检查是否所有7种状态都有视觉表现（无遗漏状态导致显示异常）
4. 检查 `failed` 状态的操作选项：是否提供"清除failed状态"按钮（对应 `clear-failed` API）
5. 检查状态文本标签：是否与后端 API 返回值保持一致（避免"ready"显示为"Running"的错误）

**验收标准**：
- 7种状态的视觉映射表（状态 → 颜色/图标/文本）
- `failed` 状态有"清除"操作入口

---

### F-04：扩展任务状态展示一致性

**文件**：前端任务相关组件、`internal/extension/task.go`

**审计方法**：
1. 在后端 `task.go` 中列出7种任务状态（pending/running/success/failed/timeout/canceled/killed）
2. 在前端代码中搜索每种状态，检查视觉映射
3. 检查 `killed`（replace策略终止）与 `canceled`（主动取消）的视觉区分——这两种状态语义不同
4. 检查任务进度展示：`::progress::` 输出的进度值如何在 UI 中呈现
5. 检查任务历史列表的时间格式：是本地时区格式化还是 UTC？

**验收标准**：
- 7种任务状态视觉映射表
- killed 与 canceled 有不同的视觉表现

---

### F-05：Cron 任务配置与展示一致性

**文件**：`web/src/pages/CronTasks.tsx`（30KB）、后端 cron handler

**审计方法**：
1. 检查前端展示的"下次执行时间"：是前端 JavaScript 计算（不可靠）还是后端返回（准确）
2. 检查 cron 表达式在前端的格式验证：是否有实时校验和预览
3. 检查 cron 任务状态（启用/禁用/运行中）的前端展示与后端数据对应
4. 检查时区标注：展示时间时是否明确标注"服务器时区 UTC+8"或类似信息
5. 检查 CronTasks.tsx（30KB 较大）内的状态管理：组件内部状态与 API 数据的边界

**验收标准**：
- 下次执行时间由后端计算返回，前端直接展示
- 时区明确标注，无时区歧义

---

### F-06：Settings 配置项前后端一致性

**文件**：`web/src/pages/Settings.tsx`（33KB）、`internal/api/settings_handler.go`

**审计方法**：
1. 列出 Settings 页面的所有可配置项（遍历整个33KB组件）
2. 对每个配置项，在 `settings_handler.go` 中找到对应的 API 字段
3. 检查保存配置的 HTTP 方法：PUT（全量替换）还是 PATCH（部分更新）？行为是否与前端发送一致
4. 检查认证模式切换的 UX：切换为 `always_token` 前是否先提示生成或保存 token
5. 检查 Settings 保存失败时的 UI 处理：是否回滚到保存前的显示状态

**验收标准**：
- Settings 的每个 UI 配置项有对应后端字段（制成对照表）
- 认证模式切换有防锁死保护流程

---

## 第 G 类：性能与资源管理

> **角色设定**：性能工程师，关注 CPU/内存/FD/goroutine，识别 O(n²) 算法、大内存分配、资源泄漏。
> **工作方式**：代码复杂度分析 + benchmark 验证 + profiling 工具确认。

---

### G-01：日志写入性能与磁盘满降级

**文件**：`internal/logging/logger.go`、`internal/logging/writer.go`、`internal/logging/disk_check.go`、`internal/logging/rotate.go`、`internal/logging/catchall.go`

**审计方法**：
1. 检查写入路径：每条日志是否经过 `bufio.Writer` 缓冲（避免每条日志一次 write syscall）
2. 检查轮转触发条件（大小/时间）和轮转时的日志连续性：轮转期间是否会丢失日志行
3. 检查磁盘满时的 `catchall.go` 缓冲：缓冲区大小、满后行为（丢弃/阻塞/报错）
4. 检查多服务并发写日志的锁竞争：多个 logger 是否共享锁，还是完全独立
5. 检查 `search.go` 的1000行搜索实现：是全文读入内存后过滤（大文件 OOM），还是流式读取

**验收标准**：
- 日志写入有 bufio 缓冲
- 搜索实现为流式读取（不全量加载到内存）
- 磁盘满有明确的降级策略（不阻塞业务）

---

### G-02：长轮询内存与连接管理

**文件**：`internal/api/longpoll.go`

**审计方法**：
1. 估算每个长轮询连接的内存开销（等待 goroutine 栈 + channel + 事件 cursor）
2. 估算200条环形缓冲区的内存固定开销（每条事件的平均字节数 × 200）
3. 检查长轮询等待的最大时间：是否有服务端超时设置（防止连接永久挂起）
4. 检查同一客户端重复建立长轮询时的旧连接清理
5. 评估50个并发连接 + 200条事件缓冲的总内存估算

**验收标准**：
- 单连接内存开销估算（记录数字）
- 有服务端超时设置（不是永久等待）
- 总内存开销在合理范围内（< 50MB for 50 connections）

---

### G-03：前端轮询策略效率

**文件**：`web/src/hooks/useLongPolling.ts`、各页面的 `useQuery` 配置

**审计方法**：
1. 检查 `useLongPolling.ts` 实现：是真正的 HTTP 长轮询（服务器 hold 请求）还是短间隔重复请求
2. 统计所有 `useQuery` 的 `refetchInterval` 配置，列出所有轮询间隔
3. 检查页面不可见时（`document.visibilityState === 'hidden'`）轮询是否暂停
4. 检查路由切换时旧页面的查询订阅是否取消
5. 检查同一页面内是否有多个重复的 API 调用可以合并

**验收标准**：
- 轮询实现方式明确（长轮询 vs 短轮询，记录现状）
- 页面不可见时有降频或暂停机制
- 路由切换后查询订阅正确取消

---

### G-04：内存分配热点

**文件**：`internal/api/adapters.go`、`internal/extension/executor.go`、`internal/core/bootstrap.go`

**审计方法**：
1. 识别频繁调用的 adapter 函数：是否每次调用都分配大量临时切片
2. 检查扩展执行的环境变量构建：是否复用 `[]string` 还是每次 `append` 创建新切片
3. 检查日志读取路径（search/tail）：读取 buffer 的大小和复用策略
4. 运行 `go test -bench=. -memprofile=mem.out ./internal/api/ ./internal/extension/`
5. 用 `go tool pprof mem.out` 识别实际内存分配热点

**验收标准**：
- 建立 benchmark 基准数据（记录各 API 路径的 alloc/op 指标）
- 识别 alloc/op > 10KB 的热点路径，提出优化建议

---

### G-05：依赖图算法复杂度

**文件**：`internal/core/dependency.go`、`internal/core/dependency_test.go`

**审计方法**：
1. 识别拓扑排序算法：Kahn（BFS）还是 DFS？确认时间复杂度 O(V+E)
2. 识别循环检测算法：是否能处理自引用（A depends_on A）和多级循环（A→B→C→A）
3. 分析热重载时依赖图重建开销：是全量重算还是增量更新
4. 运行 `go test -bench=. ./internal/core/` 获取基准性能
5. 估算50个服务场景下的计算时间

**验收标准**：
- 算法复杂度 O(V+E) 或更优
- 自引用和多级循环均有测试用例
- 50服务场景计算时间 < 10ms（benchmark 验证）

---

### G-06：资源采集性能与超时保护

**文件**：`internal/api/resource_collector.go`、`internal/api/port_collector.go`

**审计方法**：
1. 检查 `/proc/` 读取频率：是每次 API 请求实时读取，还是有缓存（缓存 TTL 是多少？）
2. 检查 `port_collector.go`：读取 `/proc/net/tcp` + `/proc/<pid>/fd/` 的开销（高连接数系统下文件可能很大）
3. 检查资源采集的超时 deadline：API handler 是否设置了 context deadline 防止采集卡住
4. 检查资源采集失败（进程已退出，/proc/<pid> 不存在）时的快速失败逻辑
5. 运行资源 API 并记录响应时间（100 个进程场景下）

**验收标准**：
- 资源采集有超时 deadline
- 进程退出后 /proc/<pid> 不存在时的错误快速返回（不等待）
- 响应时间 < 200ms（记录实测数值）

---

## 第 H 类：安全性审计

> **角色设定**：应用安全工程师，假设调用者是恶意的，对所有外部输入进行威胁建模。
> **工作方式**：构造攻击向量测试用例，验证每个安全边界的有效性。

---

### H-01：YAML 解析安全

**文件**：`internal/config/yaml_safe.go`、`internal/config/yaml_safe_test.go`

**审计方法**：
1. 检查深度限制（100层）的实现时机：是解析过程中限制，还是解析完成后深度检查
2. 检查别名展开限制（50个）：针对 YAML bomb（指数级展开）的防护是否有效
3. 构造测试用例：101层嵌套 YAML，确认被拒绝并输出错误
4. 构造 YAML bomb 测试用例（小文件展开为GB级内存），确认被限制
5. 检查 `!!python/object` 等 YAML 类型标签是否被拒绝（防止反序列化攻击）

**验收标准**：
- 深度超限在解析阶段拒绝（不是解析成功后检查）
- YAML bomb 测试：内存占用不超过合理阈值（< 100MB）
- 类型标签攻击被拒绝

---

### H-02：命令注入风险审计

**文件**：`internal/core/process.go`、`internal/extension/executor.go`、`internal/core/readiness_script.go`

**审计方法**：
1. 列出所有 `exec.Command` 调用，检查是否有 `"sh"`, `"-c"`, `"bash"` 等 shell 展开调用
2. 检查扩展的 `command` 字段处理：配置中的命令字符串是直接 `exec.Command(cmd, args...)` 还是通过 shell 执行
3. 检查 `SUPD_*` 环境变量的值构建：服务名、工作目录等是否经过特殊字符转义
4. 检查 readiness_script 的脚本路径：是否限制在 workdir 内
5. 检查进程组（Setpgid）：子进程是否被限制在进程组内，防止信号传播异常

**验收标准**：
- 无 `sh -c "$userInput"` 形式的命令执行
- 命令参数通过 `exec.Command(name, arg1, arg2...)` 传入（不拼接字符串到 shell）
- 环境变量值的特殊字符处理有说明

---

### H-03：路径穿越综合审计

**文件**：`internal/api/path_whitelist.go`、`internal/api/file_handler.go`、`internal/archive/`、`internal/storage/`

**审计方法**：
1. 对以下攻击向量逐一测试（构造实际 HTTP 请求或单元测试）：
   - 标准路径穿越：`../../../etc/passwd`
   - URL 编码穿越：`..%2F..%2Fetc%2Fpasswd`
   - 双重编码：`..%252F..%252Fetc%252Fpasswd`
   - 双点变体：`....//etc//passwd`
   - 绝对路径：`/etc/passwd`
   - null byte：`valid.yaml%00.txt`（Unix路径截断）
2. 检查 symlink 解析：`os.Lstat` vs `os.Stat`，指向白名单外的 symlink 是否被拒绝
3. 检查 archive 解压的路径（zip slip）：解压每个文件前是否验证目标路径在 workdir 内

**验收标准**：
- 上述所有攻击向量均返回403
- zip slip 测试通过（含 `../` 路径的 tar.gz 被拒绝）
- path_whitelist_test.go 覆盖上述所有向量

---

### H-04：信息泄露审计

**文件**：所有 API handler、`internal/api/errors.go`、`internal/logging/`

**审计方法**：
1. 构造触发 panic 的请求，检查响应体是否包含 Go 堆栈信息
2. 检查所有 500 错误响应：是否包含内部文件路径、数据库连接字符串等敏感信息
3. 检查日志输出（`grep -rn 'log\.' internal/` + 检查日志调用中是否有 token/密码等敏感字段）
4. 检查 export API 导出的配置包：是否包含 token 明文（config.yaml 中的认证配置）
5. 检查 API 错误消息的粒度：服务不存在（404）vs 权限不足（403）是否有不必要的区分

**验收标准**：
- 500响应不含堆栈信息/内部路径
- 日志中无 token/密码
- 导出包中敏感信息有明确处理策略（记录或过滤）

---

### H-05：Token 生命周期安全

**文件**：`internal/cli/token.go`、`internal/api/middleware.go`、`internal/api/auth_handler.go`

**审计方法**：
1. 检查 token 生成：`crypto/rand` vs `math/rand`，生成字节长度（≥16字节=128位随机性）
2. 检查 token 存储：明文存储在 config.yaml 还是哈希存储？规格对此有何要求？
3. 检查 token 比较：`subtle.ConstantTimeCompare` vs 普通字符串比较（防时序攻击）
4. 检查 token 的传输方式：Bearer token / Basic Auth / 自定义 Header？
5. 全文搜索：`grep -rn 'token\|Token\|TOKEN' internal/logging/` 确认 token 不写入日志

**验收标准**：
- token 生成使用 `crypto/rand`
- 比较使用常数时间比较
- token 不出现在任何日志调用中

---

## 第 I 类：死代码与冗余

> **角色设定**：代码质量工程师，清理无用代码，减少维护负担和混淆风险。

---

### I-01：Go 未使用代码检测

**文件**：所有 `.go` 文件

**审计方法**：
1. 运行 `go install honnef.co/go/tools/cmd/staticcheck@latest && staticcheck ./...`
2. 运行 `go install golang.org/x/tools/cmd/deadcode@latest && deadcode -test ./...`
3. 检查 `internal/core/events.go`（251字节）内容：是否只是骨架占位，有无实际逻辑
4. 逐一检查 session-notes.md 中列出的4个 TODO 占位：在代码中找到对应注释，评估是否仍有意义
5. 检查各 `*_test.go` 中定义但未被调用的 helper 函数

**验收标准**：
- staticcheck 零警告（或记录所有豁免项及原因）
- TODO 占位有明确的处置意见（实现/永久保留/删除）

---

### I-02：前端未使用代码检测

**文件**：所有 `web/src/**/*.tsx`、`web/src/**/*.ts`

**审计方法**：
1. 检查 `web/src/lib/utils.ts`（仅166字节）：定义了什么？是否在任何地方被 import？
2. 检查 `web/src/components/ui/index.ts` 的所有导出：每个导出是否被实际使用
3. 统计 `lib/i18n.ts` 中所有翻译 key，用 `grep -rn` 检查每个 key 的使用情况
4. 运行 `cd web && pnpm build 2>&1 | grep -i 'unused\|warning'` 查看构建警告
5. 检查 package.json 依赖：是否有安装但从未被 import 的包

**验收标准**：
- 识别所有未使用的 export 和翻译 key（制成清单）
- 无未使用的 npm 依赖（bundle analysis）

---

### I-03：重复代码识别

**文件**：所有代码文件

**审计方法**：
1. 检查 `api/adapters.go`：相似结构体转换逻辑的重复（如多处相同的枚举值转换）
2. 检查各 handler 文件：URL 路径参数提取（`chi.URLParam(r, "name")`）是否有统一工具函数
3. 检查各 `trigger_*.go` 文件：共同逻辑（日志记录/错误处理）是否重复
4. 前端：各 `pages/*.tsx` 中的数据获取和刷新模式是否有共同 hook 可提取
5. 运行 `jscpd web/src/` 检测前端重复代码块（如工具已安装）

**验收标准**：
- 识别并报告超过20行的重复代码块（不要求立即重构，但需记录）
- 提出可抽象为共用函数/hook 的重复模式

---

### I-04：过时注释与误导性注释清理

**文件**：所有 `.go`、`.tsx`、`.ts` 文件

**审计方法**：
1. 搜索 `grep -rn 'TODO\|FIXME\|HACK\|XXX' internal/ web/src/`，列出所有注释
2. 对每个 TODO/FIXME：评估对应问题是否已解决，还是确实待处理
3. 搜索 `grep -rn 'Task [0-9]\.' internal/`：找出引用开发阶段 Task 的注释，评估是否应清理
4. 检查函数文档注释的准确性：参数/返回值说明是否与实现一致（选取最复杂的10个函数抽查）
5. 检查注释中描述的"已废弃"或"临时"方案是否真的已更新

**验收标准**：
- 所有 TODO/FIXME 有明确的处置状态（已解决/已确认待处理/可删除）
- "Task X.X.X" 注释有明确处置意见

---

## 第 J 类：过度设计识别

> **角色设定**：务实工程师，评估每个抽象是否与当前规模匹配，对抗"为未来复杂性过度设计"的倾向。

---

### J-01：接口抽象层必要性评估

**文件**：`internal/api/interfaces.go`（11个接口，7953字节）

**审计方法**：
1. 列出全部11个接口名称和方法清单
2. 对每个接口：
   - 有多少个实现者？（1个实现的接口在测试中不用 mock 时价值有限）
   - 测试文件中是否有对应的 mock 实现？
   - 接口方法数量是否合理？（>7个方法可能需要拆分）
3. 评估这11个接口能否合并（如只有1-2个实现且无 mock 使用的接口）
4. 对照 DEV-004 偏差记录的说明，验证接口的实际使用场景

**验收标准**：
- 每个接口有明确的存在理由（列表记录）
- 识别可能冗余的接口，提供合并/保留建议

---

### J-02：配置结构复杂度评估

**文件**：`internal/config/` 所有19个文件

**审计方法**：
1. 统计配置结构体的嵌套层级（ServiceConfig/ExtensionMeta/GlobalConfig）
2. 评估 `defaults.go`（1665字节）：是否可以将默认值内联到结构体 tag 中（减少一个文件）
3. 评估 `env_merge.go`（1091字节）：功能使用频率 vs 实现复杂度
4. 评估各 `*_validate.go` 文件：校验逻辑是否可以简化或合并
5. 总体评估：19个文件的组织是否合理，是否有过度拆分

**验收标准**：
- 识别可合并的文件/函数（不要求立即执行，记录建议）
- 配置结构嵌套深度评估

---

### J-03：前端状态管理复杂度评估

**文件**：`web/src/stores/`（3个 store）、`web/src/hooks/useLongPolling.ts`

**审计方法**：
1. 分析 3 个 store 的职责（app.ts/auth.ts/editor.ts）：哪些状态真正需要全局共享
2. 检查 TanStack Query 缓存与 zustand store 的数据重叠：是否有数据在两个地方都存储
3. 评估 `app.ts`（1015字节）：内容是否过于简单，是否值得使用 zustand
4. 评估 `editor.ts`（3490字节）：8标签状态管理的复杂度是否合理
5. 检查是否有可以用 `useState` 替代全局 store 的状态

**验收标准**：
- 识别 query cache 与 store 的数据重叠
- 提供状态管理简化建议

---

## 第 K 类：配置与校验完整性

> **角色设定**：配置系统专家，确保每个配置路径有明确行为，边界值和错误情况均有处理。

---

### K-01：服务配置字段校验完整性

**文件**：`internal/config/service.go`、`internal/config/service_validate.go`、`internal/config/service_test.go`

**审计方法**：
1. 列出 `ServiceConfig` 结构体所有字段（含嵌套），制成字段清单
2. 对每个字段，在 `service_validate.go` 中找到对应校验逻辑（或标注"无校验"）
3. 重点检查：
   - `name` 字段：长度上限、字符集（含 `/` 的 name 会导致路由错误）
   - `command` 字段：是否拒绝空字符串
   - `depends_on`：自引用检测、引用不存在服务的检测时机（解析阶段 vs 运行时）
4. 检查非法 name 对 API 路由的影响（含 `/` 的服务名在 chi 路由中的行为）

**验收标准**：
- 每个字段的校验状态制成清单（有校验/无校验/不需要校验并说明原因）
- 含 `/` 的服务名有校验拒绝（或路由层有保护）

---

### K-02：扩展配置字段校验完整性

**文件**：`internal/config/extension.go`、`internal/config/extension_validate.go`、`internal/config/extension_test.go`

**审计方法**：
1. 列出 `ExtensionMeta` 结构体所有字段，制成清单
2. 检查 `actions[].id` 唯一性校验：同一扩展内是否有重复 id 的校验
3. 检查 cron 表达式的格式校验：是否在 extension_validate 阶段校验（而非等到 cron 运行时才报错）
4. 检查 `button_style` 枚举：只允许 primary/default/danger，是否有显式 switch 或 contains 校验
5. 检查 `timeout` 字段：是否校验不超过 1800s 硬上限（AGENTS.md 数值锁定表）

**验收标准**：
- action.id 唯一性校验在解析阶段执行
- cron 表达式格式在配置解析时验证
- timeout 上限 1800s 校验存在且生效

---

### K-03：运行时配置校验

**文件**：`internal/config/runtime.go`、`internal/config/runtime_validate.go`、`internal/config/runtime_resolve.go`

**审计方法**：
1. 检查 binary 路径存在性校验：是在配置加载时检查，还是等到第一次使用时才发现
2. 检查循环别名引用（A alias B, B alias A）的检测：是否有图遍历循环检测
3. 检查 binary 执行权限：`os.Access` 或类似调用检查可执行性
4. 检查 runtime 解析优先级（规格定义了三层来源），`runtime_resolve.go` 的实现是否与规格一致

**验收标准**：
- 循环别名引用被检测并有清晰错误消息
- binary 不可执行在配置加载时报错（不等到使用时）

---

### K-04：全局配置校验

**文件**：`internal/config/config.go`、`internal/config/config_test.go`

**审计方法**：
1. 检查 `auth.mode` 枚举校验：只允许 none/local_skip/always_token，其他值是否拒绝
2. 检查 `log_dir` 可写性：是否在启动时验证（而非等到写日志时才发现权限不足）
3. 检查 `workdir` 存在性和可访问性：是否在启动时验证
4. 检查配置字段的默认值填充时机和 `defaults.go` 的调用顺序
5. 检查配置文件不存在时的行为：是使用全部默认值还是报错退出

**验收标准**：
- auth.mode 非法值被拒绝
- log_dir/workdir 在启动时验证（不依赖运行时发现）

---

## 第 L 类：测试覆盖质量

> **角色设定**：测试工程师，不仅关注覆盖率数字，更关注测试的有效性——是否覆盖了真实的边界条件和失败路径。

---

### L-01：关键路径测试有效性审计

**文件**：所有 `*_test.go` 文件

**审计方法**：
1. 运行 `go test -coverprofile=coverage.out ./...` 生成覆盖率报告
2. 运行 `go tool cover -func=coverage.out | sort -k3 -n` 找出低覆盖率文件
3. 重点评估以下文件的覆盖率和测试质量：
   - `internal/core/state_machine.go`（核心）
   - `internal/extension/concurrency.go`（复杂并发）
   - `internal/watch/reload_classifier.go`（热重载分类）
   - `internal/api/adapters.go`（数据转换）
   - `internal/api/path_whitelist.go`（安全）
4. 对覆盖率低的文件，评估未覆盖代码是否是关键业务路径

**验收标准**：
- 生成并记录完整覆盖率报告
- 关键包覆盖率 ≥ 80%（state_machine/concurrency/path_whitelist）
- 低覆盖率的关键路径列出补充测试建议

---

### L-02：边界值测试完整性

**文件**：所有含上限值的测试文件

**审计方法**：
对以下每个规格数值，检查是否有边界测试（恰好等于上限、超出上限+1）：
1. 7天任务历史保留：第6天/第7天/第8天的任务清理边界
2. 200条事件环形缓冲：第200条/第201条的覆盖写行为
3. 50个文件历史版本：第50次保存后的版本管理（第51次如何处理）
4. 1800s 扩展超时上限：1800s 被允许，1801s 被拒绝
5. 100MB 上传限制：100MB-1byte 允许，100MB+1byte 拒绝
6. 全局50/单客户端5的长轮询并发：第50+1个全局连接被拒绝

**验收标准**：
- 每个规格上限值都有"上限值通过"+"上限+1被拒绝"两个测试用例

---

### L-03：竞态条件测试

**文件**：所有并发相关测试

**审计方法**：
1. 运行 `go test -race -count=10 ./...`（重复10次以暴露偶现竞态），记录完整输出
2. 重点检查并发测试的设计质量：
   - 是否使用 `sync.WaitGroup` 确保并发 goroutine 完成再断言
   - 是否测试了"并发触发多个扩展"场景
   - 是否测试了"并发操作状态机"场景
3. 检查 debounce 并发测试：多个 goroutine 同时触发防抖的行为
4. 检查 replace 策略并发测试：高频触发下的状态一致性

**验收标准**：
- `go test -race -count=10 ./...` 零竞态警告
- 并发测试用例使用正确的同步机制

---

### L-04：集成测试现状评估与补充建议

**文件**：`internal/api/server_test.go`、`internal/api/service_handler_test.go`

**审计方法**：
1. 梳理现有集成测试的覆盖范围（列出所有测试函数及其测试场景）
2. 检查是否覆盖认证流程（三种模式下的请求测试）
3. 评估是否缺少以下场景的集成测试：
   - 完整的服务生命周期（启动→ready→停止）
   - 配置热重载端到端（文件变化→重载→服务重启）
   - 长轮询与事件的端到端（事件产生→长轮询响应）
4. 对缺失的集成测试，按优先级（价值 × 实现难度）排序，给出补充建议

**验收标准**：
- 现有集成测试覆盖范围清单
- 缺失的高价值集成测试列表（按优先级排序）

---

## 执行规范

### 问题评级体系

每个发现的问题必须按以下四级评级：

| 级别 | 含义 | 示例 |
|---|---|---|
| 🔴 严重 | 影响数据正确性/安全性，必须立即修复 | 竞态条件/路径穿越/状态机错误 |
| 🟠 重要 | 影响功能/性能，影响用户体验 | 错误静默丢失/内存泄漏/UI 状态错误 |
| 🟡 改进 | 代码质量/一致性问题，建议修复 | 重复代码/错误消息质量差 |
| 🔵 建议 | 优化机会，可排期处理 | 性能优化/过度抽象简化 |

### 问题记录格式

每个发现的问题必须按以下格式记录：

```markdown
#### [审计项ID-序号] 问题标题

- **位置**：`文件路径:行号`（必须精确到行）
- **描述**：具体是什么问题，当前代码做了什么，应该做什么
- **复现方法**：如何触发这个问题（命令/请求/代码路径）
- **影响**：此问题会导致什么后果（数据错误/安全漏洞/用户困惑/性能下降）
- **修复建议**：具体的代码改动建议（不是泛泛而谈）
- **评级**：🔴/🟠/🟡/🔵
```

### 审计输出文件约定

每类审计完成后写入独立文件：

```
/home/qq/Documents/trae_projects/supd/tmp/
├── audit_results_A_logic.md          # A类：后端逻辑
├── audit_results_B_concurrency.md    # B类：并发安全
├── audit_results_C_error.md          # C类：错误处理
├── audit_results_D_api.md            # D类：API层
├── audit_results_E_ux.md             # E类：前端UX
├── audit_results_F_consistency.md    # F类：前后端一致性
├── audit_results_G_perf.md           # G类：性能
├── audit_results_H_security.md       # H类：安全
├── audit_results_I_dead.md           # I类：死代码
├── audit_results_J_over.md           # J类：过度设计
├── audit_results_K_config.md         # K类：配置校验
├── audit_results_L_test.md           # L类：测试质量
└── audit_summary.md                  # 汇总报告
```

### 每类审计输出的文件头格式

```markdown
# 审计结果：X类 — [审计域名称]

## 执行信息
- **审计日期**：YYYY-MM-DD
- **审计员**：AI Agent（Claude Sonnet）
- **审计文件数**：N 个文件，共 N 行代码
- **运行的验证命令**：（列出实际执行的命令和输出）

## 总结
- 发现问题总数：N
  - 🔴 严重：N
  - 🟠 重要：N
  - 🟡 改进：N
  - 🔵 建议：N
- 关键结论：（2-3句话概括最重要的发现）

## 详细发现

[按审计子项逐项列出，每项先说明"已核实内容"（覆盖了什么），再列出问题]
```

### 严格禁止行为

执行审计的 AI Agent 必须遵守：

- **禁止跳过任何指定文件**：每个审计项目列出的所有文件都必须完整阅读
- **禁止以"应该正确"代替实际验证**：必须阅读代码才能得出"已验证正确"的结论
- **禁止抽样代替全量**：不允许"其他handler应该类似，不再逐一检查"
- **禁止跳过验证命令**：go test/staticcheck/errcheck 等命令必须实际运行
- **禁止伪造通过结论**：未发现问题≠未认真审计，必须记录已核实的具体内容（代码行号）
- **禁止省略问题格式**：每个问题必须有文件名+行号+修复建议，缺一不可
- **禁止在会话结束前不更新 session-notes.md**：每次审计会话结束必须更新进度

---

*审计方案版本：v1.0 | 生成日期：2026-07-12 | 项目：supd*
*规格基准：`docs/需求规格说明_v1.5.md` | 代码基准：`go build/vet/test 全部通过`*

---

---

## 第 M 类：通用代码质量扫描（工具链驱动，不指定文件）

> **角色设定**：代码质量工程师，使用工具链对整个代码库进行无偏见的全量扫描，不依赖对项目结构的先验知识，发现人工分析容易忽略的系统性问题。
> **工作方式**：运行工具→分析输出→分类问题→评估影响。每个工具的原始输出必须完整保存。

---

### M-01：静态分析工具全量扫描

**扫描范围**：整个项目（不指定具体文件）

**执行步骤**：
1. 运行 `go vet ./...`，记录全部输出（零警告为基准）
2. 安装并运行 `staticcheck ./...`，记录全部 SA/S/ST/QF/U 类警告
3. 安装并运行 `errcheck ./...`，找出所有未处理的 error 返回值
4. 运行 `go build -gcflags="-e" ./...`，收集编译阶段所有 warning
5. 对每条工具输出，判断：误报（false positive）/ 真实问题，并说明理由

**评估维度**：
- `go vet` 零警告是硬性要求
- `staticcheck` 的每条警告均需有处理意见（修复/合理豁免+说明）
- `errcheck` 未处理 error 的每处均需评估风险等级

---

### M-02：竞态检测全量扫描

**扫描范围**：所有带 `_test.go` 的包

**执行步骤**：
1. 运行 `go test -race -count=5 ./...`（5次重复以暴露偶现竞态）
2. 记录完整输出，包括每个包的测试时间
3. 对任何竞态警告，追溯到具体的共享变量和访问点
4. 对测试超时（默认10分钟）的情况，记录并分析原因

**评估维度**：
- 零竞态警告是硬性要求
- 任何超时或 panic 均需记录

---

### M-03：依赖安全与版本审计

**扫描范围**：`go.mod`、`go.sum`、`web/package.json`、`web/pnpm-lock.yaml`

**执行步骤**：
1. 运行 `go list -m -u all 2>/dev/null | grep '\[' | head -30`，列出有新版本可用的依赖
2. 运行 `govulncheck ./...`（如已安装），检查 Go 依赖的已知漏洞
3. 在 `web/` 目录运行 `pnpm audit`，检查前端依赖的已知安全漏洞
4. 检查是否有 `replace` 指令替换为本地路径或非正式版本
5. 对所有使用预发布版本（rc/beta/alpha）的依赖，评估替换为稳定版本的可行性

**评估维度**：
- 高危及以上漏洞（CVSS ≥ 7.0）必须记录
- 预发布版依赖需有使用说明（已在 deviations.md 中记录的除外）

---

### M-04：代码复杂度全量分析

**扫描范围**：所有 `.go` 文件

**执行步骤**：
1. 运行 `gocyclo -over 15 ./...`（如已安装），找出圈复杂度超过15的函数
2. 统计各包的文件大小分布：`find internal/ -name '*.go' | xargs wc -l | sort -rn | head -20`
3. 统计各函数的行数：找出超过100行的函数（`awk '/^func /{...}' *.go` 或类似方法）
4. 识别嵌套深度超过4层的代码块
5. 对超标的函数/文件，评估：是合理的业务复杂度，还是应该拆分的技术债务

**评估维度**：
- 圈复杂度 > 20 的函数为重点关注项
- 单文件超过500行（不含测试）为关注项
- 记录 Top10 最复杂函数，提供可读性评价

---

### M-05：前端构建质量分析

**扫描范围**：`web/` 目录

**执行步骤**：
1. 运行 `cd web && pnpm build 2>&1`，记录完整输出（含 warning）
2. 检查构建产物大小：`du -sh web/dist/*`，识别过大的 chunk
3. 运行 TypeScript 严格模式检查：`cd web && pnpm tsc --noEmit --strict 2>&1`（如 tsconfig 未启用 strict）
4. 检查 ESLint 配置并运行：`cd web && pnpm lint 2>&1`（如有配置）
5. 分析 bundle：识别意外包含的大型库（如 lodash 全量引入）

**评估维度**：
- 构建零 warning 为理想目标
- TypeScript 类型错误均需处理
- 单个 JS chunk > 500KB（未压缩）为关注项

---

## 第 N 类：端到端功能验证（用户视角，不依赖代码知识）

> **角色设定**：QA 测试工程师，从最终用户的视角验证系统行为，不依赖对内部实现的了解，发现代码审查容易忽略的集成问题和用户体验缺陷。
> **工作方式**：启动真实服务，构造真实请求，验证完整的功能路径。

---

### N-01：核心 API 端点功能验证

**验证范围**：所有主要 API 端点（不预设具体端点列表，通过实际枚举发现）

**执行步骤**：
1. 启动服务：`SUPD_LOG_DIR=/tmp/supd-logs ./supd --workdir test_workdir run`
2. 使用 `curl -s http://localhost:PORT/api/...` 对每个端点发送合法请求，记录响应
3. 对每个端点额外发送：空 body 请求、畸形 JSON、超长字段值、特殊字符（`<>'"&\0`）
4. 检查所有 4xx 响应是否有结构化错误体（不是裸 HTML 或空响应）
5. 检查所有 500 响应体内容（不应包含堆栈信息）

**评估维度**：
- 不应存在返回空响应体的 4xx/5xx
- 不应存在 panic 导致的连接直接断开

---

### N-02：服务生命周期端到端验证

**验证范围**：服务的完整生命周期（不预设具体代码路径）

**执行步骤**：
1. 通过 API 创建一个测试服务配置，验证配置持久化
2. 启动该服务，轮询状态直到进入 `up` 或 `ready`，记录状态转换时间
3. 向服务发送 SIGTERM（通过 API），观察状态变化到 `stopping` → `down`
4. 删除该服务，验证相关资源清理（日志文件、进程残留）
5. 故意配置一个不存在的命令，验证服务进入 `failed` 状态且有可读的错误信息

**评估维度**：
- 状态转换必须有可见的 API 响应体反映
- 无残留进程（`ps aux | grep <service_name>`）
- 错误状态下有可读的失败原因

---

### N-03：扩展系统端到端验证

**验证范围**：扩展的完整执行流程

**执行步骤**：
1. 通过 API 触发一个真实扩展的执行，验证任务立即进入 `running` 状态
2. 轮询任务状态直到终态（success/failed），记录完整执行时间
3. 查询任务历史，验证历史记录包含：开始时间、结束时间、退出码、日志截断提示（如有）
4. 测试 dry_run 模式：发送含 `"dry_run": true` 的执行请求，验证不产生实际副作用
5. 测试超时行为：配置一个 `timeout=2` 的扩展并触发，验证2秒后进入 `timeout` 状态

**评估维度**：
- 任务状态转换可通过 API 实时观察
- dry_run 不产生副作用（可通过日志或文件系统变化验证）
- 超时准确（误差 < 1s）

---

### N-04：热重载功能端到端验证

**验证范围**：配置热重载的完整流程

**执行步骤**：
1. 在服务运行时，修改 workdir 中的一个服务配置文件
2. 等待防抖时间（>500ms），观察是否自动触发重载（通过日志或事件 API）
3. 验证重载后服务使用新配置运行（如修改了环境变量，验证新值生效）
4. 故意写入一个语法错误的配置文件，验证：重载被拒绝，服务继续用旧配置运行，有错误通知
5. 验证 `POST /api/reload` 手动触发的行为与自动触发行为一致

**评估维度**：
- 自动热重载在500ms防抖后触发（不早不晚）
- 配置错误时旧配置仍然生效（不中断服务）

---

### N-05：长轮询事件系统验证

**验证范围**：事件系统的端到端行为

**执行步骤**：
1. 建立一个长轮询连接（`GET /api/events/poll?since=0`），保持连接
2. 触发一个服务状态变化，验证事件在长轮询响应中出现
3. 建立第6个单客户端长轮询连接，验证被拒绝（单客户端上限5）
4. 建立51个并发长轮询连接，验证第51个被拒绝（全局上限50）
5. 断开长轮询连接，再次连接，验证历史事件可通过 `since` 参数追溯

**评估维度**：
- 事件到达延迟 < 100ms（长轮询应快速推送）
- 并发限制精确执行（不多不少）

---

## 第 O 类：架构整体性评估（不指定具体文件）

> **角色设定**：系统架构师，从整体视角评估架构决策的合理性、一致性和可维护性，发现跨越多个模块的系统性问题。
> **工作方式**：俯视整个项目，通过代码度量、依赖分析、模式识别来评估架构健康度。

---

### O-01：包依赖关系合理性

**评估范围**：所有 `internal/` 包之间的依赖关系

**执行步骤**：
1. 运行 `go list -f '{{.ImportPath}}: {{.Imports}}' ./internal/...` 列出所有包依赖
2. 绘制（或文字描述）包依赖有向图，识别依赖层级
3. 检查是否存在循环依赖（Go 编译器会拒绝，但检查是否通过巧妙的接口绕过了逻辑上的循环）
4. 检查依赖方向是否符合分层架构：高层（api/cli）依赖低层（core/extension），低层不反向依赖高层
5. 识别"上帝包"（被几乎所有包依赖的包），评估其职责是否单一

**评估维度**：
- 无循环依赖（逻辑层面）
- 依赖方向自顶向下，无逆向依赖
- "上帝包"（如 errors/config）职责单一且合理

---

### O-02：错误处理模式一致性

**评估范围**：整个代码库的错误处理风格

**执行步骤**：
1. 扫描错误返回和错误包装的模式：`fmt.Errorf("...: %w", err)` vs `errors.New()` vs 自定义错误类型
2. 检查是否有统一的错误包装约定（每层是否添加上下文？）
3. 检查 sentinel errors（`var ErrXxx = errors.New(...)` 形式）的使用是否一致
4. 评估 `internal/errors/` 包的错误码体系是否被一致使用（还是有些地方绕过）
5. 检查前端 API 调用的错误处理模式：是否有统一的 error 拦截/转换层

**评估维度**：
- 错误包装模式一致（不混用多种风格）
- 所有业务错误通过 errors 包的错误码体系表达

---

### O-03：日志记录模式一致性

**评估范围**：整个代码库的日志记录

**执行步骤**：
1. 扫描所有日志调用（`log.Printf/Println/Fatal/Panic/Fatalf` 等），统计分布
2. 检查日志级别使用是否合理：Debug/Info/Warn/Error/Fatal 的区分
3. 检查日志格式的一致性：结构化日志（key=value）还是自由文本格式
4. 检查是否有敏感信息（token、密码、私钥）出现在日志中
5. 检查错误日志是否包含足够的上下文（"failed to start service X: ..." 而非仅 "failed"）

**评估维度**：
- 日志格式统一
- 错误日志上下文充分
- 无敏感信息泄露到日志

---

### O-04：命名规范一致性

**评估范围**：整个代码库

**执行步骤**：
1. Go 端：检查导出函数/类型/变量命名是否符合 Go 惯用法（PascalCase，无缩写歧义）
2. Go 端：检查包名命名是否简洁（单词，小写，无下划线）
3. 前端：检查 React 组件命名（PascalCase）、hook 命名（use前缀）、工具函数命名（camelCase）
4. API：检查 JSON 字段名是否统一使用 snake_case，URL 路径是否统一使用 kebab-case 或全小写
5. 识别命名不一致的场景（如 `serviceId` vs `service_id` 混用）

**评估维度**：
- 命名风格在同一层级内保持一致
- 无误导性名称（如名为 `Get` 但实际有副作用的函数）

---

### O-05：配置与硬编码值全局审查

**评估范围**：整个代码库中的魔法数字和硬编码字符串

**执行步骤**：
1. 扫描 Go 代码中的数字字面量：`grep -rn '[^0-9][0-9]\{2,\}[^0-9]' internal/`（找出所有2位以上数字）
2. 对每个发现的魔法数字，判断：是否应该定义为常量？是否与规格规定的数值一致？
3. 扫描前端代码中的魔法数字和硬编码字符串（URL路径、超时值等）
4. 检查规格中规定的所有数值（AGENTS.md §3.2 表格中的13个数值），在代码中确认每个值的定义位置
5. 检查是否有数值在多处重复出现而未提取为常量（修改时需要多处同步）

**评估维度**：
- 规格规定的13个关键数值均以常量/配置形式存在（不散落为魔法数字）
- 跨多处使用的数值不重复硬编码

---

## 第 P 类：用户体验与可用性通用评估（不指定具体页面）

> **角色设定**：UX 研究员，从完全不了解内部实现的用户视角，评估系统的可用性、错误恢复、帮助信息等通用质量维度。

---

### P-01：错误信息用户友好性全局评估

**评估范围**：所有可能向用户展示的错误信息（API 响应体、CLI 输出、UI 错误提示）

**执行步骤**：
1. 收集所有 API 错误码（22个）对应的错误消息文本，逐一评估：是否用户可理解？是否提供了操作建议？
2. 收集 CLI 命令在失败时的输出，评估：是否说明了失败原因？是否提供了恢复步骤？
3. 收集前端所有 toast/alert 错误消息，评估：是否为中文？是否针对该错误类型提供了具体指导？
4. 测试特殊场景的错误消息质量：服务依赖循环、配置语法错误、扩展超时、认证失败
5. 检查错误消息的一致性：相同错误在不同入口（API/CLI/UI）的描述是否一致

**评估维度**：
- 所有面向用户的错误消息可理解（非技术性堆栈）
- 有操作性错误提供了恢复建议

---

### P-02：CLI 可用性评估

**评估范围**：所有 CLI 命令（约28个）

**执行步骤**：
1. 对每个 CLI 命令运行 `--help`，评估帮助文档的完整性（参数说明、示例、副作用说明）
2. 测试各命令的必填参数缺失时的错误提示（是否说明了哪个参数是必填的？）
3. 测试命令参数的边界值（空字符串、特殊字符、超长值）
4. 检查命令的退出码：成功为0，失败为非0，是否有文档说明各退出码含义
5. 检查 `supd validate` 命令：是否覆盖了所有配置的校验，输出是否清晰指向问题

**评估维度**：
- 所有命令有有意义的 `--help` 输出
- 错误时退出码非0

---

### P-03：系统可观测性评估

**评估范围**：日志、事件、状态 API 等可观测性接口

**执行步骤**：
1. 在系统运行时，评估日志输出的信息量：关键操作（服务启动/停止/重启）是否有日志记录？
2. 评估事件系统的覆盖：14种事件类型是否覆盖了所有用户关心的系统变化？
3. 检查"状态页"（如 Dashboard）是否能反映系统的真实健康状况（不是只有绿色）
4. 评估当系统出现问题时（服务持续崩溃/扩展执行失败），用户在 UI 上能否快速定位问题
5. 检查服务的资源使用数据（CPU/内存/端口）展示对运维的实际价值

**评估维度**：
- 关键操作均有日志痕迹（可通过日志定位问题原因）
- 用户可在5分钟内通过 UI 定位服务故障原因

---

## 第 Q 类：可维护性与文档评估

> **角色设定**：技术写作工程师 + 新接手项目的工程师视角，评估项目对维护者的友好程度。

---

### Q-01：代码自文档化质量

**评估范围**：所有 Go 包的公开 API

**执行步骤**：
1. 运行 `go doc ./internal/...` 或 `godoc` 检查每个包的文档覆盖情况
2. 对每个导出的函数/类型/方法，检查：是否有文档注释（`// FuncName ...` 格式）
3. 重点检查复杂函数（超过50行）的注释：是否说明了：目的、重要参数的含义、可能的副作用
4. 检查是否有误导性注释（注释说"X"但代码做"Y"）
5. 评估复杂算法（状态机/拓扑排序/并发策略）的注释是否足以让新开发者理解

**评估维度**：
- 所有导出 API 有文档注释
- 复杂逻辑有解释性注释

---

### Q-02：测试可读性评估

**评估范围**：所有 `*_test.go` 文件

**执行步骤**：
1. 对每个测试函数：测试名称是否清晰说明测试场景（`TestServiceStart_SuccessWithDependency` 比 `TestServiceStart` 好）
2. 检查是否使用了 Table-driven tests 组织多个用例（Go 惯用法，更可读）
3. 检查断言的错误消息：`t.Errorf("got %v, want %v", ...)` 是否提供了足够上下文
4. 检查测试中的 setup/teardown 是否有清理逻辑（避免测试间相互污染）
5. 评估测试代码的可维护性：新增一个测试用例是否容易

**评估维度**：
- 测试命名清晰反映测试场景
- 断言失败消息包含期望值和实际值

---

## 打分体系

> 满分 100 分，各类按重要性分配权重，每类内按子项平均。

### 分值分配表

| 类别 | 权重分 | 说明 |
|---|---|---|
| A 后端逻辑正确性 | 20分 | 核心业务逻辑，直接影响功能正确性 |
| B 并发安全与竞态 | 12分 | 数据安全基础，问题难以复现且影响严重 |
| C 错误处理完整性 | 8分 | 影响问题诊断和用户体验 |
| D API 层审计 | 8分 | 外部接口正确性 |
| E 前端 UI/UX 质量 | 8分 | 用户直接感知的质量 |
| F 前后端一致性 | 8分 | 集成正确性 |
| G 性能与资源管理 | 6分 | 长期稳定运行的基础 |
| H 安全性审计 | 10分 | 安全问题影响深远 |
| I 死代码与冗余 | 3分 | 代码卫生，影响维护效率 |
| J 过度设计识别 | 2分 | 架构简洁性 |
| K 配置与校验完整性 | 5分 | 输入防御层 |
| L 测试覆盖质量 | 5分 | 质量保障机制 |
| M 通用工具链扫描 | 2分 | 自动化检测 |
| N 端到端功能验证 | 1分 | 集成验证 |
| O 架构整体性 | 1分 | 架构健康度 |
| P 用户体验通用 | 0.5分 | 可用性 |
| Q 可维护性文档 | 0.5分 | 长期可维护性 |
| **合计** | **100分** | |

### 单项评分规则

每个审计子项（如 A-01、B-03）独立打分，满分为该类别权重分 / 子项数。

**评分扣减规则**：
- 发现 🔴 严重问题：该子项直接得 0 分（该类别分值严重失真时，总分上限相应降低）
- 发现 🟠 重要问题：该子项扣除 50%~80% 分值
- 发现 🟡 改进问题：该子项扣除 15%~30% 分值
- 发现 🔵 建议问题：该子项扣除 0%~10% 分值
- 未发现问题、所有验收标准通过：该子项满分

**评分修正规则**：
- 同一子项有多个问题，按最严重的一个问题作为主要扣分，其余叠加 30% 附加扣分（不低于0分）
- 某类别存在🔴严重问题时，该类别最终得分不超过该类别权重的 30%

### 总分等级

| 总分 | 评级 | 含义 |
|---|---|---|
| 90-100 | ⭐ 优秀 | 生产就绪，仅有少量改进建议 |
| 80-89 | 🟢 良好 | 可用，有若干问题建议修复 |
| 70-79 | 🟡 一般 | 有明显问题，上线前需修复重要问题 |
| 60-69 | 🟠 较差 | 有严重问题，需要系统性修复 |
| < 60 | 🔴 不及格 | 存在重大缺陷，需要立即处理严重问题 |

---

## 审计报告规范（强制要求）

### 输出文件

**审计报告唯一输出路径**：`/home/qq/Documents/trae_projects/supd/tmp/审计报告.md`

此为唯一的最终输出文件，中间过程文件按类别输出到 `tmp/audit_results_*.md`。

### 绝对禁止行为

**在整个审计过程中，严禁以任何形式修改以下内容**：
- 任何 `.go` 源代码文件
- 任何 `.tsx`、`.ts`、`.css` 前端源代码文件
- 任何 `go.mod`、`go.sum`、`package.json` 配置文件
- 任何 `docs/` 目录下的文档文件
- `docs/devlog/session-notes.md`（本文件在审计过程中禁止修改）

**唯一允许写入的路径**：`/home/qq/Documents/trae_projects/supd/tmp/` 目录

> 违反此规则的任何代码修改行为视为审计流程严重违规，需立即停止并回滚。

### 审计报告结构（`审计报告.md`）

```markdown
# supd 项目审计报告

**审计日期**：YYYY-MM-DD
**审计工具**：Claude Sonnet（AI Agent）
**规格基准**：docs/需求规格说明_v1.5.md
**代码基准**：git commit [最近一次commit hash]

---

## 执行摘要

[3-5句话总结最重要的发现，包括最严重的问题和最突出的优点]

---

## 总分：XX / 100 分（X级）

| 类别 | 权重 | 得分 | 问题数（🔴/🟠/🟡/🔵） |
|---|---|---|---|
| A 后端逻辑正确性 | 20 | XX | 0/0/0/0 |
| ... | ... | ... | ... |
| **合计** | **100** | **XX** | **总问题数** |

---

## 各类详细审计结果

### A 类：后端逻辑正确性（得分：XX/20）

#### A-01：[子项名称]（得分：X.X/2.5）

**审计过程**：
- 阅读了文件：[文件列表]
- 运行了命令：[命令及输出摘要]
- 核实了内容：[具体核实了什么]

**发现的问题**：

##### [A-01-001] 问题标题
- **位置**：`文件路径:行号`
- **描述**：...
- **影响**：...
- **修复建议**：...
- **评级**：🔴/🟠/🟡/🔵

**未发现问题的验收项**：[列出已核实通过的验收标准]

**子项评分**：X.X / 2.5
**扣分说明**：[说明扣分原因]

---

[其余子项同格式...]

---

## 问题汇总清单

### 🔴 严重问题（N个）
| 编号 | 位置 | 简述 |
|---|---|---|
| A-01-001 | file.go:123 | ... |

### 🟠 重要问题（N个）
...

### 🟡 改进建议（N个）
...

### 🔵 优化建议（N个）
...

---

## 修复优先级建议

### 立即修复（阻塞发布）
1. ...

### 近期修复（影响用户体验）
1. ...

### 排期改进（技术债务）
1. ...

---

## 审计结论

[综合性评价，包括：项目整体质量评估、最大的风险点、已有的亮点、给维护团队的建议]
```

---

*审计方案版本：v1.1（新增 M-Q 通用类 + 打分体系 + 报告规范）*
*更新日期：2026-07-12*
