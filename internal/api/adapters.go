package api

// --- EventRingBuffer 实现 core.EventPublisher ---

// Publish 实现 core.EventPublisher 接口
// REQ-2.9.7: 将事件添加到环形缓冲区
func (b *EventRingBuffer) Publish(eventType string, payload any) {
	b.Add(eventType, payload)
}
