# ElevenLabs ASR Provider

配置键：`asr.elevenlabs`

必填：
- `api_key`：ElevenLabs API Key
- `model_id`：默认 `scribe_v1`

可选：
- `language_code`：显式语言（留空自动检测）
- `file_format`：默认 `pcm_s16le_16`
- `sample_rate`：默认 16000
- `timestamps_granularity`：默认 `word`
- `region`：`us` / `eu` / 留空（默认）
- `timeout_seconds`：默认 60
- `base_url`：自定义 API 域名（留空使用官方）

Provider 名称：`elevenlabs`
