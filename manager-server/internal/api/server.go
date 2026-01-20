package api

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	navidrome "manager-server/internal/audio/navidrome"
	"manager-server/internal/config"
	"manager-server/internal/kb/ingest"
	"manager-server/internal/kb/ingest/clients"
	"manager-server/internal/kb/ingest/connectors"
	"manager-server/internal/logger"
	"manager-server/internal/mcp"
	"manager-server/internal/middleware"
	"manager-server/internal/repository"
	"manager-server/internal/security"
	"manager-server/internal/service"
	"manager-server/internal/storage"
	"manager-server/internal/vectorstore"
	"manager-server/migrations"
	"manager-server/webassets"
)

const apiBasePath = "/xiaozhi"

// Server 服务器结构体
type Server struct {
	config                 *config.Config
	db                     *gorm.DB
	router                 *gin.Engine
	authMiddleware         *middleware.AuthMiddleware
	serverSecretMiddleware *middleware.ServerSecretMiddleware
	handlers               *Handlers
	deviceHandlers         *DeviceHandlers
	adminHandlers          *AdminHandlers
	modelHandlers          *ModelHandlers
	agentHandlers          *AgentHandlers
	visionHandlers         *VisionHandlers
	configHandlers         *ConfigHandlers
	timbreHandlers         *TimbreHandlers
	serverManageHandlers   *ServerManageHandlers
	authHandlers           *AuthHandlers
	otaHandlers            *OTAHandlers
	otaMagHandlers         *OTAMagHandlers
	voicePrintHandlers     *VoicePrintHandlers
	mcpHandlers            *MCPHandlers
	memoryHandlers         *MemoryHandlers
	kbHandlers             *KBHandlers
	kbJobRunner            *service.KBJobRunner
	audioHandlers          *AudioHandlers
	audioJobRunner         *service.AudioJobRunner
	visionCleanupRunner    *service.VisionCleanupRunner
	mediaHandlers          *MediaHandlers
	workflowAPI            *WorkflowAPI
}

// NewServer 创建新的服务器实例
func NewServer(cfg *config.Config, db *gorm.DB) *Server {
	// 自动迁移数据库
	if err := migrations.AutoMigrate(db); err != nil {
		logger.Fatalf("Auto migration failed: %v", err)
	}

	// 初始化种子数据
	if err := migrations.SeedInitialData(db); err != nil {
		logger.Fatalf("Seed initial data failed: %v", err)
	}

	// 设置Gin模式
	gin.SetMode(cfg.Server.Mode)

	// 初始化仓储层
	userRepo := repository.NewSysUserRepository(db)
	userTokenRepo := repository.NewSysUserTokenRepository(db)
	deviceRepo := repository.NewDeviceRepository(db)
	paramsRepo := repository.NewSysParamsRepository(db)
	dictDataRepo := repository.NewSysDictDataRepository(db)
	dictTypeRepo := repository.NewSysDictTypeRepository(db)
	modelProviderRepo := repository.NewAIModelProviderRepository(db)
	modelConfigRepo := repository.NewAIModelConfigRepository(db)
	ttsVoiceRepo := repository.NewAITTSVoiceRepository(db)
	agentRepo := repository.NewAgentRepository(db)
	agentTemplateRepo := repository.NewAgentTemplateRepository(db)
	agentChatHistRepo := repository.NewAgentChatHistoryRepository(db)
	agentPluginMapRepo := repository.NewAgentPluginMappingRepository(db)
	agentVoicePrintRepo := repository.NewAgentVoicePrintRepository(db)
	otaRepo := repository.NewOtaRepository(db)
	kbRepo := repository.NewKBRepository(db)
	audioRepo := repository.NewAudioRepository(db)
	mediaRepo := repository.NewMediaRepository(db)
	workflowRepo := repository.NewWorkflowRepository(db)
	visionSourceRepo := repository.NewVisionSourceRepository(db)
	visionRuleRepo := repository.NewVisionRuleRepository(db)
	visionEventRepo := repository.NewVisionEventRepository(db)

	// 初始化服务层
	userService := service.NewSysUserService(userRepo, userTokenRepo)
	paramsService := service.NewSysParamsService(paramsRepo)
	deviceService := service.NewDeviceService(deviceRepo, paramsService)
	dictDataService := service.NewSysDictDataService(dictDataRepo, dictTypeRepo)
	dictTypeService := service.NewSysDictTypeService(dictTypeRepo, dictDataRepo)
	modelProviderService := service.NewAIModelProviderService(modelProviderRepo)
	modelConfigService := service.NewAIModelConfigService(modelConfigRepo, modelProviderRepo)
	ttsVoiceService := service.NewAITTSVoiceService(ttsVoiceRepo)
	timbreService := service.NewTimbreService(ttsVoiceRepo)
	agentService := service.NewAgentService(agentRepo, deviceRepo, agentChatHistRepo, agentPluginMapRepo, modelConfigService, timbreService, deviceService)
	agentTemplateService := service.NewAgentTemplateService(agentTemplateRepo)
	agentChatHistoryService := service.NewAgentChatHistoryService(agentChatHistRepo, deviceRepo)
	agentPluginMappingService := service.NewAgentPluginMappingService(agentPluginMapRepo)
	agentVoicePrintService := service.NewAgentVoicePrintService(agentVoicePrintRepo, agentRepo, paramsService)
	configService := service.NewConfigService(paramsRepo, deviceRepo, agentRepo, agentTemplateRepo, modelConfigRepo, modelProviderRepo, ttsVoiceRepo, agentPluginMapRepo, agentVoicePrintRepo, visionSourceRepo, visionRuleRepo)
	serverManageService := service.NewServerManageService(paramsRepo)
	tokenService := service.NewTokenService(userRepo, userTokenRepo)
	captchaService := service.NewCaptchaService()
	otaService := service.NewOtaService(otaRepo)
	cryptoKey := strings.TrimSpace(cfg.Vision.SecretKey)
	if cryptoKey == "" {
		cryptoKey = cfg.JWT.Secret
	}
	visionCrypto, err := security.NewCrypto(cryptoKey)
	if err != nil {
		logger.Warnf("初始化视觉密码加密失败: %v", err)
	}
	visionService := service.NewVisionService(visionSourceRepo, visionRuleRepo, visionEventRepo, mediaRepo, visionCrypto)
	visionCleanupRunner := service.NewVisionCleanupRunner(
		visionEventRepo,
		cfg.Vision.RetentionDays,
		cfg.Vision.CleanupIntervalMinutes,
	)
	if visionCleanupRunner != nil {
		visionCleanupRunner.Start()
	}
	memoryService, err := service.NewMemoryService(deviceRepo, cfg)
	if err != nil {
		logger.Warnf("初始化长记忆服务失败: %v", err)
	}
	var kbVectorStore vectorstore.Adapter = vectorstore.NewInMemoryAdapter()
	var kbStorage storage.DocumentStorage
	var kbPipeline ingest.DocumentPipeline
	kbEmbeddingResolver := service.NewKBEmbeddingResolver(kbRepo, modelConfigRepo, cfg.KnowledgeBase.ES8)

	if strings.EqualFold(cfg.KnowledgeBase.Storage.Provider, "local") {
		if localStore, err := storage.NewLocalStorage(cfg.KnowledgeBase.Storage.LocalPath); err != nil {
			logger.Warnf("初始化知识库本地存储失败: %v", err)
		} else {
			kbStorage = localStore
		}
	}

	if pipe, err := ingest.NewPipeline(context.Background()); err != nil {
		logger.Warnf("初始化知识库解析流水线失败: %v", err)
	} else {
		kbPipeline = pipe
	}

	provider := strings.TrimSpace(strings.ToLower(cfg.KnowledgeBase.VectorStore.Provider))
	switch provider {
	case "pgvector", "pg", "postgres", "postgresql":
		if store, err := vectorstore.NewPGVectorAdapter(db, kbEmbeddingResolver, cfg.KnowledgeBase.VectorStore.PGVector); err != nil {
			logger.Warnf("初始化pgvector向量存储失败: %v，回退到内存向量存储", err)
		} else {
			kbVectorStore = store
		}
	case "elasticsearch", "es":
		if cfg.KnowledgeBase.ES8.Enabled {
			if esStore, err := vectorstore.NewES8Adapter(context.Background(), cfg.KnowledgeBase.ES8, kbEmbeddingResolver); err != nil {
				logger.Warnf("初始化Elasticsearch向量存储失败: %v，回退到内存向量存储", err)
			} else {
				kbVectorStore = esStore
			}
		} else {
			logger.Warnf("已配置elasticsearch向量存储但未启用es8.enabled，回退到内存向量存储")
		}
	case "memory", "inmemory":
		// keep default in-memory store
	default:
		if cfg.KnowledgeBase.ES8.Enabled {
			if esStore, err := vectorstore.NewES8Adapter(context.Background(), cfg.KnowledgeBase.ES8, kbEmbeddingResolver); err != nil {
				logger.Warnf("初始化Elasticsearch向量存储失败: %v，回退到内存向量存储", err)
			} else {
				kbVectorStore = esStore
			}
		} else if provider != "" {
			logger.Warnf("未知的向量存储提供者: %s，回退到内存向量存储", cfg.KnowledgeBase.VectorStore.Provider)
		}
	}

	kbEvents := mcp.NewKBAgentNotifier(mcp.GlobalConnectionManager)
	jobEventHub := service.NewKBJobEventHub()
	eventSink := service.NewKBEventFanout(kbEvents, jobEventHub)
	quotaCfg := toQuotaConfig(cfg.KnowledgeBase.Ingestion.Quota)
	retryCfg := toRetryConfig(cfg.KnowledgeBase.Ingestion.Retry)
	ingestDefaults := ingest.Config{
		ChunkSize:    cfg.KnowledgeBase.Ingestion.DefaultChunkSize,
		ChunkOverlap: cfg.KnowledgeBase.Ingestion.DefaultChunkOverlap,
	}
	kbJobRunner := service.NewKBJobRunner(kbRepo, kbStorage, kbPipeline, kbVectorStore, eventSink, ingestDefaults, retryCfg)
	kbJobRunner.Start(2)
	maxUploadBytes := int64(cfg.KnowledgeBase.Storage.MaxFileSizeMB) * 1024 * 1024
	kbService := service.NewKBService(
		kbRepo,
		kbJobRunner,
		kbVectorStore,
		kbStorage,
		eventSink,
		maxUploadBytes,
		cfg.KnowledgeBase.Storage.AllowedFormats,
		quotaCfg,
		retryCfg,
	)

	var crawlerClient clients.CrawlerClient
	crawlerMode := strings.ToLower(strings.TrimSpace(cfg.KnowledgeBase.Ingestion.Crawler.Mode))
	requestTimeout := time.Duration(cfg.KnowledgeBase.Ingestion.Crawler.RequestTimeoutSeconds) * time.Second
	if requestTimeout <= 0 {
		requestTimeout = 30 * time.Second
	}
	switch crawlerMode {
	case "http", "remote":
		if strings.TrimSpace(cfg.KnowledgeBase.Ingestion.Crawler.BaseURL) != "" {
			crawlerClient = clients.NewHTTPCrawler(
				cfg.KnowledgeBase.Ingestion.Crawler.BaseURL,
				clients.WithHTTPCrawlerTimeout(requestTimeout),
				clients.WithHTTPCrawlerAPIKey(cfg.KnowledgeBase.Ingestion.Crawler.APIKey),
				clients.WithHTTPCrawlerUploadURL(cfg.KnowledgeBase.Ingestion.Crawler.UploadURL),
			)
		}
	}
	if crawlerClient == nil {
		crawlerClient = clients.NewEmbeddedCrawler(requestTimeout)
	}

	connectorConfigs := make(map[string]config.ConnectorConfig, len(cfg.KnowledgeBase.Ingestion.Connectors))
	for name, connectorCfg := range cfg.KnowledgeBase.Ingestion.Connectors {
		connectorConfigs[strings.ToLower(strings.TrimSpace(name))] = connectorCfg
	}
	if len(connectorConfigs) == 0 {
		connectorConfigs = map[string]config.ConnectorConfig{
			"url": {
				Enabled:     true,
				DisplayName: "Web URL",
				Categories:  []string{"basic"},
			},
			"rss": {
				Enabled:     true,
				DisplayName: "RSS Feed",
				Categories:  []string{"basic"},
			},
			"sitemap": {
				Enabled:     true,
				DisplayName: "Sitemap",
				Categories:  []string{"basic"},
			},
		}
	}
	connectorDefinitions := convertConnectorDefinitions(connectorConfigs)
	connectorList := connectors.BuilderWithConfig(crawlerClient, connectorConfigs)
	if len(connectorDefinitions) > 0 && len(connectorList) > 0 {
		available := make(map[string]struct{}, len(connectorList))
		for _, conn := range connectorList {
			if conn == nil {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(conn.Source()))
			if name != "" {
				available[name] = struct{}{}
			}
		}
		for name := range connectorDefinitions {
			if _, ok := available[strings.ToLower(strings.TrimSpace(name))]; !ok {
				delete(connectorDefinitions, name)
			}
		}
	}
	connectorRegistry := connectors.NewRegistry(connectorList)

	sessionTTL := time.Duration(cfg.KnowledgeBase.Ingestion.SessionTTLMinutes) * time.Minute
	kbIngestionService := service.NewKBIngestionService(
		kbRepo,
		kbService,
		connectorRegistry,
		kbStorage,
		sessionTTL,
		connectorDefinitions,
		quotaCfg,
		retryCfg,
		cfg.KnowledgeBase.Ingestion.Webhook.Enabled,
		cfg.KnowledgeBase.Ingestion.Webhook.SigningSecret,
		cfg.KnowledgeBase.Ingestion.Webhook.AllowedCIDRs,
	)

	var audioStorage storage.AudioStorage
	if strings.EqualFold(cfg.Audio.Storage.Provider, "local") {
		if store, err := storage.NewLocalAudioStorage(cfg.Audio.Storage.LocalPath); err != nil {
			logger.Warnf("初始化播客本地存储失败: %v", err)
		} else {
			audioStorage = store
		}
	}
	if audioStorage == nil {
		logger.Warnf("音频存储未正确配置，将导致上传功能不可用")
	}

	scanner := navidrome.NewDefaultScanner(&navidrome.CommandProbe{})
	var transcoder navidrome.Transcoder = navidrome.NoopTranscoder{}
	if cfg.Audio.Transcoding.Enabled {
		transcoder = &navidrome.FFMPEGTranscoder{}
	}
	audioCfg := service.AudioServiceConfig{
		MaxUploadBytes:     int64(cfg.Audio.Storage.MaxFileSizeMB) * 1024 * 1024,
		AllowedFormats:     cfg.Audio.Storage.AllowedFormats,
		StorageQuotaBytes:  int64(cfg.Audio.Storage.QuotaMB) * 1024 * 1024,
		DownloadTimeout:    time.Duration(cfg.Audio.DownloadTimeoutSeconds) * time.Second,
		TranscodingEnabled: cfg.Audio.Transcoding.Enabled,
		TranscodeFormat:    cfg.Audio.Transcoding.Format,
		TranscodeBitrate:   cfg.Audio.Transcoding.Bitrate,
		TokenSecret:        cfg.Audio.Token.Secret,
		TokenTTL:           time.Duration(cfg.Audio.Token.TTLSeconds) * time.Second,
	}
	audioService := service.NewAudioService(audioRepo, audioStorage, nil, scanner, transcoder, audioCfg)
	audioJobRunner := service.NewAudioJobRunner(audioService)
	audioService.SetJobRunner(audioJobRunner)
	audioJobRunner.Start(2)
	audioHandlers := NewAudioHandlers(audioService)

	var mediaStorage storage.MediaStorage
	switch strings.ToLower(strings.TrimSpace(cfg.Media.Storage.Provider)) {
	case "", "local":
		if store, err := storage.NewLocalMediaStorage(cfg.Media.Storage.LocalPath); err != nil {
			logger.Warnf("初始化媒体库本地存储失败: %v", err)
		} else {
			mediaStorage = store
		}
	default:
		logger.Warnf("未知的媒体存储提供者: %s，默认使用本地存储", cfg.Media.Storage.Provider)
		if store, err := storage.NewLocalMediaStorage(cfg.Media.Storage.LocalPath); err != nil {
			logger.Warnf("初始化媒体库本地存储失败: %v", err)
		} else {
			mediaStorage = store
		}
	}
	if mediaStorage == nil {
		logger.Warnf("媒体存储未正确配置，将导致上传功能不可用")
	}
	mediaService := service.NewMediaService(mediaRepo, deviceRepo, mediaStorage, cfg.Media)
	mediaHandlers := NewMediaHandlers(mediaService, paramsService)
	workflowRuntimeClient := service.NewWorkflowRuntimeClient(cfg.WorkflowRuntime)
	workflowService := service.NewWorkflowService(workflowRepo, workflowRuntimeClient)
	workflowAPI := NewWorkflowAPI(workflowService)

	// 初始化处理器
	handlers := NewHandlers(userService)
	handlers.deviceService = deviceService
	deviceHandlers := NewDeviceHandlers(deviceService, paramsService)
	adminHandlers := NewAdminHandlers(paramsService, dictDataService, dictTypeService)
	modelHandlers := NewModelHandlers(modelProviderService, modelConfigService, ttsVoiceService)
	agentHandlers := NewAgentHandlers(agentService, agentTemplateService, agentChatHistoryService, agentPluginMappingService, paramsService)
	visionHandlers := NewVisionHandlers(agentService, visionService)
	configHandlers := NewConfigHandlers(configService)
	timbreHandlers := NewTimbreHandlers(timbreService)
	serverManageHandlers := NewServerManageHandlers(serverManageService)
	authHandlers := NewAuthHandlers(userService, tokenService, captchaService, paramsService, dictDataService)
	otaHandlers := NewOTAHandlers(deviceService, paramsService)
	otaMagHandlers := NewOTAMagHandlers(otaService)
	voicePrintHandlers := NewVoicePrintHandlers(agentVoicePrintService)
	var memoryHandlers *MemoryHandlers
	if memoryService != nil {
		memoryHandlers = NewMemoryHandlers(memoryService, deviceService)
	}
	kbHandlers := NewKBHandlers(kbService, kbIngestionService, jobEventHub, deviceRepo)

	// 初始化MCP相关组件
	mcpHandlers := NewMCPHandlers(mcp.GlobalConnectionManager, mcp.GlobalWebSocketHandler, "", kbService)

	// 初始化认证中间件
	authMiddleware := middleware.NewAuthMiddleware(tokenService, paramsService)

	// 初始化服务器密钥中间件
	serverSecretMiddleware := middleware.NewServerSecretMiddleware(paramsService)

	server := &Server{
		config:                 cfg,
		db:                     db,
		router:                 gin.New(),
		authMiddleware:         authMiddleware,
		serverSecretMiddleware: serverSecretMiddleware,
		handlers:               handlers,
		deviceHandlers:         deviceHandlers,
		adminHandlers:          adminHandlers,
		modelHandlers:          modelHandlers,
		agentHandlers:          agentHandlers,
		visionHandlers:         visionHandlers,
		configHandlers:         configHandlers,
		timbreHandlers:         timbreHandlers,
		serverManageHandlers:   serverManageHandlers,
		authHandlers:           authHandlers,
		otaHandlers:            otaHandlers,
		otaMagHandlers:         otaMagHandlers,
		voicePrintHandlers:     voicePrintHandlers,
		mcpHandlers:            mcpHandlers,
		memoryHandlers:         memoryHandlers,
		kbHandlers:             kbHandlers,
		kbJobRunner:            kbJobRunner,
		audioHandlers:          audioHandlers,
		audioJobRunner:         audioJobRunner,
		visionCleanupRunner:    visionCleanupRunner,
		mediaHandlers:          mediaHandlers,
		workflowAPI:            workflowAPI,
	}

	// 设置路由
	server.setupRoutes()

	return server
}

// setupRoutes 设置路由
func (s *Server) setupRoutes() {
	s.router.RedirectTrailingSlash = false
	s.router.RedirectFixedPath = false

	// 添加中间件（跳过健康检查日志）
	s.router.Use(gin.LoggerWithConfig(gin.LoggerConfig{SkipPaths: []string{"/xiaozhi/health"}}))
	s.router.Use(gin.Recovery())

	// 添加非200状态码日志记录中间件
	s.router.Use(middleware.RequestResponseLogger())

	// 添加JSON Long类型转换中间件
	//s.router.Use(middleware.JSONLongConverterMiddleware())

	// 添加CORS中间件
	s.router.Use(func(c *gin.Context) {
		// 设置CORS头 - 对所有响应都添加
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,AccessToken,X-CSRF-Token,Authorization,Token,client-id,device-id,Client-Id,Device-Id")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()

		// 确保重定向响应也包含CORS头
		if c.Writer.Status() >= 300 && c.Writer.Status() < 400 {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type,AccessToken,X-CSRF-Token,Authorization,Token,client-id,device-id,Client-Id,Device-Id")
		}
	})

	// API 路由组
	apiGroup := s.router.Group(apiBasePath)
	{
		// 健康检查接口
		apiGroup.GET("/health", s.handlers.healthCheck)

		// 用户认证接口组（公开接口，无需认证）
		userGroup := apiGroup.Group("/user")
		{
			userGroup.GET("/captcha", s.authHandlers.generateCaptcha)
			userGroup.POST("/smsVerification", s.authHandlers.sendSmsVerification)
			userGroup.POST("/login", s.authHandlers.login)
			userGroup.POST("/register", s.authHandlers.register)
			userGroup.PUT("/retrieve-password", s.authHandlers.retrievePassword)
			userGroup.GET("/pub-config", s.authHandlers.getPublicConfig)
		}

		// 需要认证的用户接口
		authUserGroup := apiGroup.Group("/user")
		authUserGroup.Use(s.authMiddleware.Middleware())
		{
			authUserGroup.GET("/info", s.authHandlers.getUserInfo)
			authUserGroup.PUT("/change-password", s.authHandlers.changePassword)
		}

		// 系统管理接口组（保留原有接口）
		sysGroup := apiGroup.Group("/sys")
		{
			// 公开接口
			sysGroup.POST("/login", s.handlers.login)
		}

		// 需要认证的系统接口
		authSysGroup := apiGroup.Group("/sys")
		authSysGroup.Use(s.authMiddleware.Middleware())
		{
			// 用户管理接口
			authSysGroup.GET("/user/info", s.handlers.getUserInfo)
			authSysGroup.POST("/logout", s.handlers.logout)
		}

		// 设备管理接口组（需要认证）
		deviceGroup := apiGroup.Group("/device")
		deviceGroup.Use(s.authMiddleware.Middleware())
		{
			deviceGroup.POST("/register", s.deviceHandlers.registerDevice)
			deviceGroup.POST("/bind/:agentId/:deviceCode", s.deviceHandlers.bindDevice)
			deviceGroup.POST("/bind/:agentId", s.deviceHandlers.forwardToMqttGateway)
			deviceGroup.GET("/bind/:agentId", s.deviceHandlers.getUserDevices)
			deviceGroup.GET("/bind/:agentId/page", s.deviceHandlers.getUserDevicesPage)
			deviceGroup.GET("/info/:id", s.deviceHandlers.getDeviceInfo)            // 获取设备详细信息（支持系统密钥和用户token）
			deviceGroup.POST("/report-status", s.deviceHandlers.reportDeviceStatus) // 上报设备状态和使用时长（支持系统密钥和用户token）
			deviceGroup.POST("/unbind", s.deviceHandlers.unbindDevice)
			deviceGroup.PUT("/update/:id", s.deviceHandlers.updateDevice)
			deviceGroup.POST("/manual-add", s.deviceHandlers.manualAddDevice)
		}

		// 管理员接口组 - 对应原Java项目的AdminController（需要认证）
		adminGroup := apiGroup.Group("/admin")
		adminGroup.Use(s.authMiddleware.Middleware())
		{
			// 用户管理接口
			adminGroup.GET("/users", s.handlers.pageUsers)
			adminGroup.PUT("/users/:id", s.handlers.resetPassword)
			adminGroup.DELETE("/users/:id", s.handlers.deleteUser)
			adminGroup.PUT("/users/changeStatus/:status", s.handlers.changeUserStatus)

			// 设备管理（管理员视角）
			adminGroup.DELETE("/device/:id", s.deviceHandlers.deleteIfUnbound)

			// 设备管理接口（管理员视角）
			adminGroup.GET("/device/all", s.deviceHandlers.pageDevicesForAdmin)

			// 系统参数管理接口
			paramsGroup := adminGroup.Group("/params")
			{
				paramsGroup.GET("/page", s.adminHandlers.pageParams)
				paramsGroup.GET("/:id", s.adminHandlers.getParam)
				paramsGroup.POST("", s.adminHandlers.saveParam)
				paramsGroup.PUT("", s.adminHandlers.updateParam)
				paramsGroup.POST("/delete", s.adminHandlers.deleteParams)
			}

			// 字典数据管理接口
			dictGroup := adminGroup.Group("/dict")
			{
				dataGroup := dictGroup.Group("/data")
				{
					dataGroup.GET("/page", s.adminHandlers.pageDictData)
					dataGroup.GET("/:id", s.adminHandlers.getDictData)
					dataGroup.POST("/save", s.adminHandlers.saveDictData)
					dataGroup.PUT("/update", s.adminHandlers.updateDictData)
					dataGroup.POST("/delete", s.adminHandlers.deleteDictData)
					dataGroup.GET("/type/:dictType", s.adminHandlers.getDictDataByType)
				}

				// 字典类型分页
				typeGroup := dictGroup.Group("/type")
				{
					typeGroup.GET("/page", s.adminHandlers.pageDictType)
					typeGroup.GET("/:id", s.adminHandlers.getDictType)
					typeGroup.POST("/save", s.adminHandlers.saveDictType)
					typeGroup.PUT("/update", s.adminHandlers.updateDictType)
					typeGroup.POST("/delete", s.adminHandlers.deleteDictType)
				}
			}

			// 服务端管理接口
			serverGroup := adminGroup.Group("/server")
			{
				serverGroup.GET("/server-list", s.serverManageHandlers.getWsServerList)
				serverGroup.POST("/emit-action", s.serverManageHandlers.emitServerAction)
			}
		}

		// 模型管理接口组（需要认证）
		modelGroup := apiGroup.Group("/models")
		modelGroup.Use(s.authMiddleware.Middleware())
		{
			// 模型基本信息接口
			modelGroup.GET("/names", s.modelHandlers.getModelNames)
			modelGroup.GET("/llm/names", s.modelHandlers.getLlmModelNames)

			// 模型配置管理接口
			modelGroup.GET("/list", s.modelHandlers.getModelConfigList)
			modelGroup.GET("/config/:id", s.modelHandlers.getModelConfig)
			modelGroup.DELETE("/config/:id", s.modelHandlers.deleteModelConfig)
			// 与Java一致：支持 /models/{id} 删除
			modelGroup.DELETE("/:id", s.modelHandlers.deleteModelConfig)
			modelGroup.PUT("/enable/:id/:status", s.modelHandlers.enableModelConfig)
			modelGroup.PUT("/default/:id", s.modelHandlers.setDefaultModel)

			// 音色管理接口 - 与Java项目路径保持一致 (具体路由，放在前面)
			modelGroup.GET("/:modelId/voices", s.modelHandlers.getVoiceList)

			// 模型类型相关接口 (与Java项目路径保持一致，但统一参数名避免Gin冲突)
			modelGroup.GET("/:modelId/provideTypes", s.modelHandlers.getModelProviderList)
			modelGroup.POST("/:modelId/:provideCode", s.modelHandlers.addModelConfig)
			modelGroup.PUT("/:modelId/:provideCode/:id", s.modelHandlers.editModelConfig)

			// 根据模型ID获取模型详情 (通配路由，放在最后；统一参数名)
			modelGroup.GET("/:modelId", s.modelHandlers.getModelById)

			// 模型供应器管理接口
			providerGroup := modelGroup.Group("/provider")
			{
				providerGroup.GET("", s.modelHandlers.getProviderListPage)
				providerGroup.POST("", s.modelHandlers.addProvider)
				providerGroup.PUT("", s.modelHandlers.editProvider)
				providerGroup.POST("/delete", s.modelHandlers.deleteProvider)
				providerGroup.GET("/plugin/names", s.modelHandlers.getPluginNameList)
			}
		}

		// 智能体管理接口组（需要认证）
		agentGroup := apiGroup.Group("/agent")
		agentGroup.Use(s.authMiddleware.Middleware())
		{
			agentGroup.GET("/list", s.agentHandlers.getUserAgents)
			agentGroup.GET("/all", s.agentHandlers.adminAgentList)
			agentGroup.GET("/:id", s.agentHandlers.getAgentByID)
			agentGroup.POST("", s.agentHandlers.createAgent)
			agentGroup.PUT("/saveMemory/:macAddress", s.agentHandlers.updateAgentMemoryByMac)
			agentGroup.PUT("/:id", s.agentHandlers.updateAgent)
			agentGroup.PUT("/device/:deviceId/voice", s.agentHandlers.switchAgentVoice)
			agentGroup.GET("/device/:deviceId/voices", s.agentHandlers.getAgentVoiceOptions)
			agentGroup.DELETE("/:id", s.agentHandlers.deleteAgent)
			agentGroup.GET("/template", s.agentHandlers.getTemplateList)
			agentGroup.POST("/template", s.agentHandlers.createTemplate)
			agentGroup.PUT("/template/:id", s.agentHandlers.updateTemplate)
			agentGroup.DELETE("/template/:id", s.agentHandlers.deleteTemplate)
			agentGroup.GET("/:id/sessions", s.agentHandlers.getAgentSessions)
			agentGroup.GET("/:id/chat-history/:sessionId", s.agentHandlers.getAgentChatHistory)
			agentGroup.GET("/:id/chat-history/user", s.agentHandlers.getRecentlyFiftyByAgentID)
			agentGroup.POST("/audio/:audioId", s.agentHandlers.getAudioID)
			agentGroup.GET("/play/:uuid", s.agentHandlers.playAudio)

			chatHistoryGroup := agentGroup.Group("/chat-history")
			{
				chatHistoryGroup.POST("/report", s.agentHandlers.reportAgentChatHistory)
			}

			// MCP接入点
			mcpGroup := agentGroup.Group("/mcp")
			{
				mcpGroup.GET("/address/:agentId", s.agentHandlers.getAgentMcpAccessAddress)
				mcpGroup.GET("/tools/:agentId", s.agentHandlers.getAgentMcpToolsList)
			}

			// 声纹管理接口 - 与Java项目路径保持一致
			voicePrintGroup := agentGroup.Group("/voice-print")
			{
				voicePrintGroup.POST("", s.voicePrintHandlers.createVoicePrint)
				voicePrintGroup.GET("/list/:id", s.voicePrintHandlers.getVoicePrintList)
				voicePrintGroup.PUT("", s.voicePrintHandlers.updateVoicePrint)
				voicePrintGroup.DELETE("/:id", s.voicePrintHandlers.deleteVoicePrint)
			}

			visionGroup := agentGroup.Group("/:id/vision")
			{
				visionGroup.GET("/sources", s.visionHandlers.listSources)
				visionGroup.POST("/sources", s.visionHandlers.createSource)
				visionGroup.PATCH("/sources/:sourceId", s.visionHandlers.updateSource)
				visionGroup.DELETE("/sources/:sourceId", s.visionHandlers.deleteSource)
				visionGroup.GET("/rules", s.visionHandlers.listRules)
				visionGroup.POST("/rules", s.visionHandlers.createRule)
				visionGroup.PATCH("/rules/:ruleId", s.visionHandlers.updateRule)
				visionGroup.DELETE("/rules/:ruleId", s.visionHandlers.deleteRule)
				visionGroup.POST("/events/query", s.visionHandlers.queryEvents)
				visionGroup.GET("/events/:eventId", s.visionHandlers.getEvent)
			}
		}

		// 长记忆管理接口组（需要认证）
		if s.memoryHandlers != nil {
			memoryGroup := apiGroup.Group("/memory")
			memoryGroup.Use(s.authMiddleware.Middleware())
			{
				memoryGroup.GET("/device/:deviceId/profiles", s.memoryHandlers.getDeviceProfiles)
				memoryGroup.GET("/device/:deviceId/events", s.memoryHandlers.getDeviceEvents)
				memoryGroup.PUT("/device/:deviceId/profile", s.memoryHandlers.updateDeviceProfile)
				memoryGroup.DELETE("/device/:deviceId/profile/:profileId", s.memoryHandlers.deleteDeviceProfile)
				memoryGroup.GET("/device/:deviceId/context", s.memoryHandlers.getDeviceLongTermContext)
			}

			internalMemoryGroup := apiGroup.Group("/internal/memory")
			internalMemoryGroup.Use(s.serverSecretMiddleware.Middleware())
			{
				internalMemoryGroup.POST("/device/:deviceId/messages", s.memoryHandlers.storeDeviceMessages)
				internalMemoryGroup.GET("/device/:deviceId/profile", s.memoryHandlers.internalGetDeviceProfile)
				internalMemoryGroup.PUT("/device/:deviceId/profile", s.memoryHandlers.internalUpdateDeviceProfile)
				internalMemoryGroup.GET("/device/:deviceId/profiles", s.memoryHandlers.internalGetDeviceProfiles)
				internalMemoryGroup.GET("/device/:deviceId/events", s.memoryHandlers.internalGetDeviceEvents)
				internalMemoryGroup.GET("/device/:deviceId/context", s.memoryHandlers.internalGetDeviceLongTermContext)
			}
		}

		// 知识库接口组（需要认证）
		kbGroup := apiGroup.Group("/kb")
		kbGroup.Use(s.authMiddleware.Middleware())
		{
			s.kbHandlers.registerRoutes(kbGroup)
		}
		// 内部知识库接口，供后台服务使用，使用服务器密钥鉴权
		kbInternalGroup := apiGroup.Group("/internal/kb")
		kbInternalGroup.Use(s.serverSecretMiddleware.Middleware())
		{
			s.kbHandlers.registerInternalRoutes(kbInternalGroup)
		}

		internalVisionGroup := apiGroup.Group("/internal/vision")
		internalVisionGroup.Use(s.serverSecretMiddleware.Middleware())
		{
			internalVisionGroup.GET("/agents", s.visionHandlers.listAgentConfigs)
			internalVisionGroup.POST("/events", s.visionHandlers.reportEvent)
		}

		if s.audioHandlers != nil {
			audioGroup := apiGroup.Group("/audio")
			audioGroup.Use(s.authMiddleware.Middleware())
			{
				s.audioHandlers.registerRoutes(audioGroup)
			}

			subsonicGroup := apiGroup.Group("/audio/projects/:projectId/subsonic/rest")
			{
				s.audioHandlers.RegisterSubsonicRoutes(subsonicGroup)
			}
		}

		if s.mediaHandlers != nil {
			mediaGroup := apiGroup.Group("/media")
			mediaGroup.Use(s.authMiddleware.Middleware())
			{
				s.mediaHandlers.registerRoutes(mediaGroup)
			}

			publicMediaGroup := apiGroup.Group("/media/public")
			{
				s.mediaHandlers.registerPublicRoutes(publicMediaGroup)
			}

			internalMediaGroup := apiGroup.Group("/internal/media")
			internalMediaGroup.Use(s.serverSecretMiddleware.Middleware())
			{
				s.mediaHandlers.registerRoutes(internalMediaGroup)
			}
		}

		// 配置管理接口组（使用服务器密钥认证）
		configGroup := apiGroup.Group("/config")
		configGroup.Use(s.serverSecretMiddleware.Middleware())
		{
			configGroup.POST("/server-base", s.configHandlers.getServerConfig)
			configGroup.POST("/agent-models", s.configHandlers.getAgentModels)
		}

		// 音色管理接口组（需要认证）
		timbreGroup := apiGroup.Group("/ttsVoice")
		timbreGroup.Use(s.authMiddleware.Middleware())
		{
			timbreGroup.GET("", s.timbreHandlers.pageTimbre)
			timbreGroup.POST("", s.timbreHandlers.saveTimbre)
			timbreGroup.PUT("/:id", s.timbreHandlers.updateTimbre)
			timbreGroup.POST("/delete", s.timbreHandlers.deleteTimbre)
		}

		// OTA管理接口组（公开接口，无需认证）
		oaGroup := apiGroup.Group("/ota")
		{
			oaGroup.POST("", s.otaHandlers.checkOTAVersion)
			oaGroup.POST("/", s.otaHandlers.checkOTAVersion) // 添加带斜杠的路由避免重定向
			oaGroup.POST("/activate", s.otaHandlers.activateDevice)
			oaGroup.GET("", s.otaHandlers.getOTAStatus)
			oaGroup.GET("/", s.otaHandlers.getOTAStatus) // 添加带斜杠的路由避免重定向
		}

		// OTA固件管理接口组（需要认证，管理员）
		otaMagGroup := apiGroup.Group("/otaMag")
		otaMagGroup.Use(s.authMiddleware.Middleware())
		{
			otaMagGroup.GET("", s.otaMagHandlers.page)
			otaMagGroup.GET(":id", s.otaMagHandlers.get)
			otaMagGroup.GET("/getDownloadUrl/:id", s.otaMagHandlers.getDownloadUrl)
			otaMagGroup.GET("/download/:uuid", s.otaMagHandlers.download)
			otaMagGroup.POST("", s.otaMagHandlers.save)
			otaMagGroup.DELETE(":id", s.otaMagHandlers.delete)
			otaMagGroup.PUT(":id", s.otaMagHandlers.update)
		}

		if s.workflowAPI != nil {
			// 创建 /api 子组，然后在其下创建 /workflow 路由
			apiSubGroup := apiGroup.Group("/api")
			workflowGroup := apiSubGroup.Group("/workflow")
			workflowGroup.Use(s.authMiddleware.Middleware())
			s.workflowAPI.RegisterRoutes(workflowGroup)

			internalWorkflowGroup := apiSubGroup.Group("/internal/workflow")
			internalWorkflowGroup.Use(s.serverSecretMiddleware.Middleware())
			{
				internalWorkflowGroup.POST("/executions", s.workflowAPI.IngestWorkflowExecution)
			}
		}

		logger.Infof("📝 正在设置MCP路由...")
		s.setupMCPRoutes(apiGroup)
	}

	// 静态Web服务 - 服务前端页面
	s.setupWebStaticRoutes()
}

type assetLocation struct {
	fs         fs.FS
	distSubdir string
	display    string
}

func (l assetLocation) String() string {
	return l.display
}

func (l assetLocation) httpFS() http.FileSystem {
	return http.FS(l.fs)
}

func (l assetLocation) fileInfo(name string) (fs.FileInfo, error) {
	target := "."
	if name != "" && name != "." {
		target = path.Clean(name)
	}
	return fs.Stat(l.fs, target)
}

func (l assetLocation) hasDir(name string) bool {
	info, err := l.fileInfo(name)
	return err == nil && info.IsDir()
}

func (l assetLocation) hasFile(name string) bool {
	info, err := l.fileInfo(name)
	return err == nil && !info.IsDir()
}

func (l assetLocation) subdir(name string) (assetLocation, bool) {
	clean := path.Clean(name)
	if clean == "." || clean == "" {
		return l, true
	}

	sub, err := fs.Sub(l.fs, clean)
	if err != nil {
		return assetLocation{}, false
	}

	base := strings.TrimSuffix(l.display, "/")
	if base == "" {
		base = l.display
	}

	return assetLocation{
		fs:         sub,
		distSubdir: l.distSubdir,
		display:    base + "/" + clean,
	}, true
}

// setupWebStaticRoutes 设置静态Web服务路由
func (s *Server) setupWebStaticRoutes() {
	webRoot, ok := s.resolveWebRoot()
	if !ok {
		return
	}

	s.mountStaticAssets(webRoot)

	// web-cli 在 NoRoute 之前
	s.setupWebCliRoutes()

	// manager-console 在 SPA 兜底之前单独处理
	s.setupManagerConsoleRoutes()
	s.setupConsoleRootRedirect()

	// userapp（H5）静态与SPA路由
	s.setupUserAppRoutes()

	s.mountSpaFallback(webRoot)

	logger.Infof("🌐 Web interface available at http://localhost:%d", s.config.Server.Port)
	logger.Infof("📁 Static files served from: %s", webRoot)
}

// resolveAssetPath 从嵌入资源中解析资源路径
func (s *Server) resolveAssetPath(distSubdir, legacyPath, label string) (assetLocation, bool) {
	// 只使用嵌入式资产，不再尝试从磁盘加载
	embedded, err := webassets.Subdir(distSubdir)
	if err == nil {
		logger.Infof("Serving %s from embedded assets", label)
		return assetLocation{
			fs:         embedded,
			distSubdir: distSubdir,
			display:    fmt.Sprintf("embedded(dist/web/%s)", distSubdir),
		}, true
	}

	logger.Errorf("Failed to load %s from embedded assets: %v", label, err)
	return assetLocation{}, false
}

// resolveWebRoot 返回前端静态资源根路径
func (s *Server) resolveWebRoot() (assetLocation, bool) {
	if root, ok := s.resolveAssetPath("manager-console", "./web/manager-console/dist", "manager-console"); ok {
		return root, true
	}
	return s.resolveAssetPath("manager", "./web/manager", "web/manager")
}

// mountStaticAssets 注册静态资源路由
func (s *Server) mountStaticAssets(webRoot assetLocation) {
	mountDir := func(route, dir string) {
		if sub, ok := webRoot.subdir(dir); ok {
			s.router.StaticFS(route, sub.httpFS())
		}
	}

	mountFile := func(route, file string) {
		if webRoot.hasFile(file) {
			s.router.StaticFileFS(route, file, webRoot.httpFS())
		}
	}

	mountDir("/assets", "assets")
	mountDir("/js", "js")
	mountDir("/css", "css")
	mountDir("/img", "img")
	mountDir("/fonts", "fonts")
	mountDir("/static", "static")

	mountFile("/favicon.ico", "favicon.ico")
	mountFile("/service-worker.js", "service-worker.js")
	mountFile("/index.html", "index.html")
	mountFile("/offline.html", "offline.html")
	mountFile("/manifest.webmanifest", "manifest.webmanifest")
}

// mountSpaFallback 注册 SPA 兜底路由
func (s *Server) mountSpaFallback(webRoot assetLocation) {
	if !webRoot.hasFile("index.html") {
		logger.Warnf("index.html not found in %s, skip SPA fallback registration", webRoot)
		return
	}

	contextPath := strings.TrimSuffix(apiBasePath, "/")
	if contextPath != "" && !strings.HasPrefix(contextPath, "/") {
		contextPath = "/" + contextPath
	}
	isConsoleRoot := webRoot.distSubdir == "manager-console"

	allowedPrefixes := []string{}
	if isConsoleRoot {
		allowedPrefixes = append(allowedPrefixes, "/manager-console")
	}

	fsys := webRoot.httpFS()

	s.router.NoRoute(func(c *gin.Context) {
		originalPath := c.Request.URL.Path
		effectivePath := originalPath
		isContextPathRequest := false

		if contextPath != "" && contextPath != "/" && strings.HasPrefix(effectivePath, contextPath) {
			isContextPathRequest = true
			effectivePath = strings.TrimPrefix(effectivePath, contextPath)
			if effectivePath == "" {
				effectivePath = "/"
			} else if !strings.HasPrefix(effectivePath, "/") {
				effectivePath = "/" + effectivePath
			}
		}

		if strings.HasPrefix(effectivePath, "/js/") ||
			strings.HasPrefix(effectivePath, "/css/") ||
			strings.HasPrefix(effectivePath, "/img/") ||
			strings.HasPrefix(effectivePath, "/fonts/") ||
			strings.HasPrefix(effectivePath, "/static/") ||
			strings.HasSuffix(effectivePath, ".js") ||
			strings.HasSuffix(effectivePath, ".css") ||
			strings.HasSuffix(effectivePath, ".png") ||
			strings.HasSuffix(effectivePath, ".jpg") ||
			strings.HasSuffix(effectivePath, ".ico") ||
			strings.HasSuffix(effectivePath, ".woff") ||
			strings.HasSuffix(effectivePath, ".woff2") {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		serveSPA := !isConsoleRoot

		if isConsoleRoot {
			allowed := false
			for _, prefix := range allowedPrefixes {
				if prefix == "" {
					continue
				}
				if effectivePath == prefix || strings.HasPrefix(effectivePath, prefix+"/") {
					allowed = true
					break
				}
			}

			if !allowed {
				serveSPA = false
			} else {
				serveSPA = true
			}
		}

		if serveSPA {
			c.FileFromFS("index.html", fsys)
			return
		}

		if isContextPathRequest {
			c.JSON(http.StatusNotFound, gin.H{
				"code": http.StatusNotFound,
				"msg":  "API not found",
			})
			return
		}

		c.JSON(http.StatusNotFound, gin.H{
			"code": http.StatusNotFound,
			"msg":  "Page not found",
		})
	})
}

// setupWebCliRoutes 设置web-cli前端路径
func (s *Server) setupWebCliRoutes() {
	webCliRoot, ok := s.resolveAssetPath("web-cli", "./web/web-cli", "web-cli")
	if !ok {
		return
	}

	// 创建web-cli路由组，避免与NoRoute冲突
	webCliGroup := s.router.Group("/web-cli")
	{
		// 设置web-cli静态文件服务
		webCliGroup.StaticFS("/", webCliRoot.httpFS())
	}

	redirectToWebCli := func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/web-cli/")
	}

	s.router.GET("/test", redirectToWebCli)
	s.router.HEAD("/test", redirectToWebCli)

	logger.Infof("🌐 Web-CLI interface available at http://localhost:%d/web-cli/", s.config.Server.Port)
	logger.Infof("📁 Web-CLI files served from: %s", webCliRoot)
}

// setupManagerConsoleRoutes 设置 manager-console 前端路径
func (s *Server) setupManagerConsoleRoutes() {
	consoleRoot, ok := s.resolveAssetPath("manager-console", "./web/manager-console/dist", "manager-console")
	if !ok {
		return
	}

	if !consoleRoot.hasFile("index.html") {
		logger.Warnf("manager-console index not found in %s", consoleRoot)
		return
	}

	indexBytes, err := fs.ReadFile(consoleRoot.fs, "index.html")
	if err != nil {
		logger.Errorf("failed to load manager-console index: %v", err)
		return
	}

	version := computeManagerConsoleVersion(consoleRoot, indexBytes)
	versionedIndex := injectManagerConsoleVersion(indexBytes, version)

	logger.Infof("🪪 Manager console asset version: %s", version)

	serveIndex := func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", versionedIndex)
	}

	consoleFS := consoleRoot.httpFS()

	serveStaticOrIndex := func(c *gin.Context) {
		requestPath := strings.TrimPrefix(c.Param("path"), "/")
		if requestPath == "" {
			serveIndex(c)
			return
		}

		cleanPath := path.Clean(requestPath)
		if cleanPath == "." {
			serveIndex(c)
			return
		}

		if strings.HasPrefix(cleanPath, "..") {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if info, err := consoleRoot.fileInfo(cleanPath); err == nil && !info.IsDir() {
			c.Header("Cache-Control", "no-cache")
			c.FileFromFS(cleanPath, consoleFS)
			return
		}

		if strings.Contains(cleanPath, ".") {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		serveIndex(c)
	}

	s.router.GET("/manager-console", serveIndex)
	s.router.HEAD("/manager-console", serveIndex)

	consoleGroup := s.router.Group("/manager-console")
	{
		consoleGroup.GET("/*path", serveStaticOrIndex)
		consoleGroup.HEAD("/*path", serveStaticOrIndex)
	}

	logger.Infof("🌐 Manager console available at http://localhost:%d/manager-console/", s.config.Server.Port)
	logger.Infof("📁 Manager console files served from: %s", consoleRoot)
}

var managerConsoleAssetAttr = regexp.MustCompile(`(?i)(href|src)="(/manager-console/assets/[^"?]+\.(?:js|css))"`)

func injectManagerConsoleVersion(index []byte, version string) []byte {
	if version == "" {
		return index
	}

	suffix := "?v=" + version

	return managerConsoleAssetAttr.ReplaceAllFunc(index, func(attr []byte) []byte {
		matches := managerConsoleAssetAttr.FindSubmatch(attr)
		if len(matches) != 3 {
			return attr
		}

		key := string(matches[1])
		path := string(matches[2])
		return []byte(fmt.Sprintf(`%s="%s%s"`, key, path, suffix))
	})
}

func computeManagerConsoleVersion(root assetLocation, index []byte) string {
	hasher := sha1.New()
	if len(index) > 0 {
		_, _ = hasher.Write(index)
	}

	if entries, err := fs.ReadDir(root.fs, "assets"); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".js") {
				continue
			}
			data, err := fs.ReadFile(root.fs, path.Join("assets", name))
			if err != nil {
				continue
			}
			_, _ = hasher.Write(data)
		}
	}

	sum := hasher.Sum(nil)
	if len(sum) == 0 {
		return strconv.FormatInt(time.Now().Unix(), 10)
	}

	return hex.EncodeToString(sum)[:12]
}

func (s *Server) setupConsoleRootRedirect() {
	targetBase := "/manager-console/"

	redirectHandler := func(c *gin.Context) {
		c.Redirect(http.StatusFound, targetBase)
	}

	s.router.GET("/", redirectHandler)
	s.router.HEAD("/", redirectHandler)
}

// setupUserAppRoutes 设置 userapp(H5) 前端路径
func (s *Server) setupUserAppRoutes() {
	userAppRoot, ok := s.resolveAssetPath("userapp", "./web/userapp", "userapp")
	if !ok {
		logger.Warnf("[UserApp] Failed to resolve userapp assets")
		return
	}

	if !userAppRoot.hasFile("index.html") {
		logger.Warnf("[UserApp] index.html not found in %s", userAppRoot)
		return
	}

	indexBytes, err := fs.ReadFile(userAppRoot.fs, "index.html")
	if err != nil {
		logger.Errorf("[UserApp] Failed to load index.html: %v", err)
		return
	}

	userAppFS := userAppRoot.httpFS()

	serveIndex := func(c *gin.Context) {
		logger.Debugf("[UserApp] Serving index.html")
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexBytes)
	}

	// 统一处理器：既可返回静态文件，也支持SPA兜底
	serveStaticOrIndex := func(c *gin.Context) {
		reqPath := strings.TrimPrefix(c.Param("path"), "/")
		logger.Debugf("[UserApp] Serving path: %s", reqPath)

		if reqPath == "" {
			serveIndex(c)
			return
		}

		cleanPath := path.Clean(reqPath)
		if cleanPath == "." {
			serveIndex(c)
			return
		}

		if strings.HasPrefix(cleanPath, "..") {
			logger.Warnf("[UserApp] Bad request: path traversal attempt: %s", cleanPath)
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if info, err := userAppRoot.fileInfo(cleanPath); err == nil && !info.IsDir() {
			logger.Debugf("[UserApp] Serving file: %s", cleanPath)
			c.Header("Cache-Control", "no-cache")
			c.FileFromFS(cleanPath, userAppFS)
			return
		}

		// 对于不存在的文件，如果路径包含扩展名，返回404
		if strings.Contains(cleanPath, ".") {
			logger.Debugf("[UserApp] File not found: %s", cleanPath)
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		// SPA 兜底：返回 index.html
		logger.Debugf("[UserApp] Fallback to index.html for: %s", cleanPath)
		serveIndex(c)
	}

	// 使用 Group 方式注册路由，避免与其他路由冲突
	h5Group := s.router.Group("/h5")
	{
		h5Group.GET("", serveIndex)
		h5Group.HEAD("", serveIndex)
		h5Group.GET("/*path", serveStaticOrIndex)
		h5Group.HEAD("/*path", serveStaticOrIndex)
	}

	userappGroup := s.router.Group("/userapp")
	{
		userappGroup.GET("", serveIndex)
		userappGroup.HEAD("", serveIndex)
		userappGroup.GET("/*path", serveStaticOrIndex)
		userappGroup.HEAD("/*path", serveStaticOrIndex)
	}

	logger.Infof("🌐 UserApp (H5) available at http://localhost:%d/userapp/ and /h5/", s.config.Server.Port)
	logger.Infof("📁 UserApp files served from: %s", userAppRoot)
}

// setupMCPRoutes 设置MCP路由
func (s *Server) setupMCPRoutes(apiGroup *gin.RouterGroup) {
	// 注释掉根路径重定向，让前端SPA路由正常工作
	// s.router.GET("/", func(c *gin.Context) {
	//     c.Redirect(http.StatusFound, "/mcp_endpoint/")
	// })

	// MCP接入点路由组 (Legacy WebSocket-based endpoints)
	mcpGroup := apiGroup.Group("/mcp_endpoint")
	{
		// 根路径
		mcpGroup.GET("/", s.mcpHandlers.MCPRoot)

		// 健康检查
		mcpGroup.GET("/health", s.mcpHandlers.HealthCheck)

		// WebSocket端点
		mcpGroup.GET("/mcp/", s.mcpHandlers.WebSocketToolEndpoint)
		mcpGroup.GET("/call/", s.mcpHandlers.WebSocketRobotEndpoint)
	}

	// 新的MCP服务器路由组 (Proper MCP Protocol endpoints)
	// 创建 /api 子组，然后在其下创建 /mcp 路由
	apiSubGroup := apiGroup.Group("/api")
	mcpServerGroup := apiSubGroup.Group("/mcp")
	{
		// 根路径和信息
		mcpServerGroup.GET("/", s.mcpHandlers.MCPServerRoot)
		mcpServerGroup.GET("/info", s.mcpHandlers.MCPServerInfo)

		// 健康检查
		mcpServerGroup.GET("/health", s.mcpHandlers.MCPServerHealthCheck)

		// 标准MCP Streamable HTTP端点 - 单一端点支持POST和GET
		mcpServerGroup.POST("/streamable", s.mcpHandlers.MCPStreamableEndpoint)
		mcpServerGroup.GET("/streamable", s.mcpHandlers.MCPStreamableEndpoint)

		// 非标准MCP协议传输端点（保持向后兼容）
		mcpServerGroup.GET("/ws", s.mcpHandlers.MCPServerWebSocket) // WebSocket transport
		mcpServerGroup.GET("/sse", s.mcpHandlers.MCPServerSSE)
		mcpServerGroup.POST("/sse", s.mcpHandlers.MCPServerSSE)   // Server-Sent Events transport
		mcpServerGroup.POST("/http", s.mcpHandlers.MCPServerHTTP) // HTTP transport
		mcpServerGroup.PUT("/http", s.mcpHandlers.MCPServerHTTP)  // HTTP transport (alternative)
		//mcpServerGroup.GET("/http", s.mcpHandlers.MCPServerHTTP)  // HTTP transport

		// 工具管理端点
		mcpServerGroup.GET("/tools", s.mcpHandlers.MCPServerToolsList)
		mcpServerGroup.GET("/tools/details", s.mcpHandlers.MCPServerToolDetails)
		mcpServerGroup.GET("/tools/direct", s.mcpHandlers.MCPDirectTools)
		mcpServerGroup.POST("/tools/refresh", s.mcpHandlers.MCPRefreshTools)
	}

	// 打印MCP接入点信息
	s.printMCPEndpointInfo()
}

// printMCPEndpointInfo 打印MCP接入点信息
func (s *Server) printMCPEndpointInfo() {

	localIP := getLocalIP()
	port := s.config.Server.Port

	logger.Infof("=====下面的地址分别是智控台/单模块MCP接入点地址====")
	logger.Infof("智控台MCP参数配置: http://%s:%d%s/mcp_endpoint/health?key=%s",
		localIP, port, apiBasePath, "")

	// 现在直接使用agentID作为token，无需加密
	agentID := "single_module"
	logger.Infof("单模块部署MCP接入点: ws://%s:%d%s/mcp_endpoint/mcp/?token=%s",
		localIP, port, apiBasePath, agentID)

	logger.Infof("🔌 工具端WebSocket连接示例: ws://%s:%d%s/mcp_endpoint/mcp/?token=YOUR_AGENT_ID",
		localIP, port, apiBasePath)
	logger.Infof("🤖 小智端WebSocket连接示例: ws://%s:%d%s/mcp_endpoint/call/?token=YOUR_AGENT_ID",
		localIP, port, apiBasePath)

	logger.Infof("=====标准MCP协议服务器端点====")
	logger.Infof("🌟 MCP标准Streamable HTTP端点: http://%s:%d%s/api/mcp/streamable?token=YOUR_AGENT_ID",
		localIP, port, apiBasePath)
	logger.Infof("📋 MCP服务器信息: http://%s:%d%s/api/mcp/info",
		localIP, port, apiBasePath)
	logger.Infof("💚 MCP服务器健康检查: http://%s:%d%s/api/mcp/health?key=%s",
		localIP, port, apiBasePath, "")

	logger.Infof("=====非标准MCP协议端点（向后兼容）====")
	logger.Infof("🔌 MCP WebSocket端点: ws://%s:%d%s/api/mcp/ws?token=YOUR_AGENT_ID",
		localIP, port, apiBasePath)
	logger.Infof("📡 MCP SSE端点: http://%s:%d%s/api/mcp/sse?token=YOUR_AGENT_ID",
		localIP, port, apiBasePath)
	logger.Infof("🌐 MCP HTTP端点: http://%s:%d%s/api/mcp/http?token=YOUR_AGENT_ID",
		localIP, port, apiBasePath)
	logger.Infof("🛠️ MCP工具列表: http://%s:%d%s/api/mcp/tools?agent_id=YOUR_AGENT_ID",
		localIP, port, apiBasePath)
	logger.Infof("🔧 MCP直接工具列表: http://%s:%d%s/api/mcp/tools/direct",
		localIP, port, apiBasePath)
	logger.Infof("🔄 MCP刷新工具: http://%s:%d%s/api/mcp/tools/refresh",
		localIP, port, apiBasePath)

	logger.Infof("=====智能体工具现在可以直接通过MCP协议调用======")
	logger.Infof("=====工具命名格式: agent_id.tool_name ======")
}

// getLocalIP 获取本地IP地址
func getLocalIP() string {
	return "127.0.0.1"
}

// Shutdown gracefully stops background workers.
func (s *Server) Shutdown() {
	if s.kbJobRunner != nil {
		s.kbJobRunner.Stop()
	}
	if s.audioJobRunner != nil {
		s.audioJobRunner.Stop()
	}
	if s.visionCleanupRunner != nil {
		s.visionCleanupRunner.Stop()
	}
}

func convertConnectorDefinitions(configs map[string]config.ConnectorConfig) map[string]service.ConnectorDefinition {
	if len(configs) == 0 {
		return nil
	}
	result := make(map[string]service.ConnectorDefinition, len(configs))
	for name, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		def := service.ConnectorDefinition{
			Name:             name,
			DisplayName:      cfg.DisplayName,
			Description:      cfg.Description,
			Categories:       append([]string(nil), cfg.Categories...),
			Icon:             cfg.Icon,
			SupportsOAuth:    cfg.SupportsOAuth,
			OAuthProvider:    cfg.OAuthProvider,
			SupportsWebhook:  cfg.SupportsWebhook,
			DefaultParams:    cloneAnyMap(cfg.DefaultParams),
			DefaultMetadata:  cloneAnyMap(cfg.DefaultMetadata),
			ParamsSchema:     convertFieldConfigs(cfg.ParamsSchema),
			CredentialSchema: convertFieldConfigs(cfg.CredentialSchema),
			MetadataSchema:   convertFieldConfigs(cfg.MetadataSchema),
		}
		result[name] = def
	}
	return result
}

func convertFieldConfigs(fields []config.ConnectorFieldConfig) []service.ConnectorFieldDefinition {
	if len(fields) == 0 {
		return nil
	}
	result := make([]service.ConnectorFieldDefinition, 0, len(fields))
	for _, field := range fields {
		def := service.ConnectorFieldDefinition{
			Key:         field.Key,
			Label:       field.Label,
			Type:        field.Type,
			Required:    field.Required,
			Placeholder: field.Placeholder,
			Help:        field.Help,
			Default:     field.Default,
			Advanced:    field.Advanced,
		}
		if len(field.Options) > 0 {
			options := make([]service.ConnectorFieldOption, 0, len(field.Options))
			for _, opt := range field.Options {
				options = append(options, service.ConnectorFieldOption{Label: opt.Label, Value: opt.Value})
			}
			def.Options = options
		}
		result = append(result, def)
	}
	return result
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dup := make(map[string]any, len(src))
	for k, v := range src {
		dup[k] = v
	}
	return dup
}

func toQuotaConfig(cfg config.IngestionQuotaConfig) service.IngestionQuotaConfig {
	return service.IngestionQuotaConfig{
		MaxDocumentsPerDay: cfg.MaxDocumentsPerDay,
		MaxBytesPerDay:     cfg.MaxBytesPerDay,
		MaxConcurrentJobs:  cfg.MaxConcurrentJobs,
	}
}

func toRetryConfig(cfg config.IngestionRetryConfig) service.IngestionRetryConfig {
	return service.IngestionRetryConfig{
		MaxAttempts:    cfg.MaxAttempts,
		BackoffSeconds: append([]int(nil), cfg.BackoffSeconds...),
	}
}

// Run 启动服务器
func (s *Server) Run() error {
	addr := fmt.Sprintf(":%d", s.config.Server.Port)
	logger.Infof("Server starting on %s", addr)

	return s.router.Run(addr)
}

// Router exposes the underlying Gin engine so callers can wire custom servers.
func (s *Server) Router() *gin.Engine {
	return s.router
}
