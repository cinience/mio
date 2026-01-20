# VAD 音频丢失问题分析报告

## 问题描述

每轮 ASR 识别会丢失前部分的音频，导致识别结果不完整。

## 日志分析

从 `aio-server/logs/backend-server.log` 的关键日志：

```
第173行: buffer_frames=1, raw_result=false, confirmed=false, rms=0.000210
第174行: buffer_frames=4, raw_result=true, confirmed=false, rms=0.026529, active_streak=1/2
第175行: buffer_frames=5, raw_result=true, confirmed=true, rms=0.040867, active_streak=2/2
第176行: buffer_frames=1, raw_result=true, confirmed=true, rms=0.031669, active_streak=2/2
```

### 关键观察

1. **第173行到第174行之间**：buffer_frames 从 1 跳到 4，说明有2-3帧在中间被缓存
2. **第175行**：confirmed=true，缓冲区有5帧，此时应该 Drain 所有缓存并发送
3. **第176行**：buffer_frames 重置为 1，说明缓冲区已被清空

## 代码流程分析

### 1. VAD Service 的处理流程

在 `backend-server/internal/server/chat/vad/service.go`:

```go
// 第106-115行：当首次确认有语音时
if evalResult.Confirmed && !clientHadVoice {
    buffered := v.pipeline.DrainBufferedAudio(nil)  // 获取缓存的所有音频
    if len(buffered) > 0 {
        if evalResult.ShouldFlush {
            pcmFrame = buffered  // 只返回缓存，丢弃当前帧
        } else {
            pcmFrame = append(buffered, pcmFrame...)  // 合并缓存和当前帧
        }
    }
}

// 第120-123行：未确认且无语音时的处理
if !clientHadVoice && !evalResult.Confirmed && !evalResult.Raw {
    v.pipeline.TrimBufferedSilence(3)  // 修剪静音帧
    return nil, evalResult.Raw, false, false, nil  // 返回 nil，不发送
}
```

### 2. VAD Pipeline 的缓冲处理

在 `backend-server/internal/server/chat/vad/service.go`:

```go
// 第250行：每帧都会先加入缓冲区
p.buffer.AddAsrAudioData(pcm)

// 第305-308行：
result.ShouldFlush = result.Raw && !clientHaveVoice
result.Confirmed = p.strategy.confirmActivation(result.Raw, clientHaveVoice)
result.BufferFrames = p.buffer.GetFrameCount()
```

### 3. ASR Manager 的发送逻辑

在 `backend-server/internal/server/chat/managers/asr_manager.go`:

```go
// 第67-74行：获取 VAD 处理后的音频
pcmData, rawHaveVoice, confirmedHaveVoice, skipped, err := vadSvc.Evaluate(opusFrame)
if pcmData == nil {
    continue  // 如果返回 nil，跳过不发送
}

// 第101-110行：判断是否发送到 ASR
shouldSend := clientHadVoice
if !clientHadVoice && (rawHaveVoice || skipped) {
    shouldSend = true
}
if shouldSend && state.AsrAudioChannel != nil {
    state.AsrAudioChannel <- pcmData
}
```

## 问题根因

### 问题1：前置静音帧被修剪

在语音确认（confirmed=true）之前：
- **第173行**：raw=false, confirmed=false → 返回 nil，不发送，且调用 `TrimBufferedSilence(3)` 修剪缓存
- **第174行前的2-3帧**：可能是 raw=false 的静音帧，会被缓存但也可能被 `TrimBufferedSilence` 修剪掉

`TrimBufferedSilence(3)` 的逻辑（第319-333行）：
```go
func (p *vadPipeline) TrimBufferedSilence(multiplier int) {
    limit := p.windowFrameCount * multiplier
    if p.buffer.GetFrameCount() > limit {
        p.buffer.RemoveAsrAudioData(1)  // 只移除1帧
    }
}
```

这意味着如果缓冲区超过限制，会移除最早的1帧。**这可能导致语音开头被裁剪！**

### 问题2：ShouldFlush 逻辑可能导致当前帧丢失

在第305行：
```go
result.ShouldFlush = result.Raw && !clientHaveVoice
```

当 ShouldFlush=true 时，在 service.go 第110行：
```go
pcmFrame = buffered  // 只返回缓存，当前帧 pcmFrame 被覆盖
```

但实际上，当前帧已经在第250行被加入缓冲区了：
```go
p.buffer.AddAsrAudioData(pcm)
```

所以这个逻辑应该是正确的，因为 buffered 已经包含了当前帧。

### 问题3：raw=true 但 confirmed=false 的帧处理

在第174行：
- raw=true, confirmed=false, buffer_frames=4
- 此时 `pcmData` 不为 nil，会被返回
- ASR Manager 会判断 `shouldSend = !clientHadVoice && rawHaveVoice = true`
- 所以这一帧会被发送

但是，这一帧发送时只包含**当前帧本身**，不包含前面缓存的3帧（buffer_frames=4 表示缓存有4帧，但只返回当前这1帧）。

## 真正的问题

**核心问题**：在 `active_streak < minActiveFrames` 时（如第174行 active_streak=1/2），即使 `raw=true`，这一帧会被发送，但**前面缓存的帧（raw=false 的静音帧）不会被发送**。

只有在 `confirmed=true`（active_streak=2/2）时，才会 `Drain` 缓冲区发送所有帧。

**但是**，在第174行时，前面的3帧（buffer_frames=4 表示缓存有4帧）可能包含：
1. 一些 raw=false 的静音帧（已被 TrimBufferedSilence 部分修剪）
2. 一些真实的语音开头帧（被误判为 raw=false）

这些帧在第175行 Drain 时应该被发送，但是：
1. **TrimBufferedSilence 可能已经移除了一些早期帧**
2. **如果这些早期帧是真实语音，就会导致语音开头丢失**

## 定位问题源头

### 是 backend-server 的问题还是 speech-server 的问题？

**答案：主要是 backend-server 的问题**

具体原因：
1. **VAD 缓冲修剪过于激进**：`TrimBufferedSilence(3)` 在语音确认前可能移除了真实的语音开头
2. **VAD 确认机制延迟**：需要 `minActiveFrames=2` 帧连续有声才确认，导致前面的帧可能被当作静音处理
3. **缓冲策略不当**：在 confirmed=false 时，raw=true 的帧会立即发送（不带缓存），而 raw=false 的帧会被缓存但可能被修剪

### speech-server 的责任

speech-server（OpenAI Realtime ASR）只负责识别接收到的音频，不负责音频的缓冲和过滤。从代码看，它会忠实地处理所有通过 `audioStream` channel 接收到的音频帧。

## 建议的解决方案

### 方案1：减少缓冲修剪的激进程度

修改 `TrimBufferedSilence` 的逻辑，增加保留的静音帧数量：

```go
func (p *vadPipeline) TrimBufferedSilence(multiplier int) {
    // 保留更多的早期帧，避免裁剪真实语音开头
    limit := p.windowFrameCount * multiplier
    if limit <= 0 {
        limit = multiplier * 3  // 增加到3倍
    }
    if p.buffer.GetFrameCount() > limit {
        p.buffer.RemoveAsrAudioData(1)
    }
}
```

### 方案2：降低 minActiveFrames 阈值

将 `minActiveFrames` 从 2 降低到 1，减少确认延迟：

```go
// 在 vad/config.go 中
if minActiveFrames < 1 {
    minActiveFrames = 1  // 改为1，减少延迟
}
```

### 方案3：在 confirmed 前也发送缓存

修改 ASR Manager 的逻辑，在 raw=true 时也发送部分缓存：

```go
// 在 service.go 第125行前添加
if evalResult.Raw && !clientHadVoice && !evalResult.Confirmed {
    // 发送部分缓存的音频（如最近的2-3帧）
    buffered := v.pipeline.DrainPartialBuffer(2)
    if len(buffered) > 0 {
        pcmFrame = append(buffered, pcmFrame...)
    }
}
```

### 方案4（推荐）：禁用或减少 TrimBufferedSilence 的调用

在确认有语音前，完全保留缓冲区的所有帧：

```go
// 在 service.go 第120-123行
if !clientHadVoice && !evalResult.Confirmed && !evalResult.Raw {
    // v.pipeline.TrimBufferedSilence(3)  // 注释掉，避免过早修剪
    return nil, evalResult.Raw, false, false, nil
}
```

或者增加 multiplier 参数：
```go
v.pipeline.TrimBufferedSilence(10)  // 从3增加到10，保留更多帧
```

## 验证方法

1. **添加详细日志**：在 Drain 和发送音频时记录音频帧的数量和长度
2. **对比测试**：使用相同的音频输入，对比修改前后的识别结果
3. **计算丢失量**：统计 buffer_frames 的变化和实际发送的音频帧数

## 结论

每轮 ASR 识别丢失前部分音频的主要原因是 **backend-server 的 VAD 缓冲修剪逻辑过于激进**，在语音确认（confirmed=true）之前就移除了部分早期音频帧，导致真实的语音开头被裁剪。

建议采用方案4（禁用或减少 TrimBufferedSilence）作为首选解决方案，同时配合方案1（增加保留帧数）作为备选。


