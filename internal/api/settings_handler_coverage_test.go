package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/errors"
)

// fakeSettingsProvider 嵌入接口以满足契约，覆盖 settings_handler 测试所需方法。
type fakeSettingsProvider struct {
	SettingsProvider
	settings      *config.Settings
	env           *config.EnvFile
	runtimes      map[string]string
	envFiles      []string
	extDirs       []string
	defaults      config.DefaultRestart
	updateErr     error
	envErr        error
	updateEnvErr  error
	runtimeErr    error
	envFilesErr   error
	extDirsErr    error
	defaultsErr   error
}

func (f *fakeSettingsProvider) GetSettings() *config.Settings {
	if f.settings == nil {
		return &config.Settings{}
	}
	return f.settings
}
func (f *fakeSettingsProvider) UpdateSettings(s *config.Settings) error { f.settings = s; return f.updateErr }
func (f *fakeSettingsProvider) GetEnv() (*config.EnvFile, error)       { return f.env, f.envErr }
func (f *fakeSettingsProvider) UpdateEnv(e *config.EnvFile) error      { f.env = e; return f.updateEnvErr }
func (f *fakeSettingsProvider) GetRuntimesConfig() map[string]string   { return f.runtimes }
func (f *fakeSettingsProvider) UpdateRuntimesConfig(r map[string]string) error {
	f.runtimes = r
	return f.runtimeErr
}
func (f *fakeSettingsProvider) GetEnvFiles() []string        { return f.envFiles }
func (f *fakeSettingsProvider) UpdateEnvFiles(files []string) error {
	f.envFiles = files
	return f.envFilesErr
}
func (f *fakeSettingsProvider) GetExtensionDirs() []string { return f.extDirs }
func (f *fakeSettingsProvider) UpdateExtensionDirs(dirs []string) error {
	f.extDirs = dirs
	return f.extDirsErr
}
func (f *fakeSettingsProvider) GetDefaults() config.DefaultRestart { return f.defaults }
func (f *fakeSettingsProvider) UpdateDefaults(d config.DefaultRestart) error {
	f.defaults = d
	return f.defaultsErr
}

func newSettingsTestServer(t *testing.T, fp *fakeSettingsProvider) *Server {
	t.Helper()
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.settingsProvider = fp
	return server
}

// TestGetSettings 覆盖 handleGetSettings：成功、nil provider 500。
func TestGetSettings(t *testing.T) {
	fp := &fakeSettingsProvider{
		settings: &config.Settings{AuthMode: "always_token", AuthToken: "secret", LogLevel: "info"},
		envFiles: []string{"env/00-base.yaml"},
		extDirs:  []string{"extensions"},
		defaults: config.DefaultRestart{Restart: config.RestartConfig{MaxRetries: 3}},
	}
	server := newSettingsTestServer(t, fp)
	resp := doAPICall(t, server, http.MethodGet, "/api/settings", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/settings: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var out SettingsResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.AuthTokenConfigured {
		t.Errorf("AuthTokenConfigured should be true when AuthToken set")
	}
	if out.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", out.LogLevel)
	}
	if out.Defaults == nil || out.Defaults.Restart.MaxRetries != 3 {
		t.Errorf("Defaults not propagated: %+v", out.Defaults)
	}

	server.settingsProvider = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/settings", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("GET settings nil provider: expected 500, got %d", resp.Code)
	}
}

// TestUpdateSettings 覆盖 handleUpdateSettings：成功、各可选块分支、非法body、provider错误、超大body。
func TestUpdateSettings(t *testing.T) {
	server := newSettingsTestServer(t, &fakeSettingsProvider{})

	body, _ := json.Marshal(SettingsUpdateRequest{
		Settings:      config.Settings{LogLevel: "debug"},
		EnvFiles:      []string{"env/00-base.yaml"},
		ExtensionDirs: []string{"extensions"},
		Defaults:      &config.DefaultRestart{Restart: config.RestartConfig{MaxRetries: 5}},
	})
	resp := doAPICall(t, server, http.MethodPut, "/api/settings", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 缺 EnvFiles/ExtensionDirs/Defaults（全 nil）→ 仅更新 Settings
	body2, _ := json.Marshal(SettingsUpdateRequest{Settings: config.Settings{LogLevel: "warn"}})
	resp = doAPICall(t, server, http.MethodPut, "/api/settings", body2)
	if resp.Code != http.StatusOK {
		t.Errorf("PUT settings partial: expected 200, got %d", resp.Code)
	}

	// 非法 body → 400
	resp = doAPICall(t, server, http.MethodPut, "/api/settings", []byte("not-json"))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("PUT settings bad body: expected 400, got %d", resp.Code)
	}

	// UpdateSettings 错误 → 500
	server.settingsProvider = &fakeSettingsProvider{updateErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodPut, "/api/settings", body)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("PUT settings update err: expected 500, got %d", resp.Code)
	}

	// UpdateEnvFiles 错误 → 500（请求含 env_files）
	server.settingsProvider = &fakeSettingsProvider{envFilesErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodPut, "/api/settings", body)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("PUT settings envFiles err: expected 500, got %d", resp.Code)
	}

	// UpdateExtensionDirs 错误 → 500（请求含 extension_dirs）
	server.settingsProvider = &fakeSettingsProvider{extDirsErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodPut, "/api/settings", body)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("PUT settings extDirs err: expected 500, got %d", resp.Code)
	}

	// UpdateDefaults 错误 → 500（请求含 defaults）
	server.settingsProvider = &fakeSettingsProvider{defaultsErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodPut, "/api/settings", body)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("PUT settings defaults err: expected 500, got %d", resp.Code)
	}

	// 超大 body（>1MB）→ 经 MaxBytesReader 限制后 json 解析失败 → 400
	big := make([]byte, settingsMaxBodyBytes+10)
	copy(big, []byte(`{"log_level":"debug"`))
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(big))
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("PUT settings too large: expected 400, got %d", rec.Code)
	}
}

// TestGetEnv 覆盖 handleGetEnv：成功、错误、nil provider。
func TestGetEnv(t *testing.T) {
	fp := &fakeSettingsProvider{env: &config.EnvFile{Env: map[string]config.EnvVar{"FOO": {Value: "bar"}}}}
	server := newSettingsTestServer(t, fp)
	resp := doAPICall(t, server, http.MethodGet, "/api/settings/env", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET env: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	server.settingsProvider = &fakeSettingsProvider{envErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodGet, "/api/settings/env", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("GET env err: expected 500, got %d", resp.Code)
	}

	server.settingsProvider = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/settings/env", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("GET env nil: expected 500, got %d", resp.Code)
	}
}

// TestUpdateEnv 覆盖 handleUpdateEnv：成功、非法body、provider错误、nil provider。
func TestUpdateEnv(t *testing.T) {
	server := newSettingsTestServer(t, &fakeSettingsProvider{})
	body, _ := json.Marshal(config.EnvFile{Env: map[string]config.EnvVar{"FOO": {Value: "bar"}}})
	resp := doAPICall(t, server, http.MethodPut, "/api/settings/env", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT env: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	resp = doAPICall(t, server, http.MethodPut, "/api/settings/env", []byte("bad"))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("PUT env bad body: expected 400, got %d", resp.Code)
	}

	server.settingsProvider = &fakeSettingsProvider{updateEnvErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodPut, "/api/settings/env", body)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("PUT env err: expected 500, got %d", resp.Code)
	}

	server.settingsProvider = nil
	resp = doAPICall(t, server, http.MethodPut, "/api/settings/env", body)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("PUT env nil: expected 500, got %d", resp.Code)
	}
}

// TestRuntimesConfig 覆盖 handleGetRuntimesConfig / handleUpdateRuntimesConfig。
func TestRuntimesConfig(t *testing.T) {
	fp := &fakeSettingsProvider{runtimes: map[string]string{"node": "/usr/bin/node"}}
	server := newSettingsTestServer(t, fp)
	resp := doAPICall(t, server, http.MethodGet, "/api/settings/runtimes", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET runtimes: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	body, _ := json.Marshal(map[string]string{"node": "/usr/local/bin/node"})
	resp = doAPICall(t, server, http.MethodPut, "/api/settings/runtimes", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT runtimes: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 非法 body → 400
	resp = doAPICall(t, server, http.MethodPut, "/api/settings/runtimes", []byte("bad"))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("PUT runtimes bad body: expected 400, got %d", resp.Code)
	}

	// UpdateRuntimesConfig 错误 → 500
	server.settingsProvider = &fakeSettingsProvider{runtimeErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodPut, "/api/settings/runtimes", body)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("PUT runtimes err: expected 500, got %d", resp.Code)
	}

	server.settingsProvider = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/settings/runtimes", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("GET runtimes nil: expected 500, got %d", resp.Code)
	}
}
