# supd 服务配置与规范指南 (service.yaml)

本参考文档包含 `service.yaml` 的完整字段定义、就绪检测类型、7 状态机转移规则及检查清单。

---

## 1. 服务目录结构

服务根目录中，**控制面文件**（`service.yaml`、`env.yaml`、`extensions/`、`package.*.yaml`）与**业务载荷**（`bin/`、`data/`）严格分离。业务载荷只允许放在 `bin/` 和 `data/` 两个目录中，不得在根目录散落二进制、配置或临时文件。

```
<baseDir>/services/<service-name>/
├── service.yaml              # 必需：服务元数据与配置
├── README.md                 # 必需：中文服务说明与运维指引，不得包含敏感信息
├── env.yaml                  # 可选：服务专属环境变量（必须使用 env: 包装层）
├── bin/                      # 必需：版本化、可替换的程序载荷
│   ├── <service-binary>      #   服务主程序（command 应直接指向 ./bin/<binary>）
│   └── share/                #   可选：随版本发布的只读资源（如 WebUI 模板）
├── data/                     # 可选：实例私有、升级必须保留的数据
│   ├── config/               #   用户配置（迁移导出时包含，共享导出时排除）
│   ├── state/                #   运行状态（导出时排除）
│   ├── cache/                #   可再生成缓存（导出时排除）
│   ├── certs/                #   服务私有证书
│   └── web/                  #   用户可修改或运行期下载的 WebUI
├── extensions/               # 可选：服务级扩展
│   └── <ext-name>/
│       ├── meta.yaml
│       └── run.sh / run.js
├── package.default.yaml      # 可选：默认导出规则（不存在时使用内置规则：排除 data/）
├── package.migrate.yaml      # 可选：迁移导出规则（含 data/config，排除 data/cache 和 data/state）
└── package.share.yaml        # 可选：共享导出规则（仅 bin/ 和 extensions/）
```

### 1.1 README.md 规范

每个生成的服务根目录**必须**创建 `README.md`，使用中文 Markdown，供部署、维护和交接使用。至少应以一级标题覆盖以下内容：

1. `# 服务名称与版本`：服务名称、版本和一句话用途。
2. `# 目录结构与权限边界`：说明目录结构，以及 `bin/`、`data/` 的读写和权限边界。
3. `# 启动方式与就绪检测`：说明启动命令、必要前置条件与 readiness 检测方式。
4. `# 配置与环境变量`：列出非敏感变量；敏感变量只能说明名称和用途，不能写入密钥值。
5. `# 服务级扩展与 Actions`：列出服务级扩展及其 actions；没有时明确说明。
6. `# 数据持久化与升级更新`：说明持久化位置，以及升级或更新时的保留、迁移和回滚要点。
7. `# 常用运维操作`：给出启动、停止、重启与查看日志的方法。
8. `# 安全与备份注意事项`：说明最小权限、敏感数据保护和备份建议。

README **不得**包含真实密码、token、私钥或任何运行期敏感数据。

### 1.2 bin/ 与 data/ 的边界

| 文件类型 | 目录 | 原因 |
|---|---|---|
| 主二进制、辅助二进制 | `bin/` | 随版本替换 |
| 随二进制发布的只读资源 | `bin/share/` | 与程序版本绑定 |
| 用户编辑配置 | `data/config/` | 升级必须保留 |
| 运行期状态、数据库 | `data/state/` | 实例数据 |
| 可再生成缓存 | `data/cache/` | 升级可丢弃 |
| 用户私有证书 | `data/certs/` | 不导出 |
| 用户安装/在线更新的 WebUI | `data/web/` | 不应被导入升级覆盖 |
| 临时下载包、解压目录 | 系统临时目录（`tjs.tmpDir`） | 成功或失败后均清理 |
| 服务日志 | supd 日志目录 | 不放入服务目录 |

### 1.3 权限隔离

```
service.yaml、README.md、env.yaml、extensions/、package.*.yaml   管理员/supd 所有
bin/                                                    root 或 supd 所有，服务用户只读和执行
data/                                                   服务运行用户所有，可读写
```

- 更新扩展只修改 `bin/`。
- 服务进程只写 `data/`。
- 如需调整属主，只处理 `data/`，**禁止** `chown -R <serviceDir>`（会让服务用户获得修改 `bin/` 和扩展脚本的权限）。
- `bin/` 目录 `0755`，二进制 `0755`。

### 1.4 command 指向

`command` 应直接指向 `./bin/<binary>`，不增加只做转发的 `start.sh`。如需指定配置目录，通过命令行参数指向 `./data/config/`。

```yaml
# ✅ 推荐
command:
  - ./bin/my-daemon
  - --config
  - ./data/config/app.conf

# ❌ 避免：start.sh 包装
command: [./start.sh]
```

### 1.5 可更新性评估

开发服务时应明确标注是否支持二进制手动更新：

- **支持手动二进制更新**：存在稳定版本来源、可识别架构资产、二进制支持非交互安装。此时增加服务级 TJS 更新扩展（参考 `examples/10-binary-updater-ext/`）。
- **不支持**：二进制由系统包管理器维护、下载页需浏览器交互、安装修改系统依赖等。
- **暂不确定**：需要用户提供更新源/版本规则。

### 1.6 打包导出规则

服务支持多份打包规则文件（`package.<profile>.yaml`），详见需求规格说明 §2.12.2。

- **默认导出**：使用 `package.default.yaml`（可选），回退到内置规则（排除 `data/`）。
- **按规则导出**：指定 `package.<name>.yaml`（必须存在），适用于迁移（含配置不含运行数据）或共享（仅程序不含数据）等场景。

---

## 2. service.yaml 完整字段参考

**必填字段**：`name`、`version`、`command`

> **`version` 格式校验**：必须匹配正则 `^[0-9]+\.[0-9]+\.[0-9]+$`（三段数字，如 `"1.0.0"`、`"0.0.41"`），不符合会被校验拒绝。

> **`runtime` 别名前置**（可选）：设置时将 runtime 别名解析为绝对路径并前置到 command（如 `runtime: python3` + `command: [run.py]` → `[/usr/bin/python3, run.py]`）；省略时 command 数组本身即为完整命令。
>
> **内置 runtime 别名**（三层优先级：`config.yaml runtimes` > `runtimes/` 扫描 > 内置）：
>
> | 别名 | 解析路径 | 说明 |
> |---|---|---|
> | `bash` | `/bin/bash` | 内置默认 |
> | `sh` | `/bin/sh` | 内置默认 |
> | `python3` | `python3`（PATH 查找后转为绝对路径） | 内置默认 |
> | `node` | `node`（PATH 查找后转为绝对路径） | 内置默认 |
> | `tjs` | 由 Dockerfile 安装到 `/usr/local/bin/tjs` | 通过 scan 注册，详见 `06_tjs_runtime_guide.md` |
>
> 注：`python`（无 `3`）不是内置别名，应使用 `python3`；其他别名可通过 `config.yaml` 的 `runtimes:` 段或 `runtimes/` 目录扩展。

| 字段 | 类型 | 默认值 | 说明与约束 |
|---|---|---|---|
| `name` | string | 必填 | 服务名称，必须匹配 `^[a-z][a-z0-9-]*$` 且与目录名一致 |
| `version` | string | 必填 | 服务版本号，必须匹配 `^[0-9]+\.[0-9]+\.[0-9]+$`（如 `"1.0.0"`） |
| `description` | string | `""` | 服务描述 |
| `icon` | string | `"box"` | 图标名称，使用前端 IconPicker |
| `autostart` | bool | `true` | supd 启动时是否自动拉起服务；`autostart: false` 时服务初始状态为 `down` |
| `command` | list[string] | 必填 | 启动命令与参数数组（至少 1 个元素），如 `["python3", "run.py"]` 或 `["bash", "run.sh"]` |
| `runtime` | string | `""` | 运行时别名（可选），设置时前置到 command（见上表） |
| `user` | string | `""` | 运行用户（User 模式）；留空则继承 supd 启动用户。与 `uid` 互斥 |
| `group` | string | `""` | 运行组（User 模式下可选，覆盖主组 gid，保留补充组）；留空则同 user |
| `uid` | int | `0` | 直接指定 uid（UID 模式，与 `user` 互斥，不查 /etc/passwd，适用于 NAS 固定 uid 服务）；`0`=未设置 |
| `gid` | int | `0` | 直接指定 gid（UID 模式下可选，`0`=等于 uid） |
| `groups` | list[int] | `[]` | 补充组 gid 列表（UID 模式下可选） |
| `workdir` | string | `""` | 工作目录，必须为绝对路径；默认服务自身目录 |
| `depends_on` | list[string] | `[]` | 依赖的服务名称列表；不能包含自身 |
| `tags` | list[string] | `[]` | 服务分类标签，如 `["web", "demo"]` |
| `readiness` | struct | nil | 就绪检测配置 |
| `restart` | struct | nil | 重启策略配置 |
| `stop` | struct | nil | 停止策略配置（grace_seconds: 10, timeout_seconds: 60） |
| `logging` | struct | nil | 日志配置（enabled: true, max_size_mb: 10, max_files: 5） |
| `signals` | struct | nil | 自定义信号配置 |
| `package` | struct | nil | 打包导出配置 (include / exclude / default) |

> **身份配置说明**（§2.2.13）：
> - **User 模式**（`user`/`group`）：通过用户名查找 uid/gid/补充组，依赖 `/etc/passwd`。`group` 可选覆盖主组 gid（保留补充组不变）。
> - **UID 模式**（`uid`/`gid`/`groups`）：直接指定数字，不查 `/etc/passwd`，适用于 NAS 固定 uid 等场景。`gid=0` 表示等于 `uid`。
> - **互斥**：两种模式不能同时指定，配置校验报错。
> - **非 root 语义（严格拒绝）**：supd 非 root 启动时，`user`/`uid` 必须等于当前用户或留空，否则**服务拒绝启动**（返回 `ErrRuntimeUserNotFound` / HTTP 422）。

---

## 3. 子配置段详解

### 3.1 就绪检测 (`readiness`)
支持 4 种类型（锁定不可新增）。`interval_seconds` 与 `timeout_seconds` 为所有类型共用字段，默认值 `1s` / `5s`，必须为正整数。

#### A. `http_check`
通过发送 HTTP GET 请求检测就绪：
```yaml
readiness:
  type: http_check
  url: "http://127.0.0.1:8080/health"  # 必填
  expected_status: 200  # 默认 200
  interval_seconds: 1   # 默认 1 秒
  timeout_seconds: 5    # 默认 5 秒
```

#### B. `tcp_check`
通过 TCP 端口连接检测就绪：
```yaml
readiness:
  type: tcp_check
  port: 8080            # 必填正整数
  interval_seconds: 1   # 默认 1 秒
  timeout_seconds: 5    # 默认 5 秒
```

#### C. `fd_notify`
通过 systemd 风格 fd 通知检测就绪（supd 通过 extraFiles 将管道写端传给子进程 fd 3）：
```yaml
readiness:
  type: fd_notify
  fd: 3                 # 必填正整数
  interval_seconds: 1   # 默认 1 秒
  timeout_seconds: 5    # 默认 5 秒
```

#### D. `script` (注意：脚本命令键名为 `check`)
通过执行自定义脚本检测就绪（继承服务环境变量及工作目录， exit 0 为就绪）：
```yaml
readiness:
  type: script
  check:                # 注意：必须为 check 数组，不能写 command；至少 1 个元素
    - bash
    - check_ready.sh
  interval_seconds: 2   # 默认 1 秒
  timeout_seconds: 15   # 默认 5 秒
```

> **script 类型说明**：`check` 由 ProcessManager 通过 `exec.Command` 直接执行（不经 shell），因此 `check[0]` 中的 `..` 不会被 shell 展开。若 `check[0]` 为相对路径，exec 以服务工作目录为 CWD 解析，仍受 baseDir 边界约束。

### 3.2 重启策略 (`restart`)
```yaml
restart:
  policy: on-failure       # always | on-failure | never（必填，校验枚举）
  backoff_ms: 1000         # 初始退避毫秒数，默认 1000（非负）
  max_backoff_ms: 30000    # 最大退避上限，默认 30000（30 秒，非负，须 >= backoff_ms）
  multiplier: 2            # 退避倍增系数，默认 2（非负）
  max_retries: 0           # 最大尝试次数，默认 0=不限制（非负）
  reset_after_seconds: 300 # 稳定运行指定秒数后重置计数，默认 300（非负）
```

> **重要**：`restart` 段在 `service.yaml` 中**无内置默认值填充**（service 层只校验合法性）。上述默认值是 `config.yaml` 的 `defaults.restart` 全局默认值，当服务未配置 `restart` 段时使用全局默认。若服务显式配置 `restart:`，则未填字段为对应类型的零值（如 `max_retries: 0` 表示不限制）。

### 3.3 停止策略 (`stop`)
```yaml
stop:
  grace_seconds: 10        # SIGTERM 优雅退出等待预算，默认 10（须 > 0）
  timeout_seconds: 60      # 整个停止流程超时预算，默认 60（须 > 0，应 >= grace_seconds）
```
> `stop` 段触发时会先执行扩展的 `pre_stop` 钩子，总时长受 `timeout_seconds` 控制；超时后强制 SIGKILL。`grace_seconds` 是 SIGTERM 后等待进程退出的预算。

### 3.4 日志配置 (`logging`)
```yaml
logging:
  enabled: true            # 是否开启日志记录 (默认 true)
  max_size_mb: 10          # 单个日志文件上限 (默认 10MB，须 > 0)
  max_files: 5             # 轮转文件保留个数 (默认 5 个，须 > 0)
```
> 服务日志写入 `<logDir>/<service>/` 目录，由 supd 进程管理；服务进程的 stdout/stderr 被重定向到日志文件。

### 3.5 自定义信号 (`signals`)
```yaml
signals:
  reload: HUP              # 配置重载信号
  rotate_logs: USR1        # 日志轮转信号
  graceful_quit: QUIT      # 优雅退出信号
```
> **允许的信号**（8 种）：`HUP`, `INT`, `QUIT`, `USR1`, `USR2`, `PIPE`, `ALRM`, `CHLD`  
> **禁止的信号**（9 种，由 supd 框架独占保留）：`TERM`, `KILL`, `STOP`, `CONT`, `SEGV`, `ABRT`, `BUS`, `FPE`, `ILL`  
> 信号值大小写不敏感（内部转大写后校验）。空字符串跳过该字段校验。

---

## 4. 服务 7 状态机

```
pending → starting → up → ready → stopping → down
                       ↘ failed ↗
```

- `pending`: 初始化排队（依赖未就绪）。
- `starting`: 正在启动进程（含退避重试等待）。
- `up`: 进程 PID 已派生，正在进行就绪检测（无 readiness 配置时为终态）。
- `ready`: 就绪检测通过。
- `stopping`: 收到停止信号，执行 pre_stop 钩子与进程优雅退出。
- `down`: 进程已退出并清理完毕（不再自动重启）。
- `failed`: 永久失败，不再自动重启（启动超时、达到 `max_retries` 或进程异常崩溃且 restart 策略不允许重启）。

### 4.0 状态转移规则（11 条）

| # | From | Event | To | 触发条件 |
|---|---|---|---|---|
| 1 | pending | depends_ready | starting | 所有 `depends_on` 服务进入 `ready` |
| 2 | starting | process_started | up | 进程派生成功 |
| 3 | up | readiness_passed | ready | readiness 检测通过 |
| 4 | up/ready/starting | stop_requested | stopping | 用户 stop / restart |
| 5 | up/ready | restart_allowed | starting | 进程死亡且 `restart.policy` 允许，退避后重启 |
| 6 | up/ready | max_retries | failed | 进程死亡且达到 `max_retries` |
| 6 | up/ready | readiness_timeout | failed | readiness 检测超时 |
| 7 | starting | restart_allowed | starting | starting 阶段进程退出且允许重启 |
| 8 | starting | max_retries/readiness_timeout | failed | starting 阶段不允许继续重启 |
| 9 | stopping | process_exited / backoff_abort | down | 进程退出 / 退避等待中被停止 |
| 10 | down/failed | manual_start | starting | 用户手动 `start` |
| 11 | up/ready/starting | normal_exit | down | `on-failure` 策略下进程 exit 0 时不重启 |

> **自动重启不经过 `down`**：`up → starting`（规则 5/7）直接退避重启，状态机不经过 `down`。仅 `stop_requested` 或正常退出后才到 `down`。

### 4.1 clear-failed 操作（failed → down）

`POST /api/services/{name}/clear-failed` 清除 `failed` 状态，**重置为 `down`**（规格 §2.8.1 允许 down/pending，实现选择 down）。

- **前置条件**：服务当前状态必须为 `failed`；否则返回 `400 INVALID_REQUEST`（错误码 `ErrInvalidRequest`）。
- **目标状态选择理由**：
  - `pending → starting` 仅由 `depends_ready`（依赖就绪）或 bootstrap 推进；`clear-failed` 不触发依赖图重算，无上游依赖的服务会永久卡在 `pending`（表现为"等待中"假象）。
  - `down` 可由用户通过 `manual_start` 直接启动（状态机规则 `StateDown → StateStarting`），语义明确。
- **不触发下游唤醒**：`clear-failed` 调用 `ResetTo(StateDown)` 绕过状态机转移规则（不发布 `service_ready` 事件，仅发布 `admin_reset` 事件）；不调用 `OnServiceDependable`，不会唤醒依赖该服务的下游服务。
- **后续操作**：重置为 `down` 后，需显式调用 `POST /api/services/{name}/start` 启动服务。

---

## 5. service.yaml 检查清单

- [ ] `name` 匹配 `^[a-z][a-z0-9-]*$` 且与所在目录名完全一致
- [ ] `version` 匹配 `^[0-9]+\.[0-9]+\.[0-9]+$`（三段数字，如 `1.0.0`）
- [ ] 服务根目录包含中文 `README.md`，覆盖服务信息、目录权限、启动就绪、配置环境、扩展 actions、持久化升级、运维及安全备份，且不含敏感数据
- [ ] `command` 为非空字符串数组（至少 1 个元素），相对路径处于服务目录内
- [ ] `workdir` 如设置必须为绝对路径（以 `/` 开头）
- [ ] `readiness` 类型在 `fd_notify`/`tcp_check`/`http_check`/`script` 内
- [ ] `readiness.type: script` 时，配置键名为 `check:` 而不是 `command:`，且至少 1 个元素
- [ ] `readiness.type: fd_notify` 时，配置包含必填正整数 `fd:`
- [ ] `readiness.type: tcp_check` 时，配置包含必填正整数 `port:`
- [ ] `readiness.type: http_check` 时，配置包含必填 `url:`
- [ ] `readiness.interval_seconds` 与 `timeout_seconds` 均为正整数
- [ ] `restart.policy` ∈ `{always, on-failure, never}`；`max_backoff_ms >= backoff_ms`
- [ ] `stop.grace_seconds` 与 `stop.timeout_seconds` 均为正整数
- [ ] `logging.max_size_mb` 与 `logging.max_files` 均为正整数
- [ ] `signals` 中没有使用 `TERM`/`KILL` 等禁用的框架保留信号；只使用 8 种允许信号
- [ ] `depends_on` 未包含服务自身的名称（自引用拦截）
- [ ] `user`（User 模式）与 `uid`（UID 模式）不能同时指定（互斥校验）
- [ ] UID 模式下 `uid` > 0、`gid` >= 0（0=等于 uid）、`groups` 元素均 > 0（防负数回绕）
- [ ] `package.default` ∈ `{include, exclude}`（若设置）
