package core

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// httpChecker http_check类型readiness检查
// REQ-F-009: 循环发送HTTP GET到指定URL，返回expected_status（默认200）即ready
type httpChecker struct {
	url            string
	expectedStatus int
	intervalSeconds int
}

func newHTTPChecker(cfg *config.ReadinessConfig) (*httpChecker, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("readiness http_check: url is required")
	}
	expectedStatus := cfg.ExpectedStatus
	if expectedStatus == 0 {
		expectedStatus = 200
	}
	return &httpChecker{
		url:             cfg.URL,
		expectedStatus:  expectedStatus,
		intervalSeconds: cfg.IntervalSeconds,
	}, nil
}

// Check 循环发送HTTP GET，状态码匹配即返回nil
// REQ-F-009: interval_seconds间隔循环检查，超时由ctx控制
func (h *httpChecker) Check(ctx context.Context) error {
	interval := time.Duration(h.intervalSeconds) * time.Second
	client := &http.Client{Timeout: interval}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
		if err != nil {
			// URL无效，等待后重试
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
				continue
			}
		}

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == h.expectedStatus {
				return nil
			}
		}

		// 等待interval或ctx取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Close http_check无需清理
func (h *httpChecker) Close() error {
	return nil
}
