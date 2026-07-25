# supd 在线开发指南（SSH + HTTP API 混合方案）

本参考文档包含在 NAS 或远程容器环境中，基于 SSH (Dropbear) 与 HTTP API 开展无本地 Go 源码在线开发的架构、工作流与端点对照表。

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

## 2. Dropbear SSH 服务与认证配置

容器内置 Dropbear SSH 服务器，端口为 2222。Dropbear 作为 supd 管理的**普通服务**运行（`services/dropbear-ssh/`），默认由 `supd init` 生成。

- **默认设置**：`autostart: false`（默认关闭，通过 Web UI 或 API 显式启动）。
- **运行身份**：`run_as: root`（需 root 权限处理 host key 生成与用户登录切换）。
- **认证模式**：由 `services/dropbear-ssh/env.yaml` 中的 `SSH_PUBLIC_KEY` 环境变量控制。

### 认证配置 (services/dropbear-ssh/env.yaml)：
```yaml
env:
  # 1. 公钥认证模式（推荐）
  SSH_PUBLIC_KEY:
    value: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... your@email"

  # 2. 空白密码免认证模式（仅限可信局域网）：值留空即可
```

修改 `env.yaml` 后重启 `dropbear-ssh` 服务生效。

---

## 3. 在线开发核心 HTTP API 端点

### 3.1 文件操作 API
- `GET /api/files/tree?path=services/` - 浏览目录树
- `GET /api/files?path=...` - 读取文件内容
- `PUT /api/files` - 写入文件（body: `{path, content}`，自动保留 50 版本历史）
- `POST /api/files/validate` - 校验 YAML 语法
- `POST /api/files/upload` - 上传文件/二进制

### 3.2 服务管理 API
- `GET /api/services` - 列出所有服务
- `POST /api/services` - 创建新服务
- `POST /api/services/{name}/start` - 启动服务
- `POST /api/services/{name}/stop` - 停止服务
- `POST /api/services/{name}/restart` - 重启服务
- `GET /api/services/{name}/logs` - 查看服务日志

### 3.3 扩展管理 API
- `GET /api/extensions` - 列出全局扩展
- `POST /api/extensions` - 创建全局扩展
- `POST /api/extensions/{name}/run` - 手动触发扩展
- `GET /api/extensions/runs/{run_id}` - 查看扩展运行状态与结果
- `GET /api/extensions/runs/{run_id}/logs` - 查看扩展运行输出日志

---

## 4. 8 步在线开发工作流示例

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

# 4. 创建服务扩展
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

# 6. 触发扩展并查看日志
RUN_ID=$(curl -s -X POST "$API/extensions/check-health/run" | jq -r .run_id)
curl "$API/extensions/runs/$RUN_ID"
```
