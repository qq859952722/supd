# supd 项目 AI 协作协议（维护阶段）

> 本协议适用于 supd 项目 **bug修复、细节打磨、逻辑测试** 阶段。
> 所有功能开发已完成（57个Task全部实现），当前工作聚焦于修复和改进。

---

## 一、项目当前状态

- **阶段**：维护/修复阶段（初始开发100%完成）
- **代码状态**：全部Epic已完成，go build/vet/test全部通过，pnpm build通过
- **核心文档**：`docs/需求规格说明_v1.5.md`（业务规则唯一权威来源）
- **开发日志**：`docs/devlog/session-notes.md`（当前状态与上下文，每次会话必读）

---

## 二、会话启动流程

每次新会话**必须先读取**以下文件恢复上下文，再开始工作：

1. `docs/devlog/session-notes.md` — 当前工作状态、未完成事项、注意事项
2. `docs/devlog/blockers.md` — 是否有未解决的阻断问题

---

## 三、核心约束（不可违反）

### 3.1 枚举值（禁止新增成员）

| 枚举类型 | 固定值 | 规格来源 |
|---|---|---|
| 服务状态 | pending/starting/up/ready/stopping/down/failed（7种） | §2.1.1 |
| 任务状态 | pending/running/success/failed/timeout/canceled/killed（7种） | §2.2.10 |
| 触发器类型 | on_demand/on_schedule/service_lifecycle/supd_lifecycle（4种） | §2.2.3 |
| 并发策略 | replace/serialize/parallel/debounce:Ns（4种） | §2.2.7 |
| Readiness类型 | fd_notify/tcp_check/http_check/script（4种） | §2.1.6 |
| 认证模式 | none/local_skip/always_token（3种） | §2.7.1 |
| 事件类型 | 14种（见§2.9.7），禁止新增 | §2.9.7 |
| button_style | primary/default/danger（3种） | §2.2.3 |
| 错误码 | 22个（见§5.4） | §5.4 |

### 3.2 关键数值（不可自行调整）

| 参数 | 值 | 规格来源 |
|---|---|---|
| fsnotify防抖 | 500ms | §2.4.2 |
| 日志搜索上限 | 1000行 | §2.1.7 |
| 扩展运行日志上限 | 10MB硬编码 | §2.2.16 |
| 任务历史保留 | 7天（内存） | §2.2.9 |
| 事件环形缓冲 | 200条 | §2.9.7 |
| 长轮询并发上限 | 全局50/单客户端5 | §1.2 |
| stop默认grace | 10秒 | §2.1.4 |
| stop默认timeout | 60秒 | §2.1.4 |
| 扩展默认timeout | 600秒 | §2.2.3 |
| 扩展硬上限 | 1800秒 | §2.2.8 |
| 上传大小限制 | 100MB | §2.12.6 |
| 编辑器多标签上限 | 8个 | §2.9.9 |
| 文件历史版本 | 50个 | §2.3.1 |

### 3.3 架构约束

- **禁止引入数据库**（SQLite/Bolt/Badger等）
- **禁止引入SSE/WebSocket**（长轮询是规格要求）
- **禁止添加需求规格说明书中不存在的配置字段**
- **业务逻辑有疑问时，以 `docs/需求规格说明_v1.5.md` 为唯一权威来源**

---

## 四、修复工作规范

### 4.1 bug修复流程

1. **定位根因**：阅读相关代码，确认问题所在
2. **对照规格**：如涉及业务逻辑，查阅 `docs/需求规格说明_v1.5.md` 相关章节
3. **最小化修改**：只修改必要的代码，不重构无关部分
4. **验证**：执行 `go build ./...` + 相关测试确认修复
5. **更新 session-notes.md**：记录修复内容和结论

### 4.2 何时停止并请求人工介入

遇到以下情况必须停止，记录到 `docs/devlog/blockers.md` 并说明：

- 修复方案涉及业务规则变更，而规格说明书中该规则不明确
- 修复方案会影响多个模块，且影响范围难以评估
- 发现的问题与规格说明书存在矛盾
- 同一问题修复失败3次以上

### 4.3 禁止行为

- **禁止趁机重构**：修复bug时不得改动无关代码
- **禁止添加未要求的功能**：即使认为某功能很有用
- **禁止伪造测试结果**：测试必须实际运行，命令和输出必须真实
- **禁止在session-notes.md中记录未实际完成的内容**

---

## 五、已知偏差参考

以下偏差已经人工确认为可接受（详见 `docs/devlog/deviations.md`）：

- **DEV-003**：ProcessManager/EventRingBuffer/TaskHistory 等使用 sync.Mutex（合理例外）
- **DEV-004**：api/interfaces.go 定义13个接口（符合Go惯用法）
- **DEV-005**：服务级扩展默认run_as未完整接入（显式run_as已生效）
- **DEV-008**：API端点76个 vs 规格65个（多出11个实用端点，**待确认是否保留**）

---

## 六、会话结束流程

每次会话结束前必须更新 `docs/devlog/session-notes.md`：

- 本次完成了什么
- 遗留的未完成工作
- 下次会话需要注意的事项

---

## 七、代码验证命令

```bash
# 后端验证
go build ./...
go vet ./...
go test ./... -count=1

# 前端验证
cd web && pnpm build

# 服务启动（测试用）
SUPD_LOG_DIR=/tmp/supd-logs ./supd --workdir test_workdir run
```

---

*supd 项目协议 — 维护阶段。业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`*
