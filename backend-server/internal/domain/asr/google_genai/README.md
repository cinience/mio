# Google GenAI Live ASR

基于 `google.golang.org/genai` 的 Live WebSocket 流式 ASR 实现。

## 配置项

```yaml
asr:
  provider: google_genai
  google_genai:
    backend: gemini           # gemini | vertex
    api_key: "your_api_key"   # gemini 必填
    project: "gcp_project"    # vertex 必填
    location: "us-central1"   # vertex 必填
    model: "gemini-live-2.5-flash-preview"
    api_version: "v1alpha"    # gemini 默认 v1alpha，vertex 默认 v1beta1
    base_url: ""              # 可选，默认使用 SDK 的官方地址
    sample_rate: 16000
```

`sample_rate` 需要与上游音频采样率保持一致，本实现不做重采样。
