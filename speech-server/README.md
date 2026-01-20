# Speech Server

`speech-server` exposes a lightweight OpenAI Realtime compatible WebSocket
endpoint that converts audio to text with the sherpa-onnx SenseVoice model.

## Features

- Implements the OpenAI Realtime `session.update`, `input_audio_buffer.*` and
  transcription related server events used by the official client SDKs.
- Runs offline with sherpa-onnx SenseVoice (Zipformer) or Paraformer models.
- Streams sherpa-onnx Matcha TTS responses through the OpenAI Realtime
  text-to-voice API (`response.create`) when configured.
- Bundles sherpa-onnx Silero/TEN VAD support so the server can auto-commit
  turns and handle microphone streams without client-side VAD.
- Emits language, emotion, and event metadata with each transcription segment.
- Ships with an embedded default config; edit `config/config.yaml` (generated on
  first run) or point `--config` to another YAML file. The only runtime flag is
  `--addr` for ad-hoc port overrides.
- Define multiple ASR/TTS models under `asr.models` / `tts.models` and switch
  active providers via the `provider` field.
- Accepts 24 kHz mono PCM16 audio chunks and transcodes to the model sample rate.
- Bundled CLI client demonstrates end-to-end streaming ASR against local audio
  directories using `github.com/WqyJh/go-openai-realtime/v2`.

## Getting Started

### Download the SenseVoice model

```bash
mkdir -p models
curl -L -o models/sense-voice.tar.bz2 \
  https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17.tar.bz2
tar -xjf models/sense-voice.tar.bz2 -C models
```

This produces `models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/`
which the server consumes directly.

Alternatively, to download the latest `gomodels/sherpa` bundle from ModelScope,
run:

```bash
python3 scripts/download_gomodels_sherpa.py
```

The script installs the `modelscope` client if necessary and places the
downloaded files directly under `speech-server/models`.

### Build and run the server

1. Build the server binary:

   ```bash
   go build ./cmd/server
   ```

2. Launch the server and point it at the unpacked model directory:

```bash
./server --config config/config.yaml
```

The ASR section can list multiple models and selects one via `provider`:

```yaml
asr:
  provider: sense_voice
  models:
    sense_voice:
      model_dir: ./models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17
      sample_rate: 16000
      model_variant: fp32              # fp32|int8|auto
      decoding_method: greedy_search   # SenseVoice 目前只支持贪心解码
      max_active_paths: 12
      language: auto
      use_itn: true
```

   Adjust decoder/provider/thread settings in `config/config.yaml` under the
   corresponding `asr.models` entry. Leaving `threads: 0` lets the server pick
   `runtime.NumCPU()`, and an empty `provider` makes it probe for CUDA/CoreML
   before falling back to CPU. SenseVoice also honors `session.metadata`
   overrides for `skip_noise_filter`, `noise_*`, and `language_*` hints so each
   conversation can adjust the guard rails without restarts.

4. Connect with any OpenAI Realtime client SDK by changing the base URL to
   `ws://<host>:8009/v1/realtime?intent=transcription`. The SDK will receive the
   usual `session.created`, `session.updated`, and transcription events.

### Model bootstrap

If the models directory is empty, the server automatically clones the default
ModelScope repository into `./models` during startup:

```yaml
models:
  auto_download: true
  git_repo: https://www.modelscope.cn/gomodels/sherpa-small.git
  git_ref: ""
  target_dir: ./models
```

Disable auto-download if you manage model artifacts yourself. Every key in the
configuration can be overridden via environment variables using the
`SPEECH_` prefix, e.g.:

```bash
export SPEECH_MODELS_GIT_REPO=https://github.com/your-org/alt-models.git
export SPEECH_SERVER_ADDR=:8010
```

Make sure the host has `git` and `git lfs` installed so large files can be
checked out correctly.

### Model bootstrap

The server automatically clones models from ModelScope when the target directory is empty. The default configuration downloads the [`sherpa-small`](https://www.modelscope.cn/gomodels/sherpa-small.git) bundle into `./models`:

```yaml
models:
  auto_download: true
  git_repo: https://www.modelscope.cn/gomodels/sherpa-small.git
  git_ref: ""
  target_dir: ./models
```

Set `auto_download` to `false` if you prefer managing model files manually. You can override any value in the YAML via environment variables using the `SPEECH_` prefix, for example:

```bash
export SPEECH_MODELS_GIT_REPO=https://github.com/your-org/alt-models.git
export SPEECH_SERVER_ADDR=:8010
```

### Enable Matcha TTS (optional)

To synthesize speech with sherpa-onnx Matcha models, download a Matcha release
that ships the acoustic model (`*.onnx`), vocoder (`*.onnx`), `tokens.txt`,
`lexicon.txt`, and `espeak-ng-data`, then wire those paths in `config/config.yaml`.
Multiple Matcha variants can coexist; flip `tts.provider` to point at the one you
want to expose to realtime clients.

The bundled `matcha_tts_zh_en_20251010` release combines a Matcha-TTS acoustic
model with a Vocos vocoder. Its `vocab_tts.txt` mixes Kokoro-derived English
subword pieces and pinyin tokens for Chinese. A ready-to-use `tokens.txt` is not
distributed upstream—see `scripts/build_matcha_tokens.md` for dependency setup
and the helper script that mirrors the Kokoro/pinyin conversion locally.

Matcha support is toggled in `config/config.yaml`:

```yaml
tts:
  provider: matcha    # empty string disables TTS
  models:
    matcha:
      acoustic_model: ./models/matcha-icefall-zh-baker/model-steps-3.onnx
      vocoder_model: ./models/matcha-icefall-zh-baker/vocos-22khz-univ.onnx
      lexicon: ./models/matcha-icefall-zh-baker/lexicon.txt
      tokens: ./models/matcha-icefall-zh-baker/tokens.txt
      cn2an_enabled: true
      espeak_dir: ./models/matcha-icefall-zh-baker/espeak-ng-data
      voice_map: "alloy:0,ash:1,verse:2"
    matcha_tts_zh_en_20251010:
      acoustic_model: ./models/matcha_tts_zh_en_20251010/model-steps-6.onnx
      vocoder_model: ./models/matcha_tts_zh_en_20251010/vocos-16khz-univ.onnx
      lexicon: ./models/matcha-icefall-zh-baker/lexicon.txt
      tokens: ./models/matcha_tts_zh_en_20251010/tokens.txt
      cn2an_enabled: true
      espeak_dir: ./models/matcha-icefall-zh-baker/espeak-ng-data
      voice_map: "warm_female:0,alloy:0"
      default_voice: warm_female
```

When `tts.provider` is non-empty the server accepts OpenAI Realtime connections
that specify a `model` query parameter (for example
`ws://localhost:8009/v1/realtime?model=local-matcha-tts`). Clients can send
`response.create` events with `input_text` content and receive streaming
`response.output_audio.*` / `response.output_text.*` events containing the
synthesized audio and transcript.

Set `cn2an_enabled: true` on any Matcha provider to opt-in to the bundled
go-cn2an normalizer which rewrites阿拉伯数字为中文读法；leave it unset/false when you
need raw digits preserved.

### Enable server-side VAD (optional)

To let the server detect turns automatically (or ingest live microphone audio),
enable the VAD section in `config/config.yaml`:

```yaml
vad:
  enabled: true
  provider: silero
  models:
    silero:
      provider: silero
      model: ./models/vad/silero_vad.onnx
      threshold: 0.5
      min_silence: 0.4
      min_speech: 0.1
      max_speech: 20
      window_size: 512
      sample_rate: 16000
      threads: 1
      device: cpu
  pool:
    min_size: 1
    max_size: 6
    acquire_timeout_ms: 3000
```

Point `model` at a sherpa-onnx VAD release (Silero or TEN). Once enabled, clients
can supply `session.audio.input.turn_detection.server_vad` settings to override
the threshold or silence window and the server will emit the usual
`conversation.item.*` / `input_audio_buffer.*` events when a turn completes.
Place VAD model files under `./models/vad/` so the defaults above resolve without extra configuration.

See `docs/vad-support.md` for more details.

### Example client

An illustrative WebSocket client is provided under `examples/client`. It uses
`github.com/WqyJh/go-openai-realtime/v2` to open a transcription session,
streams WAV/PCM clips from disk in small chunks, and prints the realtime events.
The client continuously loops through the directory until you press `Ctrl+C`. Point
it at the sample recordings under `../xiaozhi-tester/examples` (or supply your own
directory of 16 kHz / 24 kHz mono audio):

```bash
go run ./examples/voice-to-text \
  --audio-dir ../xiaozhi-tester/examples \
  --chunk 120ms \
  --url ws://localhost:8009/v1/realtime
```

Key flags:

- `--audio-dir` – directory containing `.wav`, `.pcm`, or `.raw` mono PCM16
  files. Mixed sample rates are automatically resampled to the SenseVoice
  sample rate (16 kHz by default).
- `--chunk` – duration of audio to send per `input_audio_buffer.append`.
- `--language` – optional ISO-639-1 hint (e.g. `zh`) forwarded via
  `session.update`.

For text-to-voice synthesis, ensure `tts.provider` is set in `config/config.yaml`
to a configured Matcha entry (e.g. `matcha_tts_zh_en_20251010`) and run:

```bash
go run ./examples/text-to-voice \
  --text "你好，世界" \
  --voice marin \
  --output ./tts-output.wav
```

Additional samples demonstrate server-side VAD:

- `go run ./examples/turn-detection --file sample.wav` streams a local WAV/PCM16
  clip with `session.audio.input.turn_detection.server_vad` enabled.
- `ffmpeg -f alsa -i default -ac 1 -ar 24000 -f s16le - | go run ./examples/mic-stream`
  pipes live microphone audio via `stdin`, letting the server VAD decide when to
  commit.
  Both examples print any language/emotion/event metadata surfaced by the
  recognizer.

### Run the integration test

After downloading the model you can validate the flow end-to-end:

```bash
go test ./cmd/server -run TestRealtimeTranscription -count=1
```

The test spins up an in-process server, streams half a second of silence, and
asserts that the transcription lifecycle events are emitted.

## Audio Format

- Input: base64 encoded PCM16, mono, default 16 kHz (auto-adjustable through
  `session.update`).
- Model: resampled automatically to the configured model sample rate
  (`--sample-rate` flag) before inference.

## Realtime streaming recognition flow

1. `start.sh` ensures the SenseVoice model is downloaded (unless already
   present) and launches the WebSocket server on `:8009`.
2. Clients establish a websocket connection to `/v1/realtime` and send
   `session.update` specifying transcription mode and input audio format
   (`audio/pcm` @ 16 kHz).
3. Audio contents are read from local files, optionally resampled, split into
   ~100 ms chunks, base64 encoded, and emitted via
   `input_audio_buffer.append` events followed by
   `input_audio_buffer.commit`.
4. The server buffers incoming audio, runs sherpa-onnx SenseVoice, and returns
   transcription events (`input_audio_buffer.committed`,
   `conversation.item.*`, etc.) that are printed by the example client.

## Text-to-voice flow

1. A client establishes a websocket connection with a `model` query parameter or
   any non-`transcription` intent to opt into realtime mode.
2. Optional `session.update` events can adjust the output voice (`session.audio.output.voice`),
   playback speed, and PCM format.
3. The client sends `response.create` with `input_text` content. The server runs
   sherpa-onnx Matcha to synthesize audio and streams a sequence of
   `response.output_audio.delta`, `response.output_audio.done`,
   `response.output_text.delta`, and `response.output_text.done` events.
4. Final `response.done` and `conversation.item.*` events deliver the assistant
   message containing the PCM16 audio (base64) and transcript.

## Notes

- Ensure the sherpa-onnx runtime libraries required by the selected model are
  available on the host (refer to official sherpa-onnx documentation).
- `session.update` 支持在 `session.metadata` 下发 `skip_noise_filter`、`noise_energy_threshold`、
  `noise_min_duration`、`language_hint` 与 `language_whitelist`，从而按会话调节噪声门和语言守卫。
- Text-to-voice events are emitted only when the Matcha TTS flags are provided;
  otherwise `response.create` is rejected.
