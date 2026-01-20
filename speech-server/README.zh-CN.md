
# 语音服务（Speech Server）

`speech-server` 是一个使用 Go 编写的语音服务，兼容 OpenAI Realtime WebSocket 协议，基于 sherpa-onnx 实现离线语音识别（ASR）与可选的文本转语音（TTS），并提供服务端 VAD 池化能力。

## 核心特性

- **Realtime 协议兼容**：覆盖 `session.update`、`input_audio_buffer.*`、`conversation.item.*`、`response.*` 等事件，可直接对接官方 SDK。
- **自动模型拉取**：当目标目录为空时，可自动通过 go-git + git-lfs 从 ModelScope 克隆默认模型；支持关闭或改为自定义仓库。
- **多模型配置**：支持 SenseVoice/Paraformer ASR、Matcha TTS、Silero/TEN VAD，同时在转写结果中附带语言、情绪、事件等元信息。
- **服务端 VAD 池化**：Silero/TEN VAD 实例按最小/最大数量池化，支持超时控制；自动根据采样率设置 Silero 的窗口大小（16kHz 对应 512 样本）。
- **示例齐全**：提供文件流式识别、麦克风管道（stdin）、自动 turn detection、TTS 请求等示例。

## 目录结构

```
cmd/server            主程序入口
internal/app          应用装配、配置加载、Realtime 处理
internal/asr, tts     sherpa-onnx 封装
config/config.yaml    默认配置，可叠加用户配置 / 环境变量
examples/*            示例程序（识别、TTS 等）
docs/vad-support.md   VAD 设计背景与测试指南
```

## 快速开始

```bash
go build ./cmd/server
./server --config config/config.yaml
```

启动时流程：

1. 如果 `models.auto_download` 开启且目标目录为空，自动克隆模型仓库（需要本机安装 `git` 和 `git lfs`）。
2. 按配置加载 ASR/TTS/VAD 模型。
3. 监听配置指定的地址（默认 `:8009`），同时开放 `ws://<host>:<port>/v1/realtime` 与 `/health`。

随后即可将 OpenAI Realtime 客户端指向 `ws://<host>:8009/v1/realtime?intent=transcription` 并推送 PCM 音频。

## 配置与环境变量覆盖

配置基于 YAML，通过 Viper 合并环境变量；所有键均可使用 `SPEECH_` 前缀覆盖（大写、将`.`/`-`替换为 `_`）。

```yaml
models:
  auto_download: true
  git_repo: https://www.modelscope.cn/gomodels/sherpa-small.git
  git_ref: ""
  target_dir: ./models

asr:
  provider: sense_voice
  models:
    sense_voice:
      model_dir: ./models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17
      sample_rate: 16000
      model_variant: auto
      decoding_method: greedy_search   # SenseVoice 目前仅支持贪心解码
      max_active_paths: 12
      language: auto
      use_itn: true

tts:
  provider: matcha
  models:
    matcha:
      acoustic_model: ./models/matcha-icefall-zh-baker/model-steps-3.onnx
      vocoder_model: ./models/matcha-icefall-zh-baker/vocos-22khz-univ.onnx
      lexicon: ./models/matcha-icefall-zh-baker/lexicon.txt
      tokens: ./models/matcha-icefall-zh-baker/tokens.txt

vad:
  enabled: true
  provider: silero
  models:
    silero:
      provider: silero
      model: ./models/vad/silero_vad.onnx
      window_size: 512
      sample_rate: 16000
  pool:
    min_size: 1
    max_size: 6
    acquire_timeout_ms: 3000
```

示例环境变量覆盖：

```bash
export SPEECH_SERVER_ADDR=:8010
export SPEECH_MODELS_AUTO_DOWNLOAD=false
export SPEECH_ASR_PROVIDER=sense_voice
export SPEECH_VAD_MODELS_SILERO_MODEL=./custom/vad/silero.onnx
```

## 示例程序

- `go run ./examples/voice-to-text --audio-dir <dir>`：批量流式识别 WAV/PCM 文件。
- `ffmpeg -f alsa -i default -ac 1 -ar 16000 -f s16le - | go run ./examples/mic-stream`：通过 stdin 推送麦克风数据，输出实时元信息。
- `go run ./examples/turn-detection --file sample.wav`：演示服务端 VAD 自动 `commit`。
- `go run ./examples/text-to-voice --text "你好" --voice alloy`：通过 Realtime 协议请求 Matcha TTS。

## 注意事项

- 请确保运行环境已安装 sherpa-onnx 所需依赖。
- Silero VAD 在 16 kHz 下必须使用 `window_size=512`（代码已默认处理，但手动覆盖时需遵守要求）。
- `session.update` 的 `session.metadata` 支持 `skip_noise_filter`、`noise_energy_threshold`、`noise_min_duration`、`language_hint`、`language_whitelist` 用于会话级调整噪声门与语言守卫。
- 示例程序依赖模型文件，请提前下载或启用自动拉取能力。

## 许可

遵循仓库根目录的 `LICENSE`。
