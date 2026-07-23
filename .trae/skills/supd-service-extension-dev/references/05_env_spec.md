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
