package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// --- fd_notify tests ---

func TestNotifyChecker_Ready(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Type:            "fd_notify",
		Fd:              3,
		IntervalSeconds: 1,
		TimeoutSeconds:  5,
	}

	checker, err := NewNotifyChecker(cfg)
	if err != nil {
		t.Fatalf("NewNotifyChecker: %v", err)
	}
	defer checker.Close()

	// 模拟子进程：向写端写入数据后关闭
	// 注意：在真实场景中，写端fd通过cmd.ExtraFiles传给子进程，
	// supd在cmd.Start()后调用CloseWriter()关闭自己的写端引用。
	// 此处模拟：goroutine持有写端，先写入数据再关闭，
	// 然后主流程再CloseWriter。
	writer := checker.WriterFd()
	if writer == nil {
		t.Fatal("WriterFd should not be nil")
	}

	// 模拟子进程写入
	go func() {
		time.Sleep(100 * time.Millisecond)
		writer.Write([]byte("ready"))
		writer.Close()
	}()

	// 不需要CloseWriter，因为goroutine关闭writer后读端就能收到数据

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err != nil {
		t.Errorf("Check should return nil on ready, got: %v", err)
	}
}

func TestNotifyChecker_Timeout(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Type:            "fd_notify",
		Fd:              3,
		IntervalSeconds: 1,
		TimeoutSeconds:  1,
	}

	checker, err := NewNotifyChecker(cfg)
	if err != nil {
		t.Fatalf("NewNotifyChecker: %v", err)
	}
	defer checker.Close()

	// 关闭写端但不写入数据，读端将阻塞直到超时
	checker.CloseWriter()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := checker.Check(ctx); err == nil {
		t.Error("Check should return error on timeout")
	}
}

// --- tcp_check tests ---

func testTCPChecker_Ready(t *testing.T) {
	// 启动临时TCP服务器
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	// 接受连接
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	cfg := &config.ReadinessConfig{
		Type:            "tcp_check",
		Port:            port,
		IntervalSeconds: 1,
		TimeoutSeconds:  5,
	}

	checker, err := newTCPChecker(cfg)
	if err != nil {
		t.Fatalf("newTCPChecker: %v", err)
	}
	defer checker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err != nil {
		t.Errorf("Check should return nil when TCP port is open, got: %v", err)
	}
}

func TestTCPChecker_Ready(t *testing.T) {
	testTCPChecker_Ready(t)
}

func TestTCPChecker_Timeout(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Type:            "tcp_check",
		Port:            59999, // unlikely to be in use
		IntervalSeconds: 1,
		TimeoutSeconds:  2,
	}

	checker, err := newTCPChecker(cfg)
	if err != nil {
		t.Fatalf("newTCPChecker: %v", err)
	}
	defer checker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err == nil {
		t.Error("Check should return error when TCP port is unreachable")
	}
}

// --- http_check tests ---

func TestHTTPChecker_Ready(t *testing.T) {
	// 启动临时HTTP服务器
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)

	cfg := &config.ReadinessConfig{
		Type:            "http_check",
		URL:             url,
		ExpectedStatus:  200,
		IntervalSeconds: 1,
		TimeoutSeconds:  5,
	}

	checker, err := newHTTPChecker(cfg)
	if err != nil {
		t.Fatalf("newHTTPChecker: %v", err)
	}
	defer checker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err != nil {
		t.Errorf("Check should return nil when HTTP returns 200, got: %v", err)
	}
}

func TestHTTPChecker_StatusMismatch(t *testing.T) {
	// 启动临时HTTP服务器，返回500
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)

	cfg := &config.ReadinessConfig{
		Type:            "http_check",
		URL:             url,
		ExpectedStatus:  200,
		IntervalSeconds: 1,
		TimeoutSeconds:  2,
	}

	checker, err := newHTTPChecker(cfg)
	if err != nil {
		t.Fatalf("newHTTPChecker: %v", err)
	}
	defer checker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err == nil {
		t.Error("Check should return error when HTTP returns 500 but expected 200")
	}
}

// --- script tests ---

func TestScriptChecker_Ready(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Type:            "script",
		Check:           []string{"true"}, // exit 0
		IntervalSeconds: 1,
		TimeoutSeconds:  5,
	}

	checker, err := newScriptChecker(cfg, "", nil)
	if err != nil {
		t.Fatalf("newScriptChecker: %v", err)
	}
	defer checker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err != nil {
		t.Errorf("Check should return nil when script exits 0, got: %v", err)
	}
}

func TestScriptChecker_NotReady(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Type:            "script",
		Check:           []string{"false"}, // exit 1
		IntervalSeconds: 1,
		TimeoutSeconds:  2,
	}

	checker, err := newScriptChecker(cfg, "", nil)
	if err != nil {
		t.Fatalf("newScriptChecker: %v", err)
	}
	defer checker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err == nil {
		t.Error("Check should return error when script exits non-zero")
	}
}

func TestScriptChecker_Timeout(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Type:            "script",
		Check:           []string{"sleep", "10"},
		IntervalSeconds: 1,
		TimeoutSeconds:  1,
	}

	checker, err := newScriptChecker(cfg, "", nil)
	if err != nil {
		t.Fatalf("newScriptChecker: %v", err)
	}
	defer checker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err == nil {
		t.Error("Check should return error on timeout")
	}
}

// TestScriptChecker_DirRelativePath 验证 dir 字段使 check 中的相对路径脚本可解析。
// 不传 dir 时，相对路径脚本无法找到（exit 127）；传 dir 后正常执行（exit 0）。
func TestScriptChecker_DirRelativePath(t *testing.T) {
	dir := t.TempDir()
	// 在临时目录下创建 ready.sh（exit 0）
	scriptPath := dir + "/ready.sh"
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write ready.sh: %v", err)
	}

	cfg := &config.ReadinessConfig{
		Type:            "script",
		Check:           []string{"sh", "ready.sh"}, // 相对路径，依赖 cmd.Dir 解析
		IntervalSeconds: 1,
		TimeoutSeconds:  3,
	}

	// 不传 dir：sh 找不到 ready.sh → 非零退出 → 超时失败
	noDirChecker, err := newScriptChecker(cfg, "", nil)
	if err != nil {
		t.Fatalf("newScriptChecker: %v", err)
	}
	defer noDirChecker.Close()
	ctx1, cancel1 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel1()
	if err := noDirChecker.Check(ctx1); err == nil {
		t.Error("Check without dir should fail (relative script not found)")
	}

	// 传 dir：sh 在 dir 下找到 ready.sh → exit 0 → 立即就绪
	dirChecker, err := newScriptChecker(cfg, dir, nil)
	if err != nil {
		t.Fatalf("newScriptChecker: %v", err)
	}
	defer dirChecker.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	if err := dirChecker.Check(ctx2); err != nil {
		t.Errorf("Check with dir should succeed (relative script resolved), got: %v", err)
	}
}

// TestScriptChecker_InheritsServiceEnv 验证规格 §2.2.3: type=script 时 check 脚本继承服务的环境变量。
// 不传 env（nil）时脚本继承 os.Environ()，其中不含 SUPD_READY_VAR → 检查失败；
// 传 env（含服务 env 合并结果）时 SUPD_READY_VAR 可被脚本访问 → 检查通过。
func TestScriptChecker_InheritsServiceEnv(t *testing.T) {
	cfg := &config.ReadinessConfig{
		Type:            "script",
		Check:           []string{"sh", "-c", "test \"$SUPD_READY_VAR\" = \"yes\""},
		IntervalSeconds: 1,
		TimeoutSeconds:  2,
	}

	// 不传 env：脚本继承 os.Environ()，无 SUPD_READY_VAR → exit 1 → 超时失败
	noEnvChecker, err := newScriptChecker(cfg, "", nil)
	if err != nil {
		t.Fatalf("newScriptChecker: %v", err)
	}
	defer noEnvChecker.Close()
	ctx1, cancel1 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel1()
	if err := noEnvChecker.Check(ctx1); err == nil {
		t.Error("Check without env should fail (SUPD_READY_VAR not set)")
	}

	// 传 env（模拟 BuildServiceProcessEnv 的合并结果：os.Environ() + 服务 env）
	withEnvChecker, err := newScriptChecker(cfg, "", append(os.Environ(), "SUPD_READY_VAR=yes"))
	if err != nil {
		t.Fatalf("newScriptChecker: %v", err)
	}
	defer withEnvChecker.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	if err := withEnvChecker.Check(ctx2); err != nil {
		t.Errorf("Check with env should succeed (SUPD_READY_VAR inherited), got: %v", err)
	}
}

// --- factory function tests ---

func TestNewReadinessChecker_Factory(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.ReadinessConfig
		wantErr bool
	}{
		{
			name: "nil config",
			cfg:  nil,
			wantErr: true,
		},
		{
			name: "unsupported type",
			cfg: &config.ReadinessConfig{Type: "unknown"},
			wantErr: true,
		},
		{
			name: "fd_notify",
			cfg: &config.ReadinessConfig{Type: "fd_notify", Fd: 3, IntervalSeconds: 1, TimeoutSeconds: 5},
			wantErr: false,
		},
		{
			name: "tcp_check",
			cfg: &config.ReadinessConfig{Type: "tcp_check", Port: 8080, IntervalSeconds: 1, TimeoutSeconds: 5},
			wantErr: false,
		},
		{
			name: "http_check",
			cfg: &config.ReadinessConfig{Type: "http_check", URL: "http://localhost/health", IntervalSeconds: 1, TimeoutSeconds: 5},
			wantErr: false,
		},
		{
			name: "script",
			cfg: &config.ReadinessConfig{Type: "script", Check: []string{"true"}, IntervalSeconds: 1, TimeoutSeconds: 5},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewReadinessChecker(tt.cfg, "", nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewReadinessChecker() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if checker != nil {
				checker.Close()
			}
		})
	}
}
