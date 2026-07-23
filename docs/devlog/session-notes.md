# supd 开发会话备忘（主索引）

> 跨会话上下文传递。Agent 新会话启动时首先阅读本文件（主索引）+ `blockers.md`。
> 详细信息按需读取 `notes/` 子目录，**不要默认全量读取**。读取协议见 `notes/README.md`。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。

---

## 一、项目状态

- **阶段**：维护/修复/测试阶段（57 Task 全部完成，8 阶段任务执行计划闭合）
- **质量水位**：17 类审计评分 **97.44 / 100（⭐ 优秀）**；913+ 单元测试通过；零竞态；staticcheck/go vet 零警告
- **当前版本**：v0.0.8（版本升级见 `version-upgrade-guide.md`）

### 验证命令（每次改动后必跑）
```bash
# 后端
go build ./... && go vet ./... && go test ./... -count=1
# 前端（改前端后必须 go build 重新嵌入二进制）
cd web && pnpm build
# 服务启动（测试用）
SUPD_LOG_DIR=/tmp/supd-logs ./supd --workdir test_workdir run
```

---

## 二、核心机制摘要

> 详细备忘见 `notes/core-mechanisms.md`（涉及底层机制时按需读取）

- **生命周期**：`starting→up→ready`（唯一就绪路径）、`stopping→down`；自动重启不经过 down
- **环境变量**：4 层合并（os.Environ → 全局 env 文件 → 服务 env.yaml → 扩展 env.yaml）；`env.yaml` 必须含 `env:` 包装层；`enabled:false` 不注入
- **身份权限**：服务 user 空=继承 supd；服务级扩展 run_as 空=继承服务 user；全局扩展 run_as 空=继承 supd；服务严格/扩展宽松语义差异
- **关机**：单一 `shutdown_grace_seconds` 预算贯穿 cron stop / 扩展等待 / GracefulShutdown / HTTP Stop
- **PID1**：supd 自带 PR_SET_CHILD_SUBREAPER + SIGCHLD 回收；Docker 中禁用 `--no-pid1`；维护 PID 文件清理孤儿进程
- **前端嵌入**：`//go:embed dist` 在 `web/embed.go`，改前端后必须 `pnpm build` + `go build` 才能生效
- **watcher**：白名单只监控配置目录；黑名单 data/bin/logs/history/cache/tmp/temp/run；fsnotify 防抖 500ms

---

## 三、已知偏差（详见 `deviations.md`）

| 编号 | 内容 | 状态 |
|------|------|------|
| DEV-003 | ProcessManager 等用 sync.Mutex | 合理例外 |
| DEV-004 | api/interfaces.go 定义 13 个接口 | Go 惯用法 |
| DEV-005 | 服务级扩展 run_as 继承 | ✅ 已完全修复 |
| DEV-008 | API 端点 77 vs 规格 65 | 已确认保留 |
| DEV-009 | `SUPD_SERVICE_DIR` 规格外变量 | 已确认 |
| DEV-011 | triggers 格式 map vs 规格 list | 实现一致 |
| DEV-012 | `actions[].icon`/`enabled` 字段未实现 | 待定 |

---

## 四、关键决策

- 不引入数据库、不引入 SSE/WebSocket（长轮询是规格要求）
- 不引入 tini/dumb-init（supd 自带 PID 1 能力）
- triggers 格式用 map（DEV-011，所有 meta.yaml 与代码一致）
- meta.yaml 中 `service:` 字段冗余（YAML 解析器静默忽略，服务关联由目录结构决定）
- dropbear-ssh 是 supd 管理的普通服务（非 entrypoint 脚本），autostart: false
- 接受 97.44 分作为审计最终结果，剩余扣分项为合理偏差

---

## 五、未闭合待办（详见 `blockers.md`）

| 编号 | 扣分 | 内容 |
|------|------|------|
| L-01-001 | -0.150 | api 包覆盖率 41.9%（需大量测试代码） |
| L-04-001 | -0.250 | 缺失端到端集成测试代码（N 类已手动验证） |
| M-03-001 | -0.160 | yaml v4 rc 预发布版（等社区稳定版） |
| M-04-001 | -0.160 | superviseService 圈复杂度 43（修复需重构） |

技术债：🟡 TD-003（superviseService 重复，部分修复）、❌ TD-005（useLongPolling hook 未修复）

---

## 六、下次会话注意

- 改前端后必须 `pnpm build` + `go build` 重新嵌入二进制，否则看不到效果
- `NewReadinessChecker(cfg, dir, env)` 已变 3 参数；`OnFailure` 增加 `servicePID int`；`CronScheduler.Stop(ctx)` 带 context
- env.yaml 格式必须含 `env:` 包装层，直接写 `KEY: value` 会被静默忽略
- 前端所有 env 编辑器统一用 `web/src/lib/env-yaml` 共享工具
- 服务与扩展的非 root 语义差异需保持（服务严格拒绝、扩展宽松警告）
- Docker 镜像需重新构建才能包含 Dockerfile 变更（dropbear/env.yaml 加载等）
- 后续补充测试优先覆盖 api 包错误分支（L-01）和端到端集成测试（L-04）
- 监控 yaml v4 稳定版发布（M-03），发布后升级 go.mod

---

## 七、会话历史索引

> 按需读取对应文件，不要默认全量浏览。搜索特定主题用 `rg` 在 `notes/` 中查找。

| 日期 | 主题 | 摘要 | 详情文件 |
|------|------|------|----------|
| 2026-07-21 | Docker/tjs/发布/清理 | tjs 集成、v0.0.1 发布、工作区清理、仓库重建、readiness bug、user 字段接入 | [notes/2026-07-21.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-21.md) |
| 2026-07-22 | env/Dropbear/规格偏差 | tjs 默认配置、Dropbear SSH、env.yaml 加载 BUG、3 项规格偏差修复、前端 env 修复、v0.0.6 | [notes/2026-07-22.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-22.md) |
| 2026-07-23 | 审计/env/仪表盘/retry/热重载/访问日志 | 全面审计（97.44 分）、env 编辑器统一、仪表盘服务资源汇总、扩展 retry_on_failure 补全、热重载 RestartEngine 不更新 BUG 修复、新增代码审计+运行状态测试、HTTP 访问日志改用 slog + --log-level CLI BUG 修复、v0.0.9 | [notes/2026-07-23.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-23.md) |

---

## 八、最近会话重点（2026-07-23 HTTP 访问日志 slog 改造 + --log-level CLI BUG 修复 + v0.0.9）

**问题**：HTTP 访问日志（chi Logger 中间件）使用标准库 `log` 包，不受 `log_level` 配置控制，也不写入 supd.log 文件。

**改造**：移除 chi Logger，替换为自定义 `accessLogMiddleware`，用 `slog.Info` 输出结构化访问日志（method/path/remote_addr/status/bytes/duration_ms/request_id），受 `log_level` 控制，写入 supd.log 文件。

**审计发现 BUG**：`--log-level` CLI 标志只打印 verbose 消息，未实际覆写配置。根因：`bootstrap.Run()` 重新加载 config 文件，丢弃了 run.go 中的覆写。修复：`BootstrapConfig` 增加 `LogLevel` 字段，参照 `HTTPListen` 模式在 bootstrap 中应用覆写。

**运行状态测试**（6/6 PASS ✅）：
- A：访问日志格式（7 字段完整）
- B：错误状态码（404）正确记录
- C：config.yaml `log_level: warn` 抑制访问日志
- D：`--log-level warn` CLI 标志抑制访问日志（BUG 修复验证）
- E：访问日志写入 supd.log 文件
- F：长轮询 `duration_ms=3000.762` 精确记录 3 秒等待

**版本**：v0.0.8 → v0.0.9，提交 github

> 同日早些时候还完成了：全面审计（97.44 分）、env 编辑器统一、仪表盘服务资源汇总、扩展 retry_on_failure 补全、热重载 RestartEngine 不更新 BUG 修复、新增代码审计+运行状态测试 v0.0.8。详情见 [notes/2026-07-23.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-07-23.md)。
