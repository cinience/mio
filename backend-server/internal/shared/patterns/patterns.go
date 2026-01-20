// Package patterns 提供常用设计模式的实现
package patterns

import (
	"backend-server/internal/interfaces"
	"context"
	"fmt"
	"sync"
)

// =============================================================================
// 工厂模式 (Factory Pattern)
// =============================================================================

// ServiceFactory 服务工厂
type ServiceFactory struct {
	mu       sync.RWMutex
	creators map[string]ServiceCreator
}

// ServiceCreator 服务创建器函数类型
type ServiceCreator func(config interface{}) (interfaces.Service, error)

// NewServiceFactory 创建服务工厂
func NewServiceFactory() *ServiceFactory {
	return &ServiceFactory{
		creators: make(map[string]ServiceCreator),
	}
}

// Register 注册服务创建器
func (f *ServiceFactory) Register(serviceType string, creator ServiceCreator) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creators[serviceType] = creator
}

// Create 创建服务实例
func (f *ServiceFactory) Create(serviceType string, config interface{}) (interfaces.Service, error) {
	f.mu.RLock()
	creator, exists := f.creators[serviceType]
	f.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unsupported service type: %s", serviceType)
	}

	return creator(config)
}

// GetSupportedTypes 获取支持的类型
func (f *ServiceFactory) GetSupportedTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]string, 0, len(f.creators))
	for t := range f.creators {
		types = append(types, t)
	}
	return types
}

// AIProviderFactory AI提供者工厂
type AIProviderFactory struct {
	mu       sync.RWMutex
	creators map[string]AIProviderCreator
}

// AIProviderCreator AI提供者创建器函数类型
type AIProviderCreator func(config interface{}) (interfaces.AIProvider, error)

// NewAIProviderFactory 创建AI提供者工厂
func NewAIProviderFactory() *AIProviderFactory {
	return &AIProviderFactory{
		creators: make(map[string]AIProviderCreator),
	}
}

// Register 注册AI提供者创建器
func (f *AIProviderFactory) Register(providerType string, creator AIProviderCreator) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creators[providerType] = creator
}

// Create 创建AI提供者实例
func (f *AIProviderFactory) Create(providerType string, config interface{}) (interfaces.AIProvider, error) {
	f.mu.RLock()
	creator, exists := f.creators[providerType]
	f.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unsupported AI provider type: %s", providerType)
	}

	return creator(config)
}

// GetSupportedTypes 获取支持的类型
func (f *AIProviderFactory) GetSupportedTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]string, 0, len(f.creators))
	for t := range f.creators {
		types = append(types, t)
	}
	return types
}

// =============================================================================
// 构建者模式 (Builder Pattern)
// =============================================================================

// AppBuilder 应用程序构建者
type AppBuilder struct {
	config       map[string]interface{}
	services     []interfaces.Service
	middlewares  []interfaces.Middleware
	interceptors []func(context.Context) context.Context
	validators   []interfaces.Validator
}

// NewAppBuilder 创建应用构建者
func NewAppBuilder() *AppBuilder {
	return &AppBuilder{
		config:       make(map[string]interface{}),
		services:     make([]interfaces.Service, 0),
		middlewares:  make([]interfaces.Middleware, 0),
		interceptors: make([]func(context.Context) context.Context, 0),
		validators:   make([]interfaces.Validator, 0),
	}
}

// WithConfig 添加配置
func (b *AppBuilder) WithConfig(key string, value interface{}) *AppBuilder {
	b.config[key] = value
	return b
}

// WithService 添加服务
func (b *AppBuilder) WithService(service interfaces.Service) *AppBuilder {
	b.services = append(b.services, service)
	return b
}

// WithMiddleware 添加中间件
func (b *AppBuilder) WithMiddleware(middleware interfaces.Middleware) *AppBuilder {
	b.middlewares = append(b.middlewares, middleware)
	return b
}

// WithInterceptor 添加拦截器
func (b *AppBuilder) WithInterceptor(interceptor func(context.Context) context.Context) *AppBuilder {
	b.interceptors = append(b.interceptors, interceptor)
	return b
}

// WithValidator 添加验证器
func (b *AppBuilder) WithValidator(validator interfaces.Validator) *AppBuilder {
	b.validators = append(b.validators, validator)
	return b
}

// Build 构建应用实例
func (b *AppBuilder) Build() (*App, error) {
	app := &App{
		config:       b.config,
		services:     b.services,
		middlewares:  b.middlewares,
		interceptors: b.interceptors,
		validators:   b.validators,
		status:       "initialized",
	}

	// 验证构建结果
	if err := app.validate(); err != nil {
		return nil, fmt.Errorf("app validation failed: %w", err)
	}

	return app, nil
}

// Reset 重置构建者
func (b *AppBuilder) Reset() *AppBuilder {
	b.config = make(map[string]interface{})
	b.services = make([]interfaces.Service, 0)
	b.middlewares = make([]interfaces.Middleware, 0)
	b.interceptors = make([]func(context.Context) context.Context, 0)
	b.validators = make([]interfaces.Validator, 0)
	return b
}

// App 应用程序实例
type App struct {
	config       map[string]interface{}
	services     []interfaces.Service
	middlewares  []interfaces.Middleware
	interceptors []func(context.Context) context.Context
	validators   []interfaces.Validator
	status       string
	mu           sync.RWMutex
}

// validate 验证应用配置
func (a *App) validate() error {
	// 基本验证逻辑
	if len(a.services) == 0 {
		return fmt.Errorf("at least one service is required")
	}
	return nil
}

// Start 启动应用
func (a *App) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.status != "initialized" {
		return fmt.Errorf("app is not in initialized state")
	}

	// 启动所有服务
	for _, service := range a.services {
		if err := service.Start(ctx); err != nil {
			return fmt.Errorf("failed to start service: %w", err)
		}
	}

	a.status = "running"
	return nil
}

// Stop 停止应用
func (a *App) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.status != "running" {
		return fmt.Errorf("app is not running")
	}

	// 停止所有服务
	for i := len(a.services) - 1; i >= 0; i-- {
		if err := a.services[i].Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop service: %w", err)
		}
	}

	a.status = "stopped"
	return nil
}

// GetStatus 获取状态
func (a *App) GetStatus() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// =============================================================================
// 装饰器模式 (Decorator Pattern)
// =============================================================================

// ServiceDecorator 服务装饰器基类
type ServiceDecorator struct {
	service interfaces.Service
}

// NewServiceDecorator 创建服务装饰器
func NewServiceDecorator(service interfaces.Service) *ServiceDecorator {
	return &ServiceDecorator{service: service}
}

// Start 启动服务
func (d *ServiceDecorator) Start(ctx context.Context) error {
	return d.service.Start(ctx)
}

// Stop 停止服务
func (d *ServiceDecorator) Stop(ctx context.Context) error {
	return d.service.Stop(ctx)
}

// IsRunning 检查是否运行中
func (d *ServiceDecorator) IsRunning() bool {
	return d.service.IsRunning()
}

// HealthCheck 健康检查
func (d *ServiceDecorator) HealthCheck() error {
	return d.service.HealthCheck()
}

// LoggingServiceDecorator 日志装饰器
type LoggingServiceDecorator struct {
	*ServiceDecorator
	logger interfaces.Logger
}

// NewLoggingServiceDecorator 创建日志装饰器
func NewLoggingServiceDecorator(service interfaces.Service, logger interfaces.Logger) *LoggingServiceDecorator {
	return &LoggingServiceDecorator{
		ServiceDecorator: NewServiceDecorator(service),
		logger:           logger,
	}
}

// Start 启动服务（带日志）
func (d *LoggingServiceDecorator) Start(ctx context.Context) error {
	d.logger.Info("Starting service...")
	err := d.ServiceDecorator.Start(ctx)
	if err != nil {
		d.logger.Errorf("Failed to start service: %v", err)
	} else {
		d.logger.Info("Service started successfully")
	}
	return err
}

// Stop 停止服务（带日志）
func (d *LoggingServiceDecorator) Stop(ctx context.Context) error {
	d.logger.Info("Stopping service...")
	err := d.ServiceDecorator.Stop(ctx)
	if err != nil {
		d.logger.Errorf("Failed to stop service: %v", err)
	} else {
		d.logger.Info("Service stopped successfully")
	}
	return err
}

// MetricsServiceDecorator 监控装饰器
type MetricsServiceDecorator struct {
	*ServiceDecorator
	metrics interfaces.MetricsCollector
}

// NewMetricsServiceDecorator 创建监控装饰器
func NewMetricsServiceDecorator(service interfaces.Service, metrics interfaces.MetricsCollector) *MetricsServiceDecorator {
	return &MetricsServiceDecorator{
		ServiceDecorator: NewServiceDecorator(service),
		metrics:          metrics,
	}
}

// Start 启动服务（带监控）
func (d *MetricsServiceDecorator) Start(ctx context.Context) error {
	counter := d.metrics.Counter("service_start_attempts", map[string]string{
		"service_type": fmt.Sprintf("%T", d.service),
	})
	counter.Increment(1)

	err := d.ServiceDecorator.Start(ctx)

	if err != nil {
		errorCounter := d.metrics.Counter("service_start_errors", map[string]string{
			"service_type": fmt.Sprintf("%T", d.service),
		})
		errorCounter.Increment(1)
	} else {
		successCounter := d.metrics.Counter("service_start_success", map[string]string{
			"service_type": fmt.Sprintf("%T", d.service),
		})
		successCounter.Increment(1)
	}

	return err
}

// =============================================================================
// 策略模式 (Strategy Pattern)
// =============================================================================

// ProcessingStrategy 处理策略接口
type ProcessingStrategy interface {
	Process(ctx context.Context, data interface{}) (interface{}, error)
	GetName() string
}

// StrategyContext 策略上下文
type StrategyContext struct {
	strategy ProcessingStrategy
}

// NewStrategyContext 创建策略上下文
func NewStrategyContext(strategy ProcessingStrategy) *StrategyContext {
	return &StrategyContext{strategy: strategy}
}

// SetStrategy 设置策略
func (c *StrategyContext) SetStrategy(strategy ProcessingStrategy) {
	c.strategy = strategy
}

// Execute 执行策略
func (c *StrategyContext) Execute(ctx context.Context, data interface{}) (interface{}, error) {
	if c.strategy == nil {
		return nil, fmt.Errorf("no strategy set")
	}
	return c.strategy.Process(ctx, data)
}

// GetCurrentStrategy 获取当前策略
func (c *StrategyContext) GetCurrentStrategy() string {
	if c.strategy == nil {
		return "none"
	}
	return c.strategy.GetName()
}

// =============================================================================
// 单例模式 (Singleton Pattern)
// =============================================================================

// Singleton 单例接口
type Singleton interface {
	GetInstance() interface{}
}

// lazySingleton 懒加载单例
type lazySingleton struct {
	instance interface{}
	once     sync.Once
	creator  func() interface{}
}

// NewLazySingleton 创建懒加载单例
func NewLazySingleton(creator func() interface{}) Singleton {
	return &lazySingleton{
		creator: creator,
	}
}

// GetInstance 获取实例
func (s *lazySingleton) GetInstance() interface{} {
	s.once.Do(func() {
		s.instance = s.creator()
	})
	return s.instance
}

// =============================================================================
// 观察者模式 (Observer Pattern)
// =============================================================================

// Observer 观察者接口
type Observer interface {
	Update(event interface{}) error
}

// Subject 主题接口
type Subject interface {
	Subscribe(observer Observer)
	Unsubscribe(observer Observer)
	Notify(event interface{}) error
}

// EventSubject 事件主题实现
type EventSubject struct {
	observers []Observer
	mu        sync.RWMutex
}

// NewEventSubject 创建事件主题
func NewEventSubject() *EventSubject {
	return &EventSubject{
		observers: make([]Observer, 0),
	}
}

// Subscribe 订阅观察者
func (s *EventSubject) Subscribe(observer Observer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observers = append(s.observers, observer)
}

// Unsubscribe 取消订阅观察者
func (s *EventSubject) Unsubscribe(observer Observer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, obs := range s.observers {
		if obs == observer {
			s.observers = append(s.observers[:i], s.observers[i+1:]...)
			break
		}
	}
}

// Notify 通知所有观察者
func (s *EventSubject) Notify(event interface{}) error {
	s.mu.RLock()
	observers := make([]Observer, len(s.observers))
	copy(observers, s.observers)
	s.mu.RUnlock()

	for _, observer := range observers {
		if err := observer.Update(event); err != nil {
			return fmt.Errorf("observer update failed: %w", err)
		}
	}
	return nil
}

// =============================================================================
// 命令模式 (Command Pattern)
// =============================================================================

// Command 命令接口
type Command interface {
	Execute(ctx context.Context) error
	Undo(ctx context.Context) error
	GetName() string
}

// CommandInvoker 命令调用者
type CommandInvoker struct {
	commands []Command
	current  int
}

// NewCommandInvoker 创建命令调用者
func NewCommandInvoker() *CommandInvoker {
	return &CommandInvoker{
		commands: make([]Command, 0),
		current:  -1,
	}
}

// Execute 执行命令
func (i *CommandInvoker) Execute(ctx context.Context, cmd Command) error {
	if err := cmd.Execute(ctx); err != nil {
		return err
	}

	// 添加到历史记录
	i.commands = append(i.commands[:i.current+1], cmd)
	i.current++

	return nil
}

// Undo 撤销上一个命令
func (i *CommandInvoker) Undo(ctx context.Context) error {
	if i.current < 0 {
		return fmt.Errorf("no command to undo")
	}

	cmd := i.commands[i.current]
	if err := cmd.Undo(ctx); err != nil {
		return err
	}

	i.current--
	return nil
}

// GetHistory 获取命令历史
func (i *CommandInvoker) GetHistory() []string {
	history := make([]string, len(i.commands))
	for i, cmd := range i.commands {
		history[i] = cmd.GetName()
	}
	return history
}
