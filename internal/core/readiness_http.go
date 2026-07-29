package core

import (
	"context"
	"fmt"
	"net/http"

	"github.com/supdorg/supd/internal/config"
)

// httpChecker http_check类型readiness检查
// REQ-F-009: 循环发送HTTP GET到指定URL，返回expected_status（默认200）即ready
type httpChecker struct {
	url            string
	expectedStatus int
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
		url:            cfg.URL,
		expectedStatus: expectedStatus,
	}, nil
}

// Check 执行单次 HTTP 探测；重试间隔和总超时由调用方负责。
func (h *httpChecker) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != h.expectedStatus {
		return fmt.Errorf("readiness http_check: status %d, expected %d", resp.StatusCode, h.expectedStatus)
	}
	return nil
}

// Close http_check无需清理
func (h *httpChecker) Close() error {
	return nil
}
