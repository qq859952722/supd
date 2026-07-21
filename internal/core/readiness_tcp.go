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
	port            int
	intervalSeconds int
}

func newTCPChecker(cfg *config.ReadinessConfig) (*tcpChecker, error) {
	if cfg.Port <= 0 {
		return nil, fmt.Errorf("readiness tcp_check: port is required")
	}
	return &tcpChecker{
		port:            cfg.Port,
		intervalSeconds: cfg.IntervalSeconds,
	}, nil
}

// Check 循环尝试TCP连接，成功返回nil
// REQ-F-009: interval_seconds间隔循环检查，超时由ctx控制
func (t *tcpChecker) Check(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", t.port)
	interval := time.Duration(t.intervalSeconds) * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := net.DialTimeout("tcp", addr, interval)
		if err == nil {
			conn.Close()
			return nil
		}

		// 等待interval或ctx取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Close tcp_check无需清理
func (t *tcpChecker) Close() error {
	return nil
}
