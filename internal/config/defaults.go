package config

// ExtensionHardLimitSeconds 扩展执行时长硬上限（秒）
// REQ-2.2.8: 数值锁定 1800 秒
// O-05-001: 提取为常量，避免字面量重复
const ExtensionHardLimitSeconds = 1800

// DefaultStopGraceSeconds 停止流程默认优雅期（秒）
// REQ-2.1.4: 数值锁定 10 秒
const DefaultStopGraceSeconds = 10

// DefaultStopTimeoutSeconds 停止流程默认总超时（秒）
// REQ-2.1.4: 数值锁定 60 秒
const DefaultStopTimeoutSeconds = 60

// DefaultExtensionTimeoutSeconds 扩展默认超时（秒）
// REQ-2.2.3, REQ-2.2.8: 数值锁定 600 秒
const DefaultExtensionTimeoutSeconds = 600

// DefaultRetentionDays 数据保留天数（任务历史、运行历史共用）
// REQ-2.2.9: 任务历史保留 7 天（内存）
const DefaultRetentionDays = 7

// DefaultFileHistoryVersions 文件历史版本数
// REQ-2.3.1: 数值锁定 50 个
const DefaultFileHistoryVersions = 50

// DefaultRunHistoryRetentionSeconds 运行历史保留秒数（由 DefaultRetentionDays 派生）
// REQ-2.2.9: 数值锁定 7 天
func DefaultRunHistoryRetentionSeconds() int {
	return DefaultRetentionDays * 24 * 60 * 60
}

// SetDefaults 为未设置的字段填充默认值
// REQ-D-006: 所有字段都有默认值
// REQ-2.4.3: 数值锁定清单中的值必须与需求规格说明书一致
func SetDefaults(cfg *Config) {
	s := &cfg.Settings
	if s.HTTPListen == "" {
		s.HTTPListen = ":8080"
	}
	if s.AuthMode == "" {
		s.AuthMode = "local_skip"
	}
	if len(s.LocalNetworks) == 0 {
		s.LocalNetworks = []string{
			"192.168.0.0/16",
			"10.0.0.0/8",
			"127.0.0.0/8",
			"172.16.0.0/12",
		}
	}
	if s.LogMaxSizeMB == 0 {
		s.LogMaxSizeMB = 10
	}
	if s.LogMaxFiles == 0 {
		s.LogMaxFiles = 5
	}
	if s.LogLevel == "" {
		s.LogLevel = "info"
	}
	if s.ShutdownGraceSeconds == 0 {
		s.ShutdownGraceSeconds = 30
	}
	if s.ExtensionDefaultTimeoutSeconds == 0 {
		s.ExtensionDefaultTimeoutSeconds = DefaultExtensionTimeoutSeconds
	}
	if s.ExtensionHardLimitSeconds == 0 {
		s.ExtensionHardLimitSeconds = ExtensionHardLimitSeconds
	}
	if s.RunHistoryRetentionSeconds == 0 {
		s.RunHistoryRetentionSeconds = DefaultRunHistoryRetentionSeconds() // 7天
	}
	if s.FileHistoryVersions == 0 {
		s.FileHistoryVersions = DefaultFileHistoryVersions
	}
	if s.MaxUploadSizeMB == 0 {
		s.MaxUploadSizeMB = 100
	}

	// 默认env文件
	if len(cfg.EnvFiles) == 0 {
		cfg.EnvFiles = []string{"env/00-base.yaml"}
	}

	// 默认扩展目录
	if len(cfg.ExtensionDirs) == 0 {
		cfg.ExtensionDirs = []string{"extensions/"}
	}

	// 默认重启策略
	d := &cfg.Defaults.Restart
	if d.Policy == "" {
		d.Policy = "always"
	}
	if d.BackoffMs == 0 {
		d.BackoffMs = 1000
	}
	if d.MaxBackoffMs == 0 {
		d.MaxBackoffMs = 30000
	}
	if d.Multiplier == 0 {
		d.Multiplier = 2
	}
	if d.MaxRetries == 0 {
		d.MaxRetries = 0 // 0 = 不限制
	}
	if d.ResetAfterSeconds == 0 {
		d.ResetAfterSeconds = 300
	}
}
