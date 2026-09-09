package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/google/uuid"
)

// 写命令连续失败达到该阈值时置位错误态（磁盘满可观测状态，方案 A，2026-09-09）。
const consecutiveFailThreshold = 10

// Store SQLite 存储层。
type Store struct {
	db      *sql.DB
	dbPath  string
	writer  *writer
	epoch   string
	seq     atomic.Int64

	// changeCh 写命令成功（GlobalSeq 推进）时的内存广播信号。等待期不持有数据库连接。
	changeMu sync.Mutex
	changeCh chan struct{}

	failMu   sync.Mutex
	failures int
	lastErr  error
	errState bool
}

// Open 打开/创建 <baseDir>/data/supd.db。
// - MkdirAll(baseDir/data, 0750)
// - sql.Open("sqlite", "file:...?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
// - SetMaxOpenConns(1) 单连接策略（v5 确认）
// - Ping 校验失败返回明确错误，阻止启动（不静默降级——设计稿 §七.7）
// - 迁移在 Open 内执行，失败返回 error 阻止启动。
func Open(baseDir string) (*Store, error) {
	return openWithList(baseDir, migrations)
}

// openWithList 真正实现：使用指定迁移列表打开数据库。
// 生产 Open 调用固定 migrations；测试通过包内 test-only OpenWithMigrations 注入列表。
func openWithList(baseDir string, list []string) (*Store, error) {
	dataDir := filepath.Join(baseDir, "data")
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("create data dir %s: %w", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, "supd.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1) // 单连接策略（v5 确认）

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite ping failed for %s: %w", dbPath, err)
	}

	if err := runMigrations(db, list); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db migrations failed for %s: %w (schema version target %d)", dbPath, err, len(list))
	}

	s := &Store{
		db:       db,
		dbPath:   dbPath,
		epoch:    uuid.New().String(), // 启动 UUID，不落库
		changeCh: make(chan struct{}),
	}
	s.writer = newWriter(db, s)
	return s, nil
}

// Close 关闭存储：停止接收新命令、排空 writer 队列（带 deadline），再关闭连接。
// 纳入调用方关机预算。
func (s *Store) Close(timeout time.Duration) error {
	s.writer.close(timeout)
	return s.db.Close()
}

// Epoch 返回启动时生成的 UUID（重启后变化，供 changes 长轮询判定全量重载）。
func (s *Store) Epoch() string { return s.epoch }

// GlobalSeq 返回进程内存单调全局序列（写成功后 +1，不落库，重启归零）。
func (s *Store) GlobalSeq() int64 { return s.seq.Load() }

// signalChange 在任意写命令成功（GlobalSeq 推进）后广播一次新写事件（供 changes 长轮询等待）。
func (s *Store) signalChange() {
	s.changeMu.Lock()
	close(s.changeCh)
	s.changeCh = make(chan struct{})
	s.changeMu.Unlock()
}

// WaitForGlobalChange 等待 GlobalSeq 超过 since 的新写事件或 ctx 取消。
// 返回的 channel 在"有更新"或"ctx 取消"时关闭；等待期不持有数据库连接（纯内存广播）。
func (s *Store) WaitForGlobalChange(ctx context.Context, since int64) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		for {
			s.changeMu.Lock()
			sig := s.changeCh
			s.changeMu.Unlock()
			if s.seq.Load() > since {
				return
			}
			select {
			case <-sig:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// StoreError 磁盘满等可观测错误态。
// 返回 nil 表示正常；否则返回 {code:"store_unavailable"}，不暴露底层错误细节。
type StoreError struct {
	Code string `json:"code"`
}

// StoreError 返回当前错误态，供 API 顶层 store_error 字段暴露。
func (s *Store) StoreError() *StoreError {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	if s.errState {
		return &StoreError{Code: "store_unavailable"}
	}
	return nil
}

// recordFailure 由 writer goroutine 调用：写命令失败，连续计数 +1。
func (s *Store) recordFailure(err error) {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	s.failures++
	s.lastErr = err
	if s.failures >= consecutiveFailThreshold {
		s.errState = true
	}
}

// recordSuccess 由 writer goroutine 调用：写命令成功，连续失败计数归零并清除错误态。
func (s *Store) recordSuccess() {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	s.failures = 0
	s.lastErr = nil
	s.errState = false
}