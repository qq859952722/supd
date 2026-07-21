package core

// EventPublisher 事件发布接口
// REQ-2.9.7: 状态转移和进程退出时发布事件
// 由 API 层的 EventRingBuffer 实现，core 包不依赖 api 包
type EventPublisher interface {
	Publish(eventType string, payload any)
}
