package core

// ServiceState 服务状态枚举
// REQ-F-004: 7种状态，禁止新增（见AGENTS.md 2.3节枚举值锁定清单）
type ServiceState string

const (
	StatePending  ServiceState = "pending"  // 已加载但依赖未就绪，等待中
	StateStarting ServiceState = "starting" // 进程已启动，等待readiness信号
	StateUp       ServiceState = "up"       // 进程运行中；有readiness配置时为中间态，无readiness配置时为终态
	StateReady    ServiceState = "ready"    // 通过readiness检查，可提供服务
	StateStopping ServiceState = "stopping" // 已发送停止信号，等待进程退出
	StateDown     ServiceState = "down"     // 进程已退出，未自动重启
	StateFailed   ServiceState = "failed"   // 永久失败，不再自动重启
)
