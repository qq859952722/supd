package api

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/supdorg/supd/internal/errors"
)

// REQ-P-002~004, REQ-I-002: 认证中间件
// 三种认证模式：none / local_skip / always_token（REQ-2.7.1）

// publicPaths 不需要认证的公共路径。
// D-02-001 修复：/api/auth/verify 在 always_token 模式下形成"先有 token 才能验证 token"
// 的逻辑悖论；/api/health 在 always_token 模式下需要 token 才能探活，影响监控。
// 这两个端点本身不暴露敏感信息（health 仅返回 {"status":"ok"}，auth/verify 仅返回 valid 布尔），
// 作为公共端点跳过认证是安全的。
var publicPaths = map[string]bool{
	"/api/health":      true,
	"/api/auth/verify": true,
}

// AuthMiddleware 返回基于认证模式的 chi 中间件。
// REQ-P-002: none 模式完全免认证
// REQ-P-003: local_skip 模式，内网 IP 免认证，外网需 token
// REQ-P-004: always_token 模式，任何访问都需 token
func AuthMiddleware(authMode string, authToken string, localNetworks []string) func(http.Handler) http.Handler {
	networks := parseNetworks(localNetworks)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// D-02-001 修复：公共端点跳过认证（health/auth-verify 自身不暴露敏感信息）
			if publicPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			switch authMode {
			case "none":
				// REQ-P-002: none 模式完全免认证
				next.ServeHTTP(w, r)
				return

			case "local_skip":
				// REQ-P-003: local_skip 模式
				clientIP := extractClientIP(r)
				if isLocalIP(clientIP, networks) {
					next.ServeHTTP(w, r)
					return
				}
				// 外网 IP 需要 token
				if !requireToken(w, r, authToken) {
					return
				}
				next.ServeHTTP(w, r)
				return

			case "always_token":
				// REQ-P-004: always_token 模式，任何访问都需 token
				if !requireToken(w, r, authToken) {
					return
				}
				next.ServeHTTP(w, r)
				return

			default:
				// 未知认证模式，默认拒绝
				respondError(w, errors.ErrAuthRequired, "authentication required")
				return
			}
		})
	}
}

// requireToken 校验请求中的 Bearer token，失败时直接写入错误响应并返回 false。
// REQ-P-004: 无 token 返回 401 AUTH_REQUIRED，无效 token 返回 401 AUTH_INVALID
func requireToken(w http.ResponseWriter, r *http.Request, authToken string) bool {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		// REQ-P-004: 无 token → 401 AUTH_REQUIRED
		respondError(w, errors.ErrAuthRequired, "authentication token required")
		return false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		// REQ-P-004: token 格式错误 → 401 AUTH_INVALID
		respondError(w, errors.ErrAuthInvalid, "invalid authentication token")
		return false
	}

	token := parts[1]
	if subtle.ConstantTimeCompare([]byte(token), []byte(authToken)) != 1 {
		// REQ-P-004: 无效 token → 401 AUTH_INVALID（常数时间比较防时序攻击）
		respondError(w, errors.ErrAuthInvalid, "invalid authentication token")
		return false
	}
	return true
}

// isLocalIP 检查 IP 是否在本地网段内。
// REQ-P-003: 检查 IP 是否在 local_networks 配置的网段内
func isLocalIP(ip net.IP, networks []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	// loopback（IPv4 127.0.0.0/8 与 IPv6 ::1）始终视为本地
	if ip.IsLoopback() {
		return true
	}
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// extractClientIP 从请求中提取客户端 IP。
// REQ-P-003: 从 r.RemoteAddr 提取客户端 IP
func extractClientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr 可能没有端口，直接尝试解析
		return net.ParseIP(r.RemoteAddr)
	}
	return net.ParseIP(host)
}

// parseNetworks 将 CIDR 字符串列表解析为 IPNet 列表。
func parseNetworks(cidrs []string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		networks = append(networks, ipNet)
	}
	return networks
}

// REQ-I-003: 长轮询并发限制
// 单客户端最多 5 个并发长轮询，全局最多 50 个
// 超限返回 503 + SERVICE_BUSY 错误码

// GlobalLongPollLimit 全局长轮询并发上限
// REQ-1.2: 数值锁定 50
const GlobalLongPollLimit = 50

// PerClientLongPollLimit 单客户端长轮询并发上限
// REQ-1.2: 数值锁定 5
const PerClientLongPollLimit = 5

// LongPollLimiter 长轮询并发限制器。
type LongPollLimiter struct {
	globalSem      chan struct{}
	perClientLimit int
	clientMap      map[string]int
	mu             sync.Mutex
}

// NewLongPollLimiter 创建长轮询并发限制器。
func NewLongPollLimiter(globalLimit, perClientLimit int) *LongPollLimiter {
	return &LongPollLimiter{
		globalSem:      make(chan struct{}, globalLimit),
		perClientLimit: perClientLimit,
		clientMap:      make(map[string]int),
	}
}

// Middleware 返回长轮询并发限制中间件。
func (l *LongPollLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 全局并发检查
			select {
			case l.globalSem <- struct{}{}:
			default:
				// REQ-I-003: 全局并发超限返回 503 SERVICE_BUSY
				respondError(w, errors.ErrServiceBusy, "too many concurrent long-poll requests")
				return
			}

			// 单客户端并发检查
			clientIP := extractClientIP(r)
			clientKey := clientIP.String()

			l.mu.Lock()
			count := l.clientMap[clientKey]
			if count >= l.perClientLimit {
				l.mu.Unlock()
				// 释放全局信号量
				<-l.globalSem
				// REQ-I-003: 单客户端并发超限返回 503 SERVICE_BUSY
				respondError(w, errors.ErrServiceBusy, "too many concurrent long-poll requests from this client")
				return
			}
			l.clientMap[clientKey] = count + 1
			l.mu.Unlock()

			// 请求完成后释放资源
			defer func() {
				<-l.globalSem
				l.mu.Lock()
				l.clientMap[clientKey]--
				if l.clientMap[clientKey] <= 0 {
					delete(l.clientMap, clientKey)
				}
				l.mu.Unlock()
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// Acquire 手动获取长轮询槽位（用于需要在 handler 内部根据 wait 参数决定是否限流的场景）。
// D-03-001 修复：原 LongPollLimiter 仅通过 Middleware 应用于 /api/events，
// 日志长轮询端点 /api/services/{name}/logs?wait=true 和 /api/extensions/runs/{runID}/logs?wait=true
// 未受限制，存在资源耗尽风险。Acquire/Release 提供在 handler 内部按需调用限流的能力。
//
// 用法：
//
//	if wait && len(lines) == 0 && newPos == sincePos {
//	    clientIP := extractClientIP(r).String()
//	    if !s.longPollLimiter.Acquire(clientIP) {
//	        respondError(w, errors.ErrServiceBusy, "too many concurrent long-poll requests")
//	        return
//	    }
//	    defer s.longPollLimiter.Release(clientIP)
//	    // ... 长轮询循环
//	}
//
// 返回 true 表示获取成功（调用方必须在使用完后调用 Release），
// 返回 false 表示超限（调用方应返回 503 SERVICE_BUSY）。
func (l *LongPollLimiter) Acquire(clientKey string) bool {
	// 全局并发检查
	select {
	case l.globalSem <- struct{}{}:
	default:
		return false
	}

	// 单客户端并发检查
	l.mu.Lock()
	count := l.clientMap[clientKey]
	if count >= l.perClientLimit {
		l.mu.Unlock()
		<-l.globalSem
		return false
	}
	l.clientMap[clientKey] = count + 1
	l.mu.Unlock()

	return true
}

// Release 释放长轮询槽位（与 Acquire 配对使用）。
func (l *LongPollLimiter) Release(clientKey string) {
	<-l.globalSem
	l.mu.Lock()
	l.clientMap[clientKey]--
	if l.clientMap[clientKey] <= 0 {
		delete(l.clientMap, clientKey)
	}
	l.mu.Unlock()
}

// accessLogMiddleware 用 slog 输出结构化 HTTP 访问日志，替代 chi 内置 Logger 中间件。
//
// chi Logger 使用标准库 log 包（log.New(os.Stdout, "", log.LstdFlags)），存在两个问题：
//  1. 不受 config.Settings.LogLevel 控制（log_level 对它无效）
//  2. 不写入 supd.log 文件（直接写 os.Stdout，绕过 CatchAllLogger）
//
// 改用 slog 后：
//   - 有 level=INFO 标记，受 log_level 控制（warn/error 时自动过滤）
//   - 同时写入 supd.log 文件 + stderr（与其他 slog 日志一致）
//   - 结构化字段（method/path/status/duration 等独立字段，便于检索）
func accessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		t1 := time.Now()

		defer func() {
			slog.Info("http_request",
				"method", r.Method,
				"path", r.URL.RequestURI(),
				"remote_addr", r.RemoteAddr,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", float64(time.Since(t1).Microseconds())/1000.0,
				"request_id", chiMiddleware.GetReqID(r.Context()),
			)
		}()

		next.ServeHTTP(ww, r)
	})
}
