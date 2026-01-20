# 通用单例管理器

## 概述

本模块提供了一个类型安全的、线程安全的单例管理系统，支持泛型、优先级清理和自动资源管理。特别适用于管理需要全局唯一且需要生命周期管理的资源，如数据库连接、C++库实例、缓存客户端等。

## 设计特点

- **🎯 类型安全**: 使用Go 1.18+的泛型确保编译时类型检查
- **🔒 线程安全**: 内置读写锁，支持并发访问
- **🏗️ 延迟初始化**: 支持懒加载模式，按需创建实例
- **🔄 自动清理**: 与cleanup系统集成，支持优先级排序的资源清理
- **📊 可观测性**: 提供实例统计和列表功能
- **🧩 松耦合**: 组件自主注册，无需修改上层代码

## 核心API

### 注册单例

```go
// 基础注册
singleton.Register(singleton.SingletonConfig[*MyType]{
    Key:             "my_key",
    InitFunc:        initMyType,
    CleanupFunc:     cleanupMyType,
    CleanupName:     "MyType",
    CleanupPriority: singleton.PriorityMedium,
})

// 便捷注册方法
singleton.RegisterNativeLibrary("my_key", initFunc, cleanupFunc)  // 原生库
singleton.RegisterDatabase("my_key", initFunc, cleanupFunc)       // 数据库
singleton.RegisterService("my_key", initFunc, cleanupFunc)        // 服务
```

### 获取单例

```go
// 获取现有实例
instance, err := singleton.Get[*MyType]("my_key")

// 获取或创建实例
instance, err := singleton.GetOrCreate("my_key", initFunc)

// 检查是否存在
if singleton.Exists("my_key") {
    // 实例已存在
}
```

## 使用模式

### 1. 基本使用模式

```go
// step1: 定义类型和初始化函数
type MyService struct {
    Name string
    DB   *sql.DB
}

func initMyService() (*MyService, error) {
    db, err := sql.Open("mysql", "dsn")
    if err != nil {
        return nil, err
    }
    return &MyService{Name: "service", DB: db}, nil
}

func cleanupMyService(service *MyService) error {
    if service.DB != nil {
        return service.DB.Close()
    }
    return nil
}

// step2: 在init()中注册
func init() {
    singleton.RegisterDatabase(
        "my_service",
        initMyService,
        cleanupMyService,
    )
}

// step3: 在使用处获取
func UseMyService() error {
    service, err := singleton.GetOrCreate("my_service", initMyService)
    if err != nil {
        return err
    }
    // 使用service...
    return nil
}
```

### 2. Sherpa-ONNX重构示例

重构前（紧耦合，使用全局变量）：
```go
var tts *sherpa.OfflineTts
var once sync.Once

func init() {
    loadModel()
    cleanup.Register("Sherpa-ONNX", cleanup.PriorityLowest, cleanupGlobalResources)
}

func loadModel() *sherpa.OfflineTts {
    once.Do(func() {
        // 初始化逻辑...
        tts = sherpa.NewOfflineTts(&config)
    })
    return tts
}
```

重构后（松耦合，使用单例管理器）：
```go
func init() {
    singleton.RegisterNativeLibrary(
        singleton.KeySherpaONNX,
        initSherpaONNX,
        cleanupSherpaONNX,
    )
}

func initSherpaONNX() (*sherpa.OfflineTts, error) {
    // 初始化逻辑...
    tts := sherpa.NewOfflineTts(&config)
    if tts == nil {
        return nil, fmt.Errorf("初始化失败")
    }
    return tts, nil
}

func getSherpaONNX() (*sherpa.OfflineTts, error) {
    return singleton.GetOrCreate(singleton.KeySherpaONNX, initSherpaONNX)
}
```

## 优先级系统

清理按优先级从高到低执行：

```go
const (
    PriorityNativeLibrary = 10   // 原生库（最后清理）
    PriorityDatabase      = 40   // 数据库连接
    PriorityTTSEngine     = 70   // AI引擎
    PriorityMQTTAdapter   = 80   // 网络适配器
    PriorityManagerAPI    = 90   // API服务
    PriorityWebsocket     = 90   // WebSocket服务
)
```

**设计原则**：被依赖的组件优先级更低，最后清理。

## 错误处理

### 初始化错误
```go
func initMyService() (*MyService, error) {
    // 验证配置
    if config.Host == "" {
        return nil, fmt.Errorf("配置错误：主机地址不能为空")
    }
    
    // 尝试连接
    conn, err := connect(config.Host)
    if err != nil {
        return nil, fmt.Errorf("连接失败: %w", err)
    }
    
    return &MyService{conn: conn}, nil
}
```

### 清理错误
```go
func cleanupMyService(service *MyService) error {
    var errors []error
    
    // 清理子资源
    if err := service.closeConnections(); err != nil {
        errors = append(errors, err)
    }
    
    // 清理主资源
    if err := service.shutdown(); err != nil {
        errors = append(errors, err)
    }
    
    if len(errors) > 0 {
        return fmt.Errorf("清理过程中出现 %d 个错误", len(errors))
    }
    return nil
}
```

## 线程安全说明

- ✅ **单例管理器本身**: 内置读写锁保护，完全线程安全
- ⚠️ **单例对象内部**: 需要自行保证线程安全
- ✅ **初始化过程**: 双重检查锁定，确保只初始化一次

```go
// 如果单例对象需要线程安全，在对象内部处理
type ThreadSafeService struct {
    mu   sync.RWMutex
    data map[string]interface{}
}

func (s *ThreadSafeService) Get(key string) interface{} {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.data[key]
}
```

## 管理和调试

```go
// 获取统计信息
count := singleton.Count()
fmt.Printf("当前单例数量: %d\n", count)

// 列出所有单例
instances := singleton.ListAll()
for _, info := range instances {
    fmt.Printf("单例信息: %s\n", info)
}

// 检查特定单例
if singleton.Exists("my_key") {
    fmt.Println("单例存在")
}

// 清空所有单例（测试用）
singleton.Clear()
```

## 与cleanup系统集成

单例管理器自动与cleanup系统集成：

1. **自动注册**: 注册单例时自动注册清理函数
2. **优先级控制**: 支持清理优先级设置
3. **错误处理**: 单个清理失败不影响其他清理
4. **日志记录**: 详细的清理过程日志

```go
// 程序退出时自动调用
cleanup.Cleanup() // 会按优先级清理所有单例
```

## 最佳实践

### ✅ 推荐做法

1. **在init()中注册**: 确保导入时自动注册
2. **使用预定义常量**: 使用`singleton.Key*`常量作为key
3. **详细错误信息**: 初始化和清理函数提供清晰的错误信息
4. **幂等清理**: 清理函数支持多次调用
5. **合适的优先级**: 根据依赖关系设置清理优先级

### ❌ 避免的做法

1. **不要在全局变量中存储实例**: 使用GetOrCreate获取
2. **不要忽略错误**: 始终检查初始化错误
3. **不要在清理函数中panic**: 使用错误返回代替panic
4. **不要循环依赖**: 避免单例间的循环引用

## 迁移指南

### 从全局变量迁移

**之前**:
```go
var globalDB *sql.DB
var once sync.Once

func GetDB() *sql.DB {
    once.Do(func() {
        globalDB, _ = sql.Open("mysql", "dsn")
    })
    return globalDB
}
```

**之后**:
```go
func init() {
    singleton.RegisterDatabase("main_db", initDB, cleanupDB)
}

func GetDB() (*sql.DB, error) {
    return singleton.GetOrCreate("main_db", initDB)
}
```

### 从工厂模式迁移

**之前**:
```go
type ServiceFactory struct {
    instance *Service
    mu       sync.Mutex
}

func (f *ServiceFactory) GetService() *Service {
    f.mu.Lock()
    defer f.mu.Unlock()
    if f.instance == nil {
        f.instance = newService()
    }
    return f.instance
}
```

**之后**:
```go
func init() {
    singleton.RegisterService("my_service", newService, cleanupService)
}

func GetService() (*Service, error) {
    return singleton.GetOrCreate("my_service", newService)
}
```

这个重构不仅让代码更优雅，还提供了自动清理、错误处理和可观测性等额外好处！
