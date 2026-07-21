// Package logging 实现 supd 日志系统。
// REQ-F-010: per-service logger goroutine、日志轮转、归档、搜索
// REQ-C-003: 日志文件写互斥为唯一允许的 mutex
// REQ-C-012: 使用 log/slog（标准库）做结构化日志
package logging
