package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// 保留策略硬编码数值（设计稿 §七.6，不新增配置字段）。
const (
	retentionDays          = 30
	executionMaxRows       = 200
	retentionInterval      = 10 * time.Minute
	maxNotificationsPerTopicFallback = maxNotificationsPerTopic
)

// RunRetention 在 writer 事务内执行保留清理：
//   - 删 30 天前 closed 的 Topic 及其 notification；
//   - execution >200 条或 >30 天：删最旧 execution 及其 operation_run（不删关联 topic）；
//   - 单 Topic >500 条淘汰最旧（兜底）。
func (s *Store) RunRetention(ctx context.Context, now int64) error {
	cmd := newNotBumpCommand(func(db *sql.DB) error {
		return runRetentionTx(db, now)
	})
	return s.writer.submit(ctx, cmd)
}

func newNotBumpCommand(run func(db *sql.DB) error) *command {
	return &command{run: run, result: make(chan error, 1), bumpSeq: false}
}

func runRetentionTx(db *sql.DB, now int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	ok := false
	defer func() { if !ok { _ = tx.Rollback() } }()

	cutoff := now - retentionDays*24*60*60*1000

	// 1) 30 天前 closed 的 Topic 及其通知。
	if _, err := tx.Exec(
		`DELETE FROM notification WHERE topic_id IN (
		   SELECT id FROM notification_topic WHERE closed_at IS NOT NULL AND created_at < ? AND deleted_at IS NULL
		 )`, cutoff); err != nil {
		return fmt.Errorf("retention delete notifications for closed topics: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM notification_topic WHERE closed_at IS NOT NULL AND created_at < ? AND deleted_at IS NULL`, cutoff); err != nil {
		return fmt.Errorf("retention delete closed topics: %w", err)
	}

	// 2) execution 保留：>30 天，或 >200 条时删最旧（连带 operation_run）。
	if err := pruneExecutions(tx, cutoff); err != nil {
		return err
	}

	// 3) 单 Topic 超 500 条淘汰最旧（兜底，与追加路径内联逻辑一致）。
	if err := pruneAllTopicOverflows(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		ok = false
		return err
	}
	ok = true
	return nil
}

// pruneExecutions 删除超期/超量的 execution 及其 operation_run。
func pruneExecutions(tx *sql.Tx, cutoff int64) error {
	rows, err := tx.Query(`SELECT id, created_at FROM operation_execution ORDER BY created_at ASC, id`)
	if err != nil {
		return err
	}
	type exRow struct {
		id        string
		createdAt int64
	}
	var all []exRow
	for rows.Next() {
		var r exRow
		if err := rows.Scan(&r.id, &r.createdAt); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	var toDelete []string
	for i, r := range all {
		if r.createdAt < cutoff || i >= executionMaxRows {
			toDelete = append(toDelete, r.id)
		}
	}
	if len(toDelete) == 0 {
		return nil
	}
	ids := make([]any, 0, len(toDelete)+len(toDelete))
	placeholders := ""
	for i, id := range toDelete {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		ids = append(ids, id)
	}
	if _, err := tx.Exec(`DELETE FROM operation_run WHERE execution_id IN (`+placeholders+`)`, ids...); err != nil {
		return fmt.Errorf("retention delete runs: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM operation_execution WHERE id IN (`+placeholders+`)`, ids...); err != nil {
		return fmt.Errorf("retention delete executions: %w", err)
	}
	return nil
}

// pruneAllTopicOverflows 对所有超过 500 条的 Topic 淘汰最旧通知。
func pruneAllTopicOverflows(tx *sql.Tx) error {
	rows, err := tx.Query(
		`SELECT topic_id, COUNT(*) FROM notification GROUP BY topic_id HAVING COUNT(*) > ? ORDER BY topic_id`,
		maxNotificationsPerTopicFallback)
	if err != nil {
		return err
	}
	type overflow struct {
		topicID string
		count   int64
	}
	var overs []overflow
	for rows.Next() {
		var o overflow
		if err := rows.Scan(&o.topicID, &o.count); err != nil {
			rows.Close()
			return err
		}
		overs = append(overs, o)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, o := range overs {
		excess := o.count - maxNotificationsPerTopicFallback
		if err := pruneTopicNotifications(tx, o.topicID, maxNotificationsPerTopicFallback); err != nil {
			return fmt.Errorf("retention prune topic %s: %w", o.topicID, err)
		}
		_ = excess
	}
	return nil
}

// StartRetentionLoop 启动保留清理：启动时立即执行一次 + 每 10 分钟定时执行。
// ctx 取消后停止。本节点提供启动接口，由接入方（06-8 / 运行装配处）触发；
// 定时器硬编码 10 分钟（不新增配置字段）。
func (s *Store) StartRetentionLoop(ctx context.Context) {
	go s.retentionLoop(ctx)
}

func (s *Store) retentionLoop(ctx context.Context) {
	// 启动时立即执行一次。
	if err := s.RunRetention(ctx, nowMillis()); err != nil {
		_ = err
	}
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.RunRetention(ctx, nowMillis()); err != nil {
				_ = err
			}
		}
	}
}