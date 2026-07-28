package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readAll / contains 是测试内便捷封装，避免重复 import 样板
func readAll(r *http.Request) ([]byte, error) { return io.ReadAll(r.Body) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// newCLITestServer 创建 httptest 服务器并临时覆盖 getAPIClient 指向它。
// 默认 /api/health 返回 200，使各命令的 CheckSupdRunning 前置检查通过。
// 调用方按需在该 mux 上注册具体业务端点（需注意 PostJSON/PutJSON/GetJSON 会解码响应体，
// 返回 200 时必须附带合法 JSON 正文，否则客户端报 EOF）。
func newCLITestServer(t *testing.T) (*httptest.Server, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	s := httptest.NewServer(mux)
	orig := getAPIClient
	getAPIClient = func() *APIClient { return NewAPIClient(s.URL, "") }
	t.Cleanup(func() {
		getAPIClient = orig
		s.Close()
	})
	return s, mux
}

// resetCommandGlobals 保存并恢复所有命令层可变全局 flag，避免测试间相互污染。
func resetCommandGlobals(t *testing.T) {
	t.Helper()
	origStartAll := startAll
	origStopAll := stopAll
	origStatusJSON := statusOutputJSON
	origFollow := logsFollow
	origLines := logsLines
	origSince := logsSince
	origExtAction := extAction
	origExtEnv := extEnv
	origExportExt := exportExtension
	origExportOut := exportOutput
	origImportYes := importYes
	origReveal := tokenShowReveal
	t.Cleanup(func() {
		startAll = origStartAll
		stopAll = origStopAll
		statusOutputJSON = origStatusJSON
		logsFollow = origFollow
		logsLines = origLines
		logsSince = origSince
		extAction = origExtAction
		extEnv = origExtEnv
		exportExtension = origExportExt
		exportOutput = origExportOut
		importYes = origImportYes
		tokenShowReveal = origReveal
	})
	startAll = false
	stopAll = false
	statusOutputJSON = false
	logsFollow = false
	logsLines = 100
	logsSince = ""
	extAction = ""
	extEnv = nil
	exportExtension = false
	exportOutput = ""
	importYes = false
	tokenShowReveal = false
}

// ===== start / stop 命令成功路径 =====

func TestRunStart_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/svc1/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST，得到 %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	if err := runStart(nil, []string{"svc1"}); err != nil {
		t.Errorf("runStart 成功路径失败: %v", err)
	}
}

func TestRunStart_NoArgsError(t *testing.T) {
	resetCommandGlobals(t)
	_, _ = newCLITestServer(t)
	// 既不指定服务也不 --all → 返回错误（在 CheckSupdRunning 之后）
	if err := runStart(nil, []string{}); err == nil {
		t.Error("无参数且无 --all 应返回错误")
	}
}

func TestRunStart_AllSuccess(t *testing.T) {
	resetCommandGlobals(t)
	startAll = true
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"services":[{"name":"a"},{"name":"b"}]}`))
	})
	started := map[string]bool{}
	mux.HandleFunc("/api/services/a/start", func(w http.ResponseWriter, r *http.Request) {
		started["a"] = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("/api/services/b/start", func(w http.ResponseWriter, r *http.Request) {
		started["b"] = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})

	if err := runStart(nil, []string{}); err != nil {
		t.Errorf("runStart --all 失败: %v", err)
	}
	if !started["a"] || !started["b"] {
		t.Error("runStart --all 应启动所有服务")
	}
}

func TestRunStop_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/svc1/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST，得到 %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})

	if err := runStop(nil, []string{"svc1"}); err != nil {
		t.Errorf("runStop 成功路径失败: %v", err)
	}
}

func TestRunStop_NoArgsError(t *testing.T) {
	resetCommandGlobals(t)
	_, _ = newCLITestServer(t)
	if err := runStop(nil, []string{}); err == nil {
		t.Error("无参数且无 --all 应返回错误")
	}
}

func TestRunStop_AllSuccess(t *testing.T) {
	resetCommandGlobals(t)
	stopAll = true
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"services":[{"name":"a"},{"name":"b"}]}`))
	})
	stopped := map[string]bool{}
	mux.HandleFunc("/api/services/a/stop", func(w http.ResponseWriter, r *http.Request) {
		stopped["a"] = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("/api/services/b/stop", func(w http.ResponseWriter, r *http.Request) {
		stopped["b"] = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})

	if err := runStop(nil, []string{}); err != nil {
		t.Errorf("runStop --all 失败: %v", err)
	}
	if !stopped["a"] || !stopped["b"] {
		t.Error("runStop --all 应停止所有服务")
	}
}

// ===== restart / signal 命令成功路径 =====

func TestRunRestart_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/svc1/restart", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST，得到 %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})

	if err := runRestart(nil, []string{"svc1"}); err != nil {
		t.Errorf("runRestart 成功路径失败: %v", err)
	}
}

func TestRunSignal_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/svc1/signal", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST，得到 %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := readAll(r)
		if !contains(string(body), `"signal":"SIGTERM"`) {
			t.Errorf("signal body = %s, 期望包含 signal:SIGTERM", body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})

	if err := runSignal(nil, []string{"svc1", "SIGTERM"}); err != nil {
		t.Errorf("runSignal 成功路径失败: %v", err)
	}
}

// ===== status 命令成功路径 =====

func TestRunStatus_SingleSuccess(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/svc1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"svc1","state":"up"}`))
	})

	if err := runStatus(nil, []string{"svc1"}); err != nil {
		t.Errorf("runStatus 单服务失败: %v", err)
	}
}

func TestRunStatus_SingleJSONSuccess(t *testing.T) {
	resetCommandGlobals(t)
	statusOutputJSON = true
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/svc1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"svc1","state":"up"}`))
	})

	if err := runStatus(nil, []string{"svc1"}); err != nil {
		t.Errorf("runStatus --json 失败: %v", err)
	}
}

func TestRunStatus_AllSuccess(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"services":[{"name":"a","state":"up"},{"name":"b","state":"down"}]}`))
	})

	if err := runStatus(nil, []string{}); err != nil {
		t.Errorf("runStatus 全部失败: %v", err)
	}
}

func TestRunStatus_AllEmpty(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"services":[]}`))
	})

	if err := runStatus(nil, []string{}); err != nil {
		t.Errorf("runStatus 空列表失败: %v", err)
	}
}

// ===== logs 命令成功路径 =====

func TestRunLogs_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/svc1/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("lines") != "100" {
			t.Errorf("期望 lines=100, 得到 %s", r.URL.Query().Get("lines"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("line1\nline2\n"))
	})

	if err := runLogs(nil, []string{"svc1"}); err != nil {
		t.Errorf("runLogs 成功路径失败: %v", err)
	}
}

func TestRunLogs_SinceSuccess(t *testing.T) {
	resetCommandGlobals(t)
	logsSince = "1h"
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/svc1/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since") == "" {
			t.Error("期望 since 参数被设置")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("line1\n"))
	})

	if err := runLogs(nil, []string{"svc1"}); err != nil {
		t.Errorf("runLogs --since 失败: %v", err)
	}
}

// ===== ext 命令成功路径 =====

func TestRunExtList_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/extensions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"name":"ext1"},{"name":"ext2"}]`))
	})

	if err := runExtList(nil, []string{}); err != nil {
		t.Errorf("runExtList 成功路径失败: %v", err)
	}
}

func TestRunExtList_Empty(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/extensions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})

	if err := runExtList(nil, []string{}); err != nil {
		t.Errorf("runExtList 空列表失败: %v", err)
	}
}

func TestRunExtShow_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/extensions/ext1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"ext1"}`))
	})

	if err := runExtShow(nil, []string{"ext1"}); err != nil {
		t.Errorf("runExtShow 成功路径失败: %v", err)
	}
}

func TestRunExtRun_Success(t *testing.T) {
	resetCommandGlobals(t)
	extAction = "default"
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/extensions/ext1/run", func(w http.ResponseWriter, r *http.Request) {
		body, _ := readAll(r)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal run body: %v", err)
		}
		if payload["action"] != "default" {
			t.Errorf("action = %#v, want default", payload["action"])
		}
		if _, ok := payload["action_id"]; ok {
			t.Errorf("run body must not contain action_id: %s", body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	if err := runExtRun(nil, []string{"ext1"}); err != nil {
		t.Errorf("runExtRun 成功路径失败: %v", err)
	}
}

func TestRunExtRun_WithEnv(t *testing.T) {
	resetCommandGlobals(t)
	extAction = "default"
	extEnv = []string{"FOO=bar", "BAZ=qux"}
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/extensions/ext1/run", func(w http.ResponseWriter, r *http.Request) {
		body, _ := readAll(r)
		var payload struct {
			Action string            `json:"action"`
			Env    map[string]string `json:"env"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal run body: %v", err)
		}
		if payload.Action != "default" {
			t.Errorf("action = %q, want default", payload.Action)
		}
		if payload.Env["FOO"] != "bar" || payload.Env["BAZ"] != "qux" {
			t.Errorf("env = %#v, want FOO=bar and BAZ=qux", payload.Env)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("unmarshal raw run body: %v", err)
		}
		if _, ok := raw["action_id"]; ok {
			t.Errorf("run body must not contain action_id: %s", body)
		}
		if _, ok := raw["env_overrides"]; ok {
			t.Errorf("run body must not contain env_overrides: %s", body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	if err := runExtRun(nil, []string{"ext1"}); err != nil {
		t.Errorf("runExtRun --env 失败: %v", err)
	}
}

func TestRunExtStatus_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/extensions/ext1/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"ext1","running":false}`))
	})

	if err := runExtStatus(nil, []string{"ext1"}); err != nil {
		t.Errorf("runExtStatus 成功路径失败: %v", err)
	}
}

// ===== runtimes 命令成功路径 =====

func TestRunRuntimesList_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/runtimes", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"runtimes":[{"alias":"bun","path":"/usr/bin/bun","source":"config","available":true}],"default":"bun"}`))
	})

	if err := runRuntimesList(nil, []string{}); err != nil {
		t.Errorf("runRuntimesList 成功路径失败: %v", err)
	}
}

func TestRunRuntimesList_Empty(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/runtimes", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"runtimes":[],"default":""}`))
	})

	if err := runRuntimesList(nil, []string{}); err != nil {
		t.Errorf("runRuntimesList 空列表失败: %v", err)
	}
}

func TestRunRuntimesInstall_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"runtimes":{"node":"/usr/bin/node"}}`))
	})
	var gotBody string
	mux.HandleFunc("/api/settings/runtimes", func(w http.ResponseWriter, r *http.Request) {
		b, _ := readAll(r)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	if err := runRuntimesInstall(nil, []string{"bun", "/usr/local/bin/bun"}); err != nil {
		t.Errorf("runRuntimesInstall 成功路径失败: %v", err)
	}
	if !contains(gotBody, `"bun":"/usr/local/bin/bun"`) || !contains(gotBody, `"node":"/usr/bin/node"`) {
		t.Errorf("runtimes 安装 body 未合并既有运行时: %s", gotBody)
	}
}

func TestRunRuntimesRemove_Success(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"runtimes":{"node":"/usr/bin/node","bun":"/usr/local/bin/bun"}}`))
	})
	var gotBody string
	mux.HandleFunc("/api/settings/runtimes", func(w http.ResponseWriter, r *http.Request) {
		b, _ := readAll(r)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	if err := runRuntimesRemove(nil, []string{"bun"}); err != nil {
		t.Errorf("runRuntimesRemove 成功路径失败: %v", err)
	}
	if contains(gotBody, `"bun"`) {
		t.Errorf("移除后 body 不应含 bun: %s", gotBody)
	}
	if !contains(gotBody, `"node":"/usr/bin/node"`) {
		t.Errorf("移除后 body 应保留 node: %s", gotBody)
	}
}

// ===== export / import 命令成功路径 =====

func TestRunExport_ServiceSuccess(t *testing.T) {
	resetCommandGlobals(t)
	tmp := t.TempDir()
	exportOutput = filepath.Join(tmp, "sub", "svc1.yaml")
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/svc1/export", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("name: svc1\n"))
	})

	if err := runExport(nil, []string{"svc1"}); err != nil {
		t.Errorf("runExport 服务成功路径失败: %v", err)
	}
	data, err := os.ReadFile(exportOutput)
	if err != nil {
		t.Fatalf("导出文件未写入: %v", err)
	}
	if string(data) != "name: svc1\n" {
		t.Errorf("导出文件内容 = %q", data)
	}
}

func TestRunExport_ExtensionSuccess(t *testing.T) {
	resetCommandGlobals(t)
	exportExtension = true
	tmp := t.TempDir()
	exportOutput = filepath.Join(tmp, "ext1.yaml")
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/extensions/ext1/export", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("name: ext1\n"))
	})

	if err := runExport(nil, []string{"ext1"}); err != nil {
		t.Errorf("runExport 扩展成功路径失败: %v", err)
	}
}

func TestRunExport_NoOutputError(t *testing.T) {
	resetCommandGlobals(t)
	_, _ = newCLITestServer(t)
	if err := runExport(nil, []string{"svc1"}); err == nil {
		t.Error("未指定 -o 应返回错误")
	}
}

func TestRunImport_Success(t *testing.T) {
	resetCommandGlobals(t)
	tmp := t.TempDir()
	src := filepath.Join(tmp, "svc.yaml")
	if err := os.WriteFile(src, []byte("name: svc1\n"), 0644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/import", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/api/services/import/confirm", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	// 不带 --yes：仍应继续到 confirm
	if err := runImport(nil, []string{src}); err != nil {
		t.Errorf("runImport 成功路径失败: %v", err)
	}
}

func TestRunImport_FileNotFound(t *testing.T) {
	resetCommandGlobals(t)
	_, _ = newCLITestServer(t)
	if err := runImport(nil, []string{"/nonexistent/file.yaml"}); err == nil {
		t.Error("源文件不存在应返回错误")
	}
}

func TestRunImport_WithYes(t *testing.T) {
	resetCommandGlobals(t)
	importYes = true
	tmp := t.TempDir()
	src := filepath.Join(tmp, "svc.yaml")
	if err := os.WriteFile(src, []byte("name: svc1\n"), 0644); err != nil {
		t.Fatalf("写源文件失败: %v", err)
	}
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/services/import", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/api/services/import/confirm", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	if err := runImport(nil, []string{src}); err != nil {
		t.Errorf("runImport --yes 失败: %v", err)
	}
}

// ===== token verify 命令成功路径 =====

func TestRunTokenVerify_Valid(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"valid":true}`))
	})

	if err := runTokenVerify(nil, []string{"sometoken"}); err != nil {
		t.Errorf("runTokenVerify 有效路径应成功: %v", err)
	}
}

func TestRunTokenVerify_Invalid(t *testing.T) {
	resetCommandGlobals(t)
	_, mux := newCLITestServer(t)
	mux.HandleFunc("/api/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"valid":false}`))
	})

	if err := runTokenVerify(nil, []string{"badtoken"}); err == nil {
		t.Error("token 无效应返回错误")
	}
}

// ===== 纯函数 maskToken =====

func TestMaskToken(t *testing.T) {
	if got := maskToken("abcdefghijklmnop"); got != "abc*********mnop" {
		t.Errorf("maskToken(长) = %q", got)
	}
	if got := maskToken("short"); got != "*****" {
		t.Errorf("maskToken(短) = %q, want 5 个 *", got)
	}
}
