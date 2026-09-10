package extension

import (
	"strings"
	"testing"
)

// TestBuildSupdEnvOperationInjection 断言操作 Run 注入 5 个 SUPD_OPERATION_* 变量，
// 普通 Run 不含这 5 个变量。
func TestBuildSupdEnvOperationInjection(t *testing.T) {
	base := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "webui",
		TriggerUser:   "webui",
		ActionID:      "check",
	}
	// 普通 Run 不含操作变量。
	plain := BuildSupdEnv("run-1", "ext", base)
	for _, ev := range plain {
		if strings.HasPrefix(ev, "SUPD_OPERATION") || strings.HasPrefix(ev, "SUPD_NOTIFICATION_TOPIC_ID") {
			t.Fatalf("plain run should not contain operation env: %s", ev)
		}
	}

	// 操作 Run 注入 5 个变量。
	op := base
	op.OperationID = "check-update"
	op.OperationParams = `{"force":true}`
	op.NotificationTopicID = "topic-123"
	op.OperationExecutionID = "exec-456"
	op.OperationPhase = "global"

	got := BuildSupdEnv("run-2", "ext", op)
	env := map[string]string{}
	for _, ev := range got {
		k, v, _ := strings.Cut(ev, "=")
		env[k] = v
	}

	want := map[string]string{
		"SUPD_OPERATION":              "check-update",
		"SUPD_OPERATION_PARAMS":       `{"force":true}`,
		"SUPD_NOTIFICATION_TOPIC_ID":  "topic-123",
		"SUPD_OPERATION_EXECUTION_ID": "exec-456",
		"SUPD_OPERATION_PHASE":        "global",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}

	// 操作服务阶段 Run 绑定所属服务：额外注入 SUPD_SERVICE（无 PID），
	// SUPD_SERVICE_DIR 在 ServiceDir 非空时注入（由 RunGateway 按服务作用域补齐）。
	opService := op
	opService.ServiceName = "web-demo"
	opService.OperationPhase = "service"
	gotSvc := BuildSupdEnv("run-3", "ext", opService)
	envSvc := map[string]string{}
	for _, ev := range gotSvc {
		k, v, _ := strings.Cut(ev, "=")
		envSvc[k] = v
	}
	if envSvc["SUPD_SERVICE"] != "web-demo" {
		t.Errorf("operation service-phase run SUPD_SERVICE = %q, want web-demo", envSvc["SUPD_SERVICE"])
	}
	if _, hasPID := envSvc["SUPD_SERVICE_PID"]; hasPID {
		t.Error("operation service-phase run must not inject SUPD_SERVICE_PID")
	}
	if _, hasDir := envSvc["SUPD_SERVICE_DIR"]; hasDir {
		t.Error("operation service-phase run without ServiceDir must not inject SUPD_SERVICE_DIR")
	}
	opService.ServiceDir = "/base/services/web-demo"
	gotSvcDir := BuildSupdEnv("run-4", "ext", opService)
	envSvcDir := map[string]string{}
	for _, ev := range gotSvcDir {
		k, v, _ := strings.Cut(ev, "=")
		envSvcDir[k] = v
	}
	if envSvcDir["SUPD_SERVICE_DIR"] != "/base/services/web-demo" {
		t.Errorf("operation service-phase run SUPD_SERVICE_DIR = %q, want /base/services/web-demo", envSvcDir["SUPD_SERVICE_DIR"])
	}
}

// TestBuildSupdEnvServiceLifecycleUnchanged service_lifecycle 语义不受操作注入影响（回归）。
func TestBuildSupdEnvServiceLifecycleUnchanged(t *testing.T) {
	tc := TriggerContext{
		EventType:     "service_lifecycle",
		TriggerSource: "service_lifecycle",
		TriggerUser:   "supd",
		ServiceName:   "web-demo",
		ServicePID:    42,
		Phase:         "post_ready",
	}
	env := map[string]string{}
	for _, ev := range BuildSupdEnv("run-1", "ext", tc) {
		k, v, _ := strings.Cut(ev, "=")
		env[k] = v
	}
	if env["SUPD_SERVICE"] != "web-demo" || env["SUPD_SERVICE_PID"] != "42" {
		t.Errorf("service_lifecycle SUPD_SERVICE/PID wrong: %q / %q", env["SUPD_SERVICE"], env["SUPD_SERVICE_PID"])
	}
}

// TestNormalizeParams 断言参数校验语义：缺省 {} / 顶层必须对象 / 超 8KB 拒绝。
func TestNormalizeParams(t *testing.T) {
	// 缺省 {}。
	if got, err := normalizeParams(nil); err != nil || got != "{}" {
		t.Errorf("nil params => %q, err %v; want \"{}\"", got, err)
	}
	if got, err := normalizeParams([]byte("   ")); err != nil || got != "{}" {
		t.Errorf("blank params => %q, err %v; want \"{}\"", got, err)
	}
	// 合法对象 → 紧凑。
	if got, err := normalizeParams([]byte(`{"a": 1,  "b" : [true]}`)); err != nil || got != `{"a":1,"b":[true]}` {
		t.Errorf("compact params => %q, err %v", got, err)
	}
	// 非对象（数组）。
	if _, err := normalizeParams([]byte(`[1,2]`)); err == nil {
		t.Error("array params should be rejected (top-level must be object)")
	}
	// 非 JSON。
	if _, err := normalizeParams([]byte(`{bad`)); err == nil {
		t.Error("invalid JSON should be rejected")
	}
	// 超 8KB。
	big := []byte(`{"x":"` + strings.Repeat("a", operationParamsLimit) + `"}`)
	if _, err := normalizeParams(big); err == nil {
		t.Error("params over 8KB should be rejected")
	}
}
