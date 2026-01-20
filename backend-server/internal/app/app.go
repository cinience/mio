package app

import (
	"backend-server/internal/adapters/manager"
	"backend-server/internal/app/service"
	"backend-server/internal/config"
	"backend-server/internal/domain/vision"
	"backend-server/internal/infrastructure/errors"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/interfaces"
	"backend-server/internal/server/chat"
	chatsession "backend-server/internal/server/chat/session"
	"backend-server/internal/server/mqtt_server"
	"backend-server/internal/server/observability"
	"backend-server/internal/server/transport/mqtt_udp"
	"backend-server/internal/server/transport/types"
	"backend-server/internal/server/transport/websocket"
	"backend-server/internal/shared/cleanup"
	"backend-server/internal/shared/concurrency"
	"backend-server/internal/shared/patterns"
	"context"
	"time"
)

// App 重构后的应用程序，使用接口驱动和设计模式
type App struct {
	// 服务管理
	services       []interfaces.Service
	serviceFactory *patterns.ServiceFactory
	registry       *service.Registry

	// 组件
	wsServer            *websocket.WebSocketServer
	mqttUdpAdapter      *mqtt_udp.MqttUdpAdapter
	managerAPIService   manager_api.ManagerAPIService
	deviceStatusManager *manager_api.DeviceStatusManager
	sessionMonitor      *chatsession.SessionMonitor

	// 并发管理
	goroutineManager *concurrency.GoroutineManager
	workerPool       *concurrency.WorkerPool

	// 状态管理
	status string

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc

	// 日志
	logger interfaces.Logger

	// 追踪
	traceShutdown func(context.Context) error
}

// NewApp 使用构建者模式创建应用实例
func NewApp() *App {
	// 创建日志适配器 (适配现有日志)
	logger := observability.NewLogger()

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 创建应用实例
	app := &App{
		services:       make([]interfaces.Service, 0),
		serviceFactory: patterns.NewServiceFactory(),
		registry:       service.NewRegistry(),
		status:         "initializing",
		ctx:            ctx,
		cancel:         cancel,
		logger:         logger,
	}

	service.SetDefaultRegistry(app.registry)

	// 初始化并发管理器
	app.goroutineManager = concurrency.NewGoroutineManager(logger)
	app.workerPool = concurrency.NewWorkerPool(4, 100, logger, nil) // 4个worker，100个缓冲

	// 启动工作池
	app.workerPool.Start()

	cfg := config.GetConfig()

	// 初始化会话监控器
	sessionTimeout := time.Duration(cfg.Chat.SessionTimeoutMinutes) * time.Minute
	app.sessionMonitor = chatsession.NewSessionMonitor(10*time.Second, sessionTimeout)

	// 使用错误收集器处理初始化错误
	errorCollector := errors.NewErrorCollector()

	// 初始化Tracing
	if traceShutdown, err := observability.InitTracing(cfg, logger); err != nil {
		errorCollector.Add(errors.WrapError(err, errors.ErrorTypeInitialize, "TRACING_INIT_FAILED", "Tracing 初始化失败"))
	} else {
		app.traceShutdown = traceShutdown
	}

	// 初始化WebSocket服务器
	app.wsServer = app.newWebSocketServer()
	if app.wsServer != nil {
		log.Info("WebSocket服务器初始化成功")
	} else {
		errorCollector.Add(errors.NewInitializeError("WEBSOCKET_INIT_FAILED", "WebSocket服务器初始化失败"))
	}

	// 初始化MQTT UDP适配器
	mqttUdpAdapter, err := app.newMqttUdpAdapter()
	if err != nil {
		errorCollector.Add(errors.WrapError(err, errors.ErrorTypeInitialize, "MQTT_UDP_INIT_FAILED", "MQTT UDP适配器初始化失败"))
	} else {
		app.mqttUdpAdapter = mqttUdpAdapter
		if mqttUdpAdapter != nil {
			log.Info("MQTT UDP适配器初始化成功")
		}
	}

	// 初始化manager-api服务
	managerAPIService, err := app.newManagerAPIService()
	if err != nil {
		errorCollector.Add(errors.WrapError(err, errors.ErrorTypeInitialize, "MANAGER_API_INIT_FAILED", "Manager API服务初始化失败"))
	} else {
		app.managerAPIService = managerAPIService
		app.registry.SetManagerAPIService(managerAPIService)
		if managerAPIService != nil {
			log.Info("Manager API服务初始化成功")

			// 初始化设备状态管理器
			if service, ok := managerAPIService.(*manager_api.Service); ok {
				if httpClient := service.GetClient(); httpClient != nil {
					app.deviceStatusManager = manager_api.NewDeviceStatusManager(httpClient)
					app.registry.SetDeviceStatusManager(app.deviceStatusManager)
					log.Info("设备状态管理器初始化成功")
				}
			}
		}
	}

	if app.wsServer != nil {
		app.wsServer.SetManagerAPIService(app.managerAPIService)
	}

	// 检查初始化错误
	if errorCollector.HasErrors() {
		for _, err := range errorCollector.GetErrors() {
			log.Errorf("初始化错误: %v", err)
		}
		// 如果有严重错误，返回nil
		for _, err := range errorCollector.GetErrors() {
			if appErr, ok := err.(*errors.AppError); ok {
				if appErr.GetSeverity() == errors.SeverityCritical {
					return nil
				}
			}
		}
	}

	app.status = "initialized"
	return app
}

func (a *App) Run() {
	// 获取配置
	cfg := config.GetConfig()

	// 使用 goroutineManager 启动manager-api服务
	if a.managerAPIService != nil {
		a.goroutineManager.Go("manager-api-service", func(ctx context.Context) {
			if err := a.managerAPIService.Start(ctx); err != nil {
				log.Errorf("manager-api service start failed: %v", err)
			} else {
				log.Info("manager-api service started successfully")
			}
		})
	}

	if cfg.Vision.Ingest.Enabled && a.managerAPIService != nil {
		scheduler := vision.NewScheduler(a.managerAPIService, cfg.Vision)
		a.goroutineManager.Go("vision-scheduler", func(ctx context.Context) {
			scheduler.Run(ctx)
		})
	}

	// 使用 goroutineManager 启动WebSocket服务器
	a.goroutineManager.Go("websocket-server", func(ctx context.Context) {
		if err := a.wsServer.Start(); err != nil {
			log.Fatalf("websocket server start failed: %v", err)
		}
	})

	// 使用 goroutineManager 启动MQTT服务器
	if cfg.MQTTServer.Enable {
		a.goroutineManager.Go("mqtt-server", func(ctx context.Context) {
			err := a.startMqttServer()
			if err != nil {
				log.Errorf("startMqttServer err: %+v", err)
			}
		})
	}

	// 使用 goroutineManager 启动MQTT UDP适配器
	if a.mqttUdpAdapter != nil {
		time.Sleep(1 * time.Second)
		a.goroutineManager.Go("mqtt-udp-adapter", func(ctx context.Context) {
			a.mqttUdpAdapter.Start()
		})
	}

	// 启动结果处理goroutine
	a.goroutineManager.Go("worker-pool-results", func(ctx context.Context) {
		a.processWorkerPoolResults(ctx)
	})

	// 等待上下文取消
	<-a.ctx.Done()
}

// Close 关闭应用程序并清理资源
func (a *App) Close() {
	log.Info("正在关闭应用程序...")

	// 关闭设备状态管理器
	if a.deviceStatusManager != nil {
		a.deviceStatusManager.Close()
	}

	// 关闭manager-api服务
	if a.managerAPIService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.managerAPIService.Stop(ctx); err != nil {
			log.Errorf("关闭manager-api服务失败: %v", err)
		}
	}

	// 关闭Tracing
	if a.traceShutdown != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.traceShutdown(ctx); err != nil {
			log.Errorf("关闭Tracing失败: %v", err)
		}
	}

	log.Info("应用程序已关闭")
}

func (app *App) newMqttUdpAdapter() (*mqtt_udp.MqttUdpAdapter, error) {
	// 获取配置
	cfg := config.GetConfig()
	mqttCfg := cfg.MQTT

	if !mqttCfg.Enable {
		return nil, nil
	}

	mqttConfig := mqtt_udp.MqttConfig{
		Broker:   mqttCfg.Broker,
		Type:     mqttCfg.Type,
		Port:     mqttCfg.Port,
		ClientID: mqttCfg.ClientID,
		Username: mqttCfg.Username,
		Password: mqttCfg.Password,
	}

	udpServer, err := app.newUdpServer()
	if err != nil {
		return nil, err
	}

	return mqtt_udp.NewMqttUdpAdapter(
		&mqttConfig,
		mqtt_udp.WithUdpServer(udpServer),
		mqtt_udp.WithOnNewConnection(app.OnNewConnection),
	), nil
}

func (app *App) newUdpServer() (*mqtt_udp.UdpServer, error) {
	// 获取配置
	cfg := config.GetConfig()
	udpCfg := cfg.UDP

	udpServer := mqtt_udp.NewUDPServer(udpCfg.ListenPort, udpCfg.ExternalHost, udpCfg.ExternalPort)
	err := udpServer.Start()
	if err != nil {
		log.Fatalf("udpServer.Start err: %+v", err)
		return nil, err
	}
	log.Infof("UDP服务器启动成功: %s:%d (外部: %s:%d)",
		udpCfg.ListenHost, udpCfg.ListenPort,
		udpCfg.ExternalHost, udpCfg.ExternalPort)
	return udpServer, nil
}

func (app *App) newWebSocketServer() *websocket.WebSocketServer {
	// 获取配置
	cfg := config.GetConfig()
	wsCfg := cfg.WebSocket

	log.Infof("WebSocket服务器配置: %s:%d", wsCfg.Host, wsCfg.Port)
	return websocket.NewWebSocketServer(wsCfg.Port, websocket.WithOnNewConnection(app.OnNewConnection))
}

func (app *App) startMqttServer() error {
	return mqtt_server.StartMqttServer()
}

// 所有协议新连接都走这里
func (a *App) OnNewConnection(transport types.IConn) {
	deviceID := transport.GetDeviceID()

	// 创建聊天管理器任务
	task := &ChatManagerTask{
		deviceID:  deviceID,
		transport: transport,
		app:       a,
	}

	// 使用工作池处理任务
	if err := a.workerPool.Submit(task); err != nil {
		log.Errorf("提交聊天管理器任务失败: %v", err)
		// 如果工作池满了，回退到直接创建
		a.createChatManagerDirectly(deviceID, transport)
	}
}

// newManagerAPIService 创建manager-api服务
func (a *App) newManagerAPIService() (manager_api.ManagerAPIService, error) {
	// 获取配置
	cfg := config.GetConfig()
	apiCfg := cfg.ManagerAPI

	if !apiCfg.Enabled {
		log.Info("manager-api service is disabled")
		return nil, nil
	}

	// 创建manager-api配置
	managerConfig := &manager_api.Config{
		Enabled:                    apiCfg.Enabled,
		BaseURL:                    apiCfg.BaseURL,
		Secret:                     apiCfg.Secret,
		TimeoutSeconds:             apiCfg.TimeoutSeconds,
		RetryAttempts:              apiCfg.RetryAttempts,
		RetryDelayMs:               apiCfg.RetryDelayMs,
		MaxRetryDelayMs:            apiCfg.MaxRetryDelayMs,
		CacheTTLSeconds:            apiCfg.CacheTTLSeconds,
		HealthCheckIntervalSeconds: apiCfg.HealthCheckIntervalSeconds,
		BatchSize:                  apiCfg.BatchSize,
		FlushIntervalSeconds:       apiCfg.FlushIntervalSeconds,
		CircuitBreaker: manager_api.CircuitBreakerSettings{
			MaxFailures:         apiCfg.CircuitBreaker.MaxFailures,
			OpenTimeoutSeconds:  apiCfg.CircuitBreaker.OpenTimeoutSeconds,
			ResetTimeoutSeconds: apiCfg.CircuitBreaker.ResetTimeoutSeconds,
		},
		RateLimit: manager_api.RateLimitSettings{
			RequestsPerSecond: apiCfg.RateLimit.RequestsPerSecond,
			Burst:             apiCfg.RateLimit.Burst,
		},
	}

	service, err := manager_api.NewService(managerConfig)
	if err != nil {
		return nil, err
	}

	log.Infof("manager-api service initialized successfully: %s", apiCfg.BaseURL)
	return service, nil
}

// GetManagerAPIService 获取manager-api服务实例
func (a *App) GetManagerAPIService() manager_api.ManagerAPIService {
	return a.managerAPIService
}

// Shutdown 优雅关闭应用
func (a *App) Shutdown(ctx context.Context) error {
	log.Info("开始关闭应用服务...")

	// 取消应用上下文，通知所有goroutine停止
	a.cancel()

	// 关闭WebSocket服务器
	if a.wsServer != nil {
		log.Info("关闭WebSocket服务器...")
		if err := a.wsServer.Shutdown(ctx); err != nil {
			log.Errorf("关闭WebSocket服务器失败: %v", err)
		} else {
			log.Info("WebSocket服务器已关闭")
		}
	}

	// 关闭MQTT UDP适配器
	if a.mqttUdpAdapter != nil {
		log.Info("关闭MQTT UDP适配器...")
		if err := a.mqttUdpAdapter.Shutdown(ctx); err != nil {
			log.Errorf("关闭MQTT UDP适配器失败: %v", err)
		} else {
			log.Info("MQTT UDP适配器已关闭")
		}
	}

	// 关闭工作池
	if a.workerPool != nil {
		log.Info("关闭工作池...")
		if err := a.workerPool.Stop(10 * time.Second); err != nil {
			log.Errorf("关闭工作池失败: %v", err)
		} else {
			log.Info("工作池已关闭")
		}
	}

	// 关闭设备状态管理器
	if a.deviceStatusManager != nil {
		log.Info("关闭设备状态管理器...")
		a.deviceStatusManager.Close()
		a.registry.SetDeviceStatusManager(nil)
		log.Info("设备状态管理器已关闭")
	}

	// 关闭manager-api服务
	if a.managerAPIService != nil {
		log.Info("关闭manager-api服务...")
		if err := a.managerAPIService.Stop(ctx); err != nil {
			log.Errorf("关闭manager-api服务失败: %v", err)
		} else {
			log.Info("manager-api服务已关闭")
		}
		a.registry.SetManagerAPIService(nil)
	}

	// 关闭Tracing
	if a.traceShutdown != nil {
		log.Info("关闭Tracing...")
		if err := a.traceShutdown(ctx); err != nil {
			log.Errorf("关闭Tracing失败: %v", err)
		}
	}

	// 停止会话监控器
	if a.sessionMonitor != nil {
		log.Info("停止会话监控器...")
		a.sessionMonitor.Stop()
	}

	// 关闭goroutine管理器
	if a.goroutineManager != nil {
		log.Info("关闭goroutine管理器...")
		if err := a.goroutineManager.Shutdown(15 * time.Second); err != nil {
			log.Errorf("关闭goroutine管理器失败: %v", err)
		} else {
			log.Info("goroutine管理器已关闭")
		}
	}

	// 执行全局资源清理 - 通用的清理机制
	log.Info("执行全局资源清理...")
	if err := cleanup.Cleanup(); err != nil {
		log.Errorf("全局资源清理过程中出现错误: %v", err)
		// 不返回错误，继续完成其他清理工作
	} else {
		log.Info("全局资源清理完成")
	}

	log.Info("应用服务关闭完成")
	return nil
}

// ChatManagerTask 聊天管理器任务
type ChatManagerTask struct {
	deviceID  string
	transport types.IConn
	app       *App
}

// Execute 执行聊天管理器创建任务
func (t *ChatManagerTask) Execute(ctx context.Context) error {
	chatManager, err := chat.NewChatManager(
		t.deviceID,
		t.transport,
		t.app.GetManagerAPIService(),
		chat.WithChatSessionMonitor(t.app.sessionMonitor),
		chat.WithServiceRegistry(t.app.registry),
	)
	if err != nil {
		return err
	}

	// 使用goroutineManager启动聊天管理器
	t.app.goroutineManager.Go("chat-manager-"+t.deviceID, func(ctx context.Context) {
		chatManager.Start()
	})

	return nil
}

// GetID 获取任务ID
func (t *ChatManagerTask) GetID() string {
	return "chat-manager-" + t.deviceID
}

// GetPriority 获取任务优先级
func (t *ChatManagerTask) GetPriority() int {
	return 1 // 普通优先级
}

// createChatManagerDirectly 直接创建聊天管理器（回退方案）
func (a *App) createChatManagerDirectly(deviceID string, transport types.IConn) {
	chatManager, err := chat.NewChatManager(
		deviceID,
		transport,
		a.GetManagerAPIService(),
		chat.WithChatSessionMonitor(a.sessionMonitor),
		chat.WithServiceRegistry(a.registry),
	)
	if err != nil {
		log.Errorf("创建chatManager失败: %v", err)
		return
	}

	// 使用goroutineManager启动
	a.goroutineManager.Go("chat-manager-direct-"+deviceID, func(ctx context.Context) {
		chatManager.Start()
	})
}

// processWorkerPoolResults 处理工作池结果
func (a *App) processWorkerPoolResults(ctx context.Context) {
	const (
		successLogBatchSize   = 10
		successLogMaxInterval = 5 * time.Second
	)

	flushSuccess := func(count int, reason string) {
		if count == 0 {
			return
		}
		log.Debugf("任务执行成功 ×%d (%s)", count, reason)
	}

	var (
		pendingSuccess   int
		pendingStartTime time.Time
	)

	resetPending := func() {
		pendingSuccess = 0
		pendingStartTime = time.Time{}
	}

	for {
		select {
		case <-ctx.Done():
			flushSuccess(pendingSuccess, "shutdown")
			log.Info("工作池结果处理goroutine停止")
			return
		case result, ok := <-a.workerPool.Results():
			if !ok {
				flushSuccess(pendingSuccess, "worker pool closed")
				log.Info("工作池结果通道关闭，停止处理")
				return
			}
			if result.Error != nil {
				log.Errorf("任务 %s 执行失败: %v", result.TaskID, result.Error)
			} else {
				pendingSuccess++
				if pendingStartTime.IsZero() {
					pendingStartTime = time.Now()
				}
				if pendingSuccess >= successLogBatchSize || time.Since(pendingStartTime) >= successLogMaxInterval {
					flushSuccess(pendingSuccess, "batch")
					resetPending()
				}
			}
		}
	}
}

// GetGoroutineStats 获取goroutine统计信息
func (a *App) GetGoroutineStats() (total, running int64) {
	if a.goroutineManager != nil {
		return a.goroutineManager.Stats()
	}
	return 0, 0
}

// GetWorkerPoolStats 获取工作池统计信息
func (a *App) GetWorkerPoolStats() map[string]int64 {
	if a.workerPool != nil {
		return a.workerPool.Stats()
	}
	return make(map[string]int64)
}

// GetStatus 获取应用状态
func (a *App) GetStatus() string {
	return a.status
}
