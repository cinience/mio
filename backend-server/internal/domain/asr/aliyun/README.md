# 阿里云ASR实现

本目录包含基于阿里云官方NLS Go SDK的语音识别（ASR）服务实现。

## 实现方式

### 基于官方NLS SDK实现
- **文件**: `client.go`, `adapter.go`
- **特点**: 基于阿里云官方NLS Go SDK的现代化实现
- **依赖**: `github.com/aliyun/alibabacloud-nls-go-sdk`
- **优势**: 稳定可靠，易于维护，与阿里云NLS服务完全兼容，官方支持

## 如何使用

### 配置参数

```go
config := map[string]interface{}{
    "access_key_id":        "你的AccessKeyID",
    "access_key_secret":    "你的AccessKeySecret", 
    "appkey":              "你的AppKey",
    "host":                "nls-gateway-cn-shanghai.aliyuncs.com", // 可选，支持多种格式
    "max_sentence_silence": 800,  // 可选，默认800ms
}
```

### 使用示例

```go
// 创建ASR提供者
provider, err := asr.NewAsrProvider("aliyun", config)
if err != nil {
    log.Fatal(err)
}

// 使用流式识别
resultChan, err := provider.StreamingRecognize(ctx, audioStream)
if err != nil {
    log.Fatal(err)
}

// 处理识别结果
for result := range resultChan {
    if result.IsFinal {
        fmt.Printf("最终结果: %s\n", result.Text)
    } else {
        fmt.Printf("中间结果: %s\n", result.Text)
    }
}

// 一次性识别
text, err := provider.Process(pcmData)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("识别结果: %s\n", text)
```

## 服务地址配置

`host` 参数支持多种格式，系统会自动转换为正确的WebSocket URL格式：

### 支持的格式

1. **域名格式**（推荐）
   ```
   "host": "nls-gateway-cn-shanghai.aliyuncs.com"
   ```
   自动转换为：`wss://nls-gateway-cn-shanghai.aliyuncs.com/ws/v1`

2. **完整WebSocket URL格式**
   ```
   "host": "wss://nls-gateway-cn-shanghai.aliyuncs.com/ws/v1"
   ```
   直接使用，不进行转换

3. **不设置host**
   ```
   // 不设置host，使用官方SDK默认地址
   ```
   使用NLS SDK的`DEFAULT_URL`

### 可用地域

- **华东1（杭州）**: `nls-gateway-cn-hangzhou.aliyuncs.com`
- **华北2（北京）**: `nls-gateway-cn-beijing.aliyuncs.com`
- **华东2（上海）**: `nls-gateway-cn-shanghai.aliyuncs.com`

## 依赖说明

实现需要以下依赖（已自动添加到go.mod）：

```bash
go get github.com/aliyun/alibabacloud-nls-go-sdk
```

## 技术特性

- ✅ **官方支持** - 基于阿里云官方NLS Go SDK
- ✅ **稳定可靠** - 经过阿里云官方测试和优化
- ✅ **功能完整** - 支持所有NLS语音识别功能
- ✅ **自动更新** - 跟随官方SDK自动更新
- ✅ **错误处理** - 完善的错误处理机制
- ✅ **回调机制** - 基于事件驱动的回调处理
- ✅ **双认证支持** - 支持AccessKey和Token两种认证方式

## 接口说明

### StreamingClient
- `Connect()` - 建立连接并初始化识别器
- `Start()` - 开始语音识别
- `SendAudio(data []byte)` - 发送音频数据
- `Stop()` - 停止识别
- `GetResult()` - 获取识别结果通道
- `GetError()` - 获取错误通道
- `IsReady()` - 检查是否准备好
- `Close()` - 关闭客户端

### AliyunAdapter
- `Process(pcmData []float32)` - 一次性处理整段音频
- `StreamingRecognize(ctx, audioStream)` - 流式识别

## 故障排除

### 常见问题

1. **Token获取失败**
   - 检查AccessKeyID和AccessKeySecret是否正确
   - 确认账户权限是否足够

2. **连接失败**
   - 检查网络连接
   - 验证服务地址是否正确
   - 确认Token是否有效

3. **"malformed ws or wss URL" 错误**
   - 确保host配置格式正确
   - 推荐使用域名格式: `nls-gateway-cn-shanghai.aliyuncs.com`
   - 系统会自动转换为: `wss://nls-gateway-cn-shanghai.aliyuncs.com/ws/v1`
   - 启用Debug日志查看URL转换过程

4. **音频识别无结果**
   - 检查音频格式是否为PCM 16kHz 16bit
   - 确认AppKey配置是否正确
   - 检查音频数据是否有效

### 日志调试

启用详细日志来诊断问题：

```go
log.SetLevel(log.DebugLevel)
```

## 音频格式要求

- **格式**: PCM
- **采样率**: 16kHz
- **位深**: 16bit
- **声道**: 单声道
- **字节序**: 小端序

## 性能特点

| 特性 | 说明 |
|------|------|
| 内存占用 | 适中，经过优化 |
| 启动速度 | 快速启动 |
| 维护成本 | 很低，官方维护 |
| 稳定性 | 优秀，生产级别 |
| 功能完整性 | 完整，支持所有特性 |
| 官方支持 | 完整的官方支持 |
| 错误处理 | 完善的错误处理 |
| API兼容性 | 自动更新兼容 |

## 回调事件

- `onTaskFailed` - 任务失败时触发
- `onStarted` - 识别启动时触发
- `onSentenceBegin` - 句子开始时触发
- `onSentenceEnd` - 句子结束时触发，包含最终结果
- `onResultChanged` - 中间结果变化时触发
- `onCompleted` - 识别完成时触发
- `onClose` - 连接关闭时触发

## 更新历史

- **v2.0**: 基于阿里云官方NLS Go SDK的完整实现，移除自定义WebSocket实现