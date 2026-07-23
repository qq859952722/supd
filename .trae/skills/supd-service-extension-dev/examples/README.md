# 示例索引

所有示例均为完整、可直接复用的服务/扩展配置，内容自洽，不依赖项目特定路径。

## 示例列表

| 编号 | 目录 | 类型 | 覆盖特性 |
|---|---|---|---|
| 01 | `01-simple-service/` | 简单服务 | `http_check` readiness、Python HTTP 服务、stop/logging 配置 |
| 02 | `02-complex-service/` | 复杂服务 | `tcp_check` readiness、`autostart`、command 数组、tags、`workdir` |
| 03 | `03-on-demand-ext/` | on_demand 扩展 | 手动触发、多 action（greet/status）、action args、`button_style` |
| 04 | `04-scheduled-ext/` | on_schedule 扩展 | cron 定时触发（每分钟）、单 action |
| 05 | `05-service-lifecycle-ext/` | service_lifecycle 扩展 | `post_ready`/`on_failure`/`pre_stop` 三种生命周期钩子 |
| 06 | `06-supd-lifecycle-ext/` | supd_lifecycle 扩展 | `pre_start`/`post_ready`/`pre_shutdown` 钩子、parallel 并发、stdout 协议 |
| 07 | `07-health-check-ext/` | stdout 协议扩展 | on_demand+service_lifecycle 混合触发、多 action、`::progress::`/`::result::` 协议 |
| 08 | `08-stats-report-ext/` | 定时+手动混合扩展 | on_schedule+on_demand 混合、完整 stdout 协议输出 |
| 09 | `09-tjs-ext/` | **tjs 运行时扩展** | `runtime: tjs`、`fetch`、文件读写、`tjs:path` 模块、stdout 协议 |

## 使用方法

1. 复制对应目录到你的 workdir
2. 服务示例：放入 `<baseDir>/services/<name>/`
3. 全局扩展：放入 `<baseDir>/extensions/<name>/`
4. 服务级扩展：放入 `<baseDir>/services/<svc>/extensions/<name>/`（关联由目录位置决定，无需 meta.yaml 字段）
5. 修改 `service.yaml` 中的 name 与目录名保持一致，修改 `run.sh` 中端口/路径等参数
6. 确保 `run.sh` 有执行权限：`chmod +x run.sh`
7. supd 会通过 fsnotify 自动发现并加载，无需重启

## 注意事项

- `service.yaml` 的 `name` 字段必须与所在目录名完全一致
- `meta.yaml` 的 `entry` 字段使用相对路径（如 `./run.sh`），便于移植
- 涉及服务端口的扩展示例，端口通过 `SUPD_SERVICE_DIR` 配合 `env.yaml` 注入，避免硬编码
