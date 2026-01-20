package singleton

import (
	log "backend-server/internal/infrastructure/logger"
	"database/sql"
	"fmt"
)

// 这个文件包含了使用单例管理器的示例代码
// 可以作为其他组件的参考实现

// ================== 数据库连接单例示例 ==================

// DatabaseConnection 数据库连接结构
type DatabaseConnection struct {
	DB   *sql.DB
	DSN  string
	Pool int
}

// 数据库初始化函数示例
func initDatabase() (*DatabaseConnection, error) {
	log.Info("初始化数据库连接...")

	// 这里是示例代码，实际使用时需要替换为真实的数据库连接逻辑
	dsn := "user:password@tcp(localhost:3306)/dbname"

	// db, err := sql.Open("mysql", dsn)
	// if err != nil {
	//     return nil, fmt.Errorf("连接数据库失败: %w", err)
	// }

	// 模拟成功连接
	conn := &DatabaseConnection{
		DB:   nil, // 在实际使用中这里应该是真实的数据库连接
		DSN:  dsn,
		Pool: 10,
	}

	log.Info("数据库连接初始化完成")
	return conn, nil
}

// 数据库清理函数示例
func cleanupDatabase(conn *DatabaseConnection) error {
	if conn != nil && conn.DB != nil {
		log.Info("关闭数据库连接...")
		err := conn.DB.Close()
		if err != nil {
			return fmt.Errorf("关闭数据库连接失败: %w", err)
		}
		log.Info("数据库连接已关闭")
	}
	return nil
}

// 注册数据库单例（在实际使用的包的init函数中调用）
func ExampleRegisterDatabase() {
	RegisterDatabase(
		KeyDatabaseConn,
		initDatabase,
		cleanupDatabase,
	)
}

// 获取数据库连接（在需要使用数据库的地方调用）
func GetDatabaseConnection() (*DatabaseConnection, error) {
	return GetOrCreate(KeyDatabaseConn, initDatabase)
}

// ================== Redis客户端单例示例 ==================

// RedisClient Redis客户端结构
type RedisClient struct {
	Address  string
	Password string
	DB       int
}

// Redis初始化函数示例
func initRedis() (*RedisClient, error) {
	log.Info("初始化Redis客户端...")

	client := &RedisClient{
		Address:  "localhost:6379",
		Password: "",
		DB:       0,
	}

	// 这里应该添加真实的Redis连接逻辑
	// rdb := redis.NewClient(&redis.Options{
	//     Addr:     client.Address,
	//     Password: client.Password,
	//     DB:       client.DB,
	// })

	log.Info("Redis客户端初始化完成")
	return client, nil
}

// Redis清理函数示例
func cleanupRedis(client *RedisClient) error {
	if client != nil {
		log.Info("关闭Redis连接...")
		// 这里应该添加真实的Redis关闭逻辑
		// client.rdb.Close()
		log.Info("Redis连接已关闭")
	}
	return nil
}

// 注册Redis单例示例
func ExampleRegisterRedis() {
	RegisterDatabase(
		KeyRedisClient,
		initRedis,
		cleanupRedis,
	)
}

// ================== WebSocket连接池单例示例 ==================

// WebSocketPool WebSocket连接池
type WebSocketPool struct {
	MaxConnections int
	ActiveCount    int
}

// WebSocket池初始化函数
func initWebSocketPool() (*WebSocketPool, error) {
	log.Info("初始化WebSocket连接池...")

	pool := &WebSocketPool{
		MaxConnections: 1000,
		ActiveCount:    0,
	}

	log.Info("WebSocket连接池初始化完成")
	return pool, nil
}

// WebSocket池清理函数
func cleanupWebSocketPool(pool *WebSocketPool) error {
	if pool != nil {
		log.Infof("关闭WebSocket连接池，当前活跃连接: %d", pool.ActiveCount)
		// 这里应该添加关闭所有连接的逻辑
		log.Info("WebSocket连接池已关闭")
	}
	return nil
}

// 注册WebSocket池单例示例
func ExampleRegisterWebSocketPool() {
	RegisterService(
		KeyWebSocketPool,
		initWebSocketPool,
		cleanupWebSocketPool,
	)
}

// ================== 完整的组件单例实现示例 ==================

// MyCustomService 自定义服务示例
type MyCustomService struct {
	Name    string
	Config  map[string]interface{}
	Started bool
}

// 自定义服务初始化
func initMyCustomService() (*MyCustomService, error) {
	log.Info("初始化自定义服务...")

	service := &MyCustomService{
		Name:    "MyService",
		Config:  make(map[string]interface{}),
		Started: true,
	}

	// 添加服务启动逻辑
	log.Info("自定义服务启动完成")
	return service, nil
}

// 自定义服务清理
func cleanupMyCustomService(service *MyCustomService) error {
	if service != nil && service.Started {
		log.Infof("停止自定义服务: %s", service.Name)
		service.Started = false
		log.Info("自定义服务已停止")
	}
	return nil
}

// 在实际的组件包中，这样注册单例：
func ExampleRegisterCustomService() {
	// 在init()函数中调用
	Register(SingletonConfig[*MyCustomService]{
		Key:             "my_custom_service",
		InitFunc:        initMyCustomService,
		CleanupFunc:     cleanupMyCustomService,
		CleanupName:     "MyCustomService",
		CleanupPriority: PriorityTTSEngine, // 中等优先级
	})
}

// 在需要使用服务的地方：
func GetMyCustomService() (*MyCustomService, error) {
	return GetOrCreate("my_custom_service", initMyCustomService)
}

// ================== 使用模式最佳实践 ==================

/*
使用模式总结：

1. 在组件包的init()函数中注册单例：
   func init() {
       singleton.RegisterNativeLibrary("my_key", initFunc, cleanupFunc)
   }

2. 在需要使用的地方获取单例：
   instance, err := singleton.GetOrCreate("my_key", initFunc)
   if err != nil {
       return err
   }

3. 优先级选择建议：
   - PriorityNativeLibrary: C++库、底层原生库
   - PriorityDatabase: 数据库连接、缓存连接
   - PriorityTTSEngine: TTS/ASR/LLM等AI引擎
   - PriorityMQTTAdapter: 消息队列、网络适配器
   - PriorityManagerAPI: API服务、Web服务
   - PriorityWebsocket: WebSocket服务器

4. 错误处理：
   - 初始化函数应该返回详细的错误信息
   - 清理函数应该处理nil检查和错误恢复
   - 使用者应该检查GetOrCreate的错误返回

5. 线程安全：
   - 单例管理器内部已处理线程安全
   - 单例对象本身的线程安全需要自行保证
*/
