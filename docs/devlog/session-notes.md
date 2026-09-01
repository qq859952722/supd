# supd 开发会话备忘（主索引）

> 跨会话上下文传递。Agent 新会话启动时首先阅读本文件（主索引）+ `blockers.md`。
> 详细信息按需读取 `notes/` 子目录，**不要默认全量读取**。读取协议见 `notes/README.md`。
> 业务规则唯一权威来源：`docs/需求规格说明_v1.5.md`。偏差台账见 `deviations.md`，阻断见 `blockers.md`。
> 历史归档：2026-07-21 ~ 2026-07-26 见 `archive/session-notes-20260727-precompress.md`；2026-07-27 ~ 2026-08-01 见 `notes/` 对应日期文件。

---

## 一、项目状态

- **阶段**：维护/修复/测试阶段（57 Task 全部完成，8 阶段任务执行计划闭合）
- **质量水位**：⭐ 优秀，1000+ 单元测试通过（Go + 前端），零竞态；go vet 零警告
- **当前版本**：v0.0.54（验证 tjs Release 缓存复用；版本升级见 `version-upgrade-guide.md`）

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
- **路径解析**：全局 env_files/extension_dirs/runtimes 基于 `<baseDir>`；服务 command[0]/workdir/script readiness check[0] 基于服务根；**扩展相对 entry 与进程 CWD 基于扩展自身目录（meta.yaml 所在目录）**；裸命令保留 PATH 查找；`BuildRegistryAt(baseDir, ...)` 统一解析 runtime 路径

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
| 2026-08-31 | 扩展 CWD/entry 解析根回归修复（v0.0.44 引入） | 190 全部扩展启动失败；`buildWorkDir`/`RunExtension`/导出导入校验的 entry 解析根从 baseDir/服务根改回扩展自身目录；同步规格/Skill/示例/测试 | [notes/2026-08-31.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-31.md) |
| 2026-08-31 | v0.0.47 发版审计 | 完成工作路径与文档修复的全链路审计；Go build/vet/test、race、版本注入和示例校验全部通过，已推送 GitHub | [notes/2026-08-31.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-31.md) |
| 2026-08-31 | v0.0.48 镜像层增量更新优化 | Alpine/Debian Dockerfile 使用独立二进制层与 `COPY --link --chmod`，兼容现有 GitHub Actions workflow；未执行本地构建，已完成 CI 配置审查 | [notes/2026-08-31.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-31.md) |
| 2026-08-31 | v0.0.49 tjs 编译缓存增强 | 正式发布和手动构建 workflow 的 tjs 缓存 key 增加 schema、基础镜像/libc、架构和源版本约束；未执行本地构建 | [notes/2026-08-31.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-31.md) |
| 2026-08-31 | v0.0.51 tjs 固定 Release 构建缓存完善 | 自动发布和手动构建共用固定 `tjs-cache` Release；命中时下载复用、未命中时编译上传；workflow 级并发避免资产竞争；按 TJS_VERSION 清理旧资产，仅保留最近 5 个版本及每版四个平台/变体组合 | [notes/2026-08-31.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-31.md) |
| 2026-08-31 | v0.0.52 build-push workflow 调度修复 | 删除 build-alpine/build-debian 重复的 job-level `if`，修复 GitHub Actions 调度前解析失败和列表显示异常；未执行本地构建 | [notes/2026-08-31.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-31.md) |
| 2026-08-31 | v0.0.53 tjs Release 资产命名修复 | 修复 `gh release upload source#label` 未重命名资产导致四个任务均生成 `tjs`、Release 缓存无法命中的问题；改为上传真实命名文件并增加资产存在性校验 | [notes/2026-08-31.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-31.md) |
| 2026-08-31 | v0.0.54 tjs Release 缓存复用验证 | 无代码变更的验证性发布；确认 v0.0.52 未命中缓存属修复前历史行为，四个正确命名资产已由 v0.0.53 写入 `tjs-cache`，本版应直接命中跳过编译 | [notes/2026-08-31.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-08-31.md) |
| 2026-09-01 | 190 smartdns 底包兼容性修复 + Skill 双向门禁 | 切 Debian 底包后 musl smartdns ENOENT/relocating 两层修复（musl 解释器链接 + 官方同源 musl 运行库经 base64 离线通道入 `bin/lib/`，RPATH 命中恢复 ready）；Skill 新增 §1.8 与双向 libc 门禁 | [notes/2026-09-01.md](file:///home/qq/Documents/trae_projects/supd/docs/devlog/notes/2026-09-01.md) |

---

## 七、最近会话重点（2026-09-01 smartdns 底包兼容性修复）

- **现象**：190 切换 Debian 底包后 smartdns 突然 failed：`fork/exec /etc/supd/services/smartdns/bin/smartdns: no such file or directory`，但文件树确认二进制存在（678904 字节，与官方 Release48.2 插件同源）。
- **根因**：musl 链接二进制在 glibc 底包下 execve 因 ELF 解释器 `/lib/ld-musl-x86-64.so.1` 缺失报 ENOENT（文件存在的经典假象）；与 supd 代码无关。修复解释器后又现 `Error relocating ... symbol not found`（musl loader 误加载 glibc libssl）。
- **修复**（全程经 supd HTTP API + 一次性 tjs 扩展，SSH 因容器重建后 root 密码未清空不可用）：
  - `apt-get install musl` + 手工 `ln -sf /usr/lib/x86_64-linux-musl/libc.so /lib/ld-musl-x86-64.so.1`（Debian musl 包不自动创建链接）
  - 官方 tar.gz 中同源 musl 版 `libssl.so.3`/`libcrypto.so.3`/`libgcc_s.so.1` 经 base64+文件 API 离线上传（190 无法直连 GitHub），扩展解码安装到 `bin/lib/`，二进制 `RPATH($ORIGIN/lib)` 自动命中，无需 env 注入
  - 结果：smartdns 恢复 `ready`；一次性扩展已删除
- **Skill 记录（用户要求）**：`SKILL.md` 底包 libc 门禁改为**双向**（Alpine↔Debian）；`01_service_spec.md` 新增 §1.8「musl 二进制运行于 Debian/glibc 底包」（症状判定表、处理规则优先级、smartdns 实战案例）。
- **技术要点**：`execve` ENOENT 需区分文件不存在 vs 解释器缺失；musl loader 只搜 `/lib:/usr/lib`；musl 静态主程序无法 dlopen musl 动态插件；supd 文件 API 为文本语义（二进制走 base64）；任务日志 API 响应字段是 `lines`。
- **遗留**：容器再重建需重跑修复（建议将 alpine-init 改造为 apk/apt-get 双分支的底包感知初始化扩展）；dropbear SSH 需宿主机重清 root 密码；code-server（argon2 glibc + musl Node）待单独处理。

### 上次会话重点：tjs 固定 Release 构建缓存（v0.0.51–v0.0.54）

- 自动发布和手动镜像 workflow 共用固定 `tjs-cache` prerelease；Actions Cache 未命中时下载对应 Release asset，仍未命中才编译。
- 资产按 Alpine/Debian、amd64/arm64、`TJS_VERSION` 和 `TJS_CACHE_SCHEMA` 区分；按版本保留最近 5 个版本；同一并发组避免资产竞争；`push: false` 不写 Release。
- v0.0.53 修复 `gh release upload source#label` 未重命名资产的问题（上传真实命名文件 + 存在性校验）；v0.0.54 实测命中复用成功，并用 `tjs_version=v26.5.0` + `push=false` 手动 run 验证了"版本变更→未命中→真实编译→不上传污染缓存"全链路。

### 上上次会话重点：扩展工作目录解析回归修复

- v0.0.44 将扩展 CWD/相对 entry 解析根从扩展自身目录误改为服务根/baseDir，导致 190 全部扩展启动失败；`buildWorkDir`/`RunExtension`/导出导入校验已恢复以扩展自身目录为根，规格/Skill/示例/测试同步。
