# supd 在线开发指南（SSH + HTTP API 混合方案）

本参考文档包含在 NAS 或远程容器环境中，基于 SSH (Dropbear) 与 HTTP API 开展无本地 Go 源码在线开发的架构、CLI 命令、HTTP API 端点对照表与 Dropbear SSH 服务配置。

---

## 1. 在线开发架构

```
开发者 / 智能 IDE（Trae / Cursor / VS Code）
    ├── SSH/SFTP ──→ NAS 上的 supd 容器（端口 2222，文件编辑）
    └── HTTP API ──→ NAS 上的 supd:7979（服务/扩展控制 + 文件操作）
```

- **SSH/SFTP**：用于远程文件编辑、代码补全、YAML 高亮与代码同步。
- **HTTP API**：用于管理服务与扩展生命周期（创建、启动、停止、触发、日志查看、导出导入）。

---

## 2. supd CLI 命令

容器内 `supd` 二进制支持以下子命令（本地开发也可用）：

| 命令 | 说明 |
|---|---|
| `supd init` | 初始化 `<baseDir>`，生成默认配置与示例服务（含 `dropbear-ssh`），可用 `--dry-run` 预览、`--force` 覆盖 |
| `supd run` | 启动 supd 主进程（默认行为；不带子命令时等同 `supd run`） |
| `supd run --log-level debug` | 以 debug 日志级别启动 |
| `supd run --no-pid1` | 禁用 PID 1 子进程回收（**Docker 容器内禁用**，仅用于调试） |
| `supd run --workdir <path>` | 指定工作目录 |
| `supd validate [path] [-o json]` | 校验 `config.yaml` 语法（**仅支持 config.yaml，不直接校验服务/扩展目录**；服务/扩展用本 skill 的 `scripts/validate_dev.py`） |
| `supd runtimes list` | 列出已注册运行时（含 source/available） |
| `supd runtimes install <name> <path>` | 注册运行时别名（写入 `config.yaml` 的 `runtimes` 映射；`path` 必须是绝对路径，不下载二进制） |
| `supd runtimes remove <name>` | 移除运行时别名（从 `config.yaml` 删除映射，不删除二进制文件） |

> 启动常用：`SUPD_LOG_DIR=/tmp/supd-logs ./supd --workdir test_workdir run`

---

## 3. Dropbear SSH 服务配置

容器内置 Dropbear SSH 服务器，端口 2222。Dropbear 作为 supd 管理的**普通服务**运行（`services/dropbear-ssh/`），由 `supd init` 自动生成。

- **默认设置**：`autostart: false`（默认关闭，通过 Web UI 或 API 显式启动）。
- **运行身份**：`run_as: root`（需 root 权限处理 host key 生成与用户登录切换；非 root 时 dropbear 启动失败）。
- **就绪检测**：`tcp_check` 端口 2222。
- **host key**：由 `dropbear -R` 在首次启动时动态生成（每容器独立，避免镜像硬编码共享密钥）。
- **认证模式**：由 `services/dropbear-ssh/env.yaml` 中的 `SSH_PUBLIC_KEY` 环境变量控制。

### 认证配置 (services/dropbear-ssh/env.yaml)

```yaml
# dropbear-ssh 服务私有环境变量
# 规格 §2.2.4: 此文件由 supd 注入到 dropbear-ssh 服务进程环境
# 修改后需重启 dropbear-ssh 服务生效

env:
  # SSH_PUBLIC_KEY — SSH 公钥内容
  # 留空（默认）→ 空白密码免认证模式（dropbear -B，仅内网可信场景）
  # 填入公钥  → 公钥认证模式（dropbear -s 禁用密码登录）
  # 多个公钥用换行分隔；支持 ssh-ed25519/ssh-rsa/ecdsa-sha2-nistp256 等格式
  SSH_PUBLIC_KEY:
    value: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... your@email"
    hint: "SSH 公钥内容，留空=空白密码免认证，填入=公钥认证"

  # DROPBEAR_PORT — dropbear 监听端口（默认 2222，避开宿主机 22）
  DROPBEAR_PORT:
    value: "2222"
    hint: "dropbear 监听端口"
```

修改 `env.yaml` 后重启 `dropbear-ssh` 服务生效：

```bash
curl -X POST "$API/services/dropbear-ssh/stop"
curl -X POST "$API/services/dropbear-ssh/start"
```

> 启动 dropbear-ssh 必须以 root 运行 supd（或 `run_as: root`）；非 root 环境下 `run.sh` 会主动 `exit 1` 报错。`run.sh` 同时会自动为 `supd` 与 `root` 用户写入 `authorized_keys` 或清空密码。

---

## 4. HTTP API 端点完整对照表

### 4.1 基础与认证

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | 健康检查（返回 `{"status":"ok"}`） |
| POST | `/api/auth/verify` | 认证令牌校验 |

### 4.2 文件操作 API（`/api/files`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/files/tree?path=<rel>` | 浏览目录树 |
| GET | `/api/files?path=<rel>` | 读取文件内容 |
| PUT | `/api/files` | 写入文件（body: `{path, content}`，自动保留 50 版本历史） |
| POST | `/api/files` | 创建文件 |
| DELETE | `/api/files?path=<rel>` | 删除文件 |
| POST | `/api/files/move` | 移动/重命名文件 |
| POST | `/api/files/upload?path=<rel>` | 上传文件/二进制（multipart） |
| GET | `/api/files/history?path=<rel>` | 文件历史版本列表 |
| POST | `/api/files/rollback` | 回滚文件到指定历史版本 |
| POST | `/api/files/validate` | 校验 YAML 语法 |
| POST | `/api/files/snapshot` | 创建文件快照 |

### 4.3 服务管理 API（`/api/services`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/services` | 列出所有服务 |
| POST | `/api/services` | 创建新服务 |
| POST | `/api/services/import` | 导入服务包（上传 tar.gz） |
| POST | `/api/services/import/confirm` | 确认导入（处理冲突） |
| POST | `/api/services/start` | 批量启动所有服务 |
| POST | `/api/services/stop` | 批量停止所有服务 |
| GET | `/api/services/{name}` | 获取服务详情 |
| PUT | `/api/services/{name}` | 更新服务 |
| DELETE | `/api/services/{name}` | 删除服务 |
| POST | `/api/services/{name}/start` | 启动服务 |
| POST | `/api/services/{name}/stop` | 停止服务 |
| POST | `/api/services/{name}/restart` | 重启服务 |
| POST | `/api/services/{name}/signal` | 发送自定义信号（body: `{"signal":"HUP"}`） |
| POST | `/api/services/{name}/force-stop` | 强制停止服务（SIGKILL） |
| POST | `/api/services/{name}/clear-failed` | 清除 `failed` 状态，重置为 `down`（非 failed 状态调用返回 `400 INVALID_REQUEST`；重置后需显式 `start` 启动，不触发下游依赖唤醒） |
| PUT | `/api/services/{name}/config` | 更新 service.yaml 配置 |
| PUT | `/api/services/{name}/env` | 保存服务 env.yaml |
| GET | `/api/services/{name}/logs` | 查看服务日志 |
| GET | `/api/services/{name}/logs/search` | 搜索服务日志（上限 1000 行） |
| GET | `/api/services/{name}/resources` | 服务资源占用（CPU/内存） |
| GET | `/api/services/{name}/processes` | 服务进程树（含端口） |
| GET | `/api/services/{name}/history` | 服务状态变更历史 |
| GET | `/api/services/{name}/deaths` | 服务死亡记录 |
| GET | `/api/services/{name}/export?profile=<name>` | 导出服务包（可选 profile） |
| GET | `/api/services/{name}/export-profiles` | 列出可用 profile |

### 4.4 服务级扩展 API（`/api/services/{name}/extensions`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/services/{name}/extensions` | 列出服务级扩展 |
| POST | `/api/services/{name}/extensions` | 创建服务级扩展 |
| GET | `/api/services/{name}/extensions/{ext}` | 获取扩展详情 |
| PUT | `/api/services/{name}/extensions/{ext}` | 更新扩展 |
| DELETE | `/api/services/{name}/extensions/{ext}` | 删除扩展 |
| PUT | `/api/services/{name}/extensions/{ext}/env` | 保存扩展 env.yaml |
| POST | `/api/services/{name}/extensions/{ext}/run` | 手动触发服务级扩展 |

### 4.5 全局扩展 API（`/api/extensions`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/extensions` | 列出全局扩展 |
| POST | `/api/extensions` | 创建全局扩展 |
| POST | `/api/extensions/import` | 导入扩展包（上传 tar.gz） |
| POST | `/api/extensions/import/confirm` | 确认导入 |
| GET | `/api/extensions/{name}` | 获取扩展详情 |
| PUT | `/api/extensions/{name}` | 更新扩展 |
| DELETE | `/api/extensions/{name}` | 删除扩展 |
| PUT | `/api/extensions/{name}/env` | 保存扩展 env.yaml |
| POST | `/api/extensions/{name}/run` | 手动触发扩展（异步，立即返回 `state=running` 和 `run_id`） |
| GET | `/api/extensions/{name}/status` | 获取扩展运行状态 |
| GET | `/api/extensions/{name}/export` | 导出扩展包 |

### 4.6 扩展运行历史 API（`/api/extensions/runs`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/extensions/runs` | 列出运行历史（支持过滤） |
| DELETE | `/api/extensions/runs` | 清空运行历史 |
| GET | `/api/extensions/runs/{runID}` | 查看运行状态与结果 |
| GET | `/api/extensions/runs/{runID}/logs` | 查看运行输出日志 |
| DELETE | `/api/extensions/runs/{runID}/logs` | 删除运行日志 |
| POST | `/api/extensions/runs/{runID}/cancel` | 取消运行中的任务 |

### 4.7 其他 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/cron` | 列出定时任务 |
| GET | `/api/cron/history` | 定时任务执行历史 |
| GET | `/api/events` | 事件流长轮询（全局并发 50 / 单客户端 5） |
| GET | `/api/settings` | 获取全局设置 |
| PUT | `/api/settings` | 更新全局设置 |
| GET | `/api/settings/env` | 获取全局 env 配置 |
| PUT | `/api/settings/env` | 更新全局 env 配置 |
| GET | `/api/settings/runtimes` | 获取运行时配置 |
| PUT | `/api/settings/runtimes` | 更新运行时配置 |
| GET | `/api/runtimes` | 列出已注册运行时 |
| POST | `/api/runtimes/upload` | 上传运行时二进制 |
| DELETE | `/api/runtimes/{name}` | 删除运行时 |
| GET | `/api/system/status` | 系统状态 |
| GET | `/api/system/diagnostic` | 诊断信息（脱敏） |
| GET | `/api/system/events/recent` | 最近 200 条事件 |
| POST | `/api/reload` | 触发热重载 |

---

## 5. 8 步在线开发工作流示例

```bash
API="http://<NAS-IP>:7979/api"

# 1. 创建服务
curl -X POST "$API/services" -H "Content-Type: application/json" -d '{
  "name": "demo-app",
  "version": "1.0.0",
  "command": ["python3", "app.py"],
  "readiness": {"type": "http_check", "url": "http://127.0.0.1:8080/health", "expected_status": 200}
}'

# 2. 上传服务启动代码
curl -X POST "$API/files/upload?path=services/demo-app/app.py" -F "file=@./app.py"

# 3. 启动服务
curl -X POST "$API/services/demo-app/start"

# 4. 创建服务级扩展（关联由目录位置决定）
curl -X POST "$API/services/demo-app/extensions" -H "Content-Type: application/json" -d '{
  "name": "check-health",
  "version": "1.0.0",
  "runtime": "bash",
  "entry": "run.sh",
  "timeout_seconds": 30,
  "triggers": {"on_demand": true}
}'

# 5. 上传扩展入口脚本
curl -X POST "$API/files/upload?path=services/demo-app/extensions/check-health/run.sh" -F "file=@./run.sh"

# 6. 触发扩展并查看日志（on_demand 异步执行）
RUN_ID=$(curl -s -X POST "$API/services/demo-app/extensions/check-health/run" | jq -r .run_id)
curl "$API/extensions/runs/$RUN_ID"
curl "$API/extensions/runs/$RUN_ID/logs"

# 7. 修改 env.yaml 后重启服务生效
curl -X PUT "$API/services/demo-app/env" -H "Content-Type: application/json" -d '{
  "env": {"PORT": {"value": "8080", "hint": "服务监听端口"}}
}'
curl -X POST "$API/services/demo-app/restart"

# 8. 导出服务包（按 profile）
curl -o demo-app.tar.gz "$API/services/demo-app/export?profile=migrate"
```

> **认证模式**：默认 `local_skip`（局域网免认证）；如设为 `always_token`，所有非 GET 请求需带 `Authorization: Bearer <token>` 头。
