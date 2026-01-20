# Index Stream TTS Provider

基于参考项目 `xinnan-tech/xiaozhi-esp32-server` 中 `index_stream.py` 实现的 Golang 版本的单流式 TTS 提供者。

## 功能特性

- 🎤 **单流式处理**: 支持实时流式音频生成
- 🔊 **Opus编码**: 自动将PCM音频编码为Opus格式  
- 🌐 **HTTP API**: 通过HTTP POST请求与TTS服务交互
- ⚙️ **可配置**: 支持多种配置参数自定义
- 🔄 **上下文支持**: 完整的Context取消和超时支持

## 配置参数

| 参数名 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `voice` | string | `"xiao_he"` | 语音角色 |
| `private_voice` | string | - | 私有语音角色（优先级更高） |
| `api_url` | string | `"http://8.138.114.124:11996/tts"` | TTS API 服务地址 |
| `audio_format` | string | `"pcm"` | 音频格式 |
| `sample_rate` | int | `24000` | 采样率 |
| `channels` | int | `1` | 声道数 |
| `timeout` | int | `10` | 请求超时时间（秒） |

## 使用示例

### 基本使用

```go
package main

import (
    "context"
    "fmt"
    "backend-server/constants"
    "backend-server/internal/domain/tts"
)

func main() {
    config := map[string]interface{}{
        "voice":       "xiao_he",
        "api_url":     "http://your-tts-server.com/tts",
        "sample_rate": 24000,
        "channels":    1,
        "timeout":     10,
    }

    // 创建TTS提供者
    provider, err := tts.GetTTSProvider(constants.TtsTypeIndexStream, config)
    if err != nil {
        panic(err)
    }

    ctx := context.Background()
    text := "你好，这是一个测试文本"

    // 一次性合成
    frames, err := provider.TextToSpeech(ctx, text, 24000, 1, 60)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("生成了 %d 个Opus音频帧\n", len(frames))
}
```

### 流式合成

```go
func streamExample() {
    config := map[string]interface{}{
        "voice":       "xiao_he",
        "api_url":     "http://your-tts-server.com/tts",
        "sample_rate": 24000,
        "channels":    1,
    }

    provider, err := tts.GetTTSProvider(constants.TtsTypeIndexStream, config)
    if err != nil {
        panic(err)
    }

    ctx := context.Background()
    text := "这是一个流式合成的示例"

    // 流式合成
    outputChan, err := provider.TextToSpeechStream(ctx, text, 24000, 1, 60)
    if err != nil {
        panic(err)
    }

    // 处理音频帧
    for frame := range outputChan {
        fmt.Printf("收到音频帧: %d 字节\n", len(frame))
        // 在这里处理音频帧...
    }
}
```

### 私有语音配置

```go
config := map[string]interface{}{
    "voice":         "default_voice",
    "private_voice": "my_custom_voice", // 这个会被优先使用
    "api_url":       "http://your-tts-server.com/tts",
}
```

## API 接口

TTS 服务需要支持以下HTTP接口：

### 请求格式

```http
POST /tts
Content-Type: application/json

{
    "text": "要转换的文本",
    "character": "语音角色名称"
}
```

### 响应格式

- **成功**: HTTP 200，响应体为PCM音频数据流
- **失败**: 非200状态码，响应体为错误信息

## 实现细节

### 音频处理流程

1. **HTTP请求**: 发送文本和语音角色到TTS API
2. **流式接收**: 实时接收PCM音频数据
3. **帧处理**: 将PCM数据按帧大小分割
4. **Opus编码**: 使用内置Opus编码器编码每个PCM帧
5. **输出**: 通过channel输出编码后的Opus帧

### 采样率和帧设置

- **默认采样率**: 24000 Hz（与参考实现一致）
- **默认声道**: 1（单声道）
- **帧时长**: 可配置（推荐60ms）
- **编码格式**: Opus

### 错误处理

- 网络请求失败自动记录错误日志
- 音频编码失败会记录详细错误信息
- 支持Context取消和超时控制
- 优雅处理不完整的音频帧

## 测试

```bash
# 运行单元测试
go test ./internal/domain/tts/index_stream/

# 运行特定测试
go test -v -run TestIndexStreamTTSProvider_Configuration ./internal/domain/tts/index_stream/
```

## 依赖项

- `github.com/godeps/opus`: Opus音频编码
- `backend-server/internal/util`: 音频工具类
- `backend-server/logger`: 日志记录

## 与参考实现的对应关系

| Python (参考) | Golang (本实现) | 说明 |
|---------------|-----------------|------|
| `TTSProvider` | `IndexStreamTTSProvider` | 主要实现类 |
| `text_to_speak()` | `TextToSpeech()` | 一次性合成 |
| `text_to_speak()` 流式部分 | `TextToSpeechStream()` | 流式合成 |
| `opus_encoder` | `util.OpusEncoder` | Opus编码器 |
| `pcm_buffer` | `pcmBuffer` | PCM缓冲区 |
| `handle_opus()` | channel发送 | 音频帧输出 |

## 注意事项

1. **API兼容性**: 确保TTS服务API与预期格式兼容
2. **网络延迟**: 流式合成的实时性取决于网络延迟
3. **内存使用**: 大文本可能产生较多音频帧，注意内存管理
4. **并发安全**: OpusEncoder内部使用互斥锁保证并发安全
