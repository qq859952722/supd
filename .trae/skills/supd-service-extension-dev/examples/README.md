# 示例索引

所有示例均来自 test_workdir 中经过实际运行验证的配置，可直接复制使用。

## 示例列表

| 编号 | 目录 | 类型 | 覆盖特性 | 源文件 |
|---|---|---|---|---|
| 01 | simple-service/ | 简单服务 | http_check readiness、Python HTTP 服务、stop/logging 配置 | services/web-demo/ |
| 02 | complex-service/ | 复杂服务 | tcp_check readiness、autostart、command 数组形式、tags | services/qbittorrent/ |
| 03 | on-demand-ext/ | on_demand 扩展 | 手动触发、多 action（greet/status）、action args、button_style | services/web-demo/extensions/demo-action/ |
| 04 | scheduled-ext/ | on_schedule 扩展 | cron 定时触发（每分钟）、单 action | extensions/scheduled-ping/ |
| 05 | service-lifecycle-ext/ | service_lifecycle 扩展 | post_ready/on_failure/pre_stop 三种生命周期钩子 | services/web-demo/extensions/demo-lifecycle/ |
| 06 | supd-lifecycle-ext/ | supd_lifecycle 扩展 | post_ready/pre_shutdown 钩子、parallel 并发、stdout 协议 | extensions/supd-startup-hook/ |
| 07 | health-check-ext/ | 带 stdout 协议扩展 | on_demand+service_lifecycle 混合触发、多 action、::progress::/::result:: 协议、replace 并发 | services/qbittorrent/extensions/qbittorrent-health-check/ |
| 08 | stats-report-ext/ | on_schedule+on_demand 扩展 | 定时+手动混合触发、完整 stdout 协议输出、API 调用 | services/qbittorrent/extensions/torrent-stats-report/ |

## 使用方法

1. 复制对应目录到你的 workdir
2. 服务示例：放入 `<baseDir>/services/<name>/`
3. 扩展示例：放入 `<baseDir>/extensions/<name>/`（全局扩展）或 `<baseDir>/services/<svc>/extensions/<name>/`（服务级扩展，服务关联由目录决定，无需 meta.yaml 字段）
4. 修改 run.sh 中的端口/路径等参数匹配实际环境
5. 确保 run.sh 有执行权限：`chmod +x run.sh`
6. supd 会通过 fsnotify 自动发现并加载，无需重启

## entry 路径说明

示例中 meta.yaml 的 `entry` 字段统一使用 `./run.sh`（相对扩展目录）。
实际使用时 supd 也支持绝对路径，但相对路径更便于移植。
