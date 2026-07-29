package core

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// tcpChecker tcp_check类型readiness检查
// REQ-F-009: 循环尝试TCP连接到指定端口，连接成功即ready
type tcpChecker struct {
	port int
}

func newTCPChecker(cfg *config.ReadinessConfig) (*tcpChecker, error) {
	if cfg.Port <= 0 {
		return nil, fmt.Errorf("readiness tcp_check: port is required")
	}
	return &tcpChecker{port: cfg.Port}, nil
}

// Check 执行单次 TCP 探测；重试间隔和总超时由调用方负责。
func (t *tcpChecker) Check(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", t.port)
	dialCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// Close tcp_check无需清理
func (t *tcpChecker) Close() error {
	return nil
}
