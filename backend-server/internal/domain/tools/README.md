# FC Tools - Go Implementation

FC Tools 是一个功能完整的 Go 语言函数工具库，提供智能助手所需的各种功能，包括天气查询、新闻获取、时间查询、音乐播放和智能家居控制等。

## 🚀 特性

### 核心功能
- **类型安全**: 完整的 Go 类型系统，编译时错误检查
- **高性能**: 并发安全，支持高并发访问
- **易扩展**: 插件化架构，易于添加新功能
- **生产就绪**: 完整的错误处理、日志记录、监控指标

### 集成支持
- **Eino 框架**: 原生支持 eino 工具调用
- **LLM 兼容**: 完美集成大语言模型函数调用
- **配置驱动**: 支持环境变量和配置文件
- **监控指标**: 内置性能监控和指标收集

## 📦 安装

```bash
go get backend-server/internal/fc_tools
```

## 🔧 快速开始

### 基本使用

```go
package main

import (
    "context"
    "fmt"
    "backend-server/internal/fc_tools"
    "backend-server/internal/fc_tools/types"
)

func main() {
    // 创建模拟连接
    conn := &MockConnection{}
    
    // 初始化 FC Tools
    fcTools := fc_tools.NewFCTools(conn)
    
    // 执行天气查询
    ctx := context.Background()
    args := map[string]interface{}{
        "location": "北京",
        "lang":     "zh_CN",
    }
    
    response, err := fcTools.ExecuteFunction(ctx, conn, "get_weather", args)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }
    
    fmt.Printf("Weather: %s\n", response.Result)
}
```

### Eino 集成

```go
// 获取 eino 工具列表
einoTools := fcTools.GetEinoTools()

// 使用 eino 适配器
adapterManager := fcTools.GetAdapterManager()
weatherAdapter := adapterManager.GetAdapter("get_weather")

// 执行工具调用
result, err := weatherAdapter.InvokableRun(ctx, `{"location": "上海"}`)
```

## 📚 功能模块

### 1. 简单功能

#### 退出处理 (`handle_exit_intent`)
```go
args := map[string]interface{}{
    "say_goodbye": "再见，祝您愉快！",
}
response, err := fcTools.ExecuteFunction(ctx, conn, "handle_exit_intent", args)
```

#### 角色切换 (`change_role`)
```go
args := map[string]interface{}{
    "role":      "英语老师",
    "role_name": "Lily",
}
response, err := fcTools.ExecuteFunction(ctx, conn, "change_role", args)
```

### 2. 网络功能

#### 天气查询 (`get_weather`)
```go
args := map[string]interface{}{
    "location": "深圳",
    "lang":     "zh_CN",
}
response, err := fcTools.ExecuteFunction(ctx, conn, "get_weather", args)
```

#### 新闻获取 (`get_news_from_chinanews`)
```go
args := map[string]interface{}{
    "count":    5,
    "category": "domestic",
}
response, err := fcTools.ExecuteFunction(ctx, conn, "get_news_from_chinanews", args)
```

### 3. 复杂功能

#### 时间查询 (`get_time`)
```go
args := map[string]interface{}{
    "timezone": "Asia/Shanghai",
    "format":   "detailed",
    "include_solar_term": true,
}
response, err := fcTools.ExecuteFunction(ctx, conn, "get_time", args)
```

#### 音乐播放 (`play_music`)
```go
args := map[string]interface{}{
    "music_path": "/path/to/music.mp3",
    "action":     "play",
    "volume":     75,
    "loop":       false,
}
response, err := fcTools.ExecuteFunction(ctx, conn, "play_music", args)
```

#### Home Assistant (`hass_init`)
```go
args := map[string]interface{}{
    "base_url": "http://192.168.1.100:8123",
    "token":    "your_long_lived_access_token",
}
response, err := fcTools.ExecuteFunction(ctx, conn, "hass_init", args)
```

## ⚙️ 配置

### 环境变量配置

```bash
# 基本配置
export FC_TOOLS_ENV=production
export FC_TOOLS_DEBUG=false

# 日志配置
export FC_TOOLS_LOG_LEVEL=info
export FC_TOOLS_LOG_FORMAT=json

# 缓存配置
export FC_TOOLS_CACHE_ENABLED=true
export FC_TOOLS_CACHE_DEFAULT_TTL=5m

# HTTP 配置
export FC_TOOLS_HTTP_TIMEOUT=30s
export FC_TOOLS_HTTP_RETRIES=3

# 天气 API 配置
export FC_TOOLS_WEATHER_API_KEY=your_weather_api_key
export FC_TOOLS_WEATHER_DEFAULT_LOCATION=北京

# Home Assistant 配置
export FC_TOOLS_HASS_BASE_URL=http://192.168.1.100:8123
export FC_TOOLS_HASS_TOKEN=your_hass_token
```

### 配置文件

创建 `config.json`:

```json
{
  "environment": "production",
  "debug": false,
  "logging": {
    "level": "info",
    "format": "json",
    "output": "stdout"
  },
  "cache": {
    "enabled": true,
    "default_ttl": "5m",
    "cleanup_interval": "5m"
  },
  "plugins": {
    "get_weather": {
      "api_host": "devapi.qweather.com",
      "api_key": "your_api_key",
      "default_location": "北京"
    },
    "hass": {
      "base_url": "http://192.168.1.100:8123",
      "token": "your_token"
    }
  }
}
```

## 📊 监控和指标

### 启用监控

```go
import "backend-server/internal/fc_tools/monitoring"

// 记录指标
monitoring.RecordCounter("function_calls", 1, map[string]string{
    "function": "get_weather",
    "status":   "success",
})

// 记录执行时间
monitoring.RecordTiming("function_duration", duration, map[string]string{
    "function": "get_weather",
})

// 获取系统指标
metrics := monitoring.GetSystemMetrics()
```

### 指标类型

- **计数器**: 函数调用次数、成功/失败次数
- **计时器**: 函数执行时间、网络请求时间
- **仪表盘**: 缓存命中率、活跃连接数
- **直方图**: 响应时间分布

## 🔧 扩展开发

### 添加新功能

1. **创建功能结构体**:

```go
type MyFunction struct{}

func (f *MyFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
    // 实现功能逻辑
    return types.NewActionResponse(types.ActionDirectResponse, "success", nil), nil
}

func (f *MyFunction) GetInfo() *schema.ToolInfo {
    // 返回 eino 工具信息
}

func (f *MyFunction) GetType() types.ToolType {
    return types.ToolTypeSystemCtl
}
```

2. **注册功能**:

```go
func RegisterMyFunction() error {
    function := &MyFunction{}
    return fc_tools.RegisterGlobalFunction("my_function", function)
}
```

### 自定义连接接口

```go
type MyConnection struct {
    // 实现 types.Connection 接口
}

func (c *MyConnection) GetConfig() map[string]interface{} {
    // 返回配置
}

func (c *MyConnection) GetLogger() types.Logger {
    // 返回日志器
}
```

## 🧪 测试

### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行特定模块测试
go test ./internal/fc_tools/functions

# 运行集成测试
go test -v ./internal/fc_tools -run TestCompleteWorkflow
```

### 测试覆盖率

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## 📈 性能优化

### 缓存策略
- **天气数据**: 10分钟 TTL
- **新闻数据**: 30分钟 TTL
- **农历数据**: 24小时 TTL
- **配置数据**: 1小时 TTL

### 并发控制
- **HTTP 连接池**: 最大100个连接
- **请求重试**: 指数退避策略
- **超时控制**: 30秒默认超时

## 🚀 部署

### Docker 部署

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o fc-tools ./cmd/fc-tools

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/fc-tools .
CMD ["./fc-tools"]
```

### 生产环境配置

```yaml
# docker-compose.yml
version: '3.8'
services:
  fc-tools:
    image: fc-tools:latest
    environment:
      - FC_TOOLS_ENV=production
      - FC_TOOLS_LOG_LEVEL=info
      - FC_TOOLS_CACHE_ENABLED=true
    volumes:
      - ./config:/app/config
    ports:
      - "8080:8080"
```

## 📝 许可证

MIT License

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📞 支持

如有问题，请联系开发团队或查看文档。
