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

### 4.1 clear-failed 操作（failed → down）

`POST /api/services/{name}/clear-failed` 清除 `failed` 状态，**重置为 `down`**（规格 §2.8.1 允许 down/pending，实现选择 down）。

- **前置条件**：服务当前状态必须为 `failed`；否则返回 `400 INVALID_REQUEST`（错误码 `ErrInvalidRequest`）。
- **目标状态选择理由**：
  - `pending → starting` 仅由 `depends_ready`（依赖就绪）或 bootstrap 推进；`clear-failed` 不触发依赖图重算，无上游依赖的服务会永久卡在 `pending`（表现为"等待中"假象）。
  - `down` 可由用户通过 `manual_start` 直接启动（状态机规则 `StateDown → StateStarting`），语义明确。
- **不触发下游唤醒**：`clear-failed` 不调用 `OnServiceDependable`，不会唤醒依赖该服务的下游服务。下游服务需通过其自身的 `start`/`restart` 推进。
- **后续操作**：重置为 `down` 后，需显式调用 `POST /api/services/{name}/start` 启动服务。

---

## 5. service.yaml 检查清单

- [ ] `name` 匹配 `^[a-z][a-z0-9-]*$` 且与所在目录名完全一致
- [ ] 服务根目录包含中文 `README.md`，覆盖服务信息、目录权限、启动就绪、配置环境、扩展 actions、持久化升级、运维及安全备份，且不含敏感数据
- [ ] `command` 为非空字符串数组，相对路径处于服务目录内
- [ ] `readiness` 类型在 `fd_notify`/`tcp_check`/`http_check`/`script` 内
- [ ] `readiness.type: script` 时，配置键名为 `check:` 而不是 `command:`
- [ ] `readiness.type: fd_notify` 时，配置包含必填正整数 `fd:`
- [ ] `readiness.type: tcp_check` 时，配置包含必填正整数 `port:`
- [ ] `signals` 中没有使用 `TERM`/`KILL` 等禁用的框架保留信号
- [ ] `depends_on` 未包含服务自身的名称（自引用拦截）
- [ ] `user`（User 模式）与 `uid`（UID 模式）不能同时指定（互斥校验）
- [ ] UID 模式下 `uid` > 0、`gid` >= 0（0=等于 uid）、`groups` 元素均 > 0（防负数回绕）
