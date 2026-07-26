# Debug Session: service-status-labels
- **Status**: [OPEN]
- **Issue**: autostart=false 的服务显示“等待中”，仪表盘显示“过渡中”
- **Debug Server**: Not needed; direct API evidence available
- **Log File**: Not needed

## Reproduction Steps
1. 启动包含 autostart=false 服务的 supd 实例。
2. 打开服务列表与仪表盘。
3. 观察未启动服务及实例聚合状态。

## Hypotheses & Verification
| ID | Hypothesis | Likelihood | Effort | Evidence |
|----|------------|------------|--------|----------|
| A | 后端未将 autostart=false 服务归位为 down | High | Low | Confirmed on remote v0.0.21; fixed in current source |
| B | 前端将 pending 错译为等待中并参与过渡态聚合 | High | Low | Rejected as translation bug; mapping matches pending semantics |
| C | 前端复用了任务状态文案 | Medium | Low | Rejected; service status uses dedicated service mapping |
| D | 其他服务确实处于 starting/up/stopping | Medium | Low | Rejected; only dropbear-ssh is pending, other three are ready |
| E | 远端运行的静态资源或二进制不是当前修复版本 | High | Medium | Confirmed; remote v0.0.21, fix introduced in v0.0.30 |

## Log Evidence
- `GET http://192.168.31.188:7979/api/system/status` returned HTTP 200 and `version: v0.0.21`.
- `GET http://192.168.31.188:7979/api/services` returned `dropbear-ssh.status: pending`; adguardhome, qbittorrent and transmission are `ready`.
- Current source resets `autostart:false` state machines to `down` in `internal/core/bootstrap.go`.
- Regression test `TestBootstrap_AutostartFalse` asserts `manual-svc` is `down`.
- Git history shows v0.0.30 release explicitly fixed this issue; workspace is v0.0.31.

## Verification Conclusion
Pre-fix evidence is complete. Root cause is stale remote deployment (v0.0.21), not a missing fix in current v0.0.31 source. Post-fix verification requires upgrading and restarting the remote instance, then confirming the API returns `dropbear-ssh.status: down`.
