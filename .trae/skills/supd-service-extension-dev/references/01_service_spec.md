# supd 服务配置与规范指南 (service.yaml)

本参考文档包含 `service.yaml` 的完整字段定义、就绪检测类型、7 状态机转移规则及检查清单。

---

## 1. 服务目录结构

```
<baseDir>/services/<service-name>/
├── service.yaml          # 必需：服务元数据与配置
├── env.yaml              # 可选：服务专属环境变量（必须使用 env: 包装层）
├── <启动脚本或二进制>     # 必需：由 command/runtime 指定
└── extensions/           # 可选：服务级扩展
    └── <ext-name>/
        ├── meta.yaml
        └── run.sh
```

---

## 2. service.yaml 完整字段参考

**必填字段**：`name`、`version`、`command`

> `runtime` 为可选字段：设置时将 runtime 别名解析为绝对路径并前置到 command（如 `runtime: python` + `command: [run.py]` → `[/usr/bin/python3, run.py]`）；省略时 command 数组本身即为完整命令。

| 字段 | 类型 | 默认值 | 说明与约束 |
|---|---|---|---|
| `name` | string | 必填 | 服务名称，必须匹配 `^[a-z][a-z0-9-]*$` 且与目录名一致 |
| `version` | string | 必填 | 服务版本号，如 `"1.0.0"` |
| `description` | string | `""` | 服务描述 |
| `icon` | string | `"box"` | 图标名称，使用前端 IconPicker |
| `autostart` | bool | `true` | supd 启动时是否自动拉起服务 |
| `command` | list[string] | 必填 | 启动命令与参数数组，如 `["python3", "run.py"]` 或 `["bash", "run.sh"]` |
| `runtime` | string | `""` | 运行时别名（可选），设置时前置到 command |
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
支持 4 种类型（锁定不可新增）：

#### A. `http_check`
通过发送 HTTP GET 请求检测就绪：
```yaml
readiness:
  type: http_check
  url: "http://127.0.0.1:8080/health"
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
通过 systemd 风格 fd 通知检测就绪：
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
  check:                # 注意：必须为 check 数组，不能写 command
    - bash
    - check_ready.sh
  interval_seconds: 2   # 默认 1 秒
  timeout_seconds: 15   # 默认 5 秒
```

### 3.2 重启策略 (`restart`)
```yaml
restart:
  policy: on-failure       # always | on-failure | never
  backoff_ms: 1000         # 初始退避毫秒数
  max_backoff_ms: 300000   # 最大退避上限 (300 秒)
  multiplier: 2            # 退避倍增系数
  max_retries: 10          # 最大尝试次数
  reset_after_seconds: 60  # 稳定运行指定秒数后重置计数
```

### 3.3 停止策略 (`stop`)
```yaml
stop:
  grace_seconds: 10        # SIGTERM 优雅退出等待预算
  timeout_seconds: 60      # 整个停止流程超时预算 (须 >= grace_seconds)
```

### 3.4 日志配置 (`logging`)
```yaml
logging:
  enabled: true            # 是否开启日志记录 (默认 true)
  max_size_mb: 10          # 单个日志文件上限 (默认 10MB)
  max_files: 5             # 轮转文件保留个数 (默认 5 个)
```

### 3.5 自定义信号 (`signals`)
```yaml
signals:
  reload: HUP              # 配置重载信号
  rotate_logs: USR1        # 日志轮转信号
  graceful_quit: QUIT      # 优雅退出信号
```
> **允许的信号**：`HUP`, `INT`, `QUIT`, `USR1`, `USR2`, `PIPE`, `ALRM`, `CHLD`  
> **禁止的信号**：`TERM`, `KILL`, `STOP`, `CONT`, `SEGV`, `ABRT`, `BUS`, `FPE`, `ILL`（由 supd 框架独占保留）

---

## 4. 服务 7 状态机

```
pending → starting → up → ready → stopping → down
                       ↘ failed ↗
```

- `pending`: 初始化排队。
- `starting`: 正在启动进程。
- `up`: 进程 PID 已派生，正在进行就绪检测。
- `ready`: 就绪检测通过。
- `stopping`: 收到停止信号，执行 pre_stop 钩子与进程优雅退出。
- `down`: 进程已退出并清理完毕。
- `failed`: 启动超时、进程异常崩溃或重启尝试耗尽。

---

## 5. service.yaml 检查清单

- [ ] `name` 匹配 `^[a-z][a-z0-9-]*$` 且与所在目录名完全一致
- [ ] `command` 为非空字符串数组，相对路径处于服务目录内
- [ ] `readiness` 类型在 `fd_notify`/`tcp_check`/`http_check`/`script` 内
- [ ] `readiness.type: script` 时，配置键名为 `check:` 而不是 `command:`
- [ ] `readiness.type: fd_notify` 时，配置包含必填正整数 `fd:`
- [ ] `readiness.type: tcp_check` 时，配置包含必填正整数 `port:`
- [ ] `signals` 中没有使用 `TERM`/`KILL` 等禁用的框架保留信号
- [ ] `depends_on` 未包含服务自身的名称（自引用拦截）
- [ ] `user`（User 模式）与 `uid`（UID 模式）不能同时指定（互斥校验）
- [ ] UID 模式下 `uid` > 0、`gid` >= 0（0=等于 uid）、`groups` 元素均 > 0（防负数回绕）
