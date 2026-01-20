# ElevenLabs TTS Provider

配置键：`tts.elevenlabs`

必填：
- `api_key`：ElevenLabs API Key
- `voice_id`：TTS 语音 ID

可选：
- `model_id`：默认 `eleven_multilingual_v2`
- `output_format`：默认 `mp3_44100_128`
- `optimize_streaming_latency`：0-4
- `region`：`us` / `eu` / 留空（默认）
- `timeout_seconds`：默认 120
- `base_url`：自定义 API 域名（留空使用官方）

Provider 名称：`elevenlabs`
