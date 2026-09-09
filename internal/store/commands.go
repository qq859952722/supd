package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// nowMillis 返回当前 Unix 毫秒时间戳（本包统一使用毫秒存储 INTEGER 时间）。
func nowMillis() int64 { return time.Now().UnixMilli() }

// maxNotificationsPerTopic 单 Topic 通知上限 500 条（设计稿 §七.6，超出淘汰最旧）。
const maxNotificationsPerTopic = 500

// ErrTopicNotFound 指定 Topic 不存在（供 API 层映射 404）。
var ErrTopicNotFound = errors.New("topic not found")
var errRunNotFound = errors.New("operation run not found")

func newAuthoritativeCommand(run func(db *sql.DB) error) *command {
	return &command{run: run, result: make(chan error, 1), bumpSeq: true}
}

// CreateExecution 在一个事务内创建 execution + topic(kind=operation) + planned operation_run 行。
// 返回生成的 operation TopicID。
func (s *Store) CreateExecution(ctx context.Context, in CreateExecutionInput) (string, error) {
	topicID := uuid.New().String()
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		ok := false
		defer func() { if !ok { _ = tx.Rollback() } }()

		if _, err := tx.Exec(
			`INSERT INTO operation_execution (id, operation_id, operation_label, topic_id, created_at, finished_at, interrupted_at)
			 VALUES (?, ?, ?, ?, ?, NULL, NULL)`,
			in.ExecutionID, in.OperationID, in.OperationLabel, topicID, in.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert execution: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO notification_topic (id, kind, source_name, execution_id, service_name, created_at, closed_at, last_seq, read_seq, deleted_at)
			 VALUES (?, 'operation', ?, ?, NULL, ?, NULL, 0, 0, NULL)`,
			topicID, in.OperationID, in.ExecutionID, in.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert operation topic: %w", err)
		}
		for _, r := range in.Runs {
			if err := insertPlannedRun(tx, in.ExecutionID, r); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			ok = false
			return err
		}
		ok = true
		return nil
	})
	if err := s.writer.submit(ctx, cmd); err != nil {
		return "", err
	}
	return topicID, nil
}

func insertPlannedRun(tx *sql.Tx, executionID string, r PlannedRun) error {
	var serviceName any
	if r.ServiceName != nil {
		serviceName = *r.ServiceName
	}
	if _, err := tx.Exec(
		`INSERT INTO operation_run (run_id, execution_id, phase, service_name, extension_name, action_id, state, started_at, finished_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		r.RunID, executionID, r.Phase, serviceName, r.ExtensionName, r.ActionID, StatePending,
	); err != nil {
		return fmt.Errorf("insert planned run %s: %w", r.RunID, err)
	}
	return nil
}

// AddPlannedRuns 在第二阶段事务中追加 planned operation_run 行（服务阶段）。
// 全局阶段在 CreateExecution 事务中已预登记；服务阶段匹配的服务响应者在本方法创建（§四.1 第二事务）。
func (s *Store) AddPlannedRuns(ctx context.Context, executionID string, runs []PlannedRun) error {
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		ok := false
		defer func() { if !ok { _ = tx.Rollback() } }()
		for _, r := range runs {
			if err := insertPlannedRun(tx, executionID, r); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			ok = false
			return err
		}
		ok = true
		return nil
	})
	return s.writer.submit(ctx, cmd)
}

func isTerminal(state string) bool {
	_, ok := terminalStates[state]
	return ok
}

// allowedTransition 判断状态前进是否合法：仅 pending→running→终态前进，不回退。
func allowedTransition(cur, next string) bool {
	if isTerminal(cur) {
		return false // 已终态，忽略后续状态更新
	}
	if cur == StatePending {
		return next == StateRunning || isTerminal(next)
	}
	if cur == StateRunning {
		return next == StateRunning || isTerminal(next)
	}
	return false
}

// UpdateRunState 幂等更新 Run 状态：仅 pending→running→终态前进，不允许回退；重复终态忽略。
func (s *Store) UpdateRunState(ctx context.Context, runID string, state string, startedAt, finishedAt *int64) error {
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		ok := false
		defer func() { if !ok { _ = tx.Rollback() } }()

		var cur string
		err = tx.QueryRow(`SELECT state FROM operation_run WHERE run_id = ?`, runID).Scan(&cur)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errRunNotFound
			}
			return err
		}
		if !allowedTransition(cur, state) {
			// 无前进动作（重复终态/回退），视为幂等成功，不落字。
			_ = tx.Rollback()
			return nil
		}
		if _, err := tx.Exec(
			`UPDATE operation_run SET state = ?,
			   started_at = CASE WHEN ? <= 0 THEN started_at ELSE ? END,
			   finished_at = CASE WHEN ? <= 0 THEN finished_at ELSE ? END
			 WHERE run_id = ?`,
			state,
			int64PtrOrZero(startedAt), int64PtrOrZero(startedAt),
			int64PtrOrZero(finishedAt), int64PtrOrZero(finishedAt),
			runID,
		); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			ok = false
			return err
		}
		ok = true
		return nil
	})
	return s.writer.submit(ctx, cmd)
}

// FinishExecution 标记 Execution 完成。
func (s *Store) FinishExecution(ctx context.Context, execID string, finishedAt int64) error {
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		res, err := db.Exec(`UPDATE operation_execution SET finished_at = ? WHERE id = ? AND finished_at IS NULL`, finishedAt, execID)
		if err != nil {
			return err
		}
		_ = res
		return nil
	})
	return s.writer.submit(ctx, cmd)
}

// InterruptExecution 标记 Execution 因重启中断（写 interrupted_at；operation_run.state 保留原值不改终态）。
func (s *Store) InterruptExecution(ctx context.Context, execID string, interruptedAt int64) error {
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE operation_execution SET interrupted_at = ? WHERE id = ? AND interrupted_at IS NULL`, interruptedAt, execID)
		return err
	})
	return s.writer.submit(ctx, cmd)
}

// appendNotificationCmd 构造追加通知命令（同一事务：last_seq+1 → INSERT → UPDATE last_seq，
// 单 Topic 超 500 条淘汰最旧）。
func appendNotificationCmd(in NotificationInput) *command {
	c := &command{result: make(chan error, 1), bumpSeq: true}
	c.run = func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		ok := false
		defer func() { if !ok { _ = tx.Rollback() } }()
		if err := appendInTx(tx, in); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			ok = false
			return err
		}
		ok = true
		return nil
	}
	return c
}

// ensureDefaultTopic 在事务内按 (kind, source_name) 获取默认 Topic；不存在则创建。
// 默认 Topic（kind=service/extension/system）由首次通知自动创建（设计稿 §六.2 情形 d）。
// 唯一索引约束 (kind, source_name) WHERE deleted_at IS NULL 担保同 kind 同 source 单默认 Topic。
// source_name 语义：service 默认 Topic → 服务名；extension 默认 Topic → 扩展名。
// kind=service 时设置 service_name 列供 ListTopics 按服务名筛选；operation Topic 不经过本函数。
func ensureDefaultTopic(tx *sql.Tx, kind, sourceName string) (string, error) {
	var id string
	err := tx.QueryRow(
		`SELECT id FROM notification_topic WHERE kind = ? AND source_name = ? AND deleted_at IS NULL`,
		kind, sourceName,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = uuid.New().String() // 默认 Topic 首次创建时生成 UUIDv7
	var svc any
	if kind == TopicKindService {
		svc = sourceName
	}
	if _, err := tx.Exec(
		`INSERT INTO notification_topic (id, kind, source_name, execution_id, service_name, created_at, closed_at, last_seq, read_seq, deleted_at)
		 VALUES (?, ?, ?, NULL, ?, ?, NULL, 0, 0, NULL)`,
		id, kind, sourceName, svc, nowMillis(),
	); err != nil {
		// 并发路径（极罕见）下唯一索引冲突：writer 单线程下不会发生，防御性兜底复查询一次。
		if isUniqueViolation(err) {
			if err2 := tx.QueryRow(
				`SELECT id FROM notification_topic WHERE kind = ? AND source_name = ? AND deleted_at IS NULL`,
				kind, sourceName,
			).Scan(&id); err2 == nil {
				return id, nil
			}
		}
		return "", err
	}
	return id, nil
}

// TryAppendToDefaultTopic 默认 Topic 上追加通知（不存在则同一事务内创建），非阻塞。
// 队列满返回 false（丢弃并计数）；kind=source 命中已存在或新建的默认 Topic 后，
// 在同一事务内追加 in（in.TopicID 由本方法覆盖为默认 Topic id）。
func (s *Store) TryAppendToDefaultTopic(kind, sourceName string, in NotificationInput) bool {
	c := &command{result: make(chan error, 1), bumpSeq: true}
	c.run = func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		ok := false
		defer func() { if !ok { _ = tx.Rollback() } }()
		topicID, err := ensureDefaultTopic(tx, kind, sourceName)
		if err != nil {
			return err
		}
		in.TopicID = topicID
		if err := appendInTx(tx, in); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			ok = false
			return err
		}
		ok = true
		return nil
	}
	return s.writer.tryEnqueue(c)
}

// appendInTx 在已开启的事务内追加一条通知（last_seq+1 → INSERT → UPDATE last_seq → 超500淘汰最旧）。
// 供 appendNotificationCmd 与 TryAppendToDefaultTopic 复用；调用方负责事务 Begin/Commit/Rollback。
func appendInTx(tx *sql.Tx, in NotificationInput) error {
	var lastSeq int64
	if err := tx.QueryRow(`SELECT last_seq FROM notification_topic WHERE id = ? AND deleted_at IS NULL`, in.TopicID).Scan(&lastSeq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTopicNotFound
		}
		return err
	}
	newSeq := lastSeq + 1
	id := uuid.New().String()
	if _, err := tx.Exec(
		`INSERT INTO notification (id, topic_id, seq, level, content, created_at, source_type,
		   service_name, extension_name, action_id, run_id, execution_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.TopicID, newSeq, in.Level, in.Content, nowMillis(), in.SourceType,
		nullableStr(in.ServiceName), nullableStr(in.ExtensionName), nullableStr(in.ActionID),
		nullableStr(in.RunID), nullableStr(in.ExecutionID),
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE notification_topic SET last_seq = ? WHERE id = ?`, newSeq, in.TopicID); err != nil {
		return err
	}
	return pruneTopicNotifications(tx, in.TopicID, maxNotificationsPerTopic)
}

// pruneTopicNotifications 单 Topic 超过 max 时淘汰最旧通知（同事务）。
func pruneTopicNotifications(tx *sql.Tx, topicID string, max int64) error {
	var count int64
	if err := tx.QueryRow(`SELECT COUNT(*) FROM notification WHERE topic_id = ?`, topicID).Scan(&count); err != nil {
		return err
	}
	if count <= max {
		return nil
	}
	excess := count - max
	_, err := tx.Exec(
		`DELETE FROM notification WHERE topic_id = ? AND seq IN (
		   SELECT seq FROM notification WHERE topic_id = ? ORDER BY seq ASC LIMIT ?
		 )`, topicID, topicID, excess,
	)
	return err
}

// AppendNotification 同步追加通知（走 writer，等待结果）。
func (s *Store) AppendNotification(ctx context.Context, in NotificationInput) error {
	return s.writer.submit(ctx, appendNotificationCmd(in))
}

// TryAppendNotification 非阻塞追加通知：队列满时返回 false（丢弃并计数），不阻塞调用方。
// 用于 stdout reader 通知写入路径。
func (s *Store) TryAppendNotification(in NotificationInput) bool {
	return s.writer.tryEnqueue(appendNotificationCmd(in))
}

// CloseTopic 关闭 Topic（普通命令排队，保证先到的通知先执行——有序关闭语义）。
func (s *Store) CloseTopic(ctx context.Context, topicID string, closedAt int64) error {
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE notification_topic SET closed_at = ? WHERE id = ? AND closed_at IS NULL AND deleted_at IS NULL`, closedAt, topicID)
		return err
	})
	return s.writer.submit(ctx, cmd)
}

// MarkRead 已读游标只前进（max 语义）。
func (s *Store) MarkRead(ctx context.Context, topicID string, seq int64) error {
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE notification_topic SET read_seq = ? WHERE id = ? AND read_seq < ? AND deleted_at IS NULL`, seq, topicID, seq)
		return err
	})
	return s.writer.submit(ctx, cmd)
}

// DeleteTopic 软删除 Topic。
func (s *Store) DeleteTopic(ctx context.Context, topicID string, deletedAt int64) error {
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE notification_topic SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, deletedAt, topicID)
		return err
	})
	return s.writer.submit(ctx, cmd)
}

// DeleteAllTopics 清空：软删除全部未删除 Topic（级联保留关联 Notification/Execution/Run 数据行）。
func (s *Store) DeleteAllTopics(ctx context.Context, deletedAt int64) error {
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE notification_topic SET deleted_at = ? WHERE deleted_at IS NULL`, deletedAt)
		return err
	})
	return s.writer.submit(ctx, cmd)
}

// isUniqueViolation 判断 SQLite 唯一约束冲突错误（驱动错误字符串包含 "UNIQUE constraint failed"）。
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// MarkAllRead 将所有未删除 Topic 标记为已读。
func (s *Store) MarkAllRead(ctx context.Context) error {
	cmd := newAuthoritativeCommand(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE notification_topic SET read_seq = last_seq WHERE deleted_at IS NULL AND read_seq < last_seq`)
		return err
	})
	return s.writer.submit(ctx, cmd)
}

func nullableStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func int64PtrOrZero(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}