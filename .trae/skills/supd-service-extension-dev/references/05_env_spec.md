# supd 环境变量规范指南 (env.yaml)

本参考文档包含 `env.yaml` 文件的语法格式、3 层环境变量合并逻辑及敏感词自动化安全掩码处理机制。

---

## 1. env.yaml 强制格式

在 supd 中，所有级别（全局/服务/扩展）的 `env.yaml` 文件**必须包含 `env:` 包装层**。直接写平铺的 `KEY: value` 会被配置解析器静默忽略。

### 标准格式示例：

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
2. **修改 env.yaml 后需重启服务生效**（热重载不更新已运行进程的环境变量）
3. **路径类环境变量使用绝对路径**：相对路径在不同用户（如 nobody）下可能解析失败
4. **扩展进程不继承服务的 env.yaml 变量**：扩展有自己的 env.yaml 层（4 层合并中的第 4 层）

---

## 2. 3 层环境变量合并规则

当启动服务进程或执行扩展任务时，系统会自动将 3 层环境变量进行合并（同名变量后者覆盖前者）：

```
[第 1 层：系统环境]  os.Environ() （底图）
        ↓ (覆盖)
[第 2 层：全局环境]  <baseDir>/env.yaml (或 cfg.EnvFiles 配置的文件)
        ↓ (覆盖)
[第 3 层：专有环境]  services/<svc>/env.yaml 或 extensions/<ext>/env.yaml
```

### 合并特征：
1. `enabled: false` 的变量在合并阶段会被跳过，不会注入到子进程环境变量中。
2. 继承原 `os.Environ()` 的环境变量，并在其基础上追加或覆盖。
3. `type=script` 的 readiness 检查脚本和 `run_as` 切换用户后的进程同样继承合并后的环境变量。

---

## 3. 敏感变量与密码自动隐藏 (IsSensitive)

当环境变量名称（Key）包含以下敏感关键词（不区分大小写）时，前端 UI 编辑器和交互界面会自动将其渲染为密码掩码输入框 (`type="password"`)：

```
PASSWORD
PWD
SECRET
TOKEN
KEY
```

例如：`MYSQL_PASSWORD`, `AUTH_TOKEN`, `SECRET_KEY`, `DB_PWD` 均会被自动判别为敏感词进行掩码保护。
