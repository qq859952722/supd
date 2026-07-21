package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/extension"
	"github.com/supdorg/supd/internal/watch"
)

// TestSaveHistory_RetentionLimit50 验证文件历史版本50上限淘汰逻辑。
// L-02-001: 规格§2.3.1 文件历史版本50个；覆盖 adapters.go:1425-1461 saveHistory 淘汰逻辑。
// 使用 SnapshotFile（直接调用 saveHistory）隔离测试淘汰行为。
func TestSaveHistory_RetentionLimit50(t *testing.T) {
	tests := []struct {
		name           string
		maxVersions    int
		saves          int
		wantVersions   int
		wantOldestGone bool // v001 是否应被淘汰
		wantOldestVer  int  // 期望的最旧版本号
		wantLatestVer  int  // 期望的最新版本号
	}{
		{"UnderLimit_49Saves", 50, 49, 49, false, 1, 49},
		{"AtLimit_50Saves", 50, 50, 50, false, 1, 50},
		{"OverLimit_51Saves_EvictsOldest", 50, 51, 50, true, 2, 51},
		{"DefaultMaxVersions50_WhenZero", 0, 51, 50, true, 2, 51},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			historyDir := filepath.Join(baseDir, "history")
			srcFile := filepath.Join(baseDir, "config.yaml")

			if err := os.WriteFile(srcFile, []byte("version: 1\n"), 0644); err != nil {
				t.Fatalf("setup: write source file: %v", err)
			}

			p := &OsFileProvider{
				BaseDir:       baseDir,
				PathValidator: NewPathValidator(baseDir),
				HistoryDir:    historyDir,
				MaxVersions:   tt.maxVersions,
			}

			for i := 0; i < tt.saves; i++ {
				if err := p.SnapshotFile(srcFile); err != nil {
					t.Fatalf("SnapshotFile %d: %v", i+1, err)
				}
			}

			versions, err := p.FileHistory(srcFile)
			if err != nil {
				t.Fatalf("FileHistory: %v", err)
			}

			if len(versions) != tt.wantVersions {
				t.Fatalf("got %d versions, want %d", len(versions), tt.wantVersions)
			}

			gotOldest := versions[0].Version
			gotLatest := versions[len(versions)-1].Version
			if gotOldest != tt.wantOldestVer {
				t.Errorf("oldest version = %d, want %d", gotOldest, tt.wantOldestVer)
			}
			if gotLatest != tt.wantLatestVer {
				t.Errorf("latest version = %d, want %d", gotLatest, tt.wantLatestVer)
			}

			if tt.wantOldestGone {
				for _, v := range versions {
					if v.Version == 1 {
						t.Errorf("v001 should have been evicted but is still present")
					}
				}
			}
		})
	}
}

// TestSaveHistory_NoHistoryDir 验证 HistoryDir 为空时 saveHistory 直接返回（无副作用）。
func TestSaveHistory_NoHistoryDir(t *testing.T) {
	baseDir := t.TempDir()
	srcFile := filepath.Join(baseDir, "config.yaml")
	if err := os.WriteFile(srcFile, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p := &OsFileProvider{
		BaseDir:       baseDir,
		PathValidator: NewPathValidator(baseDir),
		HistoryDir:    "", // 未配置历史目录
	}

	if err := p.SnapshotFile(srcFile); err != nil {
		t.Fatalf("SnapshotFile: %v", err)
	}

	versions, err := p.FileHistory(srcFile)
	if err != nil {
		t.Fatalf("FileHistory: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("expected 0 versions when HistoryDir empty, got %d", len(versions))
	}
}

// TestSaveHistory_CustomMaxVersions 验证自定义上限（非50）的淘汰行为。
func TestSaveHistory_CustomMaxVersions(t *testing.T) {
	tests := []struct {
		name          string
		maxVersions   int
		saves         int
		wantVersions  int
		wantOldestVer int
		wantLatestVer int
	}{
		{"Max3_AtLimit", 3, 3, 3, 1, 3},
		{"Max3_OverLimit_EvictsOldest", 3, 4, 3, 2, 4},
		{"Max1_OverLimit", 1, 2, 1, 2, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			srcFile := filepath.Join(baseDir, "config.yaml")
			if err := os.WriteFile(srcFile, []byte("x"), 0644); err != nil {
				t.Fatalf("setup: %v", err)
			}

			p := &OsFileProvider{
				BaseDir:       baseDir,
				PathValidator: NewPathValidator(baseDir),
				HistoryDir:    filepath.Join(baseDir, "history"),
				MaxVersions:   tt.maxVersions,
			}

			for i := 0; i < tt.saves; i++ {
				if err := p.SnapshotFile(srcFile); err != nil {
					t.Fatalf("SnapshotFile %d: %v", i+1, err)
				}
			}

			versions, err := p.FileHistory(srcFile)
			if err != nil {
				t.Fatalf("FileHistory: %v", err)
			}
			if len(versions) != tt.wantVersions {
				t.Fatalf("got %d versions, want %d", len(versions), tt.wantVersions)
			}
			if got := versions[0].Version; got != tt.wantOldestVer {
				t.Errorf("oldest version = %d, want %d", got, tt.wantOldestVer)
			}
			if got := versions[len(versions)-1].Version; got != tt.wantLatestVer {
				t.Errorf("latest version = %d, want %d", got, tt.wantLatestVer)
			}
		})
	}
}

// =============================================================================
// adapters.go 单元测试补充
// L-01 修复：为 adapters.go 的 Provider/Operator 实现补充单元测试，目标覆盖率 60%+
// =============================================================================

// --- ServiceHistoryStore 测试 ---

// TestServiceHistoryStore_RecordAndGet 验证 RecordStart/RecordStop/RecordDeath 后
// GetHistory/GetDeaths 返回倒序（最新在前）的条目。
func TestServiceHistoryStore_RecordAndGet(t *testing.T) {
	s := NewServiceHistoryStore()

	// 1. 记录启动 → 停止 → 死亡
	s.RecordStart("svc1", 100)
	time.Sleep(5 * time.Millisecond) // 确保时间戳可区分
	s.RecordStop("svc1", 100, 0, 3600, "manual_stop")
	time.Sleep(5 * time.Millisecond)
	s.RecordStart("svc1", 200) // 重启
	s.RecordDeath("svc1", 200, 1, 100)

	// 2. GetHistory 返回倒序：最新启动在前
	history := s.GetHistory("svc1")
	if len(history) != 3 {
		t.Fatalf("GetHistory: expected 3 entries, got %d", len(history))
	}
	// 最新启动（PID=200）应在最前
	if history[0].PID != 200 {
		t.Errorf("GetHistory[0].PID = %d, want 200 (latest first)", history[0].PID)
	}
	if history[0].Reason != "started" {
		t.Errorf("GetHistory[0].Reason = %q, want 'started'", history[0].Reason)
	}
	// 第二条为停止记录
	if history[1].Reason != "manual_stop" {
		t.Errorf("GetHistory[1].Reason = %q, want 'manual_stop'", history[1].Reason)
	}
	if history[1].ExitCode != 0 || history[1].Duration != 3600 {
		t.Errorf("GetHistory[1] = %+v, want ExitCode=0 Duration=3600", history[1])
	}

	// 3. GetDeaths 返回倒序
	deaths := s.GetDeaths("svc1")
	if len(deaths) != 1 {
		t.Fatalf("GetDeaths: expected 1 entry, got %d", len(deaths))
	}
	if deaths[0].PID != 200 || deaths[0].ExitCode != 1 {
		t.Errorf("GetDeaths[0] = %+v, want PID=200 ExitCode=1", deaths[0])
	}
	if deaths[0].Reason != "crashed" {
		t.Errorf("GetDeaths[0].Reason = %q, want 'crashed'", deaths[0].Reason)
	}
}

// TestServiceHistoryStore_EmptyService 验证未记录的服务返回空切片（非 nil）。
func TestServiceHistoryStore_EmptyService(t *testing.T) {
	s := NewServiceHistoryStore()
	history := s.GetHistory("nonexistent")
	if history == nil {
		t.Errorf("GetHistory: expected non-nil empty slice, got nil")
	}
	if len(history) != 0 {
		t.Errorf("GetHistory: expected 0 entries, got %d", len(history))
	}
	deaths := s.GetDeaths("nonexistent")
	if deaths == nil {
		t.Errorf("GetDeaths: expected non-nil empty slice, got nil")
	}
	if len(deaths) != 0 {
		t.Errorf("GetDeaths: expected 0 entries, got %d", len(deaths))
	}
}

// TestServiceHistoryStore_RetentionLimit 验证 maxPerSvc 上限淘汰最旧条目。
func TestServiceHistoryStore_RetentionLimit(t *testing.T) {
	s := NewServiceHistoryStore()
	s.maxPerSvc = 3
	for i := 0; i < 5; i++ {
		s.RecordStart("svc1", 100+i)
	}
	history := s.GetHistory("svc1")
	if len(history) != 3 {
		t.Fatalf("expected 3 entries after trim, got %d", len(history))
	}
	// 最新启动在前：PID 104, 103, 102
	if history[0].PID != 104 {
		t.Errorf("history[0].PID = %d, want 104", history[0].PID)
	}
	if history[2].PID != 102 {
		t.Errorf("history[2].PID = %d, want 102", history[2].PID)
	}

	// 同样验证 deaths 淘汰
	for i := 0; i < 5; i++ {
		s.RecordDeath("svc1", 200+i, 1, 100)
	}
	deaths := s.GetDeaths("svc1")
	if len(deaths) != 3 {
		t.Fatalf("expected 3 deaths after trim, got %d", len(deaths))
	}
}

// --- CoreHistoryGetter 测试 ---

// TestCoreHistoryGetter_NilStore 验证 Store 为 nil 时返回空切片。
func TestCoreHistoryGetter_NilStore(t *testing.T) {
	g := &CoreHistoryGetter{}
	if h := g.GetServiceHistory("svc1"); len(h) != 0 {
		t.Errorf("nil Store: GetServiceHistory = %d entries, want 0", len(h))
	}
	if d := g.GetServiceDeaths("svc1"); len(d) != 0 {
		t.Errorf("nil Store: GetServiceDeaths = %d entries, want 0", len(d))
	}
}

// TestCoreHistoryGetter_WithStore 验证委托到 Store。
func TestCoreHistoryGetter_WithStore(t *testing.T) {
	store := NewServiceHistoryStore()
	store.RecordStart("svc1", 100)
	g := &CoreHistoryGetter{Store: store}
	if h := g.GetServiceHistory("svc1"); len(h) != 1 {
		t.Errorf("with Store: GetServiceHistory = %d entries, want 1", len(h))
	}
}

// --- CoreStateProvider 测试 ---

// TestCoreStateProvider_SetDiscovery_Nil 验证 nil receiver 和 nil 参数安全处理。
func TestCoreStateProvider_SetDiscovery_Nil(t *testing.T) {
	// nil 参数 → no-op
	p := &CoreStateProvider{
		StateMachines:  map[string]*core.StateMachine{},
		Discovery:      &watch.DiscoveryResult{},
	}
	p.SetDiscovery(nil)
	if p.Discovery == nil {
		t.Errorf("SetDiscovery(nil) should be no-op, but Discovery was set to nil")
	}

	// nil receiver → no-op（不应 panic）
	var nilP *CoreStateProvider
	nilP.SetDiscovery(&watch.DiscoveryResult{})
}

// TestCoreStateProvider_SetDiscovery_AddsAndRemoves 验证 SetDiscovery 添加新服务
// 状态机，并移除已删除目录的非运行中服务状态机。
func TestCoreStateProvider_SetDiscovery_AddsAndRemoves(t *testing.T) {
	// 初始：已有 svc1（pending）和 svc2（up，运行中）
	sm1 := core.NewStateMachine()
	sm1.SetName("svc1")
	sm2 := core.NewStateMachine()
	sm2.SetName("svc2")
	// sm2 转移到 up 状态
	sm2.Transition(core.EventDependsReady)
	sm2.Transition(core.EventProcessStarted)
	if sm2.Current() != core.StateUp {
		t.Fatalf("setup: sm2.Current = %s, want up", sm2.Current())
	}

	p := &CoreStateProvider{
		StateMachines: map[string]*core.StateMachine{
			"svc1": sm1,
			"svc2": sm2,
		},
	}

	// 新 Discovery 只有 svc2（svc1 目录被删）和新增 svc3
	enabled := false
	newDisc := &watch.DiscoveryResult{
		Services: map[string]*watch.ServiceEntry{
			"svc2": {Name: "svc2", ConfigPath: "/svc2/service.yaml"},
			"svc3": {
				Name:       "svc3",
				ConfigPath: "/svc3/service.yaml",
				Config:     &config.ServiceConfig{Name: "svc3", Autostart: &enabled},
			},
		},
	}

	p.SetDiscovery(newDisc)

	// svc1（pending，非运行）应被移除
	if _, ok := p.StateMachines["svc1"]; ok {
		t.Errorf("svc1 should be removed (non-running + dir deleted)")
	}
	// svc2（up，运行中）应保留
	if _, ok := p.StateMachines["svc2"]; !ok {
		t.Errorf("svc2 should be retained (running)")
	}
	// svc3 应被新增
	sm3, ok := p.StateMachines["svc3"]
	if !ok {
		t.Fatalf("svc3 should be added as new state machine")
	}
	if sm3.Current() != core.StatePending {
		t.Errorf("svc3 initial state = %s, want pending", sm3.Current())
	}
}

// TestCoreStateProvider_ListServiceStates 验证 ListServiceStates 返回所有状态机的状态。
func TestCoreStateProvider_ListServiceStates(t *testing.T) {
	sm1 := core.NewStateMachine()
	sm1.SetName("svc1")
	sm2 := core.NewStateMachine()
	sm2.SetName("svc2")

	p := &CoreStateProvider{
		StateMachines: map[string]*core.StateMachine{
			"svc1": sm1,
			"svc2": sm2,
		},
		Discovery: &watch.DiscoveryResult{
			Services: map[string]*watch.ServiceEntry{
				"svc1": {Name: "svc1", ConfigPath: "/svc1.yaml"},
				"svc2": {Name: "svc2", ConfigPath: "/svc2.yaml"},
			},
		},
	}

	states := p.ListServiceStates()
	if len(states) != 2 {
		t.Fatalf("expected 2 states, got %d", len(states))
	}
	if _, ok := states["svc1"]; !ok {
		t.Errorf("states missing svc1")
	}
	if _, ok := states["svc2"]; !ok {
		t.Errorf("states missing svc2")
	}
	// 默认初始状态应为 pending
	if states["svc1"].State != core.StatePending {
		t.Errorf("svc1 state = %s, want pending", states["svc1"].State)
	}
	// Enabled 默认为 true（无 Autostart 配置）
	if !states["svc1"].Enabled {
		t.Errorf("svc1 Enabled = false, want true (default)")
	}
}

// --- ConfigAuthProvider 测试 ---

// TestConfigAuthProvider_VerifyToken 验证常量时间 token 比较。
func TestConfigAuthProvider_VerifyToken(t *testing.T) {
	tests := []struct {
		name     string
		stored   string
		input    string
		want     bool
	}{
		{"Correct", "secret-token", "secret-token", true},
		{"Wrong", "secret-token", "wrong-token", false},
		{"Empty", "secret-token", "", false},
		{"BothEmpty", "", "", true},
		{"DifferentLength", "abc", "abcd", false},
		{"InputEmptyStoredSet", "abc", "", false},
		{"StoredEmptyInputSet", "", "abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &ConfigAuthProvider{AuthToken: tt.stored}
			if got := p.VerifyToken(tt.input); got != tt.want {
				t.Errorf("VerifyToken(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// --- ConfigSettingsProvider 测试 ---

// TestConfigSettingsProvider_GetSettings 验证 GetSettings 返回配置中的 Settings 引用。
func TestConfigSettingsProvider_GetSettings(t *testing.T) {
	cfg := &config.Config{Settings: config.Settings{HTTPListen: ":7979", AuthMode: "none"}}
	p := &ConfigSettingsProvider{Config: cfg, BaseDir: t.TempDir()}
	s := p.GetSettings()
	if s.HTTPListen != ":7979" {
		t.Errorf("HTTPListen = %q, want ':7979'", s.HTTPListen)
	}
	if s.AuthMode != "none" {
		t.Errorf("AuthMode = %q, want 'none'", s.AuthMode)
	}
}

// TestConfigSettingsProvider_UpdateSettings_PreservesAuthToken 验证空 auth_token 被保留。
// F-06-001: 防止前端未传 auth_token 时被清空。
func TestConfigSettingsProvider_UpdateSettings_PreservesAuthToken(t *testing.T) {
	cfg := &config.Config{Settings: config.Settings{AuthToken: "old-token"}}
	p := &ConfigSettingsProvider{Config: cfg, BaseDir: t.TempDir()}

	// 新 settings 未设 AuthToken（空字符串）→ 应保留旧值
	newSettings := &config.Settings{HTTPListen: ":9090"}
	if err := p.UpdateSettings(newSettings); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if p.Config.Settings.AuthToken != "old-token" {
		t.Errorf("AuthToken = %q, want 'old-token' (should be preserved)", p.Config.Settings.AuthToken)
	}
	if p.Config.Settings.HTTPListen != ":9090" {
		t.Errorf("HTTPListen = %q, want ':9090'", p.Config.Settings.HTTPListen)
	}

	// 显式设置新 AuthToken 应被覆盖
	newSettings2 := &config.Settings{AuthToken: "new-token"}
	if err := p.UpdateSettings(newSettings2); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if p.Config.Settings.AuthToken != "new-token" {
		t.Errorf("AuthToken = %q, want 'new-token'", p.Config.Settings.AuthToken)
	}
}

// TestConfigSettingsProvider_UpdateSettings_Nil 验证 nil 参数安全返回。
func TestConfigSettingsProvider_UpdateSettings_Nil(t *testing.T) {
	cfg := &config.Config{}
	p := &ConfigSettingsProvider{Config: cfg, BaseDir: t.TempDir()}
	if err := p.UpdateSettings(nil); err != nil {
		t.Errorf("UpdateSettings(nil) error = %v, want nil", err)
	}
}

// TestConfigSettingsProvider_GetEnv_NoFile 验证 env 文件不存在时返回空对象。
func TestConfigSettingsProvider_GetEnv_NoFile(t *testing.T) {
	cfg := &config.Config{}
	p := &ConfigSettingsProvider{Config: cfg, BaseDir: t.TempDir()}
	env, err := p.GetEnv()
	if err != nil {
		t.Errorf("GetEnv error = %v, want nil (file not exist is OK)", err)
	}
	if env == nil {
		t.Fatalf("GetEnv returned nil env")
	}
	if env.Env == nil {
		t.Errorf("GetEnv returned nil Env map, want non-nil")
	}
}

// TestConfigSettingsProvider_UpdateAndGetEnv 验证 env 文件读写往返。
func TestConfigSettingsProvider_UpdateAndGetEnv(t *testing.T) {
	baseDir := t.TempDir()
	cfg := &config.Config{}
	p := &ConfigSettingsProvider{Config: cfg, BaseDir: baseDir}

	enabled := true
	envFile := &config.EnvFile{
		Env: map[string]config.EnvVar{
			"FOO": {Value: "bar", Enabled: &enabled},
			"BAZ": {Value: "qux"},
		},
	}
	if err := p.UpdateEnv(envFile); err != nil {
		t.Fatalf("UpdateEnv: %v", err)
	}

	// 验证文件确实写入
	envPath := filepath.Join(baseDir, "env", "00-base.yaml")
	if _, err := os.Stat(envPath); err != nil {
		t.Errorf("env file not written: %v", err)
	}

	// 读回应一致
	env, err := p.GetEnv()
	if err != nil {
		t.Fatalf("GetEnv after update: %v", err)
	}
	if env.Env["FOO"].Value != "bar" {
		t.Errorf("FOO value = %q, want 'bar'", env.Env["FOO"].Value)
	}
	if env.Env["BAZ"].Value != "qux" {
		t.Errorf("BAZ value = %q, want 'qux'", env.Env["BAZ"].Value)
	}
}

// TestConfigSettingsProvider_UpdateEnv_Nil 验证 nil 参数安全返回。
func TestConfigSettingsProvider_UpdateEnv_Nil(t *testing.T) {
	p := &ConfigSettingsProvider{Config: &config.Config{}, BaseDir: t.TempDir()}
	if err := p.UpdateEnv(nil); err != nil {
		t.Errorf("UpdateEnv(nil) error = %v, want nil", err)
	}
}

// TestConfigSettingsProvider_RuntimesConfig 验证 runtimes 读写。
func TestConfigSettingsProvider_RuntimesConfig(t *testing.T) {
	cfg := &config.Config{Runtimes: map[string]string{"python": "/usr/bin/python3"}}
	p := &ConfigSettingsProvider{Config: cfg, BaseDir: t.TempDir()}

	got := p.GetRuntimesConfig()
	if got["python"] != "/usr/bin/python3" {
		t.Errorf("GetRuntimesConfig[python] = %q, want '/usr/bin/python3'", got["python"])
	}

	newRT := map[string]string{"node": "/usr/bin/node"}
	if err := p.UpdateRuntimesConfig(newRT); err != nil {
		t.Fatalf("UpdateRuntimesConfig: %v", err)
	}
	if p.Config.Runtimes["node"] != "/usr/bin/node" {
		t.Errorf("after update: Runtimes[node] = %q, want '/usr/bin/node'", p.Config.Runtimes["node"])
	}
	if _, ok := p.Config.Runtimes["python"]; ok {
		t.Errorf("after update: Runtimes[python] should be gone")
	}
}

// TestConfigSettingsProvider_EnvFiles 验证 env_files 读写。
func TestConfigSettingsProvider_EnvFiles(t *testing.T) {
	cfg := &config.Config{EnvFiles: []string{"env/00-base.yaml"}}
	p := &ConfigSettingsProvider{Config: cfg, BaseDir: t.TempDir()}

	got := p.GetEnvFiles()
	if len(got) != 1 || got[0] != "env/00-base.yaml" {
		t.Errorf("GetEnvFiles = %v, want [env/00-base.yaml]", got)
	}

	if err := p.UpdateEnvFiles([]string{"env/01.yaml", "env/02.yaml"}); err != nil {
		t.Fatalf("UpdateEnvFiles: %v", err)
	}
	if len(p.Config.EnvFiles) != 2 {
		t.Errorf("after update: EnvFiles len = %d, want 2", len(p.Config.EnvFiles))
	}
}

// TestConfigSettingsProvider_ExtensionDirs 验证 extension_dirs 读写。
func TestConfigSettingsProvider_ExtensionDirs(t *testing.T) {
	cfg := &config.Config{ExtensionDirs: []string{"/etc/supd/extensions"}}
	p := &ConfigSettingsProvider{Config: cfg, BaseDir: t.TempDir()}

	got := p.GetExtensionDirs()
	if len(got) != 1 || got[0] != "/etc/supd/extensions" {
		t.Errorf("GetExtensionDirs = %v, want [/etc/supd/extensions]", got)
	}

	if err := p.UpdateExtensionDirs([]string{"/a", "/b"}); err != nil {
		t.Fatalf("UpdateExtensionDirs: %v", err)
	}
	if len(p.Config.ExtensionDirs) != 2 {
		t.Errorf("after update: ExtensionDirs len = %d, want 2", len(p.Config.ExtensionDirs))
	}
}

// TestConfigSettingsProvider_Defaults 验证 defaults 读写。
func TestConfigSettingsProvider_Defaults(t *testing.T) {
	cfg := &config.Config{Defaults: config.DefaultRestart{Restart: config.RestartConfig{Policy: "always", MaxRetries: 3}}}
	p := &ConfigSettingsProvider{Config: cfg, BaseDir: t.TempDir()}

	got := p.GetDefaults()
	if got.Restart.Policy != "always" {
		t.Errorf("GetDefaults.Policy = %q, want 'always'", got.Restart.Policy)
	}
	if got.Restart.MaxRetries != 3 {
		t.Errorf("GetDefaults.MaxRetries = %d, want 3", got.Restart.MaxRetries)
	}

	newDefaults := config.DefaultRestart{Restart: config.RestartConfig{Policy: "never"}}
	if err := p.UpdateDefaults(newDefaults); err != nil {
		t.Fatalf("UpdateDefaults: %v", err)
	}
	if p.Config.Defaults.Restart.Policy != "never" {
		t.Errorf("after update: Policy = %q, want 'never'", p.Config.Defaults.Restart.Policy)
	}
}

// --- FileLogProvider 测试 ---

// TestFileLogProvider_GetServiceLogs_NotExist 验证日志文件不存在时返回 (nil, 0, nil)。
func TestFileLogProvider_GetServiceLogs_NotExist(t *testing.T) {
	p := &FileLogProvider{LogDir: t.TempDir()}
	lines, pos, err := p.GetServiceLogs("nosvc", 0)
	if err != nil {
		t.Errorf("err = %v, want nil for not-exist", err)
	}
	if lines != nil {
		t.Errorf("lines = %v, want nil for not-exist", lines)
	}
	if pos != 0 {
		t.Errorf("pos = %d, want 0 for not-exist", pos)
	}
}

// TestFileLogProvider_GetServiceLogs_WithExtFilter 验证过滤 [ext:...] 行。
// REQ-F-010: 服务进程日志接口应仅返回进程输出，过滤扩展日志行。
func TestFileLogProvider_GetServiceLogs_WithExtFilter(t *testing.T) {
	logDir := t.TempDir()
	svcLogDir := filepath.Join(logDir, "services", "svc1")
	if err := os.MkdirAll(svcLogDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "process line 1\n[ext:foo] ext log line\nprocess line 2\n[ext:bar] another ext\nprocess line 3\n"
	if err := os.WriteFile(filepath.Join(svcLogDir, "current"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &FileLogProvider{LogDir: logDir}
	lines, pos, err := p.GetServiceLogs("svc1", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 process lines (ext filtered), got %d: %v", len(lines), lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "[ext:") {
			t.Errorf("ext line not filtered: %s", l)
		}
	}
	if pos != int64(len(content)) {
		t.Errorf("pos = %d, want %d", pos, len(content))
	}
}

// TestFileLogProvider_GetServiceLogs_WithSincePos 验证 sincePos 偏移读取。
func TestFileLogProvider_GetServiceLogs_WithSincePos(t *testing.T) {
	logDir := t.TempDir()
	svcLogDir := filepath.Join(logDir, "services", "svc1")
	if err := os.MkdirAll(svcLogDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(svcLogDir, "current"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &FileLogProvider{LogDir: logDir}
	// 从第6字节开始读取（跳过 "line1\n"）
	lines, pos, err := p.GetServiceLogs("svc1", 6)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines after seek, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line2" {
		t.Errorf("lines[0] = %q, want 'line2'", lines[0])
	}
	if pos != int64(len(content)) {
		t.Errorf("pos = %d, want %d", pos, len(content))
	}
}

// TestFileLogProvider_SearchServiceLogs 验证日志搜索返回匹配行内容。
func TestFileLogProvider_SearchServiceLogs(t *testing.T) {
	logDir := t.TempDir()
	svcLogDir := filepath.Join(logDir, "services", "svc1")
	if err := os.MkdirAll(svcLogDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "error: connection refused\ninfo: started\nerror: timeout\n"
	if err := os.WriteFile(filepath.Join(svcLogDir, "current"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &FileLogProvider{LogDir: logDir}
	lines, err := p.SearchServiceLogs("svc1", "error", 100)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(lines), lines)
	}
}

// --- CoreWatchProvider 测试 ---

// TestCoreWatchProvider_SetDiscovery_Nil 验证 nil 参数与 nil receiver 安全。
func TestCoreWatchProvider_SetDiscovery_Nil(t *testing.T) {
	p := &CoreWatchProvider{Discovery: &watch.DiscoveryResult{}}
	p.SetDiscovery(nil)
	if p.Discovery == nil {
		t.Errorf("SetDiscovery(nil) should be no-op")
	}
	var nilP *CoreWatchProvider
	nilP.SetDiscovery(&watch.DiscoveryResult{}) // 不应 panic
}

// TestCoreWatchProvider_GetDiscovery_Nil 验证 Discovery 为 nil 时返回 nil。
func TestCoreWatchProvider_GetDiscovery_Nil(t *testing.T) {
	p := &CoreWatchProvider{}
	if d := p.GetDiscovery(); d != nil {
		t.Errorf("GetDiscovery with nil Discovery = %v, want nil", d)
	}
}

// TestCoreWatchProvider_GetDiscovery_WithServicesAndExts 验证映射服务与扩展条目。
func TestCoreWatchProvider_GetDiscovery_WithServicesAndExts(t *testing.T) {
	discovery := &watch.DiscoveryResult{
		Services: map[string]*watch.ServiceEntry{
			"web": {
				Name:       "web",
				ConfigPath: "/svc/web/service.yaml",
				Extensions: map[string]*watch.ExtensionEntry{
					"webext": {
						Name:        "webext",
						ConfigPath:  "/svc/web/extensions/webext/meta.yaml",
						ServiceName: "web",
					},
				},
			},
		},
		GlobalExts: map[string]*watch.ExtensionEntry{
			"gext": {
				Name:       "gext",
				ConfigPath: "/extensions/gext/meta.yaml",
			},
		},
	}
	p := &CoreWatchProvider{Discovery: discovery}
	d := p.GetDiscovery()
	if d == nil {
		t.Fatalf("GetDiscovery returned nil")
	}
	if len(d.Services) != 1 {
		t.Fatalf("Services count = %d, want 1", len(d.Services))
	}
	webInfo, ok := d.Services["web"]
	if !ok {
		t.Fatalf("missing 'web' in services")
	}
	if webInfo.ConfigPath != "/svc/web/service.yaml" {
		t.Errorf("web ConfigPath = %q, want '/svc/web/service.yaml'", webInfo.ConfigPath)
	}
	if len(webInfo.Extensions) != 1 {
		t.Errorf("web Extensions count = %d, want 1", len(webInfo.Extensions))
	}
	if webInfo.Extensions["webext"].ServiceName != "web" {
		t.Errorf("webext ServiceName = %q, want 'web'", webInfo.Extensions["webext"].ServiceName)
	}
	if len(d.GlobalExts) != 1 {
		t.Fatalf("GlobalExts count = %d, want 1", len(d.GlobalExts))
	}
	if d.GlobalExts["gext"].ConfigPath != "/extensions/gext/meta.yaml" {
		t.Errorf("gext ConfigPath = %q, want '/extensions/gext/meta.yaml'", d.GlobalExts["gext"].ConfigPath)
	}
	// 全局扩展的 ServiceName 应为空
	if d.GlobalExts["gext"].ServiceName != "" {
		t.Errorf("gext ServiceName = %q, want '' (global)", d.GlobalExts["gext"].ServiceName)
	}
}

// TestCoreWatchProvider_ReloadConfig 验证 ReloadConfig 触发新发现并更新 Discovery。
func TestCoreWatchProvider_ReloadConfig(t *testing.T) {
	baseDir := t.TempDir()
	logDir := t.TempDir()
	svcDir := filepath.Join(baseDir, "services", "svc1")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	svcYAML := "name: svc1\nversion: \"1\"\ncommand:\n  - sleep\n  - \"1\"\n"
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(svcYAML), 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}

	p := &CoreWatchProvider{BaseDir: baseDir, LogDir: logDir}
	if err := p.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}
	if p.Discovery == nil {
		t.Fatalf("Discovery not set after ReloadConfig")
	}
	if len(p.Discovery.Services) != 1 {
		t.Errorf("Services count = %d, want 1", len(p.Discovery.Services))
	}
}

// --- OsFileProvider 测试 ---

// TestOsFileProvider_FileTree 验证 FileTree 返回根目录下的文件和子目录节点。
func TestOsFileProvider_FileTree(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, "file1.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	subDir := filepath.Join(baseDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("world"), 0644); err != nil {
		t.Fatalf("write subdir: %v", err)
	}

	p := &OsFileProvider{BaseDir: baseDir, PathValidator: NewPathValidator(baseDir)}
	nodes, err := p.FileTree("")
	if err != nil {
		t.Fatalf("FileTree: %v", err)
	}

	var foundFile, foundDir bool
	for _, n := range nodes {
		if n.Name == "file1.txt" {
			foundFile = true
			if n.IsDir {
				t.Errorf("file1.txt IsDir = true, want false")
			}
			if n.Size != 5 {
				t.Errorf("file1.txt Size = %d, want 5", n.Size)
			}
		}
		if n.Name == "subdir" {
			foundDir = true
			if !n.IsDir {
				t.Errorf("subdir IsDir = false, want true")
			}
			if len(n.Children) != 1 {
				t.Errorf("subdir Children count = %d, want 1", len(n.Children))
			}
		}
	}
	if !foundFile {
		t.Errorf("file1.txt not found in tree")
	}
	if !foundDir {
		t.Errorf("subdir not found in tree")
	}
}

// TestOsFileProvider_FileTree_InvalidPath 验证含 ".." 的路径被拒绝。
func TestOsFileProvider_FileTree_InvalidPath(t *testing.T) {
	baseDir := t.TempDir()
	p := &OsFileProvider{BaseDir: baseDir, PathValidator: NewPathValidator(baseDir)}
	_, err := p.FileTree("../etc")
	if err == nil {
		t.Errorf("expected error for '..' path, got nil")
	}
}

// TestOsFileProvider_FileTree_WithLogDir 验证 LogDir 在 BaseDir 外时被加入为虚拟节点。
func TestOsFileProvider_FileTree_WithLogDir(t *testing.T) {
	baseDir := t.TempDir()
	logDir := t.TempDir() // 在 baseDir 外
	if err := os.MkdirAll(filepath.Join(logDir, "services"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "services", "current.log"), []byte("log"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &OsFileProvider{BaseDir: baseDir, PathValidator: NewPathValidator(baseDir), LogDir: logDir}
	nodes, err := p.FileTree("")
	if err != nil {
		t.Fatalf("FileTree: %v", err)
	}
	foundLogs := false
	for _, n := range nodes {
		if n.Name == "logs" {
			foundLogs = true
			if !n.IsDir {
				t.Errorf("logs IsDir = false, want true")
			}
		}
	}
	if !foundLogs {
		t.Errorf("expected 'logs' virtual node in tree")
	}
}

// TestOsFileProvider_ReadWriteCreateDeleteMove 覆盖文件操作 CRUD + 移动。
func TestOsFileProvider_ReadWriteCreateDeleteMove(t *testing.T) {
	baseDir := t.TempDir()
	p := &OsFileProvider{
		BaseDir:       baseDir,
		PathValidator: NewPathValidator(baseDir),
		HistoryDir:    filepath.Join(baseDir, "history"),
	}

	// WriteFile
	path := filepath.Join(baseDir, "file.txt")
	if err := p.WriteFile(path, []byte("content")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// ReadFile
	data, err := p.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("ReadFile = %q, want 'content'", data)
	}
	// CreateFile in non-existing subdir (应自动创建父目录)
	newPath := filepath.Join(baseDir, "subdir", "new.txt")
	if err := p.CreateFile(newPath, []byte("new")); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	// CreateDir
	dirPath := filepath.Join(baseDir, "newdir")
	if err := p.CreateDir(dirPath); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	if info, err := os.Stat(dirPath); err != nil || !info.IsDir() {
		t.Errorf("CreateDir did not create dir: %v", err)
	}
	// MoveFile
	movedPath := filepath.Join(baseDir, "moved.txt")
	if err := p.MoveFile(path, movedPath); err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("MoveFile: source still exists after move")
	}
	if _, err := os.Stat(movedPath); err != nil {
		t.Errorf("MoveFile: destination missing after move: %v", err)
	}
	// DeleteFile (file)
	if err := p.DeleteFile(movedPath); err != nil {
		t.Fatalf("DeleteFile (file): %v", err)
	}
	// DeleteFile (dir) → RemoveAll
	if err := p.DeleteFile(dirPath); err != nil {
		t.Fatalf("DeleteFile (dir): %v", err)
	}
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Errorf("DeleteFile (dir): dir still exists")
	}
	// DeleteFile 不存在的路径
	if err := p.DeleteFile(filepath.Join(baseDir, "nope")); err == nil {
		t.Errorf("DeleteFile on missing path: expected error, got nil")
	}
}

// TestOsFileProvider_RollbackFile 验证回滚到历史版本。
func TestOsFileProvider_RollbackFile(t *testing.T) {
	baseDir := t.TempDir()
	p := &OsFileProvider{
		BaseDir:       baseDir,
		PathValidator: NewPathValidator(baseDir),
		HistoryDir:    filepath.Join(baseDir, "history"),
	}
	srcFile := filepath.Join(baseDir, "config.yaml")
	if err := os.WriteFile(srcFile, []byte("v1"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := p.SnapshotFile(srcFile); err != nil {
		t.Fatalf("SnapshotFile: %v", err)
	}
	// 修改后再写入
	if err := os.WriteFile(srcFile, []byte("v2"), 0644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	// 回滚到 v1
	if err := p.RollbackFile(srcFile, 1); err != nil {
		t.Fatalf("RollbackFile: %v", err)
	}
	data, err := p.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("ReadFile after rollback: %v", err)
	}
	if string(data) != "v1" {
		t.Errorf("after rollback content = %q, want 'v1'", data)
	}
	// 回滚不存在的版本 → 错误
	if err := p.RollbackFile(srcFile, 99); err == nil {
		t.Errorf("RollbackFile(99): expected error, got nil")
	}
	// HistoryDir 为空 → 错误
	p2 := &OsFileProvider{BaseDir: baseDir, PathValidator: NewPathValidator(baseDir), HistoryDir: ""}
	if err := p2.RollbackFile(srcFile, 1); err == nil {
		t.Errorf("RollbackFile with empty HistoryDir: expected error, got nil")
	}
}

// TestOsFileProvider_MoveFile_SyncHistory 验证 BUG-02 修复：
// MoveFile 同步移动 history 目录，使 move 后新文件仍能 rollback。
// 复现路径：snapshot → move → rollback 应能成功（原 bug 报 500 "history version not found"）。
func TestOsFileProvider_MoveFile_SyncHistory(t *testing.T) {
	baseDir := t.TempDir()
	p := &OsFileProvider{
		BaseDir:       baseDir,
		PathValidator: NewPathValidator(baseDir),
		HistoryDir:    filepath.Join(baseDir, "history"),
		MaxVersions:   50,
	}
	srcFile := filepath.Join(baseDir, "config.yaml")
	if err := os.WriteFile(srcFile, []byte("v1"), 0644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	if err := p.SnapshotFile(srcFile); err != nil {
		t.Fatalf("SnapshotFile: %v", err)
	}
	// 修改后再次 snapshot 产生 v2
	if err := os.WriteFile(srcFile, []byte("v2"), 0644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	if err := p.SnapshotFile(srcFile); err != nil {
		t.Fatalf("SnapshotFile v2: %v", err)
	}

	// 移动文件
	movedPath := filepath.Join(baseDir, "moved.yaml")
	if err := p.MoveFile(srcFile, movedPath); err != nil {
		t.Fatalf("MoveFile: %v", err)
	}

	// 修改 moved 后回滚到 v1（验证 history 目录已同步迁移）
	if err := os.WriteFile(movedPath, []byte("v3"), 0644); err != nil {
		t.Fatalf("write v3: %v", err)
	}
	if err := p.RollbackFile(movedPath, 1); err != nil {
		t.Fatalf("RollbackFile after move (BUG-02 not fixed): %v", err)
	}
	data, err := p.ReadFile(movedPath)
	if err != nil {
		t.Fatalf("ReadFile after rollback: %v", err)
	}
	if string(data) != "v1" {
		t.Errorf("after rollback content = %q, want 'v1'", data)
	}

	// 验证旧 history 目录已不存在（已被 rename 走）
	oldHistDir := filepath.Join(p.HistoryDir, "config.yaml")
	if _, err := os.Stat(oldHistDir); !os.IsNotExist(err) {
		t.Errorf("old history dir should be moved away, but still exists: %v", err)
	}
	// 新 history 目录应存在
	newHistDir := filepath.Join(p.HistoryDir, "moved.yaml")
	if _, err := os.Stat(newHistDir); err != nil {
		t.Errorf("new history dir should exist after move: %v", err)
	}
}

// TestOsFileProvider_ValidateFile 验证 YAML 文件校验。
func TestOsFileProvider_ValidateFile(t *testing.T) {
	baseDir := t.TempDir()
	p := &OsFileProvider{BaseDir: baseDir, PathValidator: NewPathValidator(baseDir)}

	tests := []struct {
		name      string
		path      string
		content   []byte
		wantErrs  int
	}{
		{"ValidYAML", "/test.yaml", []byte("key: value\nlist:\n  - a\n  - b\n"), 0},
		{"InvalidYAML", "/test.yaml", []byte("key: [unclosed\n"), 1},
		{"NonYAML", "/test.txt", []byte("anything goes here"), 0},
		{"EmptyYAML", "/test.yaml", []byte(""), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, err := p.ValidateFile(tt.path, tt.content)
			if err != nil {
				t.Fatalf("ValidateFile err = %v, want nil", err)
			}
			if len(errs) != tt.wantErrs {
				t.Errorf("got %d errors, want %d: %v", len(errs), tt.wantErrs, errs)
			}
		})
	}
}

// TestOsFileProvider_ValidateFile_StrictUnknownField 验证 BUG-01 修复：
// 对已知配置文件（service.yaml/meta.yaml/env.yaml/config.yaml）能检测未知字段。
// 原实现对 map[string]any 调用 StrictUnmarshal，KnownFields 对 map 无效。
func TestOsFileProvider_ValidateFile_StrictUnknownField(t *testing.T) {
	baseDir := t.TempDir()
	p := &OsFileProvider{BaseDir: baseDir, PathValidator: NewPathValidator(baseDir)}

	tests := []struct {
		name     string
		path     string
		content  string
		wantErrs int
	}{
		{
			name:     "ServiceYAML_UnknownField",
			path:     "/services/demo/service.yaml",
			content:  "name: demo\nversion: \"1.0\"\nunknown_field_xyz: value\ncommand: [\"sleep\"]\n",
			wantErrs: 1,
		},
		{
			name:     "ServiceYAML_Valid",
			path:     "/services/demo/service.yaml",
			content:  "name: demo\nversion: \"1.0\"\ncommand: [\"sleep\"]\n",
			wantErrs: 0,
		},
		{
			name:     "MetaYAML_UnknownField",
			path:     "/extensions/demo/meta.yaml",
			content:  "name: demo\nversion: \"1.0\"\nentry: run.sh\nunknown_field: 123\n",
			wantErrs: 1,
		},
		{
			name:     "MetaYAML_Valid",
			path:     "/extensions/demo/meta.yaml",
			content:  "name: demo\nversion: \"1.0\"\nentry: run.sh\n",
			wantErrs: 0,
		},
		{
			name:     "EnvYAML_UnknownField",
			path:     "/services/demo/env.yaml",
			content:  "env:\n  FOO:\n    value: bar\nunknown_field: 1\n",
			wantErrs: 1,
		},
		{
			name:     "EnvYAML_Valid",
			path:     "/services/demo/env.yaml",
			content:  "env:\n  FOO:\n    value: bar\n",
			wantErrs: 0,
		},
		{
			name:     "ConfigYAML_UnknownField",
			path:     "/config.yaml",
			content:  "settings:\n  http_listen: \":8080\"\nunknown_field: 1\n",
			wantErrs: 1,
		},
		{
			name:     "ConfigYAML_Valid",
			path:     "/config.yaml",
			content:  "settings:\n  http_listen: \":8080\"\n",
			wantErrs: 0,
		},
		{
			name:     "OtherYAML_NoStrictCheck",
			path:     "/data/custom.yaml",
			content:  "any_field: value\nother_field: 123\n",
			wantErrs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, err := p.ValidateFile(tt.path, []byte(tt.content))
			if err != nil {
				t.Fatalf("ValidateFile err = %v, want nil", err)
			}
			if len(errs) != tt.wantErrs {
				t.Errorf("got %d errors, want %d: %v", len(errs), tt.wantErrs, errs)
			}
		})
	}
}

// --- ConfigRuntimeProvider 测试 ---

// TestConfigRuntimeProvider_UploadAndDelete 验证上传和删除运行时二进制文件。
func TestConfigRuntimeProvider_UploadAndDelete(t *testing.T) {
	baseDir := t.TempDir()
	p := &ConfigRuntimeProvider{Config: &config.Config{}, BaseDir: baseDir}

	data := []byte("#!/bin/sh\necho hello\n")
	if err := p.UploadRuntime("myrt", data); err != nil {
		t.Fatalf("UploadRuntime: %v", err)
	}
	// 验证文件存在
	rtPath := filepath.Join(baseDir, "runtimes", "myrt")
	if _, err := os.Stat(rtPath); err != nil {
		t.Errorf("runtime file not written: %v", err)
	}
	// 验证权限 0755
	info, _ := os.Stat(rtPath)
	if info.Mode().Perm() != 0755 {
		t.Errorf("runtime perm = %v, want 0755", info.Mode().Perm())
	}
	// 删除
	if err := p.DeleteRuntime("myrt"); err != nil {
		t.Fatalf("DeleteRuntime: %v", err)
	}
	if _, err := os.Stat(rtPath); !os.IsNotExist(err) {
		t.Errorf("runtime file still exists after delete")
	}
}

// TestConfigRuntimeProvider_DeleteRuntime_NotExist 验证删除不存在的运行时返回错误。
func TestConfigRuntimeProvider_DeleteRuntime_NotExist(t *testing.T) {
	p := &ConfigRuntimeProvider{Config: &config.Config{}, BaseDir: t.TempDir()}
	if err := p.DeleteRuntime("nonexistent"); err == nil {
		t.Errorf("DeleteRuntime on missing: expected error, got nil")
	}
}

// TestConfigRuntimeProvider_ListRuntimes 验证 ListRuntimes 返回发现的运行时列表。
func TestConfigRuntimeProvider_ListRuntimes(t *testing.T) {
	baseDir := t.TempDir()
	// 在 baseDir/runtimes 放置一个可执行文件
	rtDir := filepath.Join(baseDir, "runtimes")
	if err := os.MkdirAll(rtDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rtPath := filepath.Join(rtDir, "myruntime")
	if err := os.WriteFile(rtPath, []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &ConfigRuntimeProvider{Config: &config.Config{Runtimes: map[string]string{"myruntime": rtPath}}, BaseDir: baseDir}
	runtimes := p.ListRuntimes()

	// 至少应包含 myruntime
	found := false
	for _, rt := range runtimes {
		if rt.Alias == "myruntime" {
			found = true
			if rt.Path == "" {
				t.Errorf("myruntime Path = empty, want non-empty")
			}
		}
	}
	if !found {
		t.Errorf("myruntime not in ListRuntimes result: %v", runtimes)
	}
}

// --- CoreExtensionProvider 测试 ---

// helper：构建一个带 Enabled=true 的 ExtensionMeta
func testBuildMeta(name string, withActions bool) *config.ExtensionMeta {
	enabled := true
	meta := &config.ExtensionMeta{
		Name:     name,
		Version:  "1.0.0",
		Enabled:  &enabled,
		Runtime:  "bash",
		Entry:    "main.sh",
	}
	if withActions {
		meta.Actions = []config.Action{{ID: "run", Label: "Run"}}
	}
	return meta
}

// TestCoreExtensionProvider_SetDiscovery_Nil 验证 nil 参数和 nil receiver 安全。
func TestCoreExtensionProvider_SetDiscovery_Nil(t *testing.T) {
	p := &CoreExtensionProvider{Discovery: &watch.DiscoveryResult{}}
	p.SetDiscovery(nil)
	if p.Discovery == nil {
		t.Errorf("SetDiscovery(nil) should be no-op")
	}
	var nilP *CoreExtensionProvider
	nilP.SetDiscovery(&watch.DiscoveryResult{}) // 不应 panic
}

// TestCoreExtensionProvider_ListExtensions_DiscoveryNil 验证 Discovery 为 nil 时返回空列表。
func TestCoreExtensionProvider_ListExtensions_DiscoveryNil(t *testing.T) {
	p := &CoreExtensionProvider{}
	exts := p.ListExtensions()
	if exts != nil {
		t.Errorf("ListExtensions with nil Discovery = %v, want nil", exts)
	}
}

// TestCoreExtensionProvider_ListExtensions_WithExts 验证混合全局+服务级扩展列表。
func TestCoreExtensionProvider_ListExtensions_WithExts(t *testing.T) {
	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"gext": {
				Name:       "gext",
				ConfigPath: "/extensions/gext/meta.yaml",
				Meta:       testBuildMeta("gext", true),
			},
		},
		Services: map[string]*watch.ServiceEntry{
			"web": {
				Name: "web",
				Extensions: map[string]*watch.ExtensionEntry{
					"webext": {
						Name:        "webext",
						ConfigPath:  "/svc/web/extensions/webext/meta.yaml",
						ServiceName: "web",
						Meta:        testBuildMeta("webext", true),
					},
				},
			},
		},
	}
	p := &CoreExtensionProvider{Discovery: discovery}
	exts := p.ListExtensions()
	if len(exts) != 2 {
		t.Fatalf("expected 2 extensions, got %d: %v", len(exts), exts)
	}

	// 验证字段映射（找 gext）
	var foundGlobal, foundService bool
	for _, e := range exts {
		if e.Name == "gext" {
			foundGlobal = true
			if e.Service != "" {
				t.Errorf("gext Service = %q, want '' (global)", e.Service)
			}
			if e.Version != "1.0.0" {
				t.Errorf("gext Version = %q, want '1.0.0'", e.Version)
			}
			if !e.Enabled {
				t.Errorf("gext Enabled = false, want true")
			}
			if e.DisplayState != "active" {
				t.Errorf("gext DisplayState = %q, want 'active'", e.DisplayState)
			}
			if e.TriggerType != "on_demand" {
				t.Errorf("gext TriggerType = %q, want 'on_demand'", e.TriggerType)
			}
		}
		if e.Name == "webext" {
			foundService = true
			if e.Service != "web" {
				t.Errorf("webext Service = %q, want 'web'", e.Service)
			}
		}
	}
	if !foundGlobal {
		t.Errorf("global 'gext' not found")
	}
	if !foundService {
		t.Errorf("service-level 'webext' not found")
	}
}

// TestCoreExtensionProvider_extEntryToInfo_NilMeta 验证 nil Meta 时返回最小化 info。
func TestCoreExtensionProvider_extEntryToInfo_NilMeta(t *testing.T) {
	p := &CoreExtensionProvider{}
	info := p.extEntryToInfo("ext1", &watch.ExtensionEntry{Name: "ext1"}, "svc1")
	if info.Name != "ext1" {
		t.Errorf("Name = %q, want 'ext1'", info.Name)
	}
	if info.Service != "svc1" {
		t.Errorf("Service = %q, want 'svc1'", info.Service)
	}
	if info.Enabled {
		t.Errorf("Enabled = true, want false (default for nil Meta)")
	}
}

// TestCoreExtensionProvider_extEntryToInfo_NilEntry 验证 nil extEntry 时返回最小化 info。
func TestCoreExtensionProvider_extEntryToInfo_NilEntry(t *testing.T) {
	p := &CoreExtensionProvider{}
	info := p.extEntryToInfo("ext1", nil, "svc1")
	if info.Name != "ext1" {
		t.Errorf("Name = %q, want 'ext1'", info.Name)
	}
	if info.Enabled {
		t.Errorf("Enabled = true, want false (default for nil entry)")
	}
}

// TestCoreExtensionProvider_extEntryToInfo_WithTaskMgr 验证 TaskMgr 聚合运行统计。
func TestCoreExtensionProvider_extEntryToInfo_WithTaskMgr(t *testing.T) {
	now := time.Now()
	tm := extension.NewTaskManager(7)
	// 记录 success / failed / running 各一次
	tm.RecordRun(&extension.RunResult{
		RunID: "r1", ExtensionName: "ext1", State: extension.TaskSuccess,
		StartedAt: now.Add(-2 * time.Hour),
	})
	tm.RecordRun(&extension.RunResult{
		RunID: "r2", ExtensionName: "ext1", State: extension.TaskFailed,
		StartedAt: now.Add(-1 * time.Hour),
	})
	tm.RecordRun(&extension.RunResult{
		RunID: "r3", ExtensionName: "ext1", State: extension.TaskRunning,
		StartedAt: now.Add(-1 * time.Minute),
	})

	p := &CoreExtensionProvider{TaskMgr: tm}
	entry := &watch.ExtensionEntry{
		Name:       "ext1",
		ConfigPath: "/ext1/meta.yaml",
		Meta:       testBuildMeta("ext1", true),
	}
	info := p.extEntryToInfo("ext1", entry, "")
	if info.RunCount != 3 {
		t.Errorf("RunCount = %d, want 3", info.RunCount)
	}
	if info.SuccessCount != 1 {
		t.Errorf("SuccessCount = %d, want 1", info.SuccessCount)
	}
	if info.FailCount != 1 {
		t.Errorf("FailCount = %d, want 1", info.FailCount)
	}
	// 最新（按 StartedAt）= r3 (running)
	if info.LastStatus != "running" {
		t.Errorf("LastStatus = %q, want 'running'", info.LastStatus)
	}
	if info.LastRunAt == "" {
		t.Errorf("LastRunAt = empty, want RFC3339 timestamp")
	}
}

// TestCoreExtensionProvider_GetExtension_NotFound 验证找不到扩展返回 false。
func TestCoreExtensionProvider_GetExtension_NotFound(t *testing.T) {
	p := &CoreExtensionProvider{
		Discovery: &watch.DiscoveryResult{
			GlobalExts: map[string]*watch.ExtensionEntry{},
			Services:   map[string]*watch.ServiceEntry{},
		},
	}
	if _, ok := p.GetExtension("nonexistent"); ok {
		t.Errorf("GetExtension(nonexistent) = true, want false")
	}
}

// TestCoreExtensionProvider_GetExtension_GlobalAndService 验证全局和服务级扩展查找。
func TestCoreExtensionProvider_GetExtension_GlobalAndService(t *testing.T) {
	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"gext": {
				Name:       "gext",
				ConfigPath: "/gext/meta.yaml",
				Meta:       testBuildMeta("gext", true),
			},
		},
		Services: map[string]*watch.ServiceEntry{
			"web": {
				Name: "web",
				Extensions: map[string]*watch.ExtensionEntry{
					"webext": {
						Name:        "webext",
						ConfigPath:  "/webext/meta.yaml",
						ServiceName: "web",
						Meta:        testBuildMeta("webext", true),
					},
				},
			},
		},
	}
	p := &CoreExtensionProvider{Discovery: discovery}

	// 全局扩展
	info, ok := p.GetExtension("gext")
	if !ok {
		t.Fatalf("GetExtension(gext) not found")
	}
	if info.Service != "" {
		t.Errorf("gext Service = %q, want '' (global)", info.Service)
	}

	// 服务级扩展
	info, ok = p.GetExtension("webext")
	if !ok {
		t.Fatalf("GetExtension(webext) not found")
	}
	if info.Service != "web" {
		t.Errorf("webext Service = %q, want 'web'", info.Service)
	}
}

// TestCoreExtensionProvider_CreateExtension 验证创建扩展写入 meta.yaml。
func TestCoreExtensionProvider_CreateExtension(t *testing.T) {
	baseDir := t.TempDir()
	p := &CoreExtensionProvider{BaseDir: baseDir}

	t.Run("Global", func(t *testing.T) {
		meta := testBuildMeta("gext1", true)
		if err := p.CreateExtension(meta, ""); err != nil {
			t.Fatalf("CreateExtension: %v", err)
		}
		// 验证文件存在
		metaPath := filepath.Join(baseDir, "extensions", "gext1", "meta.yaml")
		if _, err := os.Stat(metaPath); err != nil {
			t.Errorf("global meta.yaml not written: %v", err)
		}
	})

	t.Run("Service", func(t *testing.T) {
		meta := testBuildMeta("webext", true)
		if err := p.CreateExtension(meta, "web"); err != nil {
			t.Fatalf("CreateExtension: %v", err)
		}
		// 验证文件存在
		metaPath := filepath.Join(baseDir, "services", "web", "extensions", "webext", "meta.yaml")
		if _, err := os.Stat(metaPath); err != nil {
			t.Errorf("service meta.yaml not written: %v", err)
		}
	})
}

// TestCoreExtensionProvider_UpdateExtension_NotFound 验证更新不存在的扩展返回错误。
func TestCoreExtensionProvider_UpdateExtension_NotFound(t *testing.T) {
	p := &CoreExtensionProvider{
		Discovery: &watch.DiscoveryResult{
			GlobalExts: map[string]*watch.ExtensionEntry{},
			Services:   map[string]*watch.ServiceEntry{},
		},
	}
	if err := p.UpdateExtension("nonexistent", &config.ExtensionMeta{}, ""); err == nil {
		t.Errorf("UpdateExtension on missing: expected error, got nil")
	}
}

// TestCoreExtensionProvider_DeleteExtension_NotFound 验证删除不存在的扩展返回错误。
func TestCoreExtensionProvider_DeleteExtension_NotFound(t *testing.T) {
	p := &CoreExtensionProvider{
		Discovery: &watch.DiscoveryResult{
			GlobalExts: map[string]*watch.ExtensionEntry{},
			Services:   map[string]*watch.ServiceEntry{},
		},
	}
	if err := p.DeleteExtension("nonexistent", ""); err == nil {
		t.Errorf("DeleteExtension on missing: expected error, got nil")
	}
}

// TestCoreExtensionProvider_SaveExtensionEnv_NotFound 验证保存不存在的扩展 env 返回错误。
func TestCoreExtensionProvider_SaveExtensionEnv_NotFound(t *testing.T) {
	p := &CoreExtensionProvider{
		Discovery: &watch.DiscoveryResult{
			GlobalExts: map[string]*watch.ExtensionEntry{},
			Services:   map[string]*watch.ServiceEntry{},
		},
	}
	if err := p.SaveExtensionEnv("nonexistent", &config.EnvFile{}, ""); err == nil {
		t.Errorf("SaveExtensionEnv on missing: expected error, got nil")
	}
}

// TestCoreExtensionProvider_RunExtension_NoExecutor 验证未配置 Executor 时返回错误。
func TestCoreExtensionProvider_RunExtension_NoExecutor(t *testing.T) {
	p := &CoreExtensionProvider{
		Discovery: &watch.DiscoveryResult{},
	}
	_, err := p.RunExtension(context.Background(), "ext1", "run", "", false)
	if err == nil {
		t.Errorf("RunExtension with nil Executor: expected error, got nil")
	}
}

// TestCoreExtensionProvider_RunExtension_NotFound 验证扩展不存在时返回错误。
func TestCoreExtensionProvider_RunExtension_NotFound(t *testing.T) {
	p := &CoreExtensionProvider{
		Discovery: &watch.DiscoveryResult{
			GlobalExts: map[string]*watch.ExtensionEntry{},
			Services:   map[string]*watch.ServiceEntry{},
		},
		Executor: &extension.Executor{}, // 非 nil 占位
	}
	_, err := p.RunExtension(context.Background(), "nonexistent", "run", "", false)
	if err == nil {
		t.Errorf("RunExtension on missing ext: expected error, got nil")
	}
}

// TestCoreExtensionProvider_RunExtension_DryRun 验证 dryRun=true 时不启动进程返回成功结果。
func TestCoreExtensionProvider_RunExtension_DryRun(t *testing.T) {
	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"ext1": {
				Name:       "ext1",
				ConfigPath: "/ext1/meta.yaml",
				Meta:       testBuildMeta("ext1", true),
			},
		},
	}
	p := &CoreExtensionProvider{
		Discovery: discovery,
		Executor:  &extension.Executor{}, // dryRun 路径仅检查 nil
	}
	result, err := p.RunExtension(context.Background(), "ext1", "run", "", true)
	if err != nil {
		t.Fatalf("RunExtension dryRun: %v", err)
	}
	if result == nil {
		t.Fatalf("RunExtension dryRun result = nil")
	}
	if result.State != extension.TaskSuccess {
		t.Errorf("dryRun State = %q, want 'success'", result.State)
	}
	if result.Progress != 100 {
		t.Errorf("dryRun Progress = %d, want 100", result.Progress)
	}
	if result.ResultMsg != "dry run: no process started" {
		t.Errorf("dryRun ResultMsg = %q, want 'dry run: no process started'", result.ResultMsg)
	}
	if result.ExtensionName != "ext1" {
		t.Errorf("dryRun ExtensionName = %q, want 'ext1'", result.ExtensionName)
	}
	if result.RunID == "" {
		t.Errorf("dryRun RunID = empty, want non-empty UUID")
	}
}

// TestCoreExtensionProvider_GetExtensionStatus_NotFound 验证扩展不存在返回错误。
func TestCoreExtensionProvider_GetExtensionStatus_NotFound(t *testing.T) {
	p := &CoreExtensionProvider{
		Discovery: &watch.DiscoveryResult{
			GlobalExts: map[string]*watch.ExtensionEntry{},
			Services:   map[string]*watch.ServiceEntry{},
		},
	}
	_, err := p.GetExtensionStatus("nonexistent", "")
	if err == nil {
		t.Errorf("GetExtensionStatus on missing: expected error, got nil")
	}
}

// TestCoreExtensionProvider_GetExtensionStatus_OK 验证返回扩展状态映射。
func TestCoreExtensionProvider_GetExtensionStatus_OK(t *testing.T) {
	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"ext1": {
				Name:       "ext1",
				ConfigPath: "/ext1/meta.yaml",
				Meta:       testBuildMeta("ext1", true),
			},
		},
	}
	p := &CoreExtensionProvider{Discovery: discovery}
	status, err := p.GetExtensionStatus("ext1", "")
	if err != nil {
		t.Fatalf("GetExtensionStatus: %v", err)
	}
	if status["name"] != "ext1" {
		t.Errorf("status[name] = %v, want 'ext1'", status["name"])
	}
	if status["enabled"] != true {
		t.Errorf("status[enabled] = %v, want true", status["enabled"])
	}
	if status["display_state"] != "active" {
		t.Errorf("status[display_state] = %v, want 'active'", status["display_state"])
	}
}

// --- CoreTaskProvider 测试 ---

// TestCoreTaskProvider_NilTaskMgr 验证 TaskMgr 为 nil 时各方法安全处理。
func TestCoreTaskProvider_NilTaskMgr(t *testing.T) {
	p := &CoreTaskProvider{}

	if got := p.ListRuns(extension.RunFilter{}); got != nil {
		t.Errorf("ListRuns with nil TaskMgr = %v, want nil", got)
	}
	if got := p.GetRun("any"); got != nil {
		t.Errorf("GetRun with nil TaskMgr = %v, want nil", got)
	}
	if err := p.CancelRun("any"); err == nil {
		t.Errorf("CancelRun with nil TaskMgr: expected error, got nil")
	}
}

// TestCoreTaskProvider_CancelRun_Errors 验证 CancelRun 各种错误路径。
func TestCoreTaskProvider_CancelRun_Errors(t *testing.T) {
	tm := extension.NewTaskManager(7)
	p := &CoreTaskProvider{TaskMgr: tm}

	// 1. run 不存在
	if err := p.CancelRun("nonexistent"); err == nil {
		t.Errorf("CancelRun(nonexistent): expected error, got nil")
	}

	// 2. 终态任务
	tm.RecordRun(&extension.RunResult{
		RunID: "r1", ExtensionName: "ext1", State: extension.TaskSuccess,
		StartedAt: time.Now(),
	})
	if err := p.CancelRun("r1"); err == nil {
		t.Errorf("CancelRun on terminal: expected error, got nil")
	}

	// 3. 运行中任务但 ConcurrencyManager 未注入 → "not found in concurrency manager"
	tm.RecordRun(&extension.RunResult{
		RunID: "r2", ExtensionName: "ext1", State: extension.TaskRunning,
		StartedAt: time.Now(),
	})
	err := p.CancelRun("r2")
	if err == nil {
		t.Errorf("CancelRun on running (no concMgr): expected error, got nil")
	}
}

// TestCoreTaskProvider_GetRunLogs_NotFound 验证 run 不存在返回错误。
func TestCoreTaskProvider_GetRunLogs_NotFound(t *testing.T) {
	p := &CoreTaskProvider{TaskMgr: extension.NewTaskManager(7), LogDir: t.TempDir()}
	lines, pos, err := p.GetRunLogs("nonexistent", 0)
	if err == nil {
		t.Errorf("GetRunLogs on missing run: expected error, got nil")
	}
	if lines != nil {
		t.Errorf("lines = %v, want nil", lines)
	}
	if pos != 0 {
		t.Errorf("pos = %d, want 0", pos)
	}
}

// TestCoreTaskProvider_GetRunLogs_NoLogFile 验证日志文件不存在时返回空切片。
func TestCoreTaskProvider_GetRunLogs_NoLogFile(t *testing.T) {
	tm := extension.NewTaskManager(7)
	tm.RecordRun(&extension.RunResult{
		RunID: "r1", ExtensionName: "ext1", State: extension.TaskSuccess,
		StartedAt: time.Now(),
	})
	p := &CoreTaskProvider{TaskMgr: tm, LogDir: t.TempDir()}
	lines, pos, err := p.GetRunLogs("r1", 0)
	if err != nil {
		t.Errorf("err = %v, want nil (missing file is OK)", err)
	}
	if len(lines) != 0 {
		t.Errorf("lines count = %d, want 0", len(lines))
	}
	if pos != 0 {
		t.Errorf("pos = %d, want 0", pos)
	}
}

// TestCoreTaskProvider_GetRunLogs_WithLogFile 验证读取真实日志文件并支持 sincePos。
func TestCoreTaskProvider_GetRunLogs_WithLogFile(t *testing.T) {
	logDir := t.TempDir()
	tm := extension.NewTaskManager(7)
	tm.RecordRun(&extension.RunResult{
		RunID: "r1", ExtensionName: "ext1", State: extension.TaskSuccess,
		StartedAt: time.Now(),
	})

	// 写入日志文件
	logPath := filepath.Join(logDir, "extensions", "ext1", "r1.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	p := &CoreTaskProvider{TaskMgr: tm, LogDir: logDir}
	// 全量读
	lines, pos, err := p.GetRunLogs("r1", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines count = %d, want 3", len(lines))
	}
	if pos != int64(len(content)) {
		t.Errorf("pos = %d, want %d", pos, len(content))
	}
}

// TestCoreTaskProvider_DeleteRunLogs_NotFound 验证 run 不存在返回错误。
func TestCoreTaskProvider_DeleteRunLogs_NotFound(t *testing.T) {
	p := &CoreTaskProvider{TaskMgr: extension.NewTaskManager(7), LogDir: t.TempDir()}
	if err := p.DeleteRunLogs("nonexistent"); err == nil {
		t.Errorf("DeleteRunLogs on missing run: expected error, got nil")
	}
}

// TestCoreTaskProvider_DeleteRunLogs_OK 验证清空存在的日志文件。
func TestCoreTaskProvider_DeleteRunLogs_OK(t *testing.T) {
	logDir := t.TempDir()
	tm := extension.NewTaskManager(7)
	tm.RecordRun(&extension.RunResult{
		RunID: "r1", ExtensionName: "ext1", State: extension.TaskSuccess,
		StartedAt: time.Now(),
	})
	logPath := filepath.Join(logDir, "extensions", "ext1", "r1.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &CoreTaskProvider{TaskMgr: tm, LogDir: logDir}
	if err := p.DeleteRunLogs("r1"); err != nil {
		t.Fatalf("DeleteRunLogs: %v", err)
	}
	// 文件应为空
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat after delete: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("file size after delete = %d, want 0", info.Size())
	}
}

// TestCoreTaskProvider_ListRuns_WithTaskMgr 验证委托到 TaskMgr。
func TestCoreTaskProvider_ListRuns_WithTaskMgr(t *testing.T) {
	tm := extension.NewTaskManager(7)
	tm.RecordRun(&extension.RunResult{
		RunID: "r1", ExtensionName: "ext1", State: extension.TaskSuccess,
		StartedAt: time.Now(),
	})
	p := &CoreTaskProvider{TaskMgr: tm}
	runs := p.ListRuns(extension.RunFilter{ExtensionName: "ext1"})
	if len(runs) != 1 {
		t.Errorf("ListRuns = %d runs, want 1", len(runs))
	}
}

// TestCoreTaskProvider_GetRun_WithTaskMgr 验证委托到 TaskMgr。
func TestCoreTaskProvider_GetRun_WithTaskMgr(t *testing.T) {
	tm := extension.NewTaskManager(7)
	tm.RecordRun(&extension.RunResult{
		RunID: "r1", ExtensionName: "ext1", State: extension.TaskSuccess,
		StartedAt: time.Now(),
	})
	p := &CoreTaskProvider{TaskMgr: tm}
	run := p.GetRun("r1")
	if run == nil {
		t.Fatalf("GetRun = nil, want non-nil")
	}
	if run.RunID != "r1" {
		t.Errorf("GetRun RunID = %q, want 'r1'", run.RunID)
	}
}

// TestCoreTaskProvider_ClearRuns 验证清除匹配终态任务返回删除数。
func TestCoreTaskProvider_ClearRuns(t *testing.T) {
	tm := extension.NewTaskManager(7)
	tm.RecordRun(&extension.RunResult{RunID: "r1", ExtensionName: "ext1", State: extension.TaskSuccess, StartedAt: time.Now()})
	tm.RecordRun(&extension.RunResult{RunID: "r2", ExtensionName: "ext1", State: extension.TaskFailed, StartedAt: time.Now()})
	tm.RecordRun(&extension.RunResult{RunID: "r3", ExtensionName: "ext2", State: extension.TaskSuccess, StartedAt: time.Now()})

	p := &CoreTaskProvider{TaskMgr: tm}
	deleted := p.ClearRuns(extension.RunFilter{ExtensionName: "ext1"})
	if deleted != 2 {
		t.Errorf("ClearRuns(ext1) = %d, want 2", deleted)
	}
}

// --- CoreCronProvider 测试 ---

// TestCoreCronProvider_SetDiscovery_Nil 验证 nil 参数和 nil receiver 安全。
func TestCoreCronProvider_SetDiscovery_Nil(t *testing.T) {
	p := &CoreCronProvider{Discovery: &watch.DiscoveryResult{}}
	p.SetDiscovery(nil)
	if p.Discovery == nil {
		t.Errorf("SetDiscovery(nil) should be no-op")
	}
	var nilP *CoreCronProvider
	nilP.SetDiscovery(&watch.DiscoveryResult{})
}

// TestCoreCronProvider_ListCronEntries_Nil 验证 CronScheduler 或 Discovery 为 nil 时返回 nil。
func TestCoreCronProvider_ListCronEntries_Nil(t *testing.T) {
	// CronScheduler nil
	p1 := &CoreCronProvider{Discovery: &watch.DiscoveryResult{}}
	if got := p1.ListCronEntries(); got != nil {
		t.Errorf("ListCronEntries with nil CronScheduler = %v, want nil", got)
	}
	// Discovery nil
	p2 := &CoreCronProvider{CronScheduler: extension.NewCronScheduler(nil)}
	if got := p2.ListCronEntries(); got != nil {
		t.Errorf("ListCronEntries with nil Discovery = %v, want nil", got)
	}
}

// TestCoreCronProvider_ListCronEntries_WithEntries 验证遍历全局+服务级 on_schedule 触发器
// 并跳过 disabled 扩展。
func TestCoreCronProvider_ListCronEntries_WithEntries(t *testing.T) {
	// enabled = false 的全局扩展
	disabled := false
	enabled := true
	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"gext-disabled": {
				Name: "gext-disabled",
				Meta: &config.ExtensionMeta{
					Name:    "gext-disabled",
					Enabled: &disabled,
					Triggers: config.Triggers{
						OnSchedule: []config.TriggerSchedule{
							{Cron: "* * * * *", Action: "run"},
						},
					},
				},
			},
			"gext": {
				Name: "gext",
				Meta: &config.ExtensionMeta{
					Name:    "gext",
					Enabled: &enabled,
					Triggers: config.Triggers{
						OnSchedule: []config.TriggerSchedule{
							{Cron: "*/5 * * * *", Action: "run"},
						},
					},
				},
			},
		},
		Services: map[string]*watch.ServiceEntry{
			"web": {
				Name: "web",
				Extensions: map[string]*watch.ExtensionEntry{
					"webext": {
						Name:        "webext",
						ServiceName: "web",
						Meta: &config.ExtensionMeta{
							Name:    "webext",
							Enabled: &enabled,
							Triggers: config.Triggers{
								OnSchedule: []config.TriggerSchedule{
									{Cron: "0 * * * *", Action: "daily"},
								},
							},
						},
					},
				},
			},
		},
	}
	p := &CoreCronProvider{
		CronScheduler: extension.NewCronScheduler(nil),
		Discovery:     discovery,
	}
	entries := p.ListCronEntries()
	// 应有 2 条（gext + webext），跳过 gext-disabled
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (disabled skipped), got %d: %v", len(entries), entries)
	}

	// 验证字段
	var foundGlobal, foundService bool
	for _, e := range entries {
		if e.ExtensionName == "gext" {
			foundGlobal = true
			if e.Schedule != "*/5 * * * *" {
				t.Errorf("gext Schedule = %q, want '*/5 * * * *'", e.Schedule)
			}
			if e.ActionID != "run" {
				t.Errorf("gext ActionID = %q, want 'run'", e.ActionID)
			}
			if e.Service != "" {
				t.Errorf("gext Service = %q, want '' (global)", e.Service)
			}
		}
		if e.ExtensionName == "webext" {
			foundService = true
			if e.Service != "web" {
				t.Errorf("webext Service = %q, want 'web'", e.Service)
			}
			if e.ActionID != "daily" {
				t.Errorf("webext ActionID = %q, want 'daily'", e.ActionID)
			}
		}
	}
	if !foundGlobal {
		t.Errorf("global 'gext' entry missing")
	}
	if !foundService {
		t.Errorf("service-level 'webext' entry missing")
	}
}

// TestCoreCronProvider_ListCronHistory_NilTaskMgr 验证 TaskMgr 为 nil 时返回空切片。
func TestCoreCronProvider_ListCronHistory_NilTaskMgr(t *testing.T) {
	p := &CoreCronProvider{}
	runs := p.ListCronHistory(extension.RunFilter{})
	if runs == nil {
		t.Errorf("ListCronHistory with nil TaskMgr = nil, want non-nil empty slice")
	}
	if len(runs) != 0 {
		t.Errorf("ListCronHistory with nil TaskMgr = %d runs, want 0", len(runs))
	}
}

// TestCoreCronProvider_ListCronHistory_WithTaskMgr 验证过滤 on_schedule 触发类型。
func TestCoreCronProvider_ListCronHistory_WithTaskMgr(t *testing.T) {
	tm := extension.NewTaskManager(7)
	tm.RecordRun(&extension.RunResult{
		RunID: "r1", ExtensionName: "ext1", State: extension.TaskSuccess,
		TriggerType: "on_schedule",
		StartedAt:   time.Now(),
	})
	tm.RecordRun(&extension.RunResult{
		RunID: "r2", ExtensionName: "ext1", State: extension.TaskSuccess,
		TriggerType: "on_demand", // 应被过滤掉
		StartedAt:   time.Now(),
	})

	p := &CoreCronProvider{TaskMgr: tm}
	runs := p.ListCronHistory(extension.RunFilter{})
	if len(runs) != 1 {
		t.Fatalf("expected 1 on_schedule run, got %d", len(runs))
	}
	if runs[0].RunID != "r1" {
		t.Errorf("runs[0].RunID = %q, want 'r1'", runs[0].RunID)
	}
}

// --- CoreServiceOperator 测试 ---

// TestCoreServiceOperator_SetCancelFuncs 验证多次调用合并而非覆盖。
func TestCoreServiceOperator_SetCancelFuncs(t *testing.T) {
	o := &CoreServiceOperator{}
	called := map[string]bool{}
	cf1 := map[string]context.CancelFunc{
		"svc1": func() { called["svc1"] = true },
	}
	cf2 := map[string]context.CancelFunc{
		"svc2": func() { called["svc2"] = true },
	}

	o.SetCancelFuncs(cf1)
	o.SetCancelFuncs(cf2)

	// 两者的 cancel 都应存在
	o.cancelFuncsMu.Lock()
	_, hasSvc1 := o.cancelFuncs["svc1"]
	_, hasSvc2 := o.cancelFuncs["svc2"]
	o.cancelFuncsMu.Unlock()

	if !hasSvc1 {
		t.Errorf("svc1 cancelFunc missing after second SetCancelFuncs (should be merged)")
	}
	if !hasSvc2 {
		t.Errorf("svc2 cancelFunc missing")
	}

	// 调用 svc1 的 cancel
	o.cancelFuncsMu.Lock()
	if fn, ok := o.cancelFuncs["svc1"]; ok {
		fn()
	}
	o.cancelFuncsMu.Unlock()
	if !called["svc1"] {
		t.Errorf("svc1 cancel not invoked")
	}
}

// TestCoreServiceOperator_SetDiscovery_Nil 验证 nil 参数与 nil receiver 安全。
func TestCoreServiceOperator_SetDiscovery_Nil(t *testing.T) {
	o := &CoreServiceOperator{Discovery: &watch.DiscoveryResult{}}
	o.SetDiscovery(nil)
	if o.Discovery == nil {
		t.Errorf("SetDiscovery(nil) should be no-op")
	}
	var nilO *CoreServiceOperator
	nilO.SetDiscovery(&watch.DiscoveryResult{})
}

// TestCoreServiceOperator_ClearFailedState_NotFound 验证状态机不存在返回错误。
func TestCoreServiceOperator_ClearFailedState_NotFound(t *testing.T) {
	o := &CoreServiceOperator{
		StateMachines: map[string]*core.StateMachine{},
	}
	if err := o.ClearFailedState("nonexistent"); err == nil {
		t.Errorf("ClearFailedState on missing: expected error, got nil")
	}
}

// TestCoreServiceOperator_ClearFailedState_NotFailed 验证非 failed 状态返回错误。
func TestCoreServiceOperator_ClearFailedState_NotFailed(t *testing.T) {
	sm := core.NewStateMachine()
	sm.SetName("svc1")
	// sm 默认状态 pending
	o := &CoreServiceOperator{
		StateMachines: map[string]*core.StateMachine{"svc1": sm},
	}
	err := o.ClearFailedState("svc1")
	if err == nil {
		t.Errorf("ClearFailedState on pending: expected error, got nil")
	}
}

// TestCoreServiceOperator_ClearFailedState_OK 验证 failed 状态被重置为 pending。
func TestCoreServiceOperator_ClearFailedState_OK(t *testing.T) {
	sm := core.NewStateMachine()
	sm.SetName("svc1")
	// 转移到 failed：pending → starting → up → failed (via max_retries)
	sm.Transition(core.EventDependsReady)
	sm.Transition(core.EventProcessStarted)
	sm.Transition(core.EventMaxRetries)
	if sm.Current() != core.StateFailed {
		t.Fatalf("setup: current = %s, want failed", sm.Current())
	}

	o := &CoreServiceOperator{
		StateMachines: map[string]*core.StateMachine{"svc1": sm},
	}
	if err := o.ClearFailedState("svc1"); err != nil {
		t.Fatalf("ClearFailedState: %v", err)
	}
	if sm.Current() != core.StatePending {
		t.Errorf("after ClearFailedState: state = %s, want pending", sm.Current())
	}
}

// TestCoreServiceOperator_RestartService_NotFound 验证服务不存在时 StartService 返回错误。
func TestCoreServiceOperator_RestartService_NotFound(t *testing.T) {
	o := &CoreServiceOperator{
		ProcessMgr:    core.NewProcessManager(),
		StateMachines: map[string]*core.StateMachine{},
		Discovery: &watch.DiscoveryResult{
			Services:   map[string]*watch.ServiceEntry{},
			GlobalExts: map[string]*watch.ExtensionEntry{},
		},
	}
	err := o.RestartService("nonexistent")
	if err == nil {
		t.Errorf("RestartService on missing: expected error, got nil")
	}
}

// TestCoreServiceOperator_ForceStopService_NoProcess 验证无进程时直接返回 nil。
func TestCoreServiceOperator_ForceStopService_NoProcess(t *testing.T) {
	o := &CoreServiceOperator{
		ProcessMgr:    core.NewProcessManager(),
		StateMachines: map[string]*core.StateMachine{},
	}
	// 服务无进程也无状态机 → 应安全返回 nil
	if err := o.ForceStopService("nonexistent"); err != nil {
		t.Errorf("ForceStopService on missing: err = %v, want nil", err)
	}
}

// TestCoreServiceOperator_StopService_NoProcess 验证无进程时直接返回 nil。
func TestCoreServiceOperator_StopService_NoProcess(t *testing.T) {
	o := &CoreServiceOperator{
		ProcessMgr:    core.NewProcessManager(),
		StateMachines: map[string]*core.StateMachine{},
	}
	if err := o.StopService("nonexistent"); err != nil {
		t.Errorf("StopService on missing: err = %v, want nil", err)
	}
}

// --- CoreSystemProvider 测试 ---

// TestCoreSystemProvider_GetSystemStatus 验证系统状态字段填充。
func TestCoreSystemProvider_GetSystemStatus(t *testing.T) {
	cfg := &config.Config{Settings: config.Settings{HTTPListen: ":7979", AuthMode: "none"}}
	p := &CoreSystemProvider{
		Config:    cfg,
		StartTime: time.Now().Add(-100 * time.Second),
		BaseDir:   t.TempDir(),
		Version:   "0.0.2-test",
	}

	info := p.GetSystemStatus()
	if info.Version != "0.0.2-test" {
		t.Errorf("Version = %q, want '0.0.2-test' (应透传 CoreSystemProvider.Version)", info.Version)
	}
	if info.HTTPListen != ":7979" {
		t.Errorf("HTTPListen = %q, want ':7979'", info.HTTPListen)
	}
	if info.AuthMode != "none" {
		t.Errorf("AuthMode = %q, want 'none'", info.AuthMode)
	}
	if info.Uptime < 99 || info.Uptime > 200 {
		t.Errorf("Uptime = %d, want roughly 100", info.Uptime)
	}
	if info.WorkDir == "" {
		t.Errorf("WorkDir = empty, want non-empty")
	}
	if info.MemoryMB <= 0 {
		t.Errorf("MemoryMB = %v, want > 0 (process has RSS)", info.MemoryMB)
	}
	if info.DiskTotalMB <= 0 {
		t.Errorf("DiskTotalMB = %v, want > 0 (BaseDir on real fs)", info.DiskTotalMB)
	}
	if info.DiskUsedMB < 0 || info.DiskUsedMB > info.DiskTotalMB {
		t.Errorf("DiskUsedMB = %v, want 0 <= used <= total (%v)", info.DiskUsedMB, info.DiskTotalMB)
	}
}

// TestCoreSystemProvider_GetSystemStatus_NoBaseDir 验证 BaseDir 为空时磁盘统计为 0。
func TestCoreSystemProvider_GetSystemStatus_NoBaseDir(t *testing.T) {
	cfg := &config.Config{Settings: config.Settings{}}
	p := &CoreSystemProvider{
		Config:    cfg,
		StartTime: time.Now(),
		BaseDir:   "", // 不查询磁盘
	}
	info := p.GetSystemStatus()
	if info.DiskTotalMB != 0 || info.DiskUsedMB != 0 {
		t.Errorf("Disk stats with empty BaseDir: total=%v used=%v, want 0/0",
			info.DiskTotalMB, info.DiskUsedMB)
	}
}

// --- G-04 / G-05 修复：Benchmark 基准数据 ---

// benchBuildDiscovery 构建测试用的 DiscoveryResult：
// numSvc 个服务，每个服务 numExtPerSvc 个服务级扩展，外加 numSvc 个全局扩展。
// 用于 ListExtensions / GetExtension 的性能基准测量。
func benchBuildDiscovery(numSvc, numExtPerSvc int) *watch.DiscoveryResult {
	globalExts := make(map[string]*watch.ExtensionEntry, numSvc)
	for i := 0; i < numSvc; i++ {
		name := fmt.Sprintf("gext-%d", i)
		globalExts[name] = &watch.ExtensionEntry{
			Name:       name,
			ConfigPath: fmt.Sprintf("/extensions/%s/meta.yaml", name),
			Meta:       testBuildMeta(name, true),
		}
	}

	services := make(map[string]*watch.ServiceEntry, numSvc)
	for i := 0; i < numSvc; i++ {
		svcName := fmt.Sprintf("svc-%d", i)
		exts := make(map[string]*watch.ExtensionEntry, numExtPerSvc)
		for j := 0; j < numExtPerSvc; j++ {
			extName := fmt.Sprintf("ext-%d-%d", i, j)
			exts[extName] = &watch.ExtensionEntry{
				Name:        extName,
				ConfigPath:  fmt.Sprintf("/services/%s/extensions/%s/meta.yaml", svcName, extName),
				ServiceName: svcName,
				Meta:        testBuildMeta(extName, true),
			}
		}
		services[svcName] = &watch.ServiceEntry{
			Name:       svcName,
			ConfigPath: fmt.Sprintf("/services/%s/service.yaml", svcName),
			Extensions: exts,
		}
	}

	return &watch.DiscoveryResult{
		Services:   services,
		GlobalExts: globalExts,
	}
}

// BenchmarkListExtensions 测量 ListExtensions 性能（10个服务×5扩展 + 10个全局扩展）。
// G-04 修复：建立 benchmark 基准数据
func BenchmarkListExtensions(b *testing.B) {
	// setup：10个服务×5扩展（含10个全局扩展），共 60 个扩展条目
	p := &CoreExtensionProvider{
		Discovery: benchBuildDiscovery(10, 5),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = p.ListExtensions()
	}
}

// BenchmarkGetExtension 测量 GetExtension 性能（服务级扩展查找，需遍历服务表）。
// G-04 修复：建立 benchmark 基准数据
func BenchmarkGetExtension(b *testing.B) {
	p := &CoreExtensionProvider{
		Discovery: benchBuildDiscovery(10, 5),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = p.GetExtension("ext-5-2")
	}
}

// BenchmarkListServiceStates 测量 ListServiceStates 性能（10个服务的状态聚合）。
// G-04 修复：建立 benchmark 基准数据
func BenchmarkListServiceStates(b *testing.B) {
	const numSvc = 10
	stateMachines := make(map[string]*core.StateMachine, numSvc)
	services := make(map[string]*watch.ServiceEntry, numSvc)
	for i := 0; i < numSvc; i++ {
		name := fmt.Sprintf("svc-%d", i)
		sm := core.NewStateMachine()
		sm.SetName(name)
		stateMachines[name] = sm
		services[name] = &watch.ServiceEntry{
			Name:       name,
			ConfigPath: fmt.Sprintf("/services/%s/service.yaml", name),
		}
	}
	p := &CoreStateProvider{
		StateMachines: stateMachines,
		Discovery: &watch.DiscoveryResult{
			Services: services,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = p.ListServiceStates()
	}
}

// BenchmarkGetServiceLogs 测量日志读取性能（1000行，含扩展日志过滤）。
// G-04 修复：建立 benchmark 基准数据
func BenchmarkGetServiceLogs(b *testing.B) {
	logDir := b.TempDir()
	svcLogDir := filepath.Join(logDir, "services", "svc1")
	if err := os.MkdirAll(svcLogDir, 0755); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	// 构造 1000 行日志：每 5 行混入 1 行扩展日志，验证过滤路径
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		if i%5 == 0 {
			sb.WriteString(fmt.Sprintf("[ext:foo] extension log line %d\n", i))
		} else {
			sb.WriteString(fmt.Sprintf("2026-07-18T12:00:00Z process log line %d\n", i))
		}
	}
	if err := os.WriteFile(filepath.Join(svcLogDir, "current"), []byte(sb.String()), 0644); err != nil {
		b.Fatalf("write: %v", err)
	}

	p := &FileLogProvider{LogDir: logDir}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, _ = p.GetServiceLogs("svc1", 0)
	}
}
