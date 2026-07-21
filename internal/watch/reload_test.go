package watch

import (
	"testing"

	"github.com/supdorg/supd/internal/config"
)

// REQ-F-027: ClassifyChange 对 service.yaml 各字段正确分类

func TestClassifyServiceChange_Command(t *testing.T) {
	// REQ-F-027: command → NeedRestart
	old := &config.ServiceConfig{Command: []string{"old"}}
	new_ := &config.ServiceConfig{Command: []string{"new"}}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	if len(changes) == 0 {
		t.Fatal("expected changes for command modification")
	}

	found := false
	for _, c := range changes {
		if c.Category == CategoryNeedRestart {
			for _, f := range c.Fields {
				if f == "command" {
					found = true
					if c.Detail != "重启服务后生效" {
						t.Errorf("expected detail '重启服务后生效', got '%s'", c.Detail)
					}
				}
			}
		}
	}
	if !found {
		t.Error("command change should be classified as NeedRestart")
	}
}

func TestClassifyServiceChange_User(t *testing.T) {
	// REQ-F-027: user → NeedRestart
	old := &config.ServiceConfig{User: "olduser"}
	new_ := &config.ServiceConfig{User: "newuser"}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNeedRestart, "user")
}

func TestClassifyServiceChange_Group(t *testing.T) {
	// REQ-F-027: group → NeedRestart
	old := &config.ServiceConfig{Group: "oldgroup"}
	new_ := &config.ServiceConfig{Group: "newgroup"}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNeedRestart, "group")
}

func TestClassifyServiceChange_Workdir(t *testing.T) {
	// REQ-F-027: workdir → NeedRestart
	old := &config.ServiceConfig{Workdir: "/old"}
	new_ := &config.ServiceConfig{Workdir: "/new"}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNeedRestart, "workdir")
}

func TestClassifyServiceChange_DependsOn(t *testing.T) {
	// REQ-F-027: depends_on → NextStart
	old := &config.ServiceConfig{DependsOn: []string{"svc1"}}
	new_ := &config.ServiceConfig{DependsOn: []string{"svc2"}}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNextStart, "depends_on")
}

func TestClassifyServiceChange_Readiness(t *testing.T) {
	// REQ-F-027: readiness → NextStart
	old := &config.ServiceConfig{Readiness: &config.ReadinessConfig{Type: "fd_notify"}}
	new_ := &config.ServiceConfig{Readiness: &config.ReadinessConfig{Type: "tcp_check", Port: 8080}}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNextStart, "readiness")
}

func TestClassifyServiceChange_Restart(t *testing.T) {
	// REQ-F-027: restart → Immediate
	old := &config.ServiceConfig{Restart: &config.RestartConfig{Policy: "always"}}
	new_ := &config.ServiceConfig{Restart: &config.RestartConfig{Policy: "never"}}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "restart")
}

func TestClassifyServiceChange_Logging(t *testing.T) {
	// REQ-F-027: logging → Immediate
	enabled := true
	old := &config.ServiceConfig{Logging: &config.LoggingConfig{Enabled: &enabled, MaxSizeMB: 10}}
	newMaxMB := 20
	new_ := &config.ServiceConfig{Logging: &config.LoggingConfig{Enabled: &enabled, MaxSizeMB: newMaxMB}}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "logging")
}

func TestClassifyServiceChange_Tags(t *testing.T) {
	// REQ-F-027: tags → Immediate
	old := &config.ServiceConfig{Tags: []string{"old"}}
	new_ := &config.ServiceConfig{Tags: []string{"new"}}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "tags")
}

func TestClassifyServiceChange_Stop(t *testing.T) {
	// REQ-F-027: stop → Immediate
	old := &config.ServiceConfig{Stop: &config.StopConfig{GraceSeconds: 10}}
	new_ := &config.ServiceConfig{Stop: &config.StopConfig{GraceSeconds: 20}}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "stop")
}

func TestClassifyServiceChange_Signals(t *testing.T) {
	// REQ-F-027: signals → Immediate
	old := &config.ServiceConfig{Signals: &config.SignalsConfig{Reload: "SIGHUP"}}
	new_ := &config.ServiceConfig{Signals: &config.SignalsConfig{Reload: "SIGUSR1"}}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "signals")
}

func TestClassifyServiceChange_Package(t *testing.T) {
	// REQ-F-027: package → Immediate
	old := &config.ServiceConfig{Package: &config.PackageConfig{Default: "exclude"}}
	new_ := &config.ServiceConfig{Package: &config.PackageConfig{Default: "include"}}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "package")
}

func TestClassifyServiceChange_NoChange(t *testing.T) {
	// REQ-F-027: 无变更时返回空结果
	svc := &config.ServiceConfig{Command: []string{"run"}, User: "root"}
	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", svc, svc)
	if len(changes) != 0 {
		t.Errorf("expected no changes for identical configs, got %d", len(changes))
	}
}

func TestClassifyServiceChange_MultipleCategories(t *testing.T) {
	// REQ-F-027: 同一文件不同字段的变更应分属不同类别
	old := &config.ServiceConfig{
		Command:  []string{"old"},
		DependsOn: []string{"svc1"},
		Tags:     []string{"v1"},
	}
	new_ := &config.ServiceConfig{
		Command:  []string{"new"},
		DependsOn: []string{"svc2"},
		Tags:     []string{"v2"},
	}

	changes := ClassifyChange("/etc/supd/services/myapp/service.yaml", old, new_)

	categories := make(map[ChangeCategory][]string)
	for _, c := range changes {
		categories[c.Category] = append(categories[c.Category], c.Fields...)
	}

	if _, ok := categories[CategoryNeedRestart]; !ok {
		t.Error("expected NeedRestart category for command change")
	}
	if _, ok := categories[CategoryNextStart]; !ok {
		t.Error("expected NextStart category for depends_on change")
	}
	if _, ok := categories[CategoryImmediate]; !ok {
		t.Error("expected Immediate category for tags change")
	}
}

// REQ-F-027: ClassifyChange 对 meta.yaml 各字段正确分类

func TestClassifyExtensionChange_Triggers(t *testing.T) {
	// REQ-F-027: triggers → Immediate
	onDemand := true
	old := &config.ExtensionMeta{Triggers: config.Triggers{OnDemand: &onDemand}}
	onDemand2 := false
	new_ := &config.ExtensionMeta{Triggers: config.Triggers{OnDemand: &onDemand2}}

	changes := ClassifyChange("/etc/supd/extensions/myext/meta.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "triggers")
}

func TestClassifyExtensionChange_Actions(t *testing.T) {
	// REQ-F-027: actions → Immediate
	old := &config.ExtensionMeta{Actions: []config.Action{{ID: "act1", Label: "Act1"}}}
	new_ := &config.ExtensionMeta{Actions: []config.Action{{ID: "act2", Label: "Act2"}}}

	changes := ClassifyChange("/etc/supd/extensions/myext/meta.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "actions")
}

func TestClassifyExtensionChange_Concurrency(t *testing.T) {
	// REQ-F-027: concurrency → Immediate
	old := &config.ExtensionMeta{Concurrency: "serialize"}
	new_ := &config.ExtensionMeta{Concurrency: "parallel"}

	changes := ClassifyChange("/etc/supd/extensions/myext/meta.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "concurrency")
}

func TestClassifyExtensionChange_Timeout(t *testing.T) {
	// REQ-F-027: timeout → NextRun
	old := &config.ExtensionMeta{TimeoutSeconds: 600}
	new_ := &config.ExtensionMeta{TimeoutSeconds: 1200}

	changes := ClassifyChange("/etc/supd/extensions/myext/meta.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNextRun, "timeout")
}

func TestClassifyExtensionChange_RunAs(t *testing.T) {
	// REQ-F-027: run_as → NextRun
	old := &config.ExtensionMeta{RunAs: "root"}
	new_ := &config.ExtensionMeta{RunAs: "nobody"}

	changes := ClassifyChange("/etc/supd/extensions/myext/meta.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNextRun, "run_as")
}

func TestClassifyExtensionChange_NoChange(t *testing.T) {
	// REQ-F-027: 无变更时返回空
	ext := &config.ExtensionMeta{Concurrency: "serialize", TimeoutSeconds: 600}
	changes := ClassifyChange("/etc/supd/extensions/myext/meta.yaml", ext, ext)
	if len(changes) != 0 {
		t.Errorf("expected no changes for identical extension configs, got %d", len(changes))
	}
}

// REQ-F-027: ClassifyChange 对 config.yaml 各字段正确分类

func TestClassifyConfigChange_HTTPListen(t *testing.T) {
	// REQ-F-027: http_listen → NeedSupdRestart
	old := &config.Config{Settings: config.Settings{HTTPListen: ":8080"}}
	new_ := &config.Config{Settings: config.Settings{HTTPListen: ":9090"}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNeedSupdRestart, "settings.http_listen")
}

func TestClassifyConfigChange_AuthMode(t *testing.T) {
	// REQ-F-027: auth_mode → NeedSupdRestart
	old := &config.Config{Settings: config.Settings{AuthMode: "local_skip"}}
	new_ := &config.Config{Settings: config.Settings{AuthMode: "always_token"}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNeedSupdRestart, "settings.auth_mode")
}

func TestClassifyConfigChange_AuthToken(t *testing.T) {
	// REQ-F-027: auth_token → NeedSupdRestart
	old := &config.Config{Settings: config.Settings{AuthToken: "old-token"}}
	new_ := &config.Config{Settings: config.Settings{AuthToken: "new-token"}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNeedSupdRestart, "settings.auth_token")
}

func TestClassifyConfigChange_LocalNetworks(t *testing.T) {
	// REQ-F-027: local_networks → NeedSupdRestart
	old := &config.Config{Settings: config.Settings{LocalNetworks: []string{"10.0.0.0/8"}}}
	new_ := &config.Config{Settings: config.Settings{LocalNetworks: []string{"172.16.0.0/12"}}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryNeedSupdRestart, "settings.local_networks")
}

func TestClassifyConfigChange_LogLevel(t *testing.T) {
	// REQ-F-027: log_level → Immediate
	old := &config.Config{Settings: config.Settings{LogLevel: "info"}}
	new_ := &config.Config{Settings: config.Settings{LogLevel: "debug"}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "settings.log_level")
}

func TestClassifyConfigChange_LogMaxSizeMB(t *testing.T) {
	// REQ-F-027: log_max_size_mb → Immediate
	old := &config.Config{Settings: config.Settings{LogMaxSizeMB: 10}}
	new_ := &config.Config{Settings: config.Settings{LogMaxSizeMB: 20}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "settings.log_max_size_mb")
}

func TestClassifyConfigChange_ShutdownGraceSeconds(t *testing.T) {
	// REQ-F-027: shutdown_grace_seconds → Immediate
	old := &config.Config{Settings: config.Settings{ShutdownGraceSeconds: 30}}
	new_ := &config.Config{Settings: config.Settings{ShutdownGraceSeconds: 60}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "settings.shutdown_grace_seconds")
}

func TestClassifyConfigChange_ExtensionDefaultTimeoutSeconds(t *testing.T) {
	// REQ-F-027: extension_default_timeout_seconds → Immediate
	old := &config.Config{Settings: config.Settings{ExtensionDefaultTimeoutSeconds: 600}}
	new_ := &config.Config{Settings: config.Settings{ExtensionDefaultTimeoutSeconds: 900}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "settings.extension_default_timeout_seconds")
}

func TestClassifyConfigChange_MaxUploadSizeMB(t *testing.T) {
	// REQ-F-027: max_upload_size_mb → Immediate
	old := &config.Config{Settings: config.Settings{MaxUploadSizeMB: 100}}
	new_ := &config.Config{Settings: config.Settings{MaxUploadSizeMB: 200}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "settings.max_upload_size_mb")
}

func TestClassifyConfigChange_DefaultsRestart(t *testing.T) {
	// REQ-F-027: defaults.restart → Immediate
	old := &config.Config{Defaults: config.DefaultRestart{Restart: config.RestartConfig{Policy: "always"}}}
	new_ := &config.Config{Defaults: config.DefaultRestart{Restart: config.RestartConfig{Policy: "never"}}}

	changes := ClassifyChange("/etc/supd/config.yaml", old, new_)
	assertCategoryForField(t, changes, CategoryImmediate, "defaults.restart")
}

func TestClassifyConfigChange_NoChange(t *testing.T) {
	// REQ-F-027: 无变更时返回空
	cfg := &config.Config{Settings: config.Settings{HTTPListen: ":8080"}}
	changes := ClassifyChange("/etc/supd/config.yaml", cfg, cfg)
	if len(changes) != 0 {
		t.Errorf("expected no changes for identical configs, got %d", len(changes))
	}
}

// REQ-F-027: ClassifyChange 对 env.yaml 正确分类

func TestClassifyChange_ServiceEnv(t *testing.T) {
	// REQ-F-027: env.yaml（服务）→ NeedRestart
	changes := ClassifyChange("/etc/supd/services/myapp/env.yaml", nil, nil)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Category != CategoryNeedRestart {
		t.Errorf("expected CategoryNeedRestart, got %s", changes[0].Category)
	}
	if changes[0].Detail != "重启服务后生效" {
		t.Errorf("expected '重启服务后生效', got '%s'", changes[0].Detail)
	}
}

func TestClassifyChange_ExtensionEnv(t *testing.T) {
	// REQ-F-027: env.yaml（扩展）→ NextRun
	changes := ClassifyChange("/etc/supd/extensions/myext/env.yaml", nil, nil)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Category != CategoryNextRun {
		t.Errorf("expected CategoryNextRun, got %s", changes[0].Category)
	}
	if changes[0].Detail != "下次运行生效" {
		t.Errorf("expected '下次运行生效', got '%s'", changes[0].Detail)
	}
}

func TestClassifyChange_GlobalEnv(t *testing.T) {
	// REQ-F-027: env/*.yaml（全局）→ NoImpact
	changes := ClassifyChange("/etc/supd/env/00-base.yaml", nil, nil)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Category != CategoryNoImpact {
		t.Errorf("expected CategoryNoImpact, got %s", changes[0].Category)
	}
}

func TestClassifyChange_ServiceExtensionEnv(t *testing.T) {
	// REQ-F-027: 服务级扩展的 env.yaml → NextRun
	changes := ClassifyChange("/etc/supd/services/myapp/extensions/myext/env.yaml", nil, nil)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Category != CategoryNextRun {
		t.Errorf("expected CategoryNextRun, got %s", changes[0].Category)
	}
}

func TestClassifyChange_UnknownFile(t *testing.T) {
	// 未知文件类型返回 nil
	changes := ClassifyChange("/etc/supd/unknown.txt", nil, nil)
	if len(changes) != 0 {
		t.Errorf("expected no changes for unknown file type, got %d", len(changes))
	}
}

// REQ-F-027: ReloadManager.Reload 生成正确的 ReloadResult

func TestReloadManager_ReloadWithServiceChanges(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	oldResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"old"}, Tags: []string{"v1"}},
			},
		},
	}

	newResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"new"}, Tags: []string{"v2"}},
			},
		},
	}

	result := rm.Reload(oldResult, newResult)

	// command 变更 → NeedRestart → PendingChange
	// tags 变更 → Immediate → ImmediateChanges
	if len(result.ImmediateChanges) == 0 {
		t.Error("expected immediate changes for tags modification")
	}
	if len(result.PendingChanges) == 0 {
		t.Error("expected pending changes for command modification")
	}
}

// REQ-F-027: 立即生效变更和待生效变更正确分离

func TestReloadManager_ImmediateAndPendingSeparation(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	oldResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config: &config.ServiceConfig{
					Command: []string{"old"},
					Tags:    []string{"v1"},
				},
			},
		},
	}

	newResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config: &config.ServiceConfig{
					Command: []string{"new"},
					Tags:    []string{"v2"},
				},
			},
		},
	}

	result := rm.Reload(oldResult, newResult)

	// tags → Immediate
	foundImmediate := false
	for _, c := range result.ImmediateChanges {
		for _, f := range c.Fields {
			if f == "tags" {
				foundImmediate = true
			}
		}
	}
	if !foundImmediate {
		t.Error("tags should be in immediate changes")
	}

	// command → NeedRestart → Pending
	foundPending := false
	for _, pc := range result.PendingChanges {
		for _, c := range pc.Changes {
			for _, f := range c.Fields {
				if f == "command" {
					foundPending = true
					if c.Category != CategoryNeedRestart {
						t.Errorf("command should be NeedRestart, got %s", c.Category)
					}
				}
			}
		}
	}
	if !foundPending {
		t.Error("command should be in pending changes")
	}
}

// REQ-F-027: GetPendingChanges 返回正确结果

func TestReloadManager_GetPendingChanges(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	// 初始为空
	pending := rm.GetPendingChanges("myapp")
	if len(pending) != 0 {
		t.Error("expected no pending changes initially")
	}

	// 触发一次重载
	oldResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"old"}},
			},
		},
	}
	newResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"new"}},
			},
		},
	}

	rm.Reload(oldResult, newResult)

	// 应该有待生效变更
	pending = rm.GetPendingChanges("myapp")
	if len(pending) == 0 {
		t.Error("expected pending changes after reload")
	}
	if pending[0].ServiceName != "myapp" {
		t.Errorf("expected service name 'myapp', got '%s'", pending[0].ServiceName)
	}

	// 不存在的服务应该返回空
	pending = rm.GetPendingChanges("nonexist")
	if len(pending) != 0 {
		t.Error("expected no pending changes for non-existent service")
	}
}

// REQ-F-027: ClearPendingChanges 清除后为空

func TestReloadManager_ClearPendingChanges(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	// 触发重载产生待生效变更
	oldResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"old"}},
			},
		},
	}
	newResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"new"}},
			},
		},
	}

	rm.Reload(oldResult, newResult)

	// 确认有待生效变更
	if !rm.HasPendingChanges("myapp") {
		t.Error("expected pending changes")
	}

	// 清除
	rm.ClearPendingChanges("myapp")

	// 清除后应该为空
	if rm.HasPendingChanges("myapp") {
		t.Error("expected no pending changes after clear")
	}
	pending := rm.GetPendingChanges("myapp")
	if len(pending) != 0 {
		t.Error("expected empty pending changes after clear")
	}
}

// REQ-F-027: 无变更时返回空结果

func TestReloadManager_NoChanges(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	svc := &ServiceEntry{
		Name:       "myapp",
		ConfigPath: "/etc/supd/services/myapp/service.yaml",
		Config:     &config.ServiceConfig{Command: []string{"run"}},
	}
	result := &DiscoveryResult{
		Services: map[string]*ServiceEntry{"myapp": svc},
	}

	reloadResult := rm.Reload(result, result)

	if len(reloadResult.ImmediateChanges) != 0 {
		t.Errorf("expected no immediate changes, got %d", len(reloadResult.ImmediateChanges))
	}
	if len(reloadResult.PendingChanges) != 0 {
		t.Errorf("expected no pending changes, got %d", len(reloadResult.PendingChanges))
	}
}

// REQ-F-027: ReloadConfig 便捷方法

func TestReloadManager_ReloadConfig(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	oldCfg := &config.Config{Settings: config.Settings{HTTPListen: ":8080"}}
	newCfg := &config.Config{Settings: config.Settings{HTTPListen: ":9090"}}

	result := rm.ReloadConfig("/etc/supd/config.yaml", oldCfg, newCfg)

	// http_listen → NeedSupdRestart → PendingChanges
	if len(result.PendingChanges) == 0 {
		t.Error("expected pending changes for http_listen modification")
	}
}

// REQ-F-027: ReloadServiceEnv 便捷方法

func TestReloadManager_ReloadServiceEnv(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	result := rm.ReloadServiceEnv("/etc/supd/services/myapp/env.yaml", "myapp")

	// 服务 env → NeedRestart → PendingChanges
	if len(result.PendingChanges) == 0 {
		t.Error("expected pending changes for service env modification")
	}
	if result.PendingChanges[0].ServiceName != "myapp" {
		t.Errorf("expected service name 'myapp', got '%s'", result.PendingChanges[0].ServiceName)
	}
}

// REQ-F-027: ReloadGlobalEnv 便捷方法

func TestReloadManager_ReloadGlobalEnv(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	result := rm.ReloadGlobalEnv("/etc/supd/env/00-base.yaml")

	// 全局 env → NoImpact，不放入 PendingChanges
	if len(result.ImmediateChanges) == 0 {
		t.Error("expected changes for global env modification")
	}
	if result.ImmediateChanges[0].Category != CategoryNoImpact {
		t.Errorf("expected NoImpact category, got %s", result.ImmediateChanges[0].Category)
	}
}

// REQ-F-027: 全局扩展变更正确分类

func TestReloadManager_GlobalExtensionChanges(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	oldResult := &DiscoveryResult{
		GlobalExts: map[string]*ExtensionEntry{
			"myext": {
				Name:       "myext",
				ConfigPath: "/etc/supd/extensions/myext/meta.yaml",
				Meta:       &config.ExtensionMeta{Concurrency: "serialize"},
			},
		},
	}
	newResult := &DiscoveryResult{
		GlobalExts: map[string]*ExtensionEntry{
			"myext": {
				Name:       "myext",
				ConfigPath: "/etc/supd/extensions/myext/meta.yaml",
				Meta:       &config.ExtensionMeta{Concurrency: "parallel"},
			},
		},
	}

	result := rm.Reload(oldResult, newResult)

	// concurrency → Immediate
	if len(result.ImmediateChanges) == 0 {
		t.Error("expected immediate changes for concurrency modification")
	}
}

// REQ-F-027: 服务级扩展变更正确分类

func TestReloadManager_ServiceExtensionChanges(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	oldResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"run"}},
				Extensions: map[string]*ExtensionEntry{
					"myext": {
						Name:        "myext",
						ConfigPath:  "/etc/supd/services/myapp/extensions/myext/meta.yaml",
						Meta:        &config.ExtensionMeta{TimeoutSeconds: 600},
						ServiceName: "myapp",
					},
				},
			},
		},
	}
	newResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"myapp": {
				Name:       "myapp",
				ConfigPath: "/etc/supd/services/myapp/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"run"}},
				Extensions: map[string]*ExtensionEntry{
					"myext": {
						Name:        "myext",
						ConfigPath:  "/etc/supd/services/myapp/extensions/myext/meta.yaml",
						Meta:        &config.ExtensionMeta{TimeoutSeconds: 1200},
						ServiceName: "myapp",
					},
				},
			},
		},
	}

	result := rm.Reload(oldResult, newResult)

	// timeout → NextRun → PendingChanges
	if len(result.PendingChanges) == 0 {
		t.Error("expected pending changes for extension timeout modification")
	}
	found := false
	for _, pc := range result.PendingChanges {
		if pc.ServiceName == "myapp" {
			for _, c := range pc.Changes {
				for _, f := range c.Fields {
					if f == "timeout" {
						found = true
						if c.Category != CategoryNextRun {
							t.Errorf("timeout should be NextRun, got %s", c.Category)
						}
					}
				}
			}
		}
	}
	if !found {
		t.Error("timeout change should be found in pending changes for myapp")
	}
}

// REQ-F-027: HasPendingChanges 正确工作

func TestReloadManager_HasPendingChanges(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	if rm.HasPendingChanges("myapp") {
		t.Error("expected no pending changes initially")
	}

	// 通过 ReloadConfig 产生全局待生效变更
	oldCfg := &config.Config{Settings: config.Settings{HTTPListen: ":8080"}}
	newCfg := &config.Config{Settings: config.Settings{HTTPListen: ":9090"}}
	rm.ReloadConfig("/etc/supd/config.yaml", oldCfg, newCfg)

	// 全局待生效变更用空字符串或 "global" 查询
	if !rm.HasPendingChanges("global") {
		t.Error("expected pending changes for global after config change")
	}
}

// REQ-F-027: GetAllPendingChanges 返回所有待生效变更

func TestReloadManager_GetAllPendingChanges(t *testing.T) {
	d := NewDiscovery("/etc/supd", "/var/log/supd")
	rm := NewReloadManager(d)

	// 触发两个服务的变更
	oldResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"app1": {
				Name:       "app1",
				ConfigPath: "/etc/supd/services/app1/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"old1"}},
			},
			"app2": {
				Name:       "app2",
				ConfigPath: "/etc/supd/services/app2/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"old2"}},
			},
		},
	}
	newResult := &DiscoveryResult{
		Services: map[string]*ServiceEntry{
			"app1": {
				Name:       "app1",
				ConfigPath: "/etc/supd/services/app1/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"new1"}},
			},
			"app2": {
				Name:       "app2",
				ConfigPath: "/etc/supd/services/app2/service.yaml",
				Config:     &config.ServiceConfig{Command: []string{"new2"}},
			},
		},
	}

	rm.Reload(oldResult, newResult)

	all := rm.GetAllPendingChanges()
	if len(all) != 2 {
		t.Errorf("expected 2 pending change groups, got %d", len(all))
	}
}

// REQ-F-027: FormatPendingMessage 正确格式化

func TestFormatPendingMessage_Service(t *testing.T) {
	pc := PendingChange{
		ServiceName: "myapp",
		Changes: []ClassifiedChange{
			{Fields: []string{"command", "user"}, Category: CategoryNeedRestart},
		},
	}
	msg := FormatPendingMessage(pc)
	expected := "服务 myapp 配置已更新（command, user），重启服务后生效"
	if msg != expected {
		t.Errorf("expected '%s', got '%s'", expected, msg)
	}
}

func TestFormatPendingMessage_Global(t *testing.T) {
	pc := PendingChange{
		ServiceName: "global",
		Changes: []ClassifiedChange{
			{Fields: []string{"settings.http_listen"}, Category: CategoryNeedSupdRestart},
		},
	}
	msg := FormatPendingMessage(pc)
	expected := "全局配置已更新（settings.http_listen），需重启后生效"
	if msg != expected {
		t.Errorf("expected '%s', got '%s'", expected, msg)
	}
}

func TestFormatPendingMessage_Empty(t *testing.T) {
	pc := PendingChange{ServiceName: "myapp"}
	msg := FormatPendingMessage(pc)
	if msg != "" {
		t.Errorf("expected empty message, got '%s'", msg)
	}
}

// REQ-F-027: detectFileType 测试

func TestDetectFileType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/etc/supd/services/myapp/service.yaml", "service"},
		{"/etc/supd/extensions/myext/meta.yaml", "extension"},
		{"/etc/supd/config.yaml", "config"},
		{"/etc/supd/services/myapp/env.yaml", "service_env"},
		{"/etc/supd/extensions/myext/env.yaml", "extension_env"},
		{"/etc/supd/services/myapp/extensions/ext1/env.yaml", "extension_env"},
		{"/etc/supd/env/00-base.yaml", "global_env"},
		{"/etc/supd/unknown.txt", ""},
	}

	for _, tt := range tests {
		result := detectFileType(tt.path)
		if result != tt.expected {
			t.Errorf("detectFileType(%q) = %q, want %q", tt.path, result, tt.expected)
		}
	}
}

// --- 辅助函数 ---

func assertCategoryForField(t *testing.T, changes []ClassifiedChange, expectedCategory ChangeCategory, expectedField string) {
	t.Helper()
	found := false
	for _, c := range changes {
		if c.Category == expectedCategory {
			for _, f := range c.Fields {
				if f == expectedField {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("field %q not found with category %s in changes: %+v", expectedField, expectedCategory, changes)
	}
}
