package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/watch"
)

// Server HTTP API 服务器。
// REQ-I-001, REQ-I-002: chi 路由注册 + HTTP 服务器
type Server struct {
	router     chi.Router
	httpServer *http.Server
	config     *config.Config

	// 依赖注入接口（由 adapters.go 中的 Core*Provider 实现）
	stateProvider       StateProvider
	serviceOperator     ServiceOperator
	extProvider         ExtensionProvider
	taskProvider        TaskProvider
	cronProvider        CronProvider
	settingsProvider    SettingsProvider
	runtimeProvider     RuntimeProvider
	systemProvider      SystemProvider
	fileProvider        FileProvider
	authProvider        AuthProvider
	logProvider         LogProvider
	watchProvider       WatchProvider
	serviceHistoryGetter ServiceHistoryGetter
	eventRing           *EventRingBuffer
	longPollLimiter     *LongPollLimiter
	pathValidator       *PathValidator
}

// NewServer 创建并配置 API 服务器。
func NewServer(cfg *config.Config) *Server {
	s := &Server{
		router:         chi.NewRouter(),
		config:         cfg,
		longPollLimiter: NewLongPollLimiter(GlobalLongPollLimit, PerClientLongPollLimit), // REQ-I-003: 长轮询并发限制器，全局50/单客户端5
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

// SetupStaticFiles 注册前端静态文件服务和 SPA fallback。
// REQ-F-002: 单二进制架构，前端通过 embed.FS 嵌入
func (s *Server) SetupStaticFiles(webFS http.FileSystem) {
	SetupStaticFiles(s.router, webFS)
}

// SetProviders 注入所有依赖接口。
// cmd/supd 中调用此方法完成依赖注入。
func (s *Server) SetProviders(
	stateProvider StateProvider,
	serviceOperator ServiceOperator,
	extProvider ExtensionProvider,
	taskProvider TaskProvider,
	cronProvider CronProvider,
	settingsProvider SettingsProvider,
	runtimeProvider RuntimeProvider,
	systemProvider SystemProvider,
	fileProvider FileProvider,
	authProvider AuthProvider,
	logProvider LogProvider,
	watchProvider WatchProvider,
	serviceHistoryGetter ServiceHistoryGetter,
	eventRing *EventRingBuffer,
	pathValidator *PathValidator,
) {
	s.stateProvider = stateProvider
	s.serviceOperator = serviceOperator
	s.extProvider = extProvider
	s.taskProvider = taskProvider
	s.cronProvider = cronProvider
	s.settingsProvider = settingsProvider
	s.runtimeProvider = runtimeProvider
	s.systemProvider = systemProvider
	s.fileProvider = fileProvider
	s.authProvider = authProvider
	s.logProvider = logProvider
	s.watchProvider = watchProvider
	s.serviceHistoryGetter = serviceHistoryGetter
	s.eventRing = eventRing
	s.pathValidator = pathValidator
}

// panicRecoverer 自定义 panic 恢复中间件
// C-02-003 修复：chi 的 Recoverer 中间件 panic 时返回纯文本 "Internal Server Error"，
// 改为返回与 respondError 一致的 JSON 格式错误响应 {"error":{"code":"INTERNAL_ERROR",...}}
func panicRecoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				slog.Error("panic recovered",
					"error", rv,
					"stack", string(debug.Stack()),
					"method", r.Method,
					"path", r.URL.Path,
				)
				// H-04-001 修复：响应体固定返回 "internal server error"，不泄露 rv 值（rv 仅记录到 slog）
				err := errors.NewServiceError(errors.ErrInternal, "internal server error")
				errors.WriteErrorResponse(w, err)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// setupMiddleware 配置全局中间件。
func (s *Server) setupMiddleware() {
	r := s.router

	// chi 内置中间件
	// NOTE: 不使用 chiMiddleware.RealIP — 它会用 X-Forwarded-For 覆盖 r.RemoteAddr，
	// 导致 local_skip 认证模式可被伪造 IP 头绕过（H-05-001）。
	// 如需支持反向代理，应显式校验上游可信后再解析 XFF。
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Logger)
	// C-02-003 修复：使用自定义 panic 恢复中间件，返回 JSON 格式错误响应
	// （替代 chi Recoverer 的纯文本 "Internal Server Error"）
	r.Use(panicRecoverer)

	// 认证中间件
	if s.config != nil {
		authMode := s.config.Settings.AuthMode
		authToken := s.config.Settings.AuthToken
		localNetworks := s.config.Settings.LocalNetworks
		if authMode != "" {
			r.Use(AuthMiddleware(authMode, authToken, localNetworks))
		}
	}
}

// UpdateDiscovery 热重载后更新所有持有 Discovery 引用的 providers
// N-04-001 修复：API providers 持有 Discovery 指针值拷贝，reload 后需要显式更新
func (s *Server) UpdateDiscovery(d *watch.DiscoveryResult) {
	if d == nil {
		return
	}
	// 使用类型断言调用各 provider 的 SetDiscovery 方法
	if p, ok := s.stateProvider.(*CoreStateProvider); ok {
		p.SetDiscovery(d)
	}
	if p, ok := s.serviceOperator.(*CoreServiceOperator); ok {
		p.SetDiscovery(d)
	}
	if p, ok := s.extProvider.(*CoreExtensionProvider); ok {
		p.SetDiscovery(d)
	}
	if p, ok := s.cronProvider.(*CoreCronProvider); ok {
		p.SetDiscovery(d)
	}
	if p, ok := s.watchProvider.(*CoreWatchProvider); ok {
		p.SetDiscovery(d)
	}
}

// setupRoutes 注册所有 API 路由。
// REQ-I-001: 65 个 API 端点
func (s *Server) setupRoutes() {
	r := s.router

	r.Route("/api", func(r chi.Router) {
		// 健康检查
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			// N-01-I1 修复：设置 Content-Type 为 application/json
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// C-01-001: 记录写入错误
			if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
				slog.Warn("failed to write health check response", "error", err)
			}
		})

		// 认证
		// REQ-I-002: POST /api/auth/verify
		r.Post("/auth/verify", s.handleAuthVerify)

		// 服务管理
		// REQ-I-001: 服务 CRUD 端点
		r.Route("/services", func(r chi.Router) {
			r.Get("/", s.handleListServices)                 // GET /api/services
			r.Post("/", s.handleCreateService)               // POST /api/services
			r.Post("/import", s.handleImportService)         // POST /api/services/import
			r.Post("/import/confirm", s.handleImportConfirm) // POST /api/services/import/confirm
			r.Post("/start", s.handleStartAllServices)       // POST /api/services/start (批量启动)
			r.Post("/stop", s.handleStopAllServices)         // POST /api/services/stop (批量停止)

			r.Route("/{name}", func(r chi.Router) {
				r.Get("/", s.handleGetService)         // GET /api/services/{name}
				r.Put("/", s.handleUpdateService)      // PUT /api/services/{name}
				r.Delete("/", s.handleDeleteService)   // DELETE /api/services/{name}

				// 服务操作
				r.Post("/start", s.handleStartService)   // POST /api/services/{name}/start
				r.Post("/stop", s.handleStopService)     // POST /api/services/{name}/stop
				r.Post("/restart", s.handleRestartService) // POST /api/services/{name}/restart
				r.Post("/signal", s.handleSignalService) // POST /api/services/{name}/signal
				r.Post("/force-stop", s.handleForceStopService)     // POST /api/services/{name}/force-stop
				r.Post("/clear-failed", s.handleClearFailedService) // POST /api/services/{name}/clear-failed
				r.Put("/config", s.handleUpdateServiceConfig)       // PUT /api/services/{name}/config
				r.Put("/env", s.handleSaveServiceEnv)               // PUT /api/services/{name}/env

				// 服务日志
				r.Get("/logs", s.handleServiceLogs)         // GET /api/services/{name}/logs
				r.Get("/logs/search", s.handleSearchLogs)   // GET /api/services/{name}/logs/search

				// 服务资源
				r.Get("/resources", s.handleServiceResources) // GET /api/services/{name}/resources
				r.Get("/processes", s.handleServiceProcesses) // GET /api/services/{name}/processes

				// 服务历史
				r.Get("/history", s.handleServiceHistory)  // GET /api/services/{name}/history
				r.Get("/deaths", s.handleServiceDeaths)    // GET /api/services/{name}/deaths

				// 服务导入导出
				r.Get("/export", s.handleExportService)    // GET /api/services/{name}/export

				// 服务级扩展
				r.Route("/extensions", func(r chi.Router) {
					r.Get("/", s.handleListServiceExtensions)
					r.Post("/", s.handleCreateServiceExtension)

					r.Route("/{ext}", func(r chi.Router) {
						r.Get("/", s.handleGetServiceExtension)
						r.Put("/", s.handleUpdateServiceExtension)
						r.Delete("/", s.handleDeleteServiceExtension)
						r.Put("/env", s.handleSaveServiceExtensionEnv)
						r.Post("/run", s.handleRunServiceExtension)
					})
				})
			})
		})

		// 全局扩展
		r.Route("/extensions", func(r chi.Router) {
			r.Get("/", s.handleListExtensions)
			r.Post("/", s.handleCreateExtension)
			r.Post("/import", s.handleImportExtension)
			r.Post("/import/confirm", s.handleImportExtensionConfirm)

			r.Route("/{name}", func(r chi.Router) {
				r.Get("/", s.handleGetExtension)
				r.Put("/", s.handleUpdateExtension)
				r.Delete("/", s.handleDeleteExtension)
				r.Put("/env", s.handleSaveExtensionEnv)
				r.Post("/run", s.handleRunExtension)
				r.Get("/status", s.handleGetExtensionStatus)
				r.Get("/export", s.handleExportExtension)
			})

			// 任务
			r.Route("/runs", func(r chi.Router) {
				r.Get("/", s.handleListRuns)
				r.Delete("/", s.handleClearRuns)

				r.Route("/{runID}", func(r chi.Router) {
				r.Get("/", s.handleGetRun)
				r.Get("/logs", s.handleGetRunLogs)
				r.Delete("/logs", s.handleDeleteRunLogs)
				r.Post("/cancel", s.handleCancelRun)
			})
			})
		})

		// 定时任务
		r.Get("/cron", s.handleListCron)
		r.Get("/cron/history", s.handleCronHistory)

		// 事件流
		// REQ-I-003: 长轮询端点（挂载并发限制器：全局50/单客户端5）
		r.With(s.longPollLimiter.Middleware()).Get("/events", s.handleEvents)

		// 文件操作
		r.Route("/files", func(r chi.Router) {
			r.Get("/tree", s.handleFileTree)
			r.Get("/", s.handleReadFile)
			r.Put("/", s.handleWriteFile)
			r.Post("/", s.handleCreateFile)
			r.Delete("/", s.handleDeleteFile)
			r.Post("/move", s.handleMoveFile)
			r.Post("/upload", s.handleUploadFile)
			r.Get("/history", s.handleFileHistory)
			r.Post("/rollback", s.handleRollbackFile)
			r.Post("/validate", s.handleValidateFile)
			r.Post("/snapshot", s.handleSnapshotFile)
		})

		// 设置
		r.Route("/settings", func(r chi.Router) {
			r.Get("/", s.handleGetSettings)
			r.Put("/", s.handleUpdateSettings)
			r.Get("/env", s.handleGetEnv)
			r.Put("/env", s.handleUpdateEnv)
			r.Get("/runtimes", s.handleGetRuntimesConfig)
			r.Put("/runtimes", s.handleUpdateRuntimesConfig)
		})

		// 运行时
		r.Route("/runtimes", func(r chi.Router) {
			r.Get("/", s.handleListRuntimes)
			r.Post("/upload", s.handleUploadRuntime)

			r.Route("/{name}", func(r chi.Router) {
				r.Delete("/", s.handleDeleteRuntime)
			})
		})

		// 系统
		r.Route("/system", func(r chi.Router) {
			r.Get("/status", s.handleSystemStatus)
			r.Get("/diagnostic", s.handleDiagnostic)
			r.Get("/events/recent", s.handleRecentEvents)
		})

		// 热重载
		// N-04-002 修复：POST /api/reload 手动触发配置热重载
		r.Post("/reload", s.handleReload)
	})

	// N-01-001 修复：未知路由返回结构化JSON而非裸文本
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]any{
				"code":    "NOT_FOUND",
				"message": "endpoint not found: " + r.URL.Path,
			},
		})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]any{
				"code":    "METHOD_NOT_ALLOWED",
				"message": fmt.Sprintf("method %s not allowed for %s", r.Method, r.URL.Path),
			},
		})
	})
}

// Start 启动 HTTP 服务器。
func (s *Server) Start() error {
	addr := ":8080"
	if s.config != nil && s.config.Settings.HTTPListen != "" {
		addr = s.config.Settings.HTTPListen
	}

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	return s.httpServer.ListenAndServe()
}

// Stop 优雅关闭 HTTP 服务器。
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

// Router 返回底层 chi.Router，供外部使用（如静态文件服务）。
func (s *Server) Router() chi.Router {
	return s.router
}
