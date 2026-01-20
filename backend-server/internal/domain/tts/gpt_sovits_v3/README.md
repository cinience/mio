# GPT-SoVITS V3 TTS 提供者

这个模块为backend-server项目提供了GPT-SoVITS V3的TTS（文本转语音）支持。

## 功能特性

- 支持GPT-SoVITS V3的HTTP API调用（GET请求方式）
- 完整的配置参数支持
- 支持同步和异步（流式）文本转语音
- 自动音频格式转换（WAV到Opus）
- 连接池优化，提高性能
- 完整的错误处理和日志记录
- 基于xinnan-tech/xiaozhi-esp32-server项目的GPT-SoVITS V3实现

## 配置参数

```json
{
  "type": "gpt_sovits_v3",
  "url": "http://127.0.0.1:9880",
  "refer_wav_path": "demo.wav",
  "prompt_text": "你好，欢迎使用小智语音助手。",
  "prompt_language": "auto",
  "text_language": "auto",
  "top_k": 5,
  "top_p": 1.0,
  "temperature": 1.0,
  "sample_steps": 50,
  "speed": 1.0,
  "cut_punc": "。？！.?!；;：:",
  "inp_refs": "",
  "if_sr": false,
  "audio_file_type": "wav"
}
```

### 配置参数说明

- `url`: GPT-SoVITS V3服务的API地址
- `refer_wav_path`: 参考音频文件路径，用于语音克隆
- `prompt_text`: 参考音频对应的文本
- `prompt_language`: 参考文本的语言（auto, zh, en等）
- `text_language`: 待合成文本的语言，默认为"auto"
- `top_k`: 语音合成参数，控制生成质量
- `top_p`: 语音合成参数，控制生成质量
- `temperature`: 语音合成参数，控制生成风格
- `sample_steps`: 采样步数
- `speed`: 语速控制
- `cut_punc`: 用于文本切割的标点符号
- `inp_refs`: 输入参考列表
- `if_sr`: 是否进行超分辨率处理
- `audio_file_type`: 输出音频文件的格式，默认为"wav"

## 使用方法

### 1. 基本使用

```go
package main

import (
    "context"
    "backend-server/constants"
    "backend-server/internal/domain/tts"
)

func main() {
    config := map[string]interface{}{
        "url": "http://127.0.0.1:9880",
        "refer_wav_path": "demo.wav",
        "prompt_text": "你好，欢迎使用小智语音助手。",
        "prompt_language": "zh",
        "text_language": "zh",
    }
    
    provider, err := tts.GetTTSProvider(constants.TtsTypeGptSovitsV3, config)
    if err != nil {
        panic(err)
    }
    
    ctx := context.Background()
    frames, err := provider.TextToSpeech(ctx, "你好，世界！", 24000, 1, 20)
    if err != nil {
        panic(err)
    }
    
    // 处理音频帧...
}
```

### 2. 流式使用

```go
outputChan, err := provider.TextToSpeechStream(ctx, "你好，世界！", 24000, 1, 20)
if err != nil {
    panic(err)
}

for frame := range outputChan {
    // 处理每个音频帧...
}
```

## GPT-SoVITS V3 vs V2 差异

| 特性 | V2 | V3 |
|------|----|----|
| 请求方式 | POST | GET |
| 参数传递 | JSON Body | URL Query Parameters |
| 参数命名 | `ref_audio_path` | `refer_wav_path` |
| 语言参数 | `text_lang`, `prompt_lang` | `text_language`, `prompt_language` |
| 采样控制 | `seed` | `sample_steps` |
| 超分辨率 | 无 | `if_sr` |

## 前置条件

1. **GPT-SoVITS V3服务**: 需要有运行中的GPT-SoVITS V3服务
2. **参考音频**: 需要准备参考音频文件用于语音克隆
3. **网络连接**: 确保能够访问GPT-SoVITS V3服务的API端点

## 安装GPT-SoVITS V3服务

请参考GPT-SoVITS V3的官方文档来安装和配置服务：

1. 克隆GPT-SoVITS V3仓库
2. 安装依赖
3. 下载预训练模型
4. 启动HTTP API服务（通常在端口9880）

## 测试

运行单元测试：

```bash
go test -v ./internal/domain/tts/gpt_sovits_v3/...
```

注意：一些测试需要实际运行的GPT-SoVITS V3服务，这些测试在默认情况下会被跳过。

## 参考

本实现基于xinnan-tech/xiaozhi-esp32-server项目中的GPT-SoVITS V3实现，采用了相同的API调用方式和参数格式。