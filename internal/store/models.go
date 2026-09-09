package store

// 本文件定义 store 包的数据模型。字段名与设计稿 §七.2 完全一致。

// 七种任务状态（规格 §2.2.10，禁止新增成员）。
const (
	StatePending  = "pending"
	StateRunning  = "running"
	StateSuccess  = "success"
	StateFailed   = "failed"
	StateTimeout  = "timeout"
	StateCanceled = "canceled"
	StateKilled   = "killed"
)

var terminalStates = map[string]struct{}{
	StateSuccess:  {},
	StateFailed:   {},
	StateTimeout:  {},
	StateCanceled: {},
	StateKilled:   {},
}

// Topic kind 值域。
const (
	TopicKindOperation = "operation"
	TopicKindService   = "service"
	TopicKindExtension = "extension"
	TopicKindSystem    = "system"
)

// Notification source_type 值域。
const (
	SrcTypeService   = "service"
	SrcTypeExtension = "extension"
	SrcTypeSystem    = "system"
)

// Execution 操作执行记录。
type Execution struct {
	ID             string `json:"id"`
	OperationID    string `json:"operation_id"`
	OperationLabel string `json:"operation_label"`
	TopicID        string `json:"topic_id"`
	CreatedAt      int64  `json:"created_at"`
	FinishedAt     *int64 `json:"finished_at"`
	InterruptedAt  *int64 `json:"interrupted_at"`
}

// Run operation_run 表快照（state 沿用既有七种任务状态）。
type Run struct {
	RunID         string  `json:"run_id"`
	ExecutionID   string  `json:"execution_id"`
	Phase         string  `json:"phase"` // global / service
	ServiceName   *string `json:"service_name"`
	ExtensionName string  `json:"extension_name"`
	ActionID      string  `json:"action_id"`
	State         string  `json:"state"`
	StartedAt     *int64  `json:"started_at"`
	FinishedAt    *int64  `json:"finished_at"`
}

// Topic 通知主题。
type Topic struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	SourceName  string  `json:"source_name"`
	ExecutionID *string `json:"execution_id"`
	ServiceName *string `json:"service_name"`
	CreatedAt   int64   `json:"created_at"`
	ClosedAt    *int64  `json:"closed_at"`
	LastSeq     int64   `json:"last_seq"`
	ReadSeq     int64   `json:"read_seq"`
	DeletedAt   *int64  `json:"deleted_at"`
}

// Notification 单条不可变通知。
type Notification struct {
	ID            string  `json:"id"`
	TopicID       string  `json:"topic_id"`
	Seq           int64   `json:"seq"`
	Level         string  `json:"level"`
	Content       string  `json:"content"`
	CreatedAt     int64   `json:"created_at"`
	SourceType    string  `json:"source_type"`
	ServiceName   *string `json:"service_name"`
	ExtensionName *string `json:"extension_name"`
	ActionID      *string `json:"action_id"`
	RunID         *string `json:"run_id"`
	ExecutionID   *string `json:"execution_id"`
}

// PlannedRun 创建 Execution 时预登记的计划 Run 行。
type PlannedRun struct {
	RunID         string
	Phase         string // global / service
	ServiceName   *string
	ExtensionName string
	ActionID      string
}

// CreateExecutionInput 创建 Execution 的入参。
type CreateExecutionInput struct {
	ExecutionID    string
	OperationID    string
	OperationLabel string
	CreatedAt      int64
	Runs           []PlannedRun
}

// NotificationInput 追加通知入参（source_type ∈ {service, extension, system}）。
type NotificationInput struct {
	TopicID       string
	Level         string
	Content       string
	SourceType    string
	ServiceName   *string
	ExtensionName *string
	ActionID      *string
	RunID         *string
	ExecutionID   *string
}

// TopicFilter 通知主题列表筛选。
type TopicFilter struct {
	Kind        string
	Unread      bool
	Level       string
	SourceType  string
	ServiceName string
}

// TopicItem ListTopics 返回的每个主题行（含最新通知摘要与排序字段）。
type TopicItem struct {
	Topic
	// 最新通知（可能为 nil，Topic 无通知时）。
	LastLevel          *string `json:"last_level"`
	LastContent        *string `json:"last_content"`
	LastSourceType     *string `json:"last_source_type"`
	LastNotificationAt *int64  `json:"last_activity_at"`
}

// TopicDetail GetTopic 返回的主题详情（含摘要与计数）。
type TopicDetail struct {
	Topic
	NotificationCount int64   `json:"notification_count"`
	UnreadCount       int64   `json:"unread_count"`
	LastLevel         *string `json:"last_level"`
	LastContent       *string `json:"last_content"`
	LastCreatedAt     *int64  `json:"last_created_at"`
}

// ExecutionDetail GetExecution/ListExecutions 返回的执行及其关联 runs 快照。
type ExecutionDetail struct {
	Execution
	Runs []Run `json:"runs"`
}