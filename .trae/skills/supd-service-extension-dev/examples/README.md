# 示例索引

所有示例均为完整、可直接复用的服务/扩展配置，内容自洽，不依赖项目特定路径。

## 示例列表

| 编号 | 目录 | 类型 | 覆盖特性 |
|---|---|---|---|
| 01 | `01-simple-service/` | 完整结构服务 | `http_check`、`bin/`、`env.yaml`、服务级扩展、default/migrate profile |
| 02 | `02-complex-service/` | 复杂服务 | `tcp_check` readiness、`autostart`、command 数组、tags、`workdir` |
| 03 | `03-on-demand-ext/` | on_demand 扩展 | 手动触发、多 action（greet/status）、`SUPD_ACTION` 区分 action、`button_style` |
| 04 | `04-scheduled-ext/` | on_schedule 扩展 | cron 定时触发（每分钟）、单 action |
| 05 | `05-service-lifecycle-ext/` | service_lifecycle 扩展 | `post_ready`/`on_failure`/`pre_stop` 三种生命周期钩子 |
| 06 | `06-supd-lifecycle-ext/` | supd_lifecycle 扩展 | `pre_start`/`post_ready`/`pre_shutdown` 钩子、parallel 并发、stdout 协议 |
| 07 | `07-health-check-ext/` | stdout 协议扩展 | on_demand+service_lifecycle 混合触发、多 action、`::progress::`/`::result::` 协议 |
| 08 | `08-stats-report-ext/` | 定时+手动混合扩展 | on_schedule+on_demand 混合、完整 stdout 协议输出 |
| 09 | `09-tjs-ext/` | **tjs 运行时扩展** | `runtime: tjs`、`fetch`、文件读写、`tjs:path` 模块、stdout 协议 |
| 10 | `10-binary-updater-ext/` | **二进制更新扩展** | `tjs.open` 流式下载、原子替换、版本检测、check-update/update/force-update 三 action、失败回滚 |

## 使用方法

1. 示例目录前的数字仅用于索引；复制时必须按配置中的 `name` 重命名目录（如 `01-simple-service/` → `services/web-demo/`）
2. 服务示例：放入 `<baseDir>/services/<name>/`
3. 全局扩展：放入 `<baseDir>/extensions/<name>/`
4. 服务级扩展：放入 `<baseDir>/services/<svc>/extensions/<name>/`（关联由目录位置决定，无需 meta.yaml 字段）
5. 修改 `service.yaml` 中的 name 与目录名保持一致，修改 `run.sh` 中端口/路径等参数
6. 确保 `run.sh` 有执行权限：`chmod +x run.sh`
7. supd 会通过 fsnotify 自动发现并加载，无需重启

## 注意事项

- 服务示例目录必须包含中文 `README.md`，覆盖服务信息、目录权限、启动就绪、配置环境、扩展 actions、持久化升级、运维及安全备份，且不得包含敏感数据
- `service.yaml` 的 `name` 字段必须与所在目录名完全一致
- `meta.yaml` 的 `entry` 支持绝对路径或相对所属配置根的路径；全局扩展相对 `<baseDir>`，服务扩展相对服务根。示例应写完整根相对路径（如 `extensions/my-ext/run.sh`），且不可带冗余 `./` 前缀
- 涉及服务端口的扩展示例，端口通过 `SUPD_SERVICE_DIR` 配合 `env.yaml` 注入，避免硬编码
