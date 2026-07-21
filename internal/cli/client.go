package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// APIClient HTTP API 客户端，用于 CLI 与运行中的 supd 通信。
// REQ-F-039: CLI 命令通过 HTTP API 与 supd 通信
type APIClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewAPIClient 创建 API 客户端。
func NewAPIClient(baseURL, token string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Get 发送 GET 请求。
func (c *APIClient) Get(path string) (*http.Response, error) {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuth(req)
	return c.httpClient.Do(req)
}

// Post 发送 POST 请求。
func (c *APIClient) Post(path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest("POST", c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuth(req)
	return c.httpClient.Do(req)
}

// Put 发送 PUT 请求。
func (c *APIClient) Put(path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest("PUT", c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuth(req)
	return c.httpClient.Do(req)
}

// GetJSON 发送 GET 请求并解析 JSON 响应。
func (c *APIClient) GetJSON(path string, result any) error {
	resp, err := c.Get(path)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("supd 未运行")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
	}
	return nil
}

// PostJSON 发送 POST 请求并解析 JSON 响应。
func (c *APIClient) PostJSON(path string, body any, result any) error {
	resp, err := c.Post(path, body)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("supd 未运行")
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, bodyBytes)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
	}
	return nil
}

// PutJSON 发送 PUT 请求并解析 JSON 响应。
func (c *APIClient) PutJSON(path string, body any, result any) error {
	resp, err := c.Put(path, body)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, bodyBytes)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
	}
	return nil
}

// setAuth 设置认证头。
func (c *APIClient) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// parseAPIError 解析后端错误响应并返回中文提示。
// 后端错误响应格式：{"error":{"code":"...","message":"..."}}
// P-01-02: 避免直接暴露 HTTP 状态码和原始 JSON body
func parseAPIError(statusCode int, body []byte) error {
	var errResp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Code != "" {
		if msg := cliErrorMessage(errResp.Error.Code); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		if errResp.Error.Message != "" {
			return fmt.Errorf("%s", errResp.Error.Message)
		}
		return fmt.Errorf("请求失败（错误码: %s）", errResp.Error.Code)
	}
	return fmt.Errorf("请求失败（HTTP %d）", statusCode)
}

// CheckSupdRunning 检查 supd 是否在运行。
// P-01-01: 捕获网络错误，输出用户友好中文消息，不泄漏 dial tcp 等技术堆栈
func (c *APIClient) CheckSupdRunning() error {
	resp, err := c.Get("/api/health")
	if err != nil {
		// 保留原始错误供 --verbose 调试
		verbosef("连接 supd 失败: %v", err)
		// 区分网络连接类错误（连接拒绝/超时/不可达），输出友好提示
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			var opErr *net.OpError
			if errors.As(urlErr.Err, &opErr) {
				return fmt.Errorf("无法连接到 supd 服务（请确认 supd 已启动）")
			}
			return fmt.Errorf("无法连接到 supd 服务（请确认 supd 已启动）")
		}
		return fmt.Errorf("无法连接到 supd 服务（请确认 supd 已启动）")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("supd 未运行")
	}
	return nil
}
