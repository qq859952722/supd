---
name: "supd-service-extension-dev"
description: "supd服务与扩展开发指南。当用户要求开发、修改、打包、导入supd服务或扩展，或编写service.yaml、meta.yaml、run.sh、run.js时触发本技能。包含开发规范、枚举约束、环境变量限制、热重载规则及tjs运行时配置等核心知识。"
---

# supd 服务与扩展开发技能 (AI Agent Instruction)

> **IMPORTANT**: 本 Skill 是完全自洽的。处理 `supd` 服务和扩展开发任务时，**禁止**瞎猜字段或依赖外部网络搜索，你**必须**严格遵循本技能的规范，并优先查阅本目录下的 `references/` 规格文档。

## 🎯 何时触发 (Trigger Scenarios)

当识别到用户意图包含以下操作时，必须参考本技能指南：
1. **新建或修改服务**: 编写 `service.yaml`、配置 `http_check`/`tcp_check` 等。
2. **新建或修改扩展**: 编写 `meta.yaml`、Shell 脚本 (`run.sh`) 或 TJS 脚本 (`run.js`)。
3. **打包与导入**: 将服务/扩展打包为 `.tar.gz`，或通过 API 导入/导出。
4. **环境变量配置**: 编写或修改服务/扩展的 `env.yaml`。
5. **行为排查**: 理解扩展触发器 (trigger)、并发策略 (concurrency)、或热重载行为时。

---

## 📚 知识库导航 (Knowledge Base) — 按需读取

> **原则**：以下参考文档按需读取，不要一次性全量加载。根据当前任务类型，仅读取对应行标注的文档。本 SKILL.md 已包含日常开发所需的核心约束，简单任务可直接基于此编码 + 运行校验脚本。

| 何时读取 | 参考文档 | 覆盖内容 |
|:---|:---|:---|
| 编写/修改 `service.yaml` | `references/01_service_spec.md` | 4 种 Readiness 配置、状态机 11 条转移规则、restart 策略、signals、stop/logging、检查清单 |
| 编写/修改 `meta.yaml` | `references/02_extension_spec.md` | 4 种触发器、stdout 通信协议、14 个 SUPD_* 环境变量、retry_on_failure、entry 路径安全 |
| 修改配置后问"何时生效" | `references/03_modification_matrix.md` | 热重载行为矩阵（哪些字段热生效、哪些需重启服务、哪些需重启 supd） |
| 在线开发/SSH/HTTP API | `references/04_online_dev_guide.md` | Dropbear SSH 配置、CLI 命令、76 个 API 端点对照表、导入导出流程 |
| 编写/修改 `env.yaml` | `references/05_env_spec.md` | 4 层环境变量合并规则、env.yaml 结构体格式、密码字段处理 (**必读**，极易出错) |
| `runtime: tjs` 时 | `references/06_tjs_runtime_guide.md` | tjs API 速查、run.js 模板、fetch 流式下载、WASM 工具调用、常见坑点排查 |

### 工具脚本

| 脚本 | 用途 | 调用方式 |
|:---|:---|:---|
| `scripts/validate_dev.py` | 本地校验服务/扩展配置（version 正则、entry 安全、actions、枚举等） | `python3 scripts/validate_dev.py <target_dir>` |
| `scripts/pack_dev.py` | 打包服务/扩展为 `.tar.gz`（支持 `--profile` 导出规则） | `python3 scripts/pack_dev.py <dir> [output] [--profile <name>]` |

### WASM 资产

`assets/wasm/` 内含经 txiki.js v26.6.0 实测、可直接部署的 WASI CLI 工具（`zstd.wasm` / `bsdtar.wasm`），详见 `references/06_tjs_runtime_guide.md` §11.2。

---

## 🛠️ 核心约束与易错点 (Critical Constraints)

严格遵守以下枚举和数值，**禁止**自行新增：

| 约束类型 | 允许的枚举值 / 强制约束 |
| :--- | :--- |
| **服务状态** (7种) | `pending` / `starting` / `up` / `ready` / `stopping` / `down` / `failed` |
| **任务状态** (7种) | `pending` / `running` / `success` / `failed` / `timeout` / `canceled` / `killed` |
| **触发器类型** (4种) | `on_demand` / `on_schedule` / `service_lifecycle` / `supd_lifecycle` |
| **并发策略** (4种) | `replace` / `serialize` / `parallel` / `debounce:Ns`（N 为正整数，上限 3600） |
| **Readiness类型** (4种) | `http_check` / `tcp_check` / `fd_notify` / `script` |
| **重启策略** (3种) | `always` / `on-failure` / `never`（`max_backoff_ms` 须 ≥ `backoff_ms`） |
| **认证模式** (3种) | `none` / `local_skip` / `always_token` |
| **按钮样式** (3种) | `primary` / `default` / `danger` (用于 `on_demand` 扩展) |
| **版本号格式** | 必须匹配 `^[0-9]+\.[0-9]+\.[0-9]+$`（三段数字，如 `1.0.0`） |
| **entry 路径安全** | 禁止 `..`、shell 元字符（``; | & $ ` ( ) { }``）、冗余 `./` 前缀 |
| **数值限制** | fsnotify防抖 `500ms` / stop grace `10s` / 扩展硬上限 `1800s` / 上传限制 `100MB` / serialize队列上限 `16` |
| **禁止引入** | 数据库 (SQLite/Bolt 等)、SSE (Server-Sent Events)、WebSocket |

> **⚠️ 环境变量 (`env.yaml`) 致命陷阱**：
> 1. 必须有 `env:` 作为顶层键。
> 2. 每个变量必须是对象格式：`KEY: { value: "..." }`，**绝对禁止**写成 `KEY: "value"` (会导致静默失效)！

---

## 💡 开发工作流 (Development Workflow)

1. **确认需求**: 明确开发对象是服务还是扩展。若是扩展，确定触发器类型与并发策略。
2. **规划文件布局**: 按 `bin/` + `data/` 目录规范组织服务（详见 `references/01_service_spec.md` §1）。生成服务时**必须**同时在服务根目录创建中文 `README.md`，按规范说明服务、目录权限、启动就绪、环境变量、扩展 actions、持久化升级、运维和安全备份，且不得写入真实密码、token、私钥或运行期敏感数据。评估服务是否支持二进制更新，若支持则规划更新扩展。
3. **查阅规格与示例**: 阅读 `references/` 中的规范，并在 `examples/` 中寻找可用模板。
4. **生成代码**: 严格按照规范编写配置文件、README 和代码。
5. **自动校验**: 运行 Python 脚本排查低级格式错误（需传入目标服务或扩展目录路径）：
   ```bash
   python3 .trae/skills/supd-service-extension-dev/scripts/validate_dev.py <target_dir>
   ```
6. **打包导出** (如需): 按导出场景选择默认导出或按规则文件导出：
   ```bash
   python3 .trae/skills/supd-service-extension-dev/scripts/pack_dev.py <target_dir> [output.tar.gz]
   ```

---

## 📂 示例代码库 (Examples)

需要代码模板时，请先查阅 `examples/README.md`，或直接参考以下分类：

- **服务类**: `01-simple-service/`, `02-complex-service/`
- **扩展类 (Shell)**: `03-on-demand-ext/`, `04-scheduled-ext/`, `05-service-lifecycle-ext/`, `06-supd-lifecycle-ext/`
- **扩展进阶**: `07-health-check-ext/`, `08-stats-report-ext/`
- **扩展 (TJS)**: `09-tjs-ext/`, `10-binary-updater-ext/`（二进制更新扩展：流式下载 + 原子替换 + 版本检测）
