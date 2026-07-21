package extension

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/watch"
)

// TestServiceLifecycleTriggerIntegration 扩展生命周期触发集成测试
// L-04-002: 验证服务级扩展（service_lifecycle 触发器）的 pre_start 和 post_ready 钩子
//
// 构造一个真实的服务级扩展目录结构（services/<svc>/extensions/<ext>/），
// 通过 watch.NewDiscovery 扫描真实磁盘目录，
// 用真实的 ServiceLifecycleTrigger + Dispatcher + Executor 执行 bash 脚本，
// 验证调用 OnPreStart/OnPostReady 后扩展脚本被执行并写入时间戳到 marker 文件。
//
// REQ-D-004: service_lifecycle 触发器，4 个 phase
// REQ-D-004, 2.2.5: pre_start 时 SUPD_SERVICE_PID 为空（进程尚未启动）
// REQ-D-004, 2.2.5: post_ready 时 SUPD_SERVICE_PID 为服务进程的实际 PID
// REQ-F-025: 服务级扩展通过 services/<svc>/extensions/<ext>/ 目录扫描自动发现
func TestServiceLifecycleTriggerIntegration(t *testing.T) {
	// === 1. 创建真实目录结构 ===
	baseDir := t.TempDir()
	logDir := filepath.Join(baseDir, "logs")
	// markerDir 独立于 baseDir，避免污染 discovery 扫描结果
	markerDir := t.TempDir()

	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 创建服务目录: baseDir/services/my-service/extensions/lifecycle-ext/
	svcDir := filepath.Join(baseDir, "services", "my-service")
	extDir := filepath.Join(svcDir, "extensions", "lifecycle-ext")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}

	// service.yaml（最小化配置，仅供 discovery 加载）
	if err := os.WriteFile(
		filepath.Join(svcDir, "service.yaml"),
		[]byte(`name: my-service
version: "1.0"
command:
  - sleep
  - "60"
`),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	// bash 脚本：根据 SUPD_PHASE 将触发时间戳和上下文写入对应的 marker 文件
	scriptPath := filepath.Join(extDir, "run.sh")
	preStartMarker := filepath.Join(markerDir, "pre_start.marker")
	postReadyMarker := filepath.Join(markerDir, "post_ready.marker")
	unknownMarker := filepath.Join(markerDir, "unknown.marker")

	// 脚本内容动态注入 marker 文件路径（绝对路径，t.TempDir 保证无 shell 元字符）
	scriptContent := `#!/bin/bash
# L-04-002 integration test: lifecycle trigger extension
# 根据 SUPD_PHASE 将触发时间戳和上下文信息写入对应的 marker 文件
PHASE="${SUPD_PHASE:-unknown}"
TIMESTAMP=$(date +%s%N)
SERVICE="${SUPD_SERVICE:-}"
EVENT="${SUPD_EVENT:-}"
PID="${SUPD_SERVICE_PID:-}"

case "$PHASE" in
    pre_start)
        MARKER_FILE="` + preStartMarker + `"
        ;;
    post_ready)
        MARKER_FILE="` + postReadyMarker + `"
        ;;
    *)
        echo "unknown phase: $PHASE" > "` + unknownMarker + `"
        exit 1
        ;;
esac

cat > "$MARKER_FILE" <<EOF
phase=$PHASE
timestamp=$TIMESTAMP
service=$SERVICE
event=$EVENT
service_pid=$PID
EOF

exit 0
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatal(err)
	}

	// meta.yaml（同时配置 pre_start 和 post_ready 触发器，使用不同的 action）
	metaYAML := `name: lifecycle-ext
version: "1.0.0"
description: "L-04-002 integration test extension"
enabled: true
runtime: bash
entry: ` + scriptPath + `
timeout_seconds: 15
concurrency: serialize
ui:
  show_logs: true
  button_style: default
triggers:
  service_lifecycle:
    - event: pre_start
      action: on-pre-start
    - event: post_ready
      action: on-post-ready
actions:
  - id: on-pre-start
    label: "Pre-start hook"
    button_style: default
  - id: on-post-ready
    label: "Post-ready hook"
    button_style: default
`
	if err := os.WriteFile(filepath.Join(extDir, "meta.yaml"), []byte(metaYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// === 2. 使用真实的 watch.NewDiscovery 扫描磁盘目录 ===
	discovery := watch.NewDiscovery(baseDir, logDir).Scan()

	// 验证 discovery 无错误
	if len(discovery.Errors) > 0 {
		t.Fatalf("discovery errors: %v", discovery.Errors)
	}

	// 验证 discovery 找到了服务
	svcEntry, ok := discovery.Services["my-service"]
	if !ok {
		t.Fatal("service my-service not discovered")
	}

	// 验证 discovery 找到了服务级扩展
	extEntry, ok := svcEntry.Extensions["lifecycle-ext"]
	if !ok {
		t.Fatal("extension lifecycle-ext not discovered")
	}

	// 验证扩展元数据正确加载
	if extEntry.Meta.Name != "lifecycle-ext" {
		t.Errorf("expected extension name lifecycle-ext, got %s", extEntry.Meta.Name)
	}
	if extEntry.Meta.Enabled == nil || !*extEntry.Meta.Enabled {
		t.Errorf("expected extension enabled, got %v", extEntry.Meta.Enabled)
	}
	if len(extEntry.Meta.Triggers.ServiceLifecycle) != 2 {
		t.Errorf("expected 2 service_lifecycle triggers, got %d",
			len(extEntry.Meta.Triggers.ServiceLifecycle))
	}

	// === 3. 创建真实的 Executor + Dispatcher + ServiceLifecycleTrigger ===
	executor := NewExecutor(logDir, baseDir)
	dispatcher := NewDispatcher(executor, baseDir, logDir, 1800)
	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// === 4. 验证 pre_start 触发 ===
	t.Run("PreStart", func(t *testing.T) {
		// 清理可能存在的旧 marker
		os.Remove(preStartMarker)
		os.Remove(unknownMarker)

		// 调用 OnPreStart，触发 service_lifecycle:pre_start 扩展
		results := trigger.OnPreStart(ctx, "my-service")
		if len(results) != 1 {
			t.Fatalf("expected 1 result from pre_start, got %d: %+v", len(results), results)
		}

		if results[0].State != TaskSuccess {
			t.Fatalf("expected pre_start extension to succeed, got state=%s, exit_code=%d, msg=%s",
				results[0].State, results[0].ExitCode, results[0].ResultMsg)
		}

		// 验证扩展名和触发类型
		if results[0].ExtensionName != "lifecycle-ext" {
			t.Errorf("expected extension name lifecycle-ext, got %s", results[0].ExtensionName)
		}
		if results[0].TriggerType != "service_lifecycle" {
			t.Errorf("expected trigger type service_lifecycle, got %s", results[0].TriggerType)
		}

		// 验证 unknown marker 未创建（phase 匹配成功）
		if _, err := os.Stat(unknownMarker); !os.IsNotExist(err) {
			t.Errorf("unknown.marker should not be created for pre_start, err=%v", err)
		}

		// 验证 pre_start marker 文件已创建
		data, err := os.ReadFile(preStartMarker)
		if err != nil {
			t.Fatalf("pre_start marker file not created: %v", err)
		}

		content := string(data)

		// 验证 phase=pre_start
		if !strings.Contains(content, "phase=pre_start") {
			t.Errorf("marker content should contain 'phase=pre_start', got:\n%s", content)
		}

		// 验证 service=my-service
		if !strings.Contains(content, "service=my-service") {
			t.Errorf("marker content should contain 'service=my-service', got:\n%s", content)
		}

		// 验证 event=service_lifecycle
		if !strings.Contains(content, "event=service_lifecycle") {
			t.Errorf("marker content should contain 'event=service_lifecycle', got:\n%s", content)
		}

		// REQ-D-004, 2.2.5: pre_start 时 SUPD_SERVICE_PID 应为空
		// heredoc 输出 "service_pid=" 后跟换行符（$PID 为空字符串）
		if !strings.Contains(content, "service_pid=\n") && !strings.HasSuffix(content, "service_pid=") {
			t.Errorf("pre_start: SUPD_SERVICE_PID should be empty, got:\n%s", content)
		}

		// 验证 timestamp 不为空（heredoc 会展开 $TIMESTAMP）
		if !strings.Contains(content, "timestamp=") {
			t.Errorf("marker content should contain 'timestamp=', got:\n%s", content)
		}
	})

	// === 5. 验证 post_ready 触发 ===
	t.Run("PostReady", func(t *testing.T) {
		// 清理可能存在的旧 marker
		os.Remove(postReadyMarker)
		os.Remove(unknownMarker)

		// 模拟服务进程 PID = 12345
		const servicePID = 12345
		results := trigger.OnPostReady(ctx, "my-service", servicePID)
		if len(results) != 1 {
			t.Fatalf("expected 1 result from post_ready, got %d: %+v", len(results), results)
		}

		if results[0].State != TaskSuccess {
			t.Fatalf("expected post_ready extension to succeed, got state=%s, exit_code=%d, msg=%s",
				results[0].State, results[0].ExitCode, results[0].ResultMsg)
		}

		// 验证扩展名和触发类型
		if results[0].ExtensionName != "lifecycle-ext" {
			t.Errorf("expected extension name lifecycle-ext, got %s", results[0].ExtensionName)
		}
		if results[0].TriggerType != "service_lifecycle" {
			t.Errorf("expected trigger type service_lifecycle, got %s", results[0].TriggerType)
		}

		// 验证 unknown marker 未创建
		if _, err := os.Stat(unknownMarker); !os.IsNotExist(err) {
			t.Errorf("unknown.marker should not be created for post_ready, err=%v", err)
		}

		// 验证 post_ready marker 文件已创建
		data, err := os.ReadFile(postReadyMarker)
		if err != nil {
			t.Fatalf("post_ready marker file not created: %v", err)
		}

		content := string(data)

		// 验证 phase=post_ready
		if !strings.Contains(content, "phase=post_ready") {
			t.Errorf("marker content should contain 'phase=post_ready', got:\n%s", content)
		}

		// 验证 service=my-service
		if !strings.Contains(content, "service=my-service") {
			t.Errorf("marker content should contain 'service=my-service', got:\n%s", content)
		}

		// 验证 event=service_lifecycle
		if !strings.Contains(content, "event=service_lifecycle") {
			t.Errorf("marker content should contain 'event=service_lifecycle', got:\n%s", content)
		}

		// REQ-D-004, 2.2.5: post_ready 时 SUPD_SERVICE_PID 应为实际 PID
		if !strings.Contains(content, "service_pid=12345") {
			t.Errorf("post_ready: SUPD_SERVICE_PID should be 12345, got:\n%s", content)
		}

		// 验证 timestamp 不为空
		if !strings.Contains(content, "timestamp=") {
			t.Errorf("marker content should contain 'timestamp=', got:\n%s", content)
		}
	})
}
