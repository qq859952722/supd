# MEMORY.md — supd 项目长期记忆

## 审计任务（tmp/audit_plan.md）
- 审计方案：`tmp/audit_plan.md`（v1.1，含 A–Q 类 + 打分体系）。唯一可写路径：`tmp/`；严禁改 .go/.tsx/.ts/.css/go.mod/go.sum/package.json/docs/。
- 打分：P 类 0.5 分（P-01/02/03 各 ≈0.17），Q 类 0.5 分（Q-01/02 各 0.25）。扣减：🔴=0、🟠=-50~80%、🟡=-15~30%、🔵=-0~10%。
- 报告文件约定：tmp/audit_results_P_ux.md、tmp/audit_results_Q_docs.md、tmp/审计报告.md（最终汇总，2026-07-25 已完成）。
- **审计全部完成（2026-07-25）**：P/Q（07-24）+ A–O（07-25）共 15 类全部产出独立报告，最终汇总见 `tmp/审计报告.md`。
- **各类得分**：A7.9/8 B4.95/5 C4.95/5 D5.65/6 E7.75/8 F5.95/6 G5.95/6 H5/5 I3.9/4 J3/3 K4/4 L3.7/4 M4.9/5 N5/5 O4.9/5。**综合 77.5/79 ≈ 98.1%**。
- 缺陷统计：🔴0 / 🟠0 / 🟡5（I-01 死代码×2、L-02-001 长轮询限制器无单测、L-04-001 三模式认证无集成测试、O-05-001 run.go:494 魔法数字10）。无数据/安全/并发类严重缺陷。
- 审计方案文档漂移：方案引用的 form-engine.tsx/DiffEditor.tsx/useLongPolling.ts/ui/index.ts/app.ts 实际不存在；D-01 称72路由实为76；F-02 称 adapters.go 35KB 实为9行；J-01 称11接口实为13（DEV-004）。

## 工具踩坑（跨会话必读）
- **search_content 工具在本项目持续失效**（返回 0 命中，含实际存在的模式如括号/".go"）。改用 `execute_command` 执行 `grep -rn ... --include="*.go"` 做内容检索，可靠。
- **mv 事故**：`mv a b c d/` 多源会全部移入末目录。一律一次一个源一个目标。
- **审计工具需 `go install`**：errcheck/govulncheck/gocyclo 默认未装，需 `GOFLAGS=-mod=mod go install ...@latest`，装到 `$HOME/go/bin`，运行前 `export PATH=$PATH:/home/qq/go/bin`。
- **e2e 启服务**：main 包在 `./cmd/supd`（非仓库根）；构建 `go build -o /tmp/supd-bin ./cmd/supd`，启动 `SUPD_LOG_DIR=/tmp/supd-logs /tmp/supd-bin --workdir test_workdir run --listen :9090`。长轮询端点为 `GET /api/events`（非 /api/events/poll）；限制器 503 SERVICE_BUSY 拒绝第6个单客户端连接。
- **前端 lint 缺失**：`web/` 无 ESLint 配置与 lint 脚本；`pnpm build`/`tsc --noEmit` 通过，`pnpm audit` 0 漏洞。

## 关键代码事实
- `EventPublisher.Publish(eventType string, payload any)` 签名（payload 是 `any`，非 `map[string]any`）。core 包用 `sm.publisher.Publish(...)`。
- 14 种事件类型全部实际发布（grep .Publish 确认）；system_resource_warning 仅磁盘（run.go:715）。
- 错误码 22 个，后端 DefaultMessages 中英混杂（P-01-001 待修）；CLI/前端均有纯中文覆盖。

## e2e 运行期测试（2026-07-25 全都要测战役）
- 方案 `tmp/运行状态测试方案.md`，报告 `tmp/运行状态测试_e2e报告.md`：13 适用节全 PASS，3.13 前端 N/A(headless)。
- SIGTERM 关停主进程 → 退出码 0、优雅退出日志序列、`ps` 无残留子进程（验证方式）。
- **手动 start 不级联依赖**（设计，`service_operator.go:140`）；拓扑排序仅在启动期 autostart 生效。
- 停止流 supd **不打印 SIGTERM/SIGKILL 字面日志**，正确性靠 stop 阻塞时长 + 进程回收证伪；顽固停止 grace 后 SIGKILL。
- 资源端点返回 `process_count`/`fd_count`（无 `threads` 字段）。
- zip-slip 导入返回 `200 entries=null` 未越界（建议改 400）。

## 环境注意（重要）
- **`:7979` 实例真实拓扑（2026-07-25 更正）**：`192.168.31.188:7979` 是**独立局域网主机/盒子**（开发机自身是 `192.168.31.184`）。其上 supd 以 **PID1** 运行，`--workdir /etc/supd`。**不是**本机 `/tmp/test-workdir`（`/tmp/test-workdir` 等只是本机开发/e2e 沙盒，与该实例无关，切勿混淆）。
- **访问方式**：SSH `root@192.168.31.188 -p 2222`（该实例自身的 dropbear-ssh 服务，**从开发机可免密登录**）。登录后直接改 `/etc/supd` 下的服务/扩展文件，远程 supd 的 fsnotify 会实时生效——`/etc/supd` 才是 `:7979` 真正读取的工作目录。
- **Transmission RPC 认证**：`settings.json` 中 `rpc-authentication-required:false`、`rpc-whitelist:127.0.0.1,::1`。tracker-updater 脚本走 **CSRF `X-Transmission-Session-Id`**（409 重试）认证，**无账号密码**；因白名单仅限本机，脚本用 `127.0.0.1` 连接（从开发机直连会 403）。
- **下载目录持久化坑（重要）**：transmission `settings.json` 与 qBittorrent `qBittorrent.conf` 都会在该进程**优雅退出时重写**并丢弃外部手改的未知段落。改 download-dir / SavePath 必须：先 stop（让它用旧值重写一次）→ 编辑配置文件 → 再 start（新进程加载正确值）；直接 restart 会被退出重写覆盖回旧值。
- `/tmp/supd-rt` 等本机实例仍为本机常驻沙盒，e2e 仅用仓库内 `test_workdir`，勿误伤。

## 端口归属修复（2026-07-25）
- **根因**：188 容器 yama ptrace_scope=1 → readlink /proc/<pid>/fd 对 nobody 进程 Permission denied → inode 精确匹配完全失败 → UID 降级把同 UID 的 qbittorrent/transmission 端口全部混在一起
- **修复**：UID 降级路径添加 cmdline 交叉验证（`collectInodesByCmdlineUID`）：扫描 /proc 找 UID+cmdline 匹合进程 → 重试 inode → 有结果走精确匹配，无结果退回纯 UID 匹配 + Warn 日志
- **签名变更**：`collectProcessPorts(pid int)` → `collectProcessPorts(pid int, cmdPattern string)`
- **新增辅助**：`cmdPatternFromConfig(cfg)` 提取 `ServiceConfig.Command[0]`
- **影响文件**：port_collector.go / service_handler.go / resource_handler.go
