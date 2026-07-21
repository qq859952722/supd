package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// withUnconnectableClient 临时覆盖 getAPIClient 为不可连接地址，用于 *WithoutSupd 类测试。
// L-04-003 修复：原测试依赖 localhost:8080 不可连接的假设，当真实 supd 运行时测试会失败。
// 改为显式使用 127.0.0.1:1（不可连接端口）确保测试可重复。
func withUnconnectableClient(t *testing.T) {
	t.Helper()
	orig := getAPIClient
	t.Cleanup(func() { getAPIClient = orig })
	getAPIClient = func() *APIClient { return NewAPIClient("http://127.0.0.1:1", "") }
}

// TestRunStart_NoArgsReturnsError 测试 runStart 无参数且无 --all 时返回错误
func TestRunStart_NoArgsReturnsError(t *testing.T) {
	withUnconnectableClient(t)
	// 保存原始状态
	origStartAll := startAll
	defer func() { startAll = origStartAll }()
	startAll = false

	err := runStart(nil, []string{})
	if err == nil {
		t.Errorf("无参数且 supd 未运行时应返回错误")
	}
}

// TestRunStart_AllFlagWithoutSupd 测试 --all 标志在 supd 未运行时返回错误
func TestRunStart_AllFlagWithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	origStartAll := startAll
	defer func() { startAll = origStartAll }()
	startAll = true

	err := runStart(nil, []string{})
	if err == nil {
		t.Errorf("--all 但 supd 未运行时应返回错误")
	}
}

// TestRunStop_NoArgsReturnsError 测试 runStop 无参数且无 --all 时返回错误
func TestRunStop_NoArgsReturnsError(t *testing.T) {
	withUnconnectableClient(t)
	origStopAll := stopAll
	defer func() { stopAll = origStopAll }()
	stopAll = false

	err := runStop(nil, []string{})
	if err == nil {
		t.Errorf("无参数且 supd 未运行时应返回错误")
	}
}

// TestRunStop_AllFlagWithoutSupd 测试 --all 标志在 supd 未运行时返回错误
func TestRunStop_AllFlagWithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	origStopAll := stopAll
	defer func() { stopAll = origStopAll }()
	stopAll = true

	err := runStop(nil, []string{})
	if err == nil {
		t.Errorf("--all 但 supd 未运行时应返回错误")
	}
}

// TestRunRestart_WithoutSupd 测试 runRestart 在 supd 未运行时返回错误
func TestRunRestart_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runRestart(nil, []string{"svc1"})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestStartService_Success 测试 startService 成功调用 API
func TestStartService_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services/svc1/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST，得到 %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := startService(client, "svc1"); err != nil {
		t.Errorf("startService 失败: %v", err)
	}
}

// TestStartService_APIError 测试 startService 处理后端错误
func TestStartService_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services/svc1/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "SERVICE_RUNNING",
				"message": "服务运行中，无法执行此操作",
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := startService(client, "svc1")
	if err == nil {
		t.Errorf("API 错误应返回 error")
	}
}

// TestStartService_HTTPError 测试 startService 处理非 200/201 状态码
func TestStartService_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services/svc1/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"服务器内部错误"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := startService(client, "svc1")
	if err == nil {
		t.Errorf("500 错误应返回 error")
	}
}

// TestStartService_NetworkError 测试 startService 处理网络错误
func TestStartStartService_NetworkError(t *testing.T) {
	// 使用一个不可达的地址
	client := NewAPIClient("http://localhost:1", "")
	err := startService(client, "svc1")
	if err == nil {
		t.Errorf("网络错误应返回 error")
	}
}

// TestStopService_Success 测试 stopService 成功调用 API
func TestStopService_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services/svc1/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST，得到 %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := stopService(client, "svc1"); err != nil {
		t.Errorf("stopService 失败: %v", err)
	}
}

// TestStopService_APIError 测试 stopService 处理后端错误
func TestStopService_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services/svc1/stop", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "SERVICE_NOT_FOUND",
				"message": "服务不存在",
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := stopService(client, "svc1")
	if err == nil {
		t.Errorf("API 错误应返回 error")
	}
}

// TestStartAllServices_Success 测试 startAllServices 启动所有服务
func TestStartAllServices_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{
				{"name": "svc1"},
				{"name": "svc2"},
				{"name": "svc3"},
			},
		})
	})
	startedCount := 0
	mux.HandleFunc("/api/services/svc1/start", func(w http.ResponseWriter, r *http.Request) {
		startedCount++
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	})
	mux.HandleFunc("/api/services/svc2/start", func(w http.ResponseWriter, r *http.Request) {
		startedCount++
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	})
	// svc3 返回错误
	mux.HandleFunc("/api/services/svc3/start", func(w http.ResponseWriter, r *http.Request) {
		startedCount++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"服务器内部错误"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	// startAllServices 在单个服务失败时不应返回错误（继续启动其他服务）
	if err := startAllServices(client); err != nil {
		t.Errorf("startAllServices 不应返回错误: %v", err)
	}
	if startedCount != 3 {
		t.Errorf("应尝试启动 3 个服务，实际 %d", startedCount)
	}
}

// TestStartAllServices_EmptyList 测试空服务列表
func TestStartAllServices_EmptyList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := startAllServices(client); err != nil {
		t.Errorf("空列表不应返回错误: %v", err)
	}
}

// TestStartAllServices_ListError 测试获取服务列表失败
func TestStartAllServices_ListError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"服务器内部错误"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := startAllServices(client)
	if err == nil {
		t.Errorf("获取服务列表失败应返回错误")
	}
}

// TestStartAllServices_SkipsEmptyName 测试服务列表中空 name 被跳过
func TestStartAllServices_SkipsEmptyName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{
				{"name": ""}, // 空 name 应跳过
				{"name": "svc1"},
			},
		})
	})
	mux.HandleFunc("/api/services/svc1/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := startAllServices(client); err != nil {
		t.Errorf("startAllServices 失败: %v", err)
	}
}

// TestStopAllServices_Success 测试 stopAllServices 停止所有服务
func TestStopAllServices_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{
				{"name": "svc1"},
				{"name": "svc2"},
			},
		})
	})
	stoppedCount := 0
	mux.HandleFunc("/api/services/svc1/stop", func(w http.ResponseWriter, r *http.Request) {
		stoppedCount++
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	})
	mux.HandleFunc("/api/services/svc2/stop", func(w http.ResponseWriter, r *http.Request) {
		stoppedCount++
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := stopAllServices(client); err != nil {
		t.Errorf("stopAllServices 失败: %v", err)
	}
	if stoppedCount != 2 {
		t.Errorf("应停止 2 个服务，实际 %d", stoppedCount)
	}
}

// TestStopAllServices_ListError 测试获取服务列表失败
func TestStopAllServices_ListError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := stopAllServices(client)
	if err == nil {
		t.Errorf("获取服务列表失败应返回错误")
	}
}

// TestStopAllServices_PartialFailure 测试部分服务停止失败时不影响其他
func TestStopAllServices_PartialFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{
				{"name": "svc1"},
				{"name": "svc2"}, // 这个会失败
			},
		})
	})
	mux.HandleFunc("/api/services/svc1/stop", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	})
	mux.HandleFunc("/api/services/svc2/stop", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"服务器内部错误"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	// 部分失败时不应返回错误（继续停止其他服务）
	if err := stopAllServices(client); err != nil {
		t.Errorf("部分失败不应返回错误: %v", err)
	}
}

// TestParseAPIError_WithKnownCode 测试 parseAPIError 处理已知错误码（应返回中文消息）
func TestParseAPIError_WithKnownCode(t *testing.T) {
	body := []byte(`{"error":{"code":"SERVICE_NOT_FOUND","message":"service not found"}}`)
	err := parseAPIError(http.StatusNotFound, body)
	if err == nil {
		t.Errorf("应返回 error")
	}
	// SERVICE_NOT_FOUND 应映射到中文消息
	if err.Error() != "服务不存在" {
		t.Errorf("错误消息 = %q, want '服务不存在'", err.Error())
	}
}

// TestParseAPIError_WithUnknownCode 测试 parseAPIError 处理未知错误码（应回退到 message）
func TestParseAPIError_WithUnknownCode(t *testing.T) {
	body := []byte(`{"error":{"code":"UNKNOWN_CODE","message":"custom error message"}}`)
	err := parseAPIError(http.StatusBadRequest, body)
	if err == nil {
		t.Errorf("应返回 error")
	}
	if err.Error() != "custom error message" {
		t.Errorf("错误消息 = %q, want 'custom error message'", err.Error())
	}
}

// TestParseAPIError_WithCodeNoMessage 测试 parseAPIError 错误码无 message 时回退到错误码
func TestParseAPIError_WithCodeNoMessage(t *testing.T) {
	body := []byte(`{"error":{"code":"UNKNOWN_CODE"}}`)
	err := parseAPIError(http.StatusBadRequest, body)
	if err == nil {
		t.Errorf("应返回 error")
	}
	expected := "请求失败（错误码: UNKNOWN_CODE）"
	if err.Error() != expected {
		t.Errorf("错误消息 = %q, want %q", err.Error(), expected)
	}
}

// TestParseAPIError_InvalidJSON 测试 parseAPIError 处理无效 JSON
func TestParseAPIError_InvalidJSON(t *testing.T) {
	body := []byte(`not a json`)
	err := parseAPIError(http.StatusInternalServerError, body)
	if err == nil {
		t.Errorf("应返回 error")
	}
	expected := fmt.Sprintf("请求失败（HTTP %d）", http.StatusInternalServerError)
	if err.Error() != expected {
		t.Errorf("错误消息 = %q, want %q", err.Error(), expected)
	}
}

// TestCliErrorMessage_KnownCodes 测试所有已知错误码都有中文映射
func TestCliErrorMessage_KnownCodes(t *testing.T) {
	knownCodes := []string{
		"AUTH_REQUIRED", "AUTH_INVALID",
		"SERVICE_NOT_FOUND", "SERVICE_EXISTS", "SERVICE_RUNNING", "SERVICE_BUSY",
		"DEPENDENCY_CYCLE", "DEPENDENCY_MISSING",
		"SERVICE_CONFIG_INVALID",
		"RUNTIME_NOT_FOUND", "RUNTIME_NOT_EXECUTABLE", "RUNTIME_USER_NOT_FOUND",
		"EXTENSION_NOT_FOUND", "EXTENSION_FAILED",
		"RUN_NOT_FOUND", "RUN_ALREADY_DONE",
		"FILE_NOT_FOUND", "FILE_PERMISSION", "FILE_TOO_LARGE", "FILE_ACCESS_DENIED",
		"INVALID_REQUEST", "INTERNAL_ERROR",
	}
	for _, code := range knownCodes {
		msg := cliErrorMessage(code)
		if msg == "" {
			t.Errorf("错误码 %s 缺少中文映射", code)
		}
	}
}

// TestCliErrorMessage_UnknownCode 测试未知错误码返回空字符串
func TestCliErrorMessage_UnknownCode(t *testing.T) {
	msg := cliErrorMessage("UNKNOWN_CODE_XYZ")
	if msg != "" {
		t.Errorf("未知错误码应返回空字符串，got %q", msg)
	}
}

// TestCheckSupdRunning_Success 测试 CheckSupdRunning 在 supd 运行时返回 nil
func TestCheckSupdRunning_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := client.CheckSupdRunning(); err != nil {
		t.Errorf("supd 运行时不应返回错误: %v", err)
	}
}

// TestCheckSupdRunning_NotOKStatus 测试 CheckSupdRunning 在非 200 状态时返回错误
func TestCheckSupdRunning_NotOKStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := client.CheckSupdRunning()
	if err == nil {
		t.Errorf("非 200 状态应返回错误")
	}
	if err.Error() != "supd 未运行" {
		t.Errorf("错误消息 = %q, want 'supd 未运行'", err.Error())
	}
}

// TestAPIClient_PostJSON_Success 测试 PostJSON 成功路径
func TestAPIClient_PostJSON_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST，得到 %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"result": "created"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	var result map[string]string
	if err := client.PostJSON("/api/test", map[string]string{"key": "value"}, &result); err != nil {
		t.Errorf("PostJSON 失败: %v", err)
	}
	if result["result"] != "created" {
		t.Errorf("result = %v, want {result: created}", result)
	}
}

// TestAPIClient_PostJSON_ServiceUnavailable 测试 PostJSON 处理 503 状态
func TestAPIClient_PostJSON_ServiceUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := client.PostJSON("/api/test", nil, nil)
	if err == nil {
		t.Errorf("503 应返回错误")
	}
	if err.Error() != "supd 未运行" {
		t.Errorf("错误消息 = %q, want 'supd 未运行'", err.Error())
	}
}

// TestAPIClient_GetJSON_ServiceUnavailable 测试 GetJSON 处理 503 状态
func TestAPIClient_GetJSON_ServiceUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := client.GetJSON("/api/test", nil)
	if err == nil {
		t.Errorf("503 应返回错误")
	}
	if err.Error() != "supd 未运行" {
		t.Errorf("错误消息 = %q, want 'supd 未运行'", err.Error())
	}
}

// TestAPIClient_PostJSON_NetworkError 测试 PostJSON 网络错误
func TestAPIClient_PostJSON_NetworkError(t *testing.T) {
	client := NewAPIClient("http://localhost:1", "")
	err := client.PostJSON("/api/test", nil, nil)
	if err == nil {
		t.Errorf("网络错误应返回 error")
	}
}

// TestAPIClient_DeleteMethod 测试 Post/Get/Put 方法在 DELETE 端点上的行为
// 补充 client.go 中各 HTTP method 的覆盖
func TestAPIClient_Put_JSONError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":"INVALID_REQUEST","message":"请求参数错误"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := client.PutJSON("/api/test", map[string]string{"k": "v"}, nil)
	if err == nil {
		t.Errorf("400 应返回错误")
	}
	if err.Error() != "请求参数错误" {
		t.Errorf("错误消息 = %q, want '请求参数错误'", err.Error())
	}
}

// ===== runStatus 测试 =====

// TestRunStatus_WithoutSupd 测试 runStatus 在 supd 未运行时返回错误
func TestRunStatus_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runStatus(nil, []string{})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestShowServiceStatus_Success 测试 showServiceStatus 成功
func TestShowServiceStatus_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services/svc1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"name":  "svc1",
			"state": "up",
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := showServiceStatus(client, "svc1"); err != nil {
		t.Errorf("showServiceStatus 失败: %v", err)
	}
}

// TestShowServiceStatus_NotFound 测试 showServiceStatus 处理 404
func TestShowServiceStatus_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services/svc1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"SERVICE_NOT_FOUND","message":"服务不存在"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := showServiceStatus(client, "svc1")
	if err == nil {
		t.Errorf("404 应返回错误")
	}
}

// TestShowAllServicesStatus_Success 测试 showAllServicesStatus 成功
func TestShowAllServicesStatus_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{
				{"name": "svc1", "state": "up"},
				{"name": "svc2", "state": "down"},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := showAllServicesStatus(client); err != nil {
		t.Errorf("showAllServicesStatus 失败: %v", err)
	}
}

// TestShowAllServicesStatus_EmptyList 测试 showAllServicesStatus 空列表
func TestShowAllServicesStatus_EmptyList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := showAllServicesStatus(client); err != nil {
		t.Errorf("空列表不应返回错误: %v", err)
	}
}

// TestShowAllServicesStatus_ListError 测试 showAllServicesStatus 获取列表失败
func TestShowAllServicesStatus_ListError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"服务器内部错误"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := showAllServicesStatus(client)
	if err == nil {
		t.Errorf("获取列表失败应返回错误")
	}
}

// TestPrintServiceStatus 测试 printServiceStatus 输出
func TestPrintServiceStatus(t *testing.T) {
	// 仅验证不 panic 即可
	svc := map[string]any{
		"name":  "test-svc",
		"state": "running",
	}
	printServiceStatus(svc)

	// 空 map 也不应 panic
	emptySvc := map[string]any{}
	printServiceStatus(emptySvc)
}

// ===== runReload 测试 =====

// TestRunReload_WithoutSupd 测试 runReload 在 supd 未运行时返回错误
// L-04-003 修复：使用不可连接地址避免环境依赖
func TestRunReload_WithoutSupd(t *testing.T) {
	origGetAPIClient := getAPIClient
	defer func() { getAPIClient = origGetAPIClient }()
	getAPIClient = func() *APIClient { return NewAPIClient("http://127.0.0.1:1", "") }

	err := runReload(nil, []string{})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestRunReload_Success 测试 runReload 成功调用 POST /api/reload
// P-02-001 修复后：reload 调用 POST /api/reload（而非错误的 POST /api/settings）
func TestRunReload_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST，得到 %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"status":            "ok",
			"services":          5,
			"global_extensions": 3,
			"scan_errors":       0,
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	origGetAPIClient := getAPIClient
	defer func() { getAPIClient = origGetAPIClient }()
	getAPIClient = func() *APIClient { return NewAPIClient(server.URL, "") }

	if err := runReload(nil, []string{}); err != nil {
		t.Errorf("runReload 失败: %v", err)
	}
}

// TestRunReload_Partial 测试 runReload 部分成功（scan_errors > 0）
func TestRunReload_Partial(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/reload", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"status":            "partial",
			"services":          4,
			"global_extensions": 3,
			"scan_errors":       1,
			"error_details": []map[string]string{
				{"path": "services/broken/service.yaml", "message": "yaml: invalid"},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	origGetAPIClient := getAPIClient
	defer func() { getAPIClient = origGetAPIClient }()
	getAPIClient = func() *APIClient { return NewAPIClient(server.URL, "") }

	if err := runReload(nil, []string{}); err != nil {
		t.Errorf("partial 状态不应返回 error: %v", err)
	}
}

// TestRunReload_APIError 测试 runReload 处理 API 错误
func TestRunReload_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/reload", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"服务器内部错误"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	origGetAPIClient := getAPIClient
	defer func() { getAPIClient = origGetAPIClient }()
	getAPIClient = func() *APIClient { return NewAPIClient(server.URL, "") }

	err := runReload(nil, []string{})
	if err == nil {
		t.Errorf("API 错误应返回 error")
	}
}

// ===== runSignal 测试 =====

// TestRunSignal_WithoutSupd 测试 runSignal 在 supd 未运行时返回错误
func TestRunSignal_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runSignal(nil, []string{"svc1", "SIGTERM"})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// ===== runTokenGenerate/Show/Verify 测试 =====

// TestRunTokenGenerate_Success 测试 runTokenGenerate 成功生成 token
func TestRunTokenGenerate_Success(t *testing.T) {
	// runTokenGenerate 不依赖 supd 运行，应直接成功
	if err := runTokenGenerate(nil, []string{}); err != nil {
		t.Errorf("runTokenGenerate 失败: %v", err)
	}
}

// TestRunTokenShow_FileNotFound 测试 runTokenShow 文件不存在时返回错误
func TestRunTokenShow_FileNotFound(t *testing.T) {
	origCfgPath := cfgPath
	defer func() { cfgPath = origCfgPath }()
	cfgPath = "/nonexistent/path/to/config.yaml"

	err := runTokenShow(nil, []string{})
	if err == nil {
		t.Errorf("文件不存在时应返回错误")
	}
}

// TestRunTokenShow_Success 测试 runTokenShow 成功显示 token
func TestRunTokenShow_Success(t *testing.T) {
	origCfgPath := cfgPath
	defer func() { cfgPath = origCfgPath }()

	tmpDir := t.TempDir()
	cfgPath = tmpDir + "/config.yaml"
	content := `settings:
  http_listen: ":8080"
  auth_mode: "local_skip"
  auth_token: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	if err := runTokenShow(nil, []string{}); err != nil {
		t.Errorf("runTokenShow 失败: %v", err)
	}
}

// TestRunTokenShow_NoToken 测试 runTokenShow 找不到 token 时返回错误
func TestRunTokenShow_NoToken(t *testing.T) {
	origCfgPath := cfgPath
	defer func() { cfgPath = origCfgPath }()

	tmpDir := t.TempDir()
	cfgPath = tmpDir + "/config.yaml"
	content := `settings:
  http_listen: ":8080"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	err := runTokenShow(nil, []string{})
	if err == nil {
		t.Errorf("找不到 token 应返回错误")
	}
}

// TestRunTokenVerify_WithoutSupd 测试 runTokenVerify 在 supd 未运行时返回错误
func TestRunTokenVerify_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runTokenVerify(nil, []string{"some-token"})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// ===== runVersion 测试 =====

// TestRunVersion_Success 测试 runVersion 输出版本信息
func TestRunVersion_Success(t *testing.T) {
	origVersion := Version
	origBuildTime := BuildTime
	defer func() {
		Version = origVersion
		BuildTime = origBuildTime
	}()
	Version = "v1.2.3"
	BuildTime = "2026-07-19"

	if err := runVersion(nil, []string{}); err != nil {
		t.Errorf("runVersion 失败: %v", err)
	}
}

// ===== runExtList/Show/Status/Run 测试 =====

// TestRunExtList_WithoutSupd 测试 runExtList 在 supd 未运行时返回错误
func TestRunExtList_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runExtList(nil, []string{})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestRunExtShow_WithoutSupd 测试 runExtShow 在 supd 未运行时返回错误
func TestRunExtShow_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runExtShow(nil, []string{"ext1"})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestRunExtRun_WithoutSupd 测试 runExtRun 在 supd 未运行时返回错误
func TestRunExtRun_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	origAction := extAction
	defer func() { extAction = origAction }()
	extAction = "default"

	err := runExtRun(nil, []string{"ext1"})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestRunExtRun_NoAction 测试 runExtRun 未指定 --action 时返回错误
func TestRunExtRun_NoAction(t *testing.T) {
	origAction := extAction
	defer func() { extAction = origAction }()
	extAction = ""

	err := runExtRun(nil, []string{"ext1"})
	if err == nil {
		t.Errorf("应返回错误")
	}
}

// TestRunExtStatus_WithoutSupd 测试 runExtStatus 在 supd 未运行时返回错误
func TestRunExtStatus_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runExtStatus(nil, []string{"ext1"})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestSplitEnvVar_Comprehensive 测试 splitEnvVar 各种输入
func TestSplitEnvVar_Comprehensive(t *testing.T) {
	tests := []struct {
		input   string
		wantLen int
		wantKey string
		wantVal string
	}{
		{"KEY=value", 2, "KEY", "value"},
		{"KEY=", 2, "KEY", ""},
		{"KEY", 1, "KEY", ""},
		{"PATH=/usr/bin:/bin", 2, "PATH", "/usr/bin:/bin"},
		{"=value", 2, "", "value"},
		{"A=B=C=D", 2, "A", "B=C=D"},
		{"", 1, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parts := splitEnvVar(tt.input)
			if len(parts) != tt.wantLen {
				t.Errorf("splitEnvVar(%q) len = %d, want %d", tt.input, len(parts), tt.wantLen)
				return
			}
			if tt.wantLen >= 1 && parts[0] != tt.wantKey {
				t.Errorf("splitEnvVar(%q) key = %q, want %q", tt.input, parts[0], tt.wantKey)
			}
			if tt.wantLen >= 2 && parts[1] != tt.wantVal {
				t.Errorf("splitEnvVar(%q) value = %q, want %q", tt.input, parts[1], tt.wantVal)
			}
		})
	}
}

// ===== runRuntimes 测试 =====

// TestRunRuntimesList_WithoutSupd 测试 runRuntimesList 在 supd 未运行时返回错误
func TestRunRuntimesList_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runRuntimesList(nil, []string{})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestRunRuntimesInstall_WithoutSupd 测试 runRuntimesInstall 在 supd 未运行时返回错误
// 注：cobra 在 CLI 层通过 exactArgs(2) 校验参数，函数内部直接访问 args[0]/args[1]；
// 因此测试需传入合法的 2 个参数（绝对路径），让其进入 CheckSupdRunning 错误路径。
func TestRunRuntimesInstall_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runRuntimesInstall(nil, []string{"bun", "/usr/local/bin/bun"})
	if err == nil {
		t.Errorf("应返回错误")
	}
}

// TestRunRuntimesInstall_InvalidPath 测试 runRuntimesInstall 非绝对路径返回错误
func TestRunRuntimesInstall_InvalidPath(t *testing.T) {
	err := runRuntimesInstall(nil, []string{"bun", "relative/path"})
	if err == nil {
		t.Errorf("非绝对路径应返回错误")
	}
}

// TestRunRuntimesRemove_WithoutSupd 测试 runRuntimesRemove 在 supd 未运行时返回错误
// 注：cobra 在 CLI 层通过 exactArgs(1) 校验参数，函数内部直接访问 args[0]；
// 因此测试需传入合法的 1 个参数，让其进入 CheckSupdRunning 错误路径。
func TestRunRuntimesRemove_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runRuntimesRemove(nil, []string{"bun"})
	if err == nil {
		t.Errorf("应返回错误")
	}
}

// ===== runLogs/fetchLogs 测试 =====

// TestRunLogs_WithoutSupd 测试 runLogs 在 supd 未运行时返回错误
func TestRunLogs_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	origFollow := logsFollow
	origLines := logsLines
	origSince := logsSince
	defer func() {
		logsFollow = origFollow
		logsLines = origLines
		logsSince = origSince
	}()
	logsFollow = false
	logsLines = 100
	logsSince = ""

	err := runLogs(nil, []string{"svc1"})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestFetchLogs_Success 测试 fetchLogs 成功获取日志
func TestFetchLogs_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services/svc1/logs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("line1\nline2\nline3\n"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	if err := fetchLogs(client, "/api/services/svc1/logs?lines=100"); err != nil {
		t.Errorf("fetchLogs 失败: %v", err)
	}
}

// TestFetchLogs_APIError 测试 fetchLogs 处理 API 错误
func TestFetchLogs_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services/svc1/logs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"SERVICE_NOT_FOUND","message":"服务不存在"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	err := fetchLogs(client, "/api/services/svc1/logs?lines=100")
	if err == nil {
		t.Errorf("404 应返回错误")
	}
}

// ===== runExport/runImport 测试 =====

// TestRunExport_WithoutSupd 测试 runExport 在 supd 未运行时返回错误
func TestRunExport_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runExport(nil, []string{"svc1"})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestRunImport_WithoutSupd 测试 runImport 在 supd 未运行时返回错误
func TestRunImport_WithoutSupd(t *testing.T) {
	withUnconnectableClient(t)
	err := runImport(nil, []string{"ext1"})
	if err == nil {
		t.Errorf("supd 未运行时应返回错误")
	}
}

// TestParseDuration_AdditionalCases 测试 parseDuration 更多场景
func TestParseDuration_AdditionalCases(t *testing.T) {
	tests := []struct {
		input    string
		wantErr  bool
		wantStr  string
	}{
		{"1h", false, "1h0m0s"},
		{"30m", false, "30m0s"},
		{"60s", false, "1m0s"},
		{"", true, ""},
		{"  ", true, ""},
		{"1x", true, ""}, // 不支持的后缀，time.ParseDuration 会失败
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := parseDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseDuration(%q) 应返回错误", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseDuration(%q) 不应返回错误: %v", tt.input, err)
				}
				if d.String() != tt.wantStr {
					t.Errorf("parseDuration(%q) = %v, want %v", tt.input, d.String(), tt.wantStr)
				}
			}
		})
	}
}

// TestSetAuth_TokenPresent 测试 setAuth 在有 token 时设置 Authorization 头
func TestSetAuth_TokenPresent(t *testing.T) {
	client := NewAPIClient("http://localhost", "my-secret-token")
	req, err := http.NewRequest("GET", "http://localhost/test", nil)
	if err != nil {
		t.Fatalf("NewRequest 失败: %v", err)
	}
	client.setAuth(req)
	auth := req.Header.Get("Authorization")
	if auth != "Bearer my-secret-token" {
		t.Errorf("Authorization = %q, want 'Bearer my-secret-token'", auth)
	}
}

// TestSetAuth_NoToken 测试 setAuth 在无 token 时不设置 Authorization 头
func TestSetAuth_NoToken(t *testing.T) {
	client := NewAPIClient("http://localhost", "")
	req, err := http.NewRequest("GET", "http://localhost/test", nil)
	if err != nil {
		t.Fatalf("NewRequest 失败: %v", err)
	}
	client.setAuth(req)
	auth := req.Header.Get("Authorization")
	if auth != "" {
		t.Errorf("无 token 时 Authorization 应为空，got %q", auth)
	}
}

// TestAPIClient_Get_NetworkError 测试 Get 网络错误
func TestAPIClient_Get_NetworkError(t *testing.T) {
	client := NewAPIClient("http://localhost:1", "")
	_, err := client.Get("/api/test")
	if err == nil {
		t.Errorf("网络错误应返回 error")
	}
}

// TestAPIClient_Post_NetworkError 测试 Post 网络错误
func TestAPIClient_Post_NetworkError(t *testing.T) {
	client := NewAPIClient("http://localhost:1", "")
	_, err := client.Post("/api/test", map[string]string{"k": "v"})
	if err == nil {
		t.Errorf("网络错误应返回 error")
	}
}

// TestAPIClient_Put_NetworkError 测试 Put 网络错误
func TestAPIClient_Put_NetworkError(t *testing.T) {
	client := NewAPIClient("http://localhost:1", "")
	_, err := client.Put("/api/test", map[string]string{"k": "v"})
	if err == nil {
		t.Errorf("网络错误应返回 error")
	}
}

// TestAPIClient_PostJSON_DecodeError 测试 PostJSON 响应体非 JSON 时返回错误
func TestAPIClient_PostJSON_DecodeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not a json"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	var result map[string]string
	err := client.PostJSON("/api/test", nil, &result)
	if err == nil {
		t.Errorf("非 JSON 响应应返回错误")
	}
}

// TestAPIClient_GetJSON_DecodeError 测试 GetJSON 响应体非 JSON 时返回错误
func TestAPIClient_GetJSON_DecodeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not a json"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	var result map[string]string
	err := client.GetJSON("/api/test", &result)
	if err == nil {
		t.Errorf("非 JSON 响应应返回错误")
	}
}

// TestAPIClient_PutJSON_DecodeError 测试 PutJSON 响应体非 JSON 时返回错误
func TestAPIClient_PutJSON_DecodeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not a json"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	var result map[string]string
	err := client.PutJSON("/api/test", map[string]string{"k": "v"}, &result)
	if err == nil {
		t.Errorf("非 JSON 响应应返回错误")
	}
}

// TestAPIClient_GetJSON_NetworkError 测试 GetJSON 网络错误
func TestAPIClient_GetJSON_NetworkError(t *testing.T) {
	client := NewAPIClient("http://localhost:1", "")
	err := client.GetJSON("/api/test", nil)
	if err == nil {
		t.Errorf("网络错误应返回 error")
	}
}

// TestAPIClient_PutJSON_NetworkError 测试 PutJSON 网络错误
func TestAPIClient_PutJSON_NetworkError(t *testing.T) {
	client := NewAPIClient("http://localhost:1", "")
	err := client.PutJSON("/api/test", map[string]string{"k": "v"}, nil)
	if err == nil {
		t.Errorf("网络错误应返回 error")
	}
}

// TestExactArgs 测试 exactArgs 函数
func TestExactArgs(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		args    []string
		wantErr bool
	}{
		{"exact_match", 2, []string{"a", "b"}, false},
		{"too_few", 2, []string{"a"}, true},
		{"too_many", 2, []string{"a", "b", "c"}, true},
		{"zero_args", 0, []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := exactArgs(tt.n)
			err := fn(nil, tt.args)
			if tt.wantErr && err == nil {
				t.Errorf("exactArgs(%d)(%v) 应返回错误", tt.n, tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("exactArgs(%d)(%v) 不应返回错误: %v", tt.n, tt.args, err)
			}
		})
	}
}

// TestMaximumNArgs 测试 maximumNArgs 函数
func TestMaximumNArgs(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		args    []string
		wantErr bool
	}{
		{"exact_max", 2, []string{"a", "b"}, false},
		{"under_max", 2, []string{"a"}, false},
		{"zero_args", 2, []string{}, false},
		{"over_max", 2, []string{"a", "b", "c"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := maximumNArgs(tt.n)
			err := fn(nil, tt.args)
			if tt.wantErr && err == nil {
				t.Errorf("maximumNArgs(%d)(%v) 应返回错误", tt.n, tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("maximumNArgs(%d)(%v) 不应返回错误: %v", tt.n, tt.args, err)
			}
		})
	}
}

// TestGetConfigPath 测试 getConfigPath 函数
func TestGetConfigPath(t *testing.T) {
	origCfgPath := cfgPath
	origWorkDir := workDir
	defer func() {
		cfgPath = origCfgPath
		workDir = origWorkDir
	}()

	// 测试自定义 cfgPath
	cfgPath = "/custom/path/config.yaml"
	if got := getConfigPath(); got != "/custom/path/config.yaml" {
		t.Errorf("getConfigPath() = %q, want /custom/path/config.yaml", got)
	}

	// 测试使用 workdir
	cfgPath = ""
	workDir = "/custom/workdir"
	if got := getConfigPath(); got != "/custom/workdir/config.yaml" {
		t.Errorf("getConfigPath() = %q, want /custom/workdir/config.yaml", got)
	}
}

// TestGetAPIClient 测试 getAPIClient 返回正确的客户端
func TestGetAPIClient(t *testing.T) {
	client := getAPIClient()
	if client == nil {
		t.Fatalf("getAPIClient 不应返回 nil")
	}
	if client.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want http://localhost:8080", client.baseURL)
	}
}

// TestReadFile_Success 测试 readFile 成功
func TestReadFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/test.txt"
	expected := "hello world"
	if err := os.WriteFile(path, []byte(expected), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	content, err := readFile(path)
	if err != nil {
		t.Errorf("readFile 失败: %v", err)
	}
	if content != expected {
		t.Errorf("content = %q, want %q", content, expected)
	}
}

// TestReadFile_NotFound 测试 readFile 文件不存在
func TestReadFile_NotFound(t *testing.T) {
	_, err := readFile("/nonexistent/file.txt")
	if err == nil {
		t.Errorf("文件不存在应返回错误")
	}
}

// TestVerbosef_VerboseMode 测试 verbosef 在 verbose 模式下输出
func TestVerbosef_VerboseMode(t *testing.T) {
	origVerbose := verbose
	defer func() { verbose = origVerbose }()
	verbose = true
	// 仅验证不 panic
	verbosef("test message: %s", "arg")
}

// TestInfof_NormalMode 测试 infof 在非 quiet 模式下输出
func TestInfof_NormalMode(t *testing.T) {
	origQuiet := quiet
	defer func() { quiet = origQuiet }()
	quiet = false
	// 仅验证不 panic
	infof("test message: %s", "arg")
}
