# supd 服务与扩展修改、维护及热重载行为矩阵

本参考文档包含为已有服务添加扩展、修改已有服务/扩展的完整流程、热重载生效矩阵及配置同步关联规则。

---

## 1. 为已有服务添加扩展

**目标**：目标服务 `<svc>` 已在运行中，需为其新增一个扩展而**不重启该服务**。

### 步骤流程：
1. **确定扩展类型与路径**：
   - 服务级扩展：`<baseDir>/services/<svc>/extensions/<ext-name>/`（推荐，随服务一同导出打包）。
   - 全局扩展：`<baseDir>/extensions/<ext-name>/`（跨服务通用）。
2. **创建目录与文件**：
   - 包含 `meta.yaml` 及入口脚本 `run.sh`。
   - 确保入口脚本添加可执行权限：`chmod +x run.sh`。
3. **编写脚本逻辑**：
   - 脚本通过环境变量 `$SUPD_SERVICE`、`$SUPD_SERVICE_DIR` 读取服务上下文。
   - 避免硬编码端口，优先读取服务环境或配置。
4. **验证热重载自动注册**：
   - 新增扩展目录被系统 `fsnotify`（500ms 防抖）自动感知注册，**无需重启 supd** 亦**无需重启运行中的服务**。

---

## 2. 热重载行为对照矩阵

`supd` 包含基于 `fsnotify` 的实时监控与防抖重载机制。不同配置项变更后的生效模式如下：

### 2.1 扩展 (Extension) 热重载矩阵

| 修改对象 | 生效方式 | 是否影响运行中的任务 |
|---|---|---|
| `meta.yaml` 元数据（description/ui/icon） | 热重载，即时生效 | 否 |
| `meta.yaml` triggers 触发器配置 | 热重载，即时生效 | 否（已在运行任务继续） |
| `meta.yaml` actions 动作定义 | 热重载，即时生效 | 否 |
| `meta.yaml` concurrency 并发策略 | 热重载，即时生效 | 否（已在运行任务按旧策略） |
| `meta.yaml` timeout_seconds | 热重载，即时生效 | 否 |
| `meta.yaml` enabled 禁用/启用状态 | 热重载，即时生效 | 否（已运行任务不强杀） |
| `meta.yaml` run_as 运行身份 | 热重载，下次执行生效 | 否 |
| `meta.yaml` run_as_uid/run_as_gid/run_as_groups（UID 模式） | 热重载，下次执行生效 | 否 |
| `run.sh` 入口脚本内容 | **下次执行时自动生效** | 否（已运行任务使用旧脚本） |
| `entry` 入口路径 / `runtime` | 热重载，下次执行生效 | 否 |

### 2.2 服务 (Service) 热重载矩阵

| 修改对象 | 生效方式 | 是否影响运行中的服务进程 |
|---|---|---|
| 元数据（description/icon/tags） | 热重载，即时生效 | 否 |
| `depends_on` 依赖关系 | 热重载，即时生效 | 否 |
| `readiness` 检测配置 | 热重载，**下次启动生效** | 否 |
| `command` / `args` / `runtime` | 热重载，**标记为待重启** | **否**（必须手动 stop→start 或 restart 生效） |
| `stop` / `restart` 策略 | 热重载，下次停止/重启生效 | 否 |
| `logging` 配置 | 热重载，即时生效 | 否 |
| `user` / `group` 运行身份（User 模式） | 热重载，**下次启动生效** | 否 |
| `uid` / `gid` / `groups` 运行身份（UID 模式） | 热重载，**下次启动生效** | 否 |

> **关键原则**：根据规格说明书，热重载绝不会主动强杀重启正在正常运行的服务进程。命令/环境类变更仅标记为“待重启”，等待用户手动重启后生效。

---

## 3. 修改场景同步检查矩阵

修改配置或脚本时，必须同步检查并更新相关的关联位置：

| 修改对象 | 必须同步检查的位置 | 常见错误风险 |
|---|---|---|
| **服务监听端口** | `service.yaml` 的 `readiness.port` + **所有扩展脚本中的端口变量** | 导致 readiness 校验失败或扩展连不上服务 |
| **启动命令** | `command` + `args` + `signals` | 造成进程启动命令不符合预期 |
| **服务名/扩展名** | 对应目录名称 + YAML 中的 `name` 字段 | 目录名与 YAML `name` 不一致会导致校验拒绝 |
| **脚本路径** | YAML 中的 `entry` + 实际磁盘路径 + `chmod +x` 权限 | 造成 `permission denied` 或 `file not found` |
| **超时时间** | `timeout_seconds` ≤ 1800 硬上限 | 超出 1800s 校验报错 |
| **执行身份** | `user`/`group`（User 模式）与 `uid`/`gid`/`groups`（UID 模式）互斥；扩展 `run_as` 与 `run_as_uid` 互斥 | 同时指定两种模式校验报错；负数 uid/gid 校验报错 |
| **服务环境变量** | `services/<svc>/env.yaml` + `run.sh` 中的环境变量读取 | 单改 YAML 未在脚本中消费 |
