package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/supdorg/supd/internal/errors"
)

// REQ-I-005: 长轮询请求参数

// LongPollRequest 长轮询请求参数。
// REQ-I-005: since=ISO8601, wait=整数秒(默认30,最大60), limit=整数(默认500)
type LongPollRequest struct {
	SinceNano int64 // 单调时钟纳秒，0=从头拉取
	Wait      int   // 秒，默认30，最大60
	Limit     int   // 单次最大条数，默认500
}

// LongPollResponse 长轮询响应。
// REQ-I-005: data/next_since/has_more 格式
type LongPollResponse struct {
	Data      []any  `json:"data"`
	NextSince string `json:"next_since"` // ISO8601带时区
	HasMore   bool   `json:"has_more"`
}

// ParseLongPollRequest 从HTTP请求解析长轮询参数。
// REQ-I-005: since为ISO8601，wait整数秒，limit整数
// refWall 为单调时钟的墙钟参考时间点，用于将ISO8601 since转为单调纳秒。
func ParseLongPollRequest(r *http.Request, refWall time.Time) (*LongPollRequest, error) {
	req := &LongPollRequest{
		SinceNano: 0,
		Wait:      30,  // REQ-I-005: 默认30秒
		Limit:     500, // REQ-I-005: 默认500
	}

	// since参数：ISO8601字符串→解析为time.Time→转为monotonic纳秒
	// N-01-003 修复：支持数字时间戳（0=从头拉取），改善错误消息
	sinceStr := r.URL.Query().Get("since")
	// N-05-I1 修复：URL 解码将 ISO8601 时区偏移中的 '+' 转为空格，需还原
	sinceStr = strings.ReplaceAll(sinceStr, " ", "+")
	if sinceStr != "" {
		// 先尝试解析为数字（单调纳秒，0=从头拉取）
		if n, err := strconv.ParseInt(sinceStr, 10, 64); err == nil {
			req.SinceNano = n
		} else {
			t, err := time.Parse(time.RFC3339Nano, sinceStr)
			if err != nil {
				t, err = time.Parse(time.RFC3339, sinceStr)
				if err != nil {
					return nil, fmt.Errorf("invalid since parameter: expected RFC3339 timestamp or numeric value")
				}
			}
			// 将ISO8601转为单调时钟纳秒（基于refWall参考点的近似转换）
			req.SinceNano = t.Sub(refWall).Nanoseconds()
		}
	}

	// wait参数：默认30，最大60
	waitStr := r.URL.Query().Get("wait")
	if waitStr != "" {
		wait, err := strconv.Atoi(waitStr)
		if err != nil {
			return nil, fmt.Errorf("invalid wait parameter")
		}
		if wait < 1 {
			wait = 1
		}
		if wait > 60 { // REQ-I-005: 最大60秒
			wait = 60
		}
		req.Wait = wait
	}

	// limit参数：默认500
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			return nil, fmt.Errorf("invalid limit parameter")
		}
		if limit < 1 {
			return nil, fmt.Errorf("limit must be at least 1")
		}
		req.Limit = limit
	}

	return req, nil
}

// RingEvent 环形缓冲区中的事件。
type RingEvent struct {
	Nano    int64     // 单调时钟纳秒
	Time    time.Time // 墙钟时间（用于转ISO8601）
	Type    string    // 事件类型
	Payload any       // 事件数据
}

// DefaultEventRingCapacity 事件环形缓冲区容量
// REQ-2.9.7: 数值锁定 200 条
const DefaultEventRingCapacity = 200

// EventRingBuffer 事件环形缓冲。
// REQ-2.9.7: 200条上限，超出丢最旧
type EventRingBuffer struct {
	mu        sync.RWMutex
	ring      []RingEvent
	cap       int        // 200
	head      int        // 下一个写入位置
	size      int        // 当前事件数量
	startNano int64      // 缓冲区最早事件的monotonic纳秒
	refWall   time.Time  // 墙钟参考时间点
	sigMu     sync.Mutex // 保护signalCh
	signalCh  chan struct{} // 通知channel，Add时关闭并重建
}

// NewEventRingBuffer 创建环形缓冲区。
// REQ-2.9.7: capacity=200（数值锁定）
func NewEventRingBuffer(capacity int) *EventRingBuffer {
	return &EventRingBuffer{
		ring:     make([]RingEvent, capacity),
		cap:      capacity,
		refWall:  time.Now(),
		signalCh: make(chan struct{}),
	}
}

// Add 添加事件到环形缓冲。
// REQ-2.9.7: 超出200条时丢最旧
func (b *EventRingBuffer) Add(eventType string, payload any) {
	now := time.Now()
	nano := now.Sub(b.refWall).Nanoseconds()

	b.mu.Lock()

	b.ring[b.head] = RingEvent{
		Nano:    nano,
		Time:    now,
		Type:    eventType,
		Payload: payload,
	}

	b.head = (b.head + 1) % b.cap
	if b.size < b.cap {
		b.size++
	}

	// 更新startNano
	if b.size == 1 {
		b.startNano = nano
	} else if b.size == b.cap {
		// 缓冲区已满，最早事件在head位置
		b.startNano = b.ring[b.head].Nano
	}

	b.mu.Unlock()

	// 通知等待者
	b.sigMu.Lock()
	close(b.signalCh)
	b.signalCh = make(chan struct{})
	b.sigMu.Unlock()
}

// Since 从指定monotonic纳秒之后获取事件。
// 返回事件列表和has_more标记。
func (b *EventRingBuffer) Since(sinceNano int64, limit int) ([]RingEvent, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return nil, false
	}

	// 从最旧事件开始遍历，收集Nano > sinceNano的事件
	var all []RingEvent
	startIdx := (b.head - b.size + b.cap) % b.cap
	for i := 0; i < b.size; i++ {
		idx := (startIdx + i) % b.cap
		if b.ring[idx].Nano > sinceNano {
			all = append(all, b.ring[idx])
		}
	}

	if len(all) == 0 {
		return nil, false
	}

	if len(all) <= limit {
		return all, false
	}

	return all[:limit], true
}

// Recent 返回最新的 limit 条事件（按时间正序排列：旧→新）。
// N-05-I2 修复：recent 端点应返回最新事件而非最旧事件。
// 与 Since 不同，此方法从最新事件倒序收集，保证返回的是最近的 limit 条。
func (b *EventRingBuffer) Recent(limit int) []RingEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 || limit <= 0 {
		return nil
	}

	if limit > b.size {
		limit = b.size
	}

	// 从最新事件（head-1）开始倒序收集 limit 条
	result := make([]RingEvent, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (b.head - 1 - i + b.cap) % b.cap
		result = append(result, b.ring[idx])
	}

	// 反转为正序（旧→新），与 Since 返回顺序保持一致
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// WaitForEvent 等待新事件或context取消。
// 这是长轮询的核心：阻塞等待直到有新事件或context取消。
// 返回一个channel，当有新事件时关闭。
func (b *EventRingBuffer) WaitForEvent(ctx context.Context, sinceNano int64) <-chan struct{} {
	ch := make(chan struct{})

	go func() {
		defer close(ch)

		for {
			// 先获取当前signal channel（在检查数据之前）
			b.sigMu.Lock()
			sig := b.signalCh
			b.sigMu.Unlock()

			// 检查当前是否有新事件
			b.mu.RLock()
			if b.size > 0 {
				newestIdx := (b.head - 1 + b.cap) % b.cap
				if b.ring[newestIdx].Nano > sinceNano {
					b.mu.RUnlock()
					return
				}
			}
			b.mu.RUnlock()

			// 等待信号或context取消
			select {
			case <-ctx.Done():
				return
			case <-sig:
				// 有新事件可能添加，循环检查
			}
		}
	}()

	return ch
}

// WallToMono 将墙钟时间转为单调时钟纳秒（基于参考点的近似转换）。
func (b *EventRingBuffer) WallToMono(t time.Time) int64 {
	return t.Sub(b.refWall).Nanoseconds()
}

// MonoToWall 将单调时钟纳秒转为墙钟时间（基于参考点的近似转换）。
func (b *EventRingBuffer) MonoToWall(nano int64) time.Time {
	return b.refWall.Add(time.Duration(nano) * time.Nanosecond)
}

// RefWall 返回缓冲区的墙钟参考时间点，供ParseLongPollRequest使用。
func (b *EventRingBuffer) RefWall() time.Time {
	return b.refWall
}

// formatSince 将monotonic纳秒转为ISO8601带时区字符串。
func formatSince(nano int64, refWall time.Time) string {
	delta := time.Duration(nano) * time.Nanosecond
	return refWall.Add(delta).Format(time.RFC3339Nano)
}

// HandleLongPoll 通用长轮询处理函数。
// REQ-I-005: 有数据立即返回，无数据等待wait秒后返回空data
// refWall: 单调时钟的墙钟参考时间点
// fetcher: 从特定数据源获取since之后的数据，返回(data, nextSinceNano, hasMore, error)
// waitNotify: 可选的通知函数，用于在等待期间接收新事件通知，nil则使用纯超时等待
func HandleLongPoll(
	w http.ResponseWriter,
	r *http.Request,
	refWall time.Time,
	fetcher func(sinceNano int64, limit int) ([]any, int64, bool, error),
	waitNotify func(ctx context.Context, sinceNano int64) <-chan struct{},
) {
	// 1. 解析请求参数
	req, err := ParseLongPollRequest(r, refWall)
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}

	// 2. 调用fetcher获取数据
	data, nextSince, hasMore, err := fetcher(req.SinceNano, req.Limit)
	if err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	// 3. 如果有数据，立即返回
	if len(data) > 0 {
		respondJSON(w, http.StatusOK, LongPollResponse{
			Data:      data,
			NextSince: formatSince(nextSince, refWall),
			HasMore:   hasMore,
		})
		return
	}

	// 4. 如果无数据，等待wait秒（带context取消）
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(req.Wait)*time.Second)
	defer cancel()

	if waitNotify != nil {
		<-waitNotify(ctx, req.SinceNano)
	} else {
		<-ctx.Done()
	}

	// 5. 等待后再次获取数据，超时返回空data
	data, nextSince, hasMore, err = fetcher(req.SinceNano, req.Limit)
	if err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	// 如果nextSince为0（无事件），使用当前时间
	if nextSince == 0 {
		nextSince = time.Since(refWall).Nanoseconds()
	}

	respondJSON(w, http.StatusOK, LongPollResponse{
		Data:      data,
		NextSince: formatSince(nextSince, refWall),
		HasMore:   hasMore,
	})
}
