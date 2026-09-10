package store

import (
	"context"
	"database/sql"
	"errors"
)

func executionSelect() string {
	return `SELECT id, operation_id, operation_label, topic_id, created_at, finished_at, interrupted_at FROM operation_execution`
}

func scanExecutionRow(sc interface{ Scan(...any) error }) (Execution, error) {
	var e Execution
	var f, i sql.NullInt64
	err := sc.Scan(&e.ID, &e.OperationID, &e.OperationLabel, &e.TopicID, &e.CreatedAt, &f, &i)
	e.FinishedAt = nullToPtr(f)
	e.InterruptedAt = nullToPtr(i)
	return e, err
}

func loadRuns(ctx context.Context, db *sql.DB, executionID string) ([]Run, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT run_id, execution_id, phase, service_name, extension_name, action_id, state, started_at, finished_at
		 FROM operation_run WHERE execution_id = ? ORDER BY phase, run_id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuns(rows)
}

func scanRuns(rows *sql.Rows) ([]Run, error) {
	runs := []Run{}
	for rows.Next() {
		var r Run
		var sv sql.NullString
		var sa, fa sql.NullInt64
		if err := rows.Scan(&r.RunID, &r.ExecutionID, &r.Phase, &sv, &r.ExtensionName, &r.ActionID, &r.State, &sa, &fa); err != nil {
			return nil, err
		}
		r.ServiceName = strNullToPtr(sv)
		r.StartedAt = nullToPtr(sa)
		r.FinishedAt = nullToPtr(fa)
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// GetExecution 返回 execution 及其关联 runs 快照。
func (s *Store) GetExecution(ctx context.Context, id string) (*ExecutionDetail, error) {
	e, err := scanExecutionRow(s.db.QueryRowContext(ctx, executionSelect()+` WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	runs, err := loadRuns(ctx, s.db, e.ID)
	if err != nil {
		return nil, err
	}
	return &ExecutionDetail{Execution: e, Runs: runs}, nil
}

// ListExecutions 返回 execution 分页列表（含关联 runs 快照），按 created_at 倒序。
func (s *Store) ListExecutions(ctx context.Context, limit, offset int) ([]ExecutionDetail, error) {
	rows, err := s.db.QueryContext(ctx, executionSelect()+` ORDER BY created_at DESC, id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	var execs []Execution
	for rows.Next() {
		e, err := scanExecutionRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		execs = append(execs, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	out := make([]ExecutionDetail, 0, len(execs))
	for _, e := range execs {
		runs, err := loadRuns(ctx, s.db, e.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, ExecutionDetail{Execution: e, Runs: runs})
	}
	return out, nil
}

// GetLastExecutionForOperation 返回某操作最近一次 execution；无记录返回 nil。
func (s *Store) GetLastExecutionForOperation(ctx context.Context, operationID string) (*Execution, error) {
	e, err := scanExecutionRow(s.db.QueryRowContext(ctx,
		executionSelect()+` WHERE operation_id = ? ORDER BY created_at DESC, id LIMIT 1`, operationID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListUnfinishedExecutions 返回未完成 execution（finished_at IS NULL AND interrupted_at IS NULL），
// 供重启恢复。
func (s *Store) ListUnfinishedExecutions(ctx context.Context) ([]Execution, error) {
	rows, err := s.db.QueryContext(ctx,
		executionSelect()+` WHERE finished_at IS NULL AND interrupted_at IS NULL ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Execution{} // 非 nil：保持与包内其他列表查询一致（null 序列化防护）
	for rows.Next() {
		e, err := scanExecutionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func topicBaseSelect() string {
	return `SELECT notification_topic.id, notification_topic.kind, notification_topic.source_name, notification_topic.execution_id,
	                notification_topic.service_name, notification_topic.created_at, notification_topic.closed_at,
	                notification_topic.last_seq, notification_topic.read_seq, notification_topic.deleted_at
	         FROM notification_topic`
}

// topicTargets 生成按 topicBaseSelect 列顺序的扫描目标（前 10 列）。
// scs[0]=execution_id(NullString) scs[1]=service_name(NullString)
// scs[2]=closed_at(NullInt64) scs[3]=deleted_at(NullInt64)。
func topicTargets(t *Topic, scs []any) []any {
	return []any{
		&t.ID, &t.Kind, &t.SourceName,
		scs[0], scs[1],
		&t.CreatedAt,
		scs[2],
		&t.LastSeq, &t.ReadSeq,
		scs[3],
	}
}

// ListTopics 按筛选返回主题列表。按每个 Topic 最新 notification.created_at 倒序；
// 无通知的 Topic 使用 Topic.created_at。
func (s *Store) ListTopics(ctx context.Context, f TopicFilter) ([]TopicItem, error) {
	unread := ""
	if f.Unread {
		unread = "1"
	}
	q := `SELECT notification_topic.id, notification_topic.kind, notification_topic.source_name, notification_topic.execution_id,
	                notification_topic.service_name, notification_topic.created_at, notification_topic.closed_at,
	                notification_topic.last_seq, notification_topic.read_seq, notification_topic.deleted_at,
	                n.level, n.content, n.source_type, n.created_at
	       FROM notification_topic
	       LEFT JOIN notification n ON n.topic_id = notification_topic.id AND n.seq = notification_topic.last_seq
	       WHERE notification_topic.deleted_at IS NULL
	         AND (?1 = '' OR notification_topic.kind = ?1)
	         AND (?2 = '' OR notification_topic.last_seq > notification_topic.read_seq)
	         AND (?3 = '' OR n.level = ?3)
	         AND (?4 = '' OR (n.source_type = ?4 OR notification_topic.kind = ?4))
	         AND (?5 = '' OR notification_topic.service_name = ?5)
	       ORDER BY COALESCE(n.created_at, notification_topic.created_at) DESC, notification_topic.id`
	rows, err := s.db.QueryContext(ctx, q, f.Kind, unread, f.Level, f.SourceType, f.ServiceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopicItem{} // 非 nil：避免空列表 JSON 序列化为 null（前端 .some() 崩溃）
	for rows.Next() {
		var t Topic
		var eid, sv sql.NullString
		var closed, del sql.NullInt64
		scs := []any{&eid, &sv, &closed, &del}
		var level, content, src sql.NullString
		var lastAt sql.NullInt64
		targets := append(topicTargets(&t, scs), &level, &content, &src, &lastAt)
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		t.ExecutionID = strNullToPtr(eid)
		t.ServiceName = strNullToPtr(sv)
		t.ClosedAt = nullToPtr(closed)
		t.DeletedAt = nullToPtr(del)
		out = append(out, TopicItem{
			Topic:              t,
			LastLevel:          sqlNullToStrPtr(level),
			LastContent:        sqlNullToStrPtr(content),
			LastSourceType:     sqlNullToStrPtr(src),
			LastNotificationAt: nullToPtr(lastAt),
		})
	}
	return out, rows.Err()
}

// GetOperationTopic 按 execution_id 返回该操作执行关联的 operation Topic；不存在/已删除返回 nil。
// 供 NotifyRouter 操作 Run 路由决策（§六.2 情形 a）判断 Topic 是否已关闭/删除。
func (s *Store) GetOperationTopic(ctx context.Context, executionID string) (*Topic, error) {
	var t Topic
	var eid, sv sql.NullString
	var closed, del sql.NullInt64
	scs := []any{&eid, &sv, &closed, &del}
	err := s.db.QueryRowContext(ctx,
		topicBaseSelect()+` WHERE execution_id = ? AND deleted_at IS NULL`, executionID,
	).Scan(topicTargets(&t, scs)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.ExecutionID = strNullToPtr(eid)
	t.ServiceName = strNullToPtr(sv)
	t.ClosedAt = nullToPtr(closed)
	t.DeletedAt = nullToPtr(del)
	return &t, nil
}

// GetTopic 返回主题详情（含最后摘要与计数）；不存在返回 nil。
func (s *Store) GetTopic(ctx context.Context, id string) (*TopicDetail, error) {
	q := `
		SELECT t.id, t.kind, t.source_name, t.execution_id, t.service_name, t.created_at, t.closed_at, t.last_seq, t.read_seq, t.deleted_at,
		       (SELECT COUNT(*) FROM notification WHERE topic_id = t.id),
		       (SELECT level FROM notification WHERE topic_id = t.id ORDER BY seq DESC LIMIT 1),
		       (SELECT content FROM notification WHERE topic_id = t.id ORDER BY seq DESC LIMIT 1),
		       (SELECT created_at FROM notification WHERE topic_id = t.id ORDER BY seq DESC LIMIT 1)
		FROM notification_topic t WHERE t.id = ? AND t.deleted_at IS NULL`
	var t Topic
	var eid, sv sql.NullString
	var closed, del sql.NullInt64
	scs := []any{&eid, &sv, &closed, &del}
	var cnt int64
	var level, content sql.NullString
	var lastAt sql.NullInt64
	targets := append(topicTargets(&t, scs), &cnt, &level, &content, &lastAt)
	err := s.db.QueryRowContext(ctx, q, id).Scan(targets...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.ExecutionID = strNullToPtr(eid)
	t.ServiceName = strNullToPtr(sv)
	t.ClosedAt = nullToPtr(closed)
	t.DeletedAt = nullToPtr(del)
	unread := t.LastSeq - t.ReadSeq
	if unread < 0 {
		unread = 0
	}
	return &TopicDetail{
		Topic:             t,
		NotificationCount: cnt,
		UnreadCount:       unread,
		LastLevel:         sqlNullToStrPtr(level),
		LastContent:       sqlNullToStrPtr(content),
		LastCreatedAt:     nullToPtr(lastAt),
	}, nil
}

// ListNotifications 按 seq 正序分页返回某 Topic 的通知。
func (s *Store) ListNotifications(ctx context.Context, topicID string, sinceSeq int64, limit int) ([]Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, topic_id, seq, level, content, created_at, source_type, service_name, extension_name, action_id, run_id, execution_id
		 FROM notification WHERE topic_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?`,
		topicID, sinceSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNotifications(rows)
}

func scanNotifications(rows *sql.Rows) ([]Notification, error) {
	out := []Notification{} // 非 nil：避免空列表 JSON 序列化为 null
	for rows.Next() {
		var n Notification
		if err := scanNotificationRow(rows, &n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func scanNotificationRow(row interface{ Scan(...any) error }, n *Notification) error {
	var sv, ev, aid, rid, eid sql.NullString
	err := row.Scan(&n.ID, &n.TopicID, &n.Seq, &n.Level, &n.Content, &n.CreatedAt, &n.SourceType,
		&sv, &ev, &aid, &rid, &eid)
	n.ServiceName = strNullToPtr(sv)
	n.ExtensionName = strNullToPtr(ev)
	n.ActionID = strNullToPtr(aid)
	n.RunID = strNullToPtr(rid)
	n.ExecutionID = strNullToPtr(eid)
	return err
}

func sqlNullToStrPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func strNullToPtr(n sql.NullString) *string {
	if !n.Valid || n.String == "" {
		return nil
	}
	s := n.String
	return &s
}

func nullToPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
