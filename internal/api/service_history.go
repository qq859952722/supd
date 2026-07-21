package api

import (
	"sync"
	"time"

	"github.com/supdorg/supd/internal/core"
)

// --- ServiceHistoryStore ---

// ServiceHistoryStore 内存中的服务历史记录存储
type ServiceHistoryStore struct {
	mu        sync.Mutex
	history   map[string][]HistoryEntry
	deaths    map[string][]HistoryEntry
	maxPerSvc int // 每个服务保留的最大条目数
}

// NewServiceHistoryStore 创建服务历史存储
func NewServiceHistoryStore() *ServiceHistoryStore {
	return &ServiceHistoryStore{
		history:   make(map[string][]HistoryEntry),
		deaths:    make(map[string][]HistoryEntry),
		maxPerSvc: 100,
	}
}

// RecordStart 记录服务启动
func (s *ServiceHistoryStore) RecordStart(name string, pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := HistoryEntry{
		Time:   time.Now().Format(time.RFC3339),
		PID:    pid,
		Reason: "started",
	}
	s.history[name] = append(s.history[name], entry)
	// 限制条目数
	if len(s.history[name]) > s.maxPerSvc {
		s.history[name] = s.history[name][len(s.history[name])-s.maxPerSvc:]
	}
}

// RecordStop 记录服务停止
func (s *ServiceHistoryStore) RecordStop(name string, pid int, exitCode int, duration int64, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := HistoryEntry{
		Time:     time.Now().Format(time.RFC3339),
		PID:      pid,
		ExitCode: exitCode,
		Duration: duration,
		Reason:   reason,
	}
	s.history[name] = append(s.history[name], entry)
	if len(s.history[name]) > s.maxPerSvc {
		s.history[name] = s.history[name][len(s.history[name])-s.maxPerSvc:]
	}
}

// RecordDeath 记录服务异常退出
func (s *ServiceHistoryStore) RecordDeath(name string, pid int, exitCode int, duration int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := HistoryEntry{
		Time:     time.Now().Format(time.RFC3339),
		PID:      pid,
		ExitCode: exitCode,
		Duration: duration,
		Reason:   "crashed",
	}
	s.deaths[name] = append(s.deaths[name], entry)
	if len(s.deaths[name]) > s.maxPerSvc {
		s.deaths[name] = s.deaths[name][len(s.deaths[name])-s.maxPerSvc:]
	}
}

// GetHistory 获取服务历史
func (s *ServiceHistoryStore) GetHistory(name string) []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.history[name]
	if entries == nil {
		return []HistoryEntry{}
	}
	// 返回倒序副本（最新的在前）
	result := make([]HistoryEntry, len(entries))
	for i, e := range entries {
		result[len(entries)-1-i] = e
	}
	return result
}

// GetDeaths 获取服务异常退出记录
func (s *ServiceHistoryStore) GetDeaths(name string) []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.deaths[name]
	if entries == nil {
		return []HistoryEntry{}
	}
	// 返回倒序副本
	result := make([]HistoryEntry, len(entries))
	for i, e := range entries {
		result[len(entries)-1-i] = e
	}
	return result
}

// --- ServiceHistoryGetter 适配器 ---

type CoreHistoryGetter struct {
	ProcessMgr    *core.ProcessManager
	StateMachines map[string]*core.StateMachine
	Store         *ServiceHistoryStore
}

func (g *CoreHistoryGetter) GetServiceHistory(name string) []HistoryEntry {
	if g.Store != nil {
		return g.Store.GetHistory(name)
	}
	return []HistoryEntry{}
}

func (g *CoreHistoryGetter) GetServiceDeaths(name string) []HistoryEntry {
	if g.Store != nil {
		return g.Store.GetDeaths(name)
	}
	return []HistoryEntry{}
}
