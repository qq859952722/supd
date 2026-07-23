---
name: "supd-service-extension-dev"
description: "supd 服务与扩展的开发、修改、打包、导入与在线开发一条龙流程指南。当用户要求创建新服务/扩展、为已有服务添加扩展、修改已有扩展或服务配置、打包/导入服务扩展，或编写 service.yaml/meta.yaml/run.sh 时调用。"
---

# supd 服务/扩展一条龙开发指南

本 Skill **完全自洽**，所有规格信息均内嵌于 `references/` 目录，无需读取项目其他文件即可独立使用。

---

## 何时触发

- 开发新服务（含 `service.yaml`）或新扩展（4 种触发器类型均支持）
- 为已有服务新增扩展（无需重启服务）
- 修改已有扩展或服务配置
- 打包或导入服务/扩展 `.tar.gz` 压缩包
- 配置在线开发（Dropbear SSH + HTTP API）

---

## 关键概念速查

**目录布局**（`<baseDir>` 为 supd 工作目录）：
```
<baseDir>/
├── env.yaml                               # 全局环境变量（可选）
├── services/
│   └── <svc-name>/
│       ├── service.yaml                   # 服务配置（必需）
│       ├── env.yaml                       # 服务环境变量（可选）
│       └── extensions/
│           └── <ext-name>/               # 服务级扩展（绑定该服务）
│               ├── meta.yaml
│               ├── run.sh
│               └── env.yaml
└── extensions/
    └── <ext-name>/                       # 全局扩展（跨服务通用）
        ├── meta.yaml
        ├── run.sh
        └── env.yaml
```

**4 种触发器**：`on_demand`（手动）/ `on_schedule`（cron）/ `service_lifecycle`（服务事件）/ `supd_lifecycle`（系统事件）  
**7 种服务状态**：`pending` → `starting` → `up` → `ready` → `stopping` → `down` / `failed`  
**4 种就绪检测**：`http_check` / `tcp_check` / `fd_notify` / `script`（枚举锁定不可新增）

---

## 一条龙开发七阶段流程

```
[1.需求确认] → [2.配置开发] → [3.自动校验] → [4.修改热重载]
                                                    │
[7.运行验证] ← [6.双次导入] ← [5.打包导出] ←────────┘
```

| 阶段 | 操作 | 参考资源 |
|---|---|---|
| 1. 需求确认 | 明确对象、触发器类型（4选1）、并发策略（4选1）、运行身份 | 本文档 |
| 2. 配置开发 | 编写 `service.yaml` / `meta.yaml` / `run.sh` | `references/01_service_spec.md`, `references/02_extension_spec.md` |
| 3. 自动校验 | 运行 `scripts/validate_dev.py` 自动排查格式错误 | `scripts/validate_dev.py` |
| 4. 修改热重载 | 理解各字段热重载生效时机，避免错误期望 | `references/03_modification_matrix.md` |
| 5. 打包导出 | 使用 `scripts/pack_dev.py` 打包为 `.tar.gz` | `scripts/pack_dev.py` |
| 6. 双次导入 | `POST /api/import` 预览 → `POST /api/import/confirm` 确认 | `references/04_online_dev_guide.md` |
| 7. 运行验证 | SSH 进入容器或调用 API 验证服务运行状态 | `references/04_online_dev_guide.md` |

---

## 辅助工具（可直接运行，无外部依赖）

```bash
# 校验服务或扩展目录（自动检查名称、权限、枚举、env.yaml 包装层等）
python3 <skill_dir>/scripts/validate_dev.py <service_or_extension_dir>

# 打包为符合规范的 .tar.gz（自动过滤 .git/data/logs 等垃圾文件）
python3 <skill_dir>/scripts/pack_dev.py <dir_path> [output.tar.gz]
```

---

## 核心约束（不可违反）

| 约束类型 | 枚举/数值 |
|---|---|
| 服务状态（7种） | `pending` / `starting` / `up` / `ready` / `stopping` / `down` / `failed` |
| 任务状态（7种） | `pending` / `running` / `success` / `failed` / `timeout` / `canceled` / `killed` |
| 触发器类型（4种） | `on_demand` / `on_schedule` / `service_lifecycle` / `supd_lifecycle` |
| 并发策略（4种） | `replace` / `serialize` / `parallel` / `debounce:Ns` |
| Readiness类型（4种） | `http_check` / `tcp_check` / `fd_notify` / `script` |
| 认证模式（3种） | `none` / `local_skip` / `always_token` |
| button_style（3种） | `primary` / `default` / `danger` |
| fsnotify 防抖 | `500ms` |
| 扩展默认/硬上限 timeout | `600s` / `1800s` |
| stop 默认 grace/timeout | `10s` / `60s` |
| 上传大小限制 | `100MB` |
| 禁止引入 | 数据库（SQLite/Bolt 等）、SSE、WebSocket |

---

## 结构化参考手册（按需调阅）

> 所有规格自洽于 `references/` 内，不依赖项目外部文档。

| 细分领域 | 文件 | 包含内容 |
|---|---|---|
| 服务配置与状态机 | `references/01_service_spec.md` | `service.yaml` 全字段、4 种 Readiness 配置、7 状态机、检查清单 |
| 扩展配置与协议 | `references/02_extension_spec.md` | `meta.yaml` 全字段、4 种触发器示例、14 个 `SUPD_*` 变量、stdout 协议 |
| 修改与热重载矩阵 | `references/03_modification_matrix.md` | 为已有服务添加扩展流程、服务/扩展热重载行为矩阵 |
| 在线开发（SSH+API） | `references/04_online_dev_guide.md` | Dropbear SSH 端口 2222、HTTP API 端点、8 步开发示例 |
| 环境变量（env.yaml） | `references/05_env_spec.md` | 强制 `env:` 包装层、3 层合并规则、敏感词自动掩码 |

---

## 示例索引（examples/）

| 目录 | 类型 | 覆盖特性 |
|---|---|---|
| `examples/01-simple-service/` | 简单服务 | `http_check` readiness、Python HTTP 服务、stop/logging 配置 |
| `examples/02-complex-service/` | 复杂服务 | `tcp_check` readiness、`autostart`、command 数组、tags |
| `examples/03-on-demand-ext/` | 手动扩展 | `on_demand`、多 action、action args、`button_style` |
| `examples/04-scheduled-ext/` | 定时扩展 | `on_schedule` cron、单 action |
| `examples/05-service-lifecycle-ext/` | 服务生命周期扩展 | `post_ready`/`on_failure`/`pre_stop` 三种钩子 |
| `examples/06-supd-lifecycle-ext/` | 系统生命周期扩展 | `pre_start`/`post_ready`/`pre_shutdown` 钩子、parallel 并发 |
| `examples/07-health-check-ext/` | stdout 协议扩展 | 混合触发、`::progress::`/`::result::` 协议、replace 并发 |
| `examples/08-stats-report-ext/` | 定时+手动混合扩展 | 完整 stdout 协议、API 调用示例 |
