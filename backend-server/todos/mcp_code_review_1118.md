# MCP 实现代码审查报告

**审查日期**: 2025-11-18  
**审查范围**: backend-server/internal/domain/mcp/  
**审查人**: AI Assistant

## 📋 概览

MCP (Model Context Protocol) 实现包含全局、本地和设备三个管理器，支持多种传输协议（SSE、WebSocket、HTTP），总体架构合理但存在一些需要优化的问题。

### 架构优势
- ✅ 清晰的三层管理器架构（Global、Local、Device）
- ✅ 支持多种传输协议
- ✅ 实现了 Eino 框架的 tool.InvokableTool 接口
- ✅ 包含重连和健康检查机制
- ✅ 使用并发安全的数据结构

---

## 🔴 严重问题

### 1. 全局变量滥用和单例模式混乱

**位置**: 
- `global_manage.go:64-66`
- `manager.go:20-23`
- `local_manager.go:24-27`
- `mcp_pool.go:17-23`

**问题描述**:
```go
// 多处使用全局变量和 sync.Once 单例
var (
    globalManager *GlobalMCPManager
    once          sync.Once
)

var (
    mcpManager *MCPManager
    mcpOnce    sync.Once
)

var mcpClientPool *McpClientPool

func init() {
    mcpClientPool = &McpClientPool{...}
    go mcpClientPool.checkOffline()
}
```

**影响**:
- 测试困难，无法进行单元测试和依赖注入
- 在 init() 中启动 goroutine 可能导致资源泄漏
- 多个单例模式混用导致依赖关系不清晰
- 无法在测试中重置状态

**优化建议**:
```go
// 使用依赖注入而非全局变量
type MCPService struct {
    globalManager *GlobalMCPManager
    localManager  *LocalMCPManager
    deviceManager *McpClientPool
}

func NewMCPService(ctx context.Context, cfg config.MCPConfig) *MCPService {
    service := &MCPService{
        globalManager: NewGlobalMCPManager(ctx, cfg.Global),
        localManager:  NewLocalMCPManager(),
        deviceManager: NewMcpClientPool(ctx),
    }
    return service
}
```

### 2. Context 生命周期管理混乱

**位置**: 
- `global_manage.go:89-94`
- `device_manager.go:69-83`

**问题描述**:
```go
// global_manage.go
func (g *GlobalMCPManager) ensureContextLocked() {
    if g.ctx != nil && g.ctx.Err() == nil {
        return
    }
    g.ctx, g.cancel = context.WithCancel(context.Background())
}

// device_manager.go
func NewDeviceMCPSession(deviceID string) *DeviceMcpSession {
    ctx, cancel := context.WithCancel(context.Background())
    // 直接创建新的 context，不接受父 context
}
```

**影响**:
- Context 生命周期不受控制
- 无法从外部优雅地取消操作
- 可能导致 goroutine 泄漏

**优化建议**:
```go
// 接受父 context
func NewGlobalMCPManager(ctx context.Context, cfg ReconnectConfig) *GlobalMCPManager {
    ctx, cancel := context.WithCancel(ctx)
    return &GlobalMCPManager{
        ctx:    ctx,
        cancel: cancel,
        // ...
    }
}

func NewDeviceMCPSession(ctx context.Context, deviceID string) *DeviceMcpSession {
    ctx, cancel := context.WithCancel(ctx)
    return &DeviceMcpSession{
        Ctx:      ctx,
        cancel:   cancel,
        deviceID: deviceID,
    }
}
```

### 3. 锁的粒度问题和潜在死锁

**位置**: 
- `global_manage.go:696-726`
- `device_manager.go:370-415`

**问题描述**:
```go
// 在持有读锁的情况下启动 goroutine
func (g *GlobalMCPManager) monitorConnections(ctx context.Context) {
    // ...
    g.mu.RLock()
    for name, conn := range g.servers {
        go func(name string, conn *MCPServerConnection) {
            // goroutine 中可能会调用其他需要锁的方法
            conn.refreshTools(checkCtx)
        }(name, conn)
    }
    g.mu.RUnlock()
}
```

**影响**:
- goroutine 中调用 refreshTools 可能触发 updateGlobalTools，需要写锁
- 可能导致死锁或锁竞争
- 锁持有时间过长影响并发性能

**优化建议**:
```go
func (g *GlobalMCPManager) monitorConnections(ctx context.Context) {
    // 先复制需要的数据，再释放锁
    g.mu.RLock()
    serversCopy := make([]*MCPServerConnection, 0, len(g.servers))
    for _, conn := range g.servers {
        serversCopy = append(serversCopy, conn)
    }
    g.mu.RUnlock()

    // 在锁外执行可能耗时的操作
    for _, conn := range serversCopy {
        go func(conn *MCPServerConnection) {
            // 安全地执行检查
        }(conn)
    }
}
```

---

## 🟡 中等问题

### 4. 错误处理不完善

**位置**: 
- `mcp_tool.go:88-108`
- `device_manager.go:213-255`

**问题描述**:
```go
// mcp_tool.go - 只处理了 session closed 错误
if err != nil && isSessionClosedError(err) {
    // 重连逻辑
} else if err != nil {
    return retContent, fmt.Errorf("调用工具失败: %v", err)
}

// device_manager.go - 重试逻辑简单
for retry := 0; retry < maxRetries; retry++ {
    if err := mc.attemptConnection(deviceID, mcpEndpoint); err != nil {
        // 所有错误都重试，没有区分临时错误和永久错误
        continue
    }
}
```

**影响**:
- 无法区分可恢复错误和不可恢复错误
- 缺少错误分类和不同的处理策略
- 没有熔断机制防止雪崩

**优化建议**:
```go
// 定义错误类型
type ErrorType int

const (
    ErrorTypeTemporary ErrorType = iota  // 临时错误，可重试
    ErrorTypePermanent                    // 永久错误，不应重试
    ErrorTypeRateLimited                  // 限流错误，需要退避
)

func classifyError(err error) ErrorType {
    if err == nil {
        return ErrorTypeTemporary
    }
    
    errStr := err.Error()
    if strings.Contains(errStr, "connection refused") ||
       strings.Contains(errStr, "timeout") {
        return ErrorTypeTemporary
    }
    
    if strings.Contains(errStr, "unauthorized") ||
       strings.Contains(errStr, "invalid credentials") {
        return ErrorTypePermanent
    }
    
    if strings.Contains(errStr, "rate limit") {
        return ErrorTypeRateLimited
    }
    
    return ErrorTypeTemporary
}

// 使用熔断器
type CircuitBreaker struct {
    failures    int
    lastFailure time.Time
    state       string // "closed", "open", "half-open"
    threshold   int
}

func (cb *CircuitBreaker) Call(fn func() error) error {
    if cb.state == "open" {
        if time.Since(cb.lastFailure) > 60*time.Second {
            cb.state = "half-open"
        } else {
            return fmt.Errorf("circuit breaker is open")
        }
    }
    
    err := fn()
    if err != nil {
        cb.failures++
        cb.lastFailure = time.Now()
        if cb.failures >= cb.threshold {
            cb.state = "open"
        }
        return err
    }
    
    cb.failures = 0
    cb.state = "closed"
    return nil
}
```

### 5. Schema 转换逻辑复杂且重复

**位置**: 
- `global_manage.go:387-579`

**问题描述**:
- `normalizeJSONSchema` 和 `cleanSchemaForOpenAPI` 功能重复
- 递归处理逻辑复杂，难以维护
- 没有缓存机制，每次都重新转换

**影响**:
- 代码重复，维护成本高
- 性能问题，频繁的 schema 转换
- 难以扩展新的 schema 类型

**优化建议**:
```go
// 使用统一的 Schema 处理器
type SchemaProcessor struct {
    cache sync.Map // 缓存转换结果
}

func (sp *SchemaProcessor) Process(schema interface{}) (interface{}, error) {
    // 生成缓存键
    key := generateCacheKey(schema)
    
    // 检查缓存
    if cached, ok := sp.cache.Load(key); ok {
        return cached, nil
    }
    
    // 处理 schema
    normalized := sp.normalize(schema)
    cleaned := sp.clean(normalized)
    
    // 存入缓存
    sp.cache.Store(key, cleaned)
    return cleaned, nil
}

// 分离关注点
func (sp *SchemaProcessor) normalize(schema interface{}) interface{} {
    // 规范化逻辑
}

func (sp *SchemaProcessor) clean(schema interface{}) interface{} {
    // 清理逻辑
}
```

### 6. 资源泄漏风险

**位置**: 
- `device_manager.go:302-320`
- `mcp_pool.go:23`

**问题描述**:
```go
// device_manager.go - startToolDiscovery 中启动的 ticker 可能泄漏
func (mc *McpClientInstance) startToolDiscovery() {
    go func() {
        tick := time.NewTicker(30 * time.Second)
        defer tick.Stop() // 只有 goroutine 退出才会停止
        
        for {
            select {
            case <-mc.ctx.Done():
                return
            case <-tick.C:
                mc.discoverTools()
            }
        }
    }()
}

// mcp_pool.go - init 中启动的 goroutine 无法停止
func init() {
    mcpClientPool = &McpClientPool{...}
    go mcpClientPool.checkOffline() // 无法停止的 goroutine
}
```

**影响**:
- goroutine 泄漏
- ticker 资源泄漏
- 无法优雅关闭

**优化建议**:
```go
type McpClientPool struct {
    device2McpClient cmap.ConcurrentMap[string, *DeviceMcpSession]
    ctx              context.Context
    cancel           context.CancelFunc
}

func NewMcpClientPool(ctx context.Context) *McpClientPool {
    ctx, cancel := context.WithCancel(ctx)
    pool := &McpClientPool{
        device2McpClient: cmap.New[*DeviceMcpSession](),
        ctx:              ctx,
        cancel:           cancel,
    }
    go pool.checkOffline()
    return pool
}

func (p *McpClientPool) checkOffline() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-p.ctx.Done():
            return
        case <-ticker.C:
            p.sweepOfflineClients()
        }
    }
}

func (p *McpClientPool) Close() error {
    p.cancel()
    return nil
}
```

---

## 🟢 次要问题

### 7. 魔法数字过多

**位置**: 多处

**问题描述**:
```go
Timeout: 600 * time.Second  // global_manage.go:82
time.Sleep(time.Second)     // global_manage.go:750
30 * time.Second            // 多处
5 * time.Second             // 多处
```

**优化建议**:
```go
const (
    DefaultHTTPTimeout        = 600 * time.Second
    DefaultReconnectInterval  = 30 * time.Second
    DefaultHealthCheckTimeout = 5 * time.Second
    DefaultRetryDelay        = 1 * time.Second
    DefaultToolRefreshInterval = 60 * time.Second
    DefaultOfflineCheckInterval = 30 * time.Second
)
```

### 8. 日志级别使用不当

**位置**: 多处

**问题描述**:
```go
// device_manager.go:336
log.Errorf("获取工具列表失败: %v", err)  // 应该是 Warn 级别

// global_manage.go:158
log.Errorf("连接到MCP服务器 %s 失败: %v", serverCfg.Name, err)
// 如果配置了多个服务器，某个失败不应该是 Error
```

**优化建议**:
```go
// 根据严重程度使用合适的日志级别
// Error: 影响核心功能的错误
// Warn: 可以继续运行但需要注意的问题
// Info: 重要的状态变化
// Debug: 详细的调试信息

// 示例
if err := conn.refreshTools(ctx); err != nil {
    if isSessionClosedError(err) {
        log.Warn("MCP服务器连接已关闭", 
            "server", name, 
            "error", err)
    } else {
        log.Debug("工具刷新暂时失败，将在下次重试",
            "server", name,
            "error", err)
    }
}
```

### 9. 缺少 Metrics 和可观测性

**问题描述**:
- 没有指标收集（连接数、工具数、调用次数等）
- 没有追踪信息（trace ID）
- 难以监控和排查问题

**优化建议**:
```go
type MCPMetrics struct {
    // 连接指标
    ActiveConnections   prometheus.Gauge
    TotalConnections    prometheus.Counter
    ConnectionErrors    prometheus.Counter
    
    // 工具指标
    RegisteredTools     prometheus.Gauge
    ToolCalls           prometheus.Counter
    ToolCallDuration    prometheus.Histogram
    ToolCallErrors      prometheus.Counter
    
    // 重连指标
    ReconnectAttempts   prometheus.Counter
    ReconnectSuccesses  prometheus.Counter
}

func (m *MCPMetrics) RecordToolCall(toolName string, duration time.Duration, err error) {
    m.ToolCalls.Inc()
    m.ToolCallDuration.Observe(duration.Seconds())
    if err != nil {
        m.ToolCallErrors.Inc()
    }
}
```

### 10. Transport 实现不完整

**位置**: 
- `websocket_transport.go:50-54`
- `iot_over_mcp_transport.go:31-34`

**问题描述**:
```go
// Start 方法只有 TODO 注释，没有实际实现
func (t *WebsocketTransport) Start(ctx context.Context) error {
    // TODO: 启动连接/监听消息等
    return nil
}

func (t *IotOverMcpTransport) Start(ctx context.Context) error {
    // TODO: 启动连接/监听消息等
    return nil
}
```

**影响**:
- Transport 可能没有正确初始化
- 通知处理器可能不工作
- 连接状态不可靠

**优化建议**:
```go
type WebsocketTransport struct {
    conn          *websocket.Conn
    notifyHandler func(notification mcp.JSONRPCNotification)
    ctx           context.Context
    cancel        context.CancelFunc
    started       bool
    mu            sync.RWMutex
}

func (t *WebsocketTransport) Start(ctx context.Context) error {
    t.mu.Lock()
    defer t.mu.Unlock()
    
    if t.started {
        return fmt.Errorf("transport already started")
    }
    
    t.ctx, t.cancel = context.WithCancel(ctx)
    
    // 启动消息监听
    go t.receiveLoop()
    
    t.started = true
    return nil
}

func (t *WebsocketTransport) receiveLoop() {
    defer func() {
        if r := recover(); r != nil {
            log.Errorf("WebSocket receive loop panic: %v", r)
        }
    }()
    
    for {
        select {
        case <-t.ctx.Done():
            return
        default:
            var notif mcp.JSONRPCNotification
            if err := t.conn.ReadJSON(&notif); err != nil {
                if websocket.IsUnexpectedCloseError(err) {
                    return
                }
                log.Errorf("读取 WebSocket 消息失败: %v", err)
                continue
            }
            
            if t.notifyHandler != nil {
                t.notifyHandler(notif)
            }
        }
    }
}
```

---

## 📊 代码质量指标

| 指标 | 评分 | 说明 |
|------|------|------|
| 架构设计 | 7/10 | 整体架构清晰，但模块耦合度偏高 |
| 错误处理 | 6/10 | 基本的错误处理存在，但不够细致 |
| 并发安全 | 7/10 | 使用了锁和并发安全的数据结构，但有死锁风险 |
| 资源管理 | 5/10 | 存在资源泄漏风险，context 管理不规范 |
| 可测试性 | 4/10 | 大量全局变量和单例，难以测试 |
| 可观测性 | 3/10 | 缺少 metrics 和追踪 |
| 代码可读性 | 7/10 | 命名清晰，但函数过长 |

---

## 🎯 优先级建议

### P0 - 立即修复（影响稳定性）
1. ✅ 修复资源泄漏问题（Context、Goroutine、Ticker）
2. ✅ 完善 Transport 的 Start 实现
3. ✅ 修复潜在的死锁问题

### P1 - 近期修复（影响可维护性）
4. ⚠️ 重构全局变量为依赖注入
5. ⚠️ 改进错误处理和分类
6. ⚠️ 优化 Schema 转换逻辑

### P2 - 长期优化（提升质量）
7. 📈 添加 Metrics 和可观测性
8. 📈 添加熔断器和限流
9. 📈 提取魔法数字为常量
10. 📈 改进日志级别使用

---

## 🔧 重构建议

### 短期重构（1-2周）
```go
// 1. 引入依赖注入容器
type Container struct {
    MCPService    *MCPService
    GlobalManager *GlobalMCPManager
    LocalManager  *LocalMCPManager
    DevicePool    *McpClientPool
}

// 2. 统一 Context 管理
func NewMCPService(ctx context.Context, cfg config.MCPConfig) (*MCPService, error) {
    // 所有组件共享同一个父 context
}

// 3. 添加优雅关闭
func (s *MCPService) Shutdown(ctx context.Context) error {
    // 按依赖顺序关闭各个组件
}
```

### 长期重构（1-2月）
```go
// 1. 模块化拆分
// - mcp/manager    - 管理器接口和实现
// - mcp/transport  - 传输层实现
// - mcp/tool       - 工具相关
// - mcp/schema     - Schema 处理
// - mcp/metrics    - 指标收集

// 2. 抽象接口
type MCPManager interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    GetTools() map[string]tool.InvokableTool
    RegisterTool(tool tool.InvokableTool) error
}

// 3. 使用设计模式
// - 策略模式处理不同的重连策略
// - 工厂模式创建不同类型的 Transport
// - 观察者模式处理连接状态变化
```

---

## 📝 总结

MCP 实现的核心功能已经完备，但在**资源管理**、**错误处理**和**可测试性**方面需要改进。建议优先修复 P0 问题以确保系统稳定性，然后逐步进行架构优化。

**关键改进方向**:
1. 🔴 消除全局状态，改用依赖注入
2. 🔴 规范 Context 生命周期管理
3. 🟡 完善错误处理和重试策略
4. 🟡 添加可观测性支持
5. 🟢 提升代码质量和可维护性

**预期收益**:
- 减少 goroutine 和资源泄漏
- 提高系统稳定性和可维护性
- 便于单元测试和集成测试
- 更好的监控和问题排查能力


