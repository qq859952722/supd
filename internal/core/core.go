// Package core 实现 supd 核心监督器逻辑。
// REQ-F-004~013: 服务状态机、依赖图、进程管理、日志、readiness等
// REQ-C-003: goroutine 间通过 channel 通信，禁止共享状态+mutex（logger文件写除外）
package core
