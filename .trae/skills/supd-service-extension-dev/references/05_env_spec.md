# supd 环境变量规范指南 (env.yaml)

本参考文档包含 `env.yaml` 文件的语法格式、4 层环境变量合并逻辑及敏感词自动化安全掩码处理机制。

---

## 1. env.yaml 强制格式

在 supd 中，所有级别（全局/服务/扩展）的 `env.yaml` 文件**必须包含 `env:` 包装层**。直接写平铺的 `KEY: value` 会被配置解析器静默忽略（变量 `Value` 字段为空，等价于未设置）。

### 标准格式示例

```yaml
env:
  # 必须包含 env: 顶层键
  DATABASE_URL:
    value: "postgres://user:pass@localhost:5432/dbname"
    enabled: true        # 可选，默认 true。设置为 false 时不会注入子进程
    hint: "数据库连接串"   # 可选，前端 UI 提示说明

  API_KEY:
    value: "sk-1234567890"
    # enabled 缺省默认为 true

  DEBUG_MODE:
    value: "false"
    enabled: false       # 禁用该变量注入
```

### ⚠️ 常见错误：简单键值对格式（变量不生效）

```yaml
# ❌ 错误写法 — Value 字段为空，变量不会注入子进程
env:
  TRANSMISSION_HOME: /path/to/config
  API_KEY: sk-1234567890

# ✅ 正确写法 — 必须使用结构体格式，值写在 value: 字段下
env:
  TRANSMISSION_HOME:
    value: /path/to/config
  API_KEY:
    value: sk-1234567890
```

**原因**：supd 内部将每个变量解析为 `EnvVar` 结构体（含 `value`/`enabled`/`hint` 字段），简单键值对格式会导致 `value` 字段为空字符串，`ToInjectEnv` 返回空值或注入失败。此问题**无报错、静默失败**，极易踩坑。

### 服务通过 env.yaml 配置运行时参数的最佳实践

1. **服务环境变量放在 `env.yaml`，不要通过命令行传递**（如 `/usr/bin/env` 包装），这是 supd 的标准机制
2. **修改 env.yaml 后需重启服务生效**（热重载不更新已运行进程的环境变量；`type=script` readiness 脚本和 `run_as` 切换用户后的进程同样使用合并后的环境变量）
3. **路径类环境变量使用绝对路径**：相对路径在不同用户（如 nobody）下可能解析失败
4. **扩展进程不继承服务的 env.yaml 变量**：扩展有自己的 env.yaml 层（见下方"4 层合并规则"）
5. **env.yaml 可被 env 目录中的多个文件覆盖**：全局 env 目录 `env/*.yaml` 按文件名字母序加载，后者覆盖前者

---

## 2. 4 层环境变量合并规则

supd 扩展环境采用“`os.Environ()` 系统底图 + 最多 4 层 env 文件覆盖 + `SUPD_*` 最终覆盖”。服务进程使用系统底图、显式全局 env 文件和服务 env。所有场景均为同名变量后者覆盖前者，包括 `enabled` 状态。

### 2.1 服务进程（BuildServiceProcessEnv）

```
[第 0 层：系统底图]  os.Environ()
        ↓ (覆盖)
[第 1 层：全局 env 文件]  config.yaml 的 env_files 列表（按列表顺序加载，后者覆盖前者；可用相对或绝对路径）
        ↓ (覆盖)
[第 2 层：服务 env]  <baseDir>/services/<svc>/env.yaml
```

> 服务进程**不包含**扩展私有 env 层（扩展 env 仅在扩展执行时注入）。

### 2.2 扩展进程（buildMergedEnv）

```
[第 0 层：系统底图]  os.Environ()
        ↓ (覆盖)
[第 1 层：全局 env 文件]  <baseDir>/env/*.yaml（按文件名字母序加载）
        ↓ (覆盖)
[第 2 层：全局扩展私有 env]  <baseDir>/extensions/<ext>/env.yaml
        ↓ (覆盖)
[第 3 层：服务 env]  <baseDir>/services/<svc>/env.yaml
        ↓ (覆盖)
[第 4 层：服务级扩展私有 env]  <baseDir>/services/<svc>/extensions/<ext>/env.yaml
```

> 扩展进程在上述合并之后，再追加 `SUPD_*` 上下文环境变量（`SUPD_*` 可覆盖同名既有变量）。

### 2.3 合并特征

1. `enabled: false` 的变量在合并阶段会被跳过，不会注入到子进程环境变量中。
2. 继承原 `os.Environ()` 的环境变量，并在其基础上追加或覆盖；同名变量就地覆盖以保留原顺序。
3. `type=script` 的 readiness 检查脚本和 `run_as` 切换用户后的进程同样继承合并后的环境变量。
4. 全局 env 目录 `<baseDir>/env/` 中可放多个 `*.yaml` / `*.yml` 文件（如 `00-base.yaml`、`10-logging.yaml`），按文件名字母序加载，后者覆盖前者；适合按主题分组管理全局变量。

### 2.4 全局 env 文件配置

`config.yaml` 的 `env_files` 字段（列表）用于显式指定服务进程使用的全局 env 文件路径和顺序（相对路径以 `<baseDir>` 为根，也允许绝对路径）；默认配置通常为 `["env/00-base.yaml"]`，但运行时收到空列表时不会自动扫描 `env/`。该字段不影响扩展合并：扩展始终扫描 `<baseDir>/env/` 下所有 `.yaml` 和 `.yml` 文件并按文件名字母序加载。

---

## 3. 敏感变量与密码自动隐藏 (IsSensitive)

当环境变量名称（Key）包含以下敏感关键词（**不区分大小写**，使用 `strings.Contains` 子串匹配）时，前端 UI 编辑器和交互界面会自动将其渲染为密码掩码输入框 (`type="password"`)：

```
PASSWORD
PWD
SECRET
TOKEN
KEY
```

例如：`MYSQL_PASSWORD`, `AUTH_TOKEN`, `SECRET_KEY`, `DB_PWD`, `API_KEY` 均会被自动判别为敏感词进行掩码保护。

> 注：因采用子串匹配，变量名包含 `KEYWORD`、`KEYS` 等也会触发掩码；这是启发式识别，无白名单豁免。
