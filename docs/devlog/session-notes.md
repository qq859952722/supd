# supd 开发会话备忘（主索引）

> 跨会话上下文传递。Agent 新会话启动时首先阅读本文件（主索引）+ `blockers.md`。
> 详细信息按需读取 `notes/` 子目录，**不要默认全量读取**。读取协议见 `notes/README.md`。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。
> 历史归档：2026-07-21 ~ 2026-07-26 见 `archive/session-notes-20260727-precompress.md`；2026-07-27 ~ 2026-08-01 见 `notes/` 对应日期文件。

---

## 一、项目状态

- **阶段**：维护/修复/测试阶段（57 Task 全部完成，8 阶段任务执行计划闭合）
- **质量水位**：⭐ 优秀，1000+ 单元测试通过（Go + 前端），零竞态；go vet 零警告
- **当前版本**：v0.0.45（runtime CLI 相对路径与 settings API 契约修复；版本升级见 `version-upgrade-guide.md`）

### 验证命令（每次改动后必跑）
```bash
go build ./... && go vet ./... && go test ./... -count=1   # 后端
cd web && pnpm build                                        # 前端（改前端后必须 go build 重新嵌入二进制）
SUPD_LOG_DIR=/tmp/supd-logs ./supd --workdir test_workdir run  # 服务启动（测试用）
```

---

## 二、核心机制摘要

> 详细备忘见 `notes/core-mechanisms.md`（涉及底层机制时按需读取）

- **生命周期**：`starting→up→ready`（唯一就绪路径）、`stopping→down`；自动重启不经过 down；`autostart:false` 初始为 `down`（§2.8.1）
- **环境变量**：4 层合并（os.Environ → 全局 env 文件 → 服务 env.yaml → 扩展 env.yaml）；`env.yaml` 必须含 `env:` 包装层；`enabled:false` 不注入
- **身份权限**：User 模式与 UID 模式互斥；服务 user/uid 空=继承 supd；服务级扩展空=继承服务身份；全局扩展空=继承 supd；服务严格拒绝/扩展宽松警告
- **关机**：单一 `shutdown_grace_seconds` 预算贯穿 cron stop / 扩展等待 / GracefulShutdown / HTTP Stop
- **PID1**：supd 自带 PR_SET_CHILD_SUBREAPER + SIGCHLD 回收；Docker 中禁用 `--no-pid1`；维护 PID 文件清理孤儿进程
- **前端嵌入**：`//go:embed dist` 在 `web/embed.go`，改前端后必须 `pnpm build` + `go build` 才能生效
- **watcher**：白名单只监控配置目录；黑名单 data/bin/logs/history/cache/tmp/temp/run；fsnotify 防抖 500ms
- **端口探测**：受管 PID 进程树 fd socket inode 精确匹配；Docker 部署需 `cap_add: SYS_PTRACE`
- **路径解析**：全局 env_files/extension_dirs/runtimes 及全局扩展相对 entry 基于 `<baseDir>`；服务 command[0]/workdir/script readiness check[0] 及服务扩展相对 entry 基于服务根；裸命令保留 PATH 查找；`BuildRegistryAt(baseDir, ...)` 统一解析 runtime 路径

---

## 三、已知偏差与待办

> **当前状态**：无活动偏差，无阻断。R-01～R-09 技术改进项已全部闭环（详见 `deviations.md` 与 `notes/` 历史归档）。

---

## 四、关键决策

- 不引入数据库、不引入 SSE/WebSocket（长轮询是规格要求）
- 不引入 tini/dumb-init（supd 自带 PID 1 能力）
- triggers 格式用 map（规格 v1.5 §2.2.3）
- meta.yaml 中 `service:` 字段冗余（服务关联由目录结构决定）
- dropbear-ssh 是 supd 管理的普通服务（非 entrypoint 脚本），autostart: false
- 远程运维优先用 `scripts/remote_ssh.sh`（SSH_ASKPASS 注入空密码，不改 ssh 服务配置）

---

## 五、下次会话注意

- 改前端后必须 `pnpm build` + `go build` 重新嵌入二进制，否则看不到效果
- `NewReadinessChecker(cfg, dir, env)` 为 3 参数；`OnFailure` 含 `servicePID int`；`CronScheduler.Stop(ctx)` 带 context
- env.yaml 必须含 `env:` 包装层，直接写 `KEY: value` 会被静默忽略
- 前端所有 env 编辑器统一用 `web/src/lib/env-yaml` 共享工具
- 服务与扩展的非 root 语义差异需保持（服务严格拒绝、扩展宽松警告）
- Docker 镜像需重新构建才能包含 Dockerfile 变更
- 监控 yaml v4 稳定版发布后升级 go.mod

---

## 六、会话历史索引（近期）

> 完整历史：2026-07-27 之前见 `archive/session-notes-20260727-precompress.md`；其余用 `rg` 在 `notes/` 中查找。

| 日期 | 主题 | 摘要 | 详情文件 |
|------|------|------|----------|
| 2026-08-02 | Skill 完善 + 远程服务优化 + code-server 修复 | Skill 目录契约/校验/打包修复；远程 code-server 启动修复；12 服务 README；11 ready / 1 down | [notes/2026-08-02.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-02.md) |
| 2026-08-02 | tracker-updater v1.3.0 + 下载扩展 uid 1000 | BEP-15 UDP 测速；49 优质 tier+1 垫底=50 tier；tracker/transmission-updater run_as_uid 1000 | [notes/2026-08-02.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-02.md) |
| 2026-08-03 | 多服务同名扩展查找竞态修复 + v0.0.42 | `GetExtensionForService(service, name)` 精确查找；7 新测试 | [notes/2026-08-03.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-03.md) |
| 2026-08-04 | 远程目录结构优化 + SSH 空密码连接固化 | smartdns rules 迁入 config/；8 服务绝对路径相对化；`remote_ssh.sh` 封装脚本；skill 补 §3.1/§3.2 | [notes/2026-08-04.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-04.md) |
| 2026-08-04 | workdir 相对路径支持 + v0.0.43 | `core.ResolveWorkdir` 统一解析；移除 workdir 绝对路径校验；6 处构建逻辑统一 | [notes/2026-08-04.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-04.md) |
| 2026-08-04 | 全配置路径统一 + v0.0.44 | env_files/extension_dirs/runtimes/extension entry/CWD/command/readiness 全链路支持绝对+相对路径 | [notes/2026-08-04.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-04.md) |
| 2026-08-04 | runtime CLI 路径补漏 + API 契约修复 + v0.0.45 | `runtimes install` 接受相对 `<baseDir>` 路径；install/remove 直接发送 runtime map | [notes/2026-08-04.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-04.md) |
| 2026-08-04 | 远程扩展规范化：bash→tjs + 8 服务 updater | 全局扩展 alpine-init/auto-create-users 转 tjs；8 服务 updater 扩展（adguardhome/backrest/dnscrypt-proxy/filebrowser/lucky/openlist/s-ui/smartdns） | [notes/2026-08-04.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-04.md) |
| 2026-08-05 | 服务日志前缀时间 bug 修复 | `LogViewer.tsx` 长轮询用 `Date.now()` 作 timestamp 导致同批次日志前缀时间全相同；新增 `parseLogContent` 从 content 解析 RFC3339 时间戳+级别 | [notes/2026-08-05.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-05.md) |

---

## 七、最近会话重点（2026-08-05 服务日志前缀时间 bug 修复）

- **现象**：服务日志前缀时间全部相同（都是查看时刻），不反映日志实际产生时间。
- **根因**：`web/src/components/service/LogViewer.tsx` 长轮询批量获取日志时 `timestamp: Date.now() / 1000`，用接收时刻而非日志实际时间。
- **修复**：新增 `parseLogContent()` 从日志 content（格式 `[RFC3339] [level] message`）解析实际时间戳与级别，message 去掉 supd 前缀避免重复显示；解析失败兜底 `Date.now()` + `detectLevel`。
- **验证**：pnpm build / go build / go vet 全通过。
- **遗留**：待部署到远程验证效果；远程 dnscrypt-proxy.toml 路径修复 + README 重写为运维操作不记录于 docs。
