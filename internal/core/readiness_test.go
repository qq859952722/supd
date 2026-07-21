package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
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

	checker, err := newScriptChecker(cfg)
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

	checker, err := newScriptChecker(cfg)
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

	checker, err := newScriptChecker(cfg)
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
			checker, err := NewReadinessChecker(tt.cfg)
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
