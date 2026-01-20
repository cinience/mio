# Google GenAI Live TTS

基于 `google.golang.org/genai` 的 Live WebSocket 流式 TTS 实现，输出 PCM 并转换为 Opus 帧。

## 配置项

```yaml
tts:
  provider: google_genai
  google_genai:
    backend: gemini           # gemini | vertex
    api_key: "your_api_key"   # gemini 必填
    project: "gcp_project"    # vertex 必填
    location: "us-central1"   # vertex 必填
    model: "gemini-live-2.5-flash-preview"
    voice: "Kore"             # 预置音色名称，可选
    language_code: "zh-CN"    # 可选
    api_version: "v1alpha"    # gemini 默认 v1alpha，vertex 默认 v1beta1
    base_url: ""              # 可选，默认使用 SDK 的官方地址
    sample_rate: 24000
    frame_duration: 20
```

`sample_rate` 与 `frame_duration` 会影响输出 Opus 帧编码参数。
