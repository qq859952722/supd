package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// migrations 版本化迁移列表。索引 i 对应 schema 版本 i+1。
// 字段名与设计稿 §七.2 完全一致；operation_run.service_name 全局阶段为 NULL，
// 服务阶段必须非空，由 CHECK 约束与应用层双重校验。
var migrations = []string{
	// v1：初始 schema（四张表，含外键、索引、CHECK）。
	`
CREATE TABLE operation_execution (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL,
  operation_label TEXT NOT NULL,
  topic_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  finished_at INTEGER NULL,
  interrupted_at INTEGER NULL
);

CREATE TABLE operation_run (
  run_id TEXT PRIMARY KEY,
  execution_id TEXT NOT NULL REFERENCES operation_execution(id) ON DELETE CASCADE,
  phase TEXT NOT NULL,
  service_name TEXT NULL,
  extension_name TEXT NOT NULL,
  action_id TEXT NOT NULL,
  state TEXT NOT NULL,
  started_at INTEGER NULL,
  finished_at INTEGER NULL,
  CHECK ((phase='global' AND service_name IS NULL) OR (phase='service' AND service_name IS NOT NULL))
);

CREATE TABLE notification_topic (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('operation','service','extension','system')),
  source_name TEXT NOT NULL,
  execution_id TEXT NULL,
  service_name TEXT NULL,
  created_at INTEGER NOT NULL,
  closed_at INTEGER NULL,
  last_seq INTEGER NOT NULL DEFAULT 0,
  read_seq INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER NULL
);

CREATE TABLE notification (
  id TEXT PRIMARY KEY,
  topic_id TEXT NOT NULL REFERENCES notification_topic(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  level TEXT NOT NULL CHECK (level IN ('info','success','warning','error')),
  content TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  source_type TEXT NOT NULL CHECK (source_type IN ('service','extension','system')),
  service_name TEXT NULL,
  extension_name TEXT NULL,
  action_id TEXT NULL,
  run_id TEXT NULL,
  execution_id TEXT NULL,
  UNIQUE (topic_id, seq)
);

CREATE INDEX idx_notification_topic_seq ON notification (topic_id, seq);
CREATE INDEX idx_notification_topic_kind ON notification_topic (kind);
CREATE INDEX idx_operation_run_execution ON operation_run (execution_id);
CREATE INDEX idx_operation_execution_created ON operation_execution (created_at);

-- 默认 Topic 唯一性：仅对 kind IN (service, extension, system) 的默认 Topic 建部分唯一索引；
-- operation Topic 不参与该唯一性。
CREATE UNIQUE INDEX idx_notification_topic_default_unique
  ON notification_topic (source_name)
  WHERE kind IN ('service','extension','system') AND deleted_at IS NULL;
`,
	// v2：修正默认 Topic 唯一索引键为 (kind, source_name)。
	// v1 仅对 source_name 建索引，同名服务与同名全局扩展（不同 kind）会相互冲突；
	// 按设计稿 §七.2 约束默认 Topic 仅 (kind, source_name) 组合唯一，operation Topic 不受影响。
	`
DROP INDEX IF EXISTS idx_notification_topic_default_unique;
CREATE UNIQUE INDEX idx_notification_topic_default_unique
  ON notification_topic (kind, source_name)
  WHERE kind IN ('service','extension','system') AND deleted_at IS NULL;
`,
}

// currentVersion 读取 user_version pragma。
func currentVersion(db *sql.DB) (int, error) {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return v, nil
}

// splitStatements 按分号拆分 SQL 语句（schema 脚本不含引号内分号或注释，可直接拆分）。
func splitStatements(s string) []string {
	raw := strings.Split(s, ";")
	stmts := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		stmts = append(stmts, p)
	}
	return stmts
}

// runMigrations 逐版本按事务执行未应用迁移；任何失败回滚并返回错误（含版本号）。
func runMigrations(db *sql.DB, list []string) error {
	current, err := currentVersion(db)
	if err != nil {
		return err
	}
	if current > len(list) {
		return fmt.Errorf("db schema version %d is newer than supported version %d", current, len(list))
	}
	for i := current; i < len(list); i++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("migration v%d begin: %w", i+1, err)
		}
		ok := false
		defer func() {
			if !ok {
				_ = tx.Rollback()
			}
		}()
		for _, stmt := range splitStatements(list[i]) {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration v%d: %w", i+1, err)
			}
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			return fmt.Errorf("migration v%d set version: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			ok = false
			return fmt.Errorf("migration v%d commit: %w", i+1, err)
		}
		ok = true
	}
	return nil
}