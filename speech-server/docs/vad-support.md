# speech-server VAD 支持方案

## 背景

当前 `speech-server` 依赖客户端显式发送 `input_audio_buffer.commit` 来触发转写，这对于上传整段音频或能自行切分的客户端是可行的。但为了对齐 OpenAI Realtime 协议的完整能力，并服务直接推送麦克风流的设备，需要在服务端内建语音活动检测（VAD）以识别讲话边界，实现自动转写与自然的对话轮次管理。

## 目标场景

- **自动 turn detection**：会话开启时客户端通过 `session.audio.input.turn_detection` 指定检测阈值、静音时长等配置。服务器需根据该配置在接收音频的同时执行 VAD，并在检测到暂停后自动完成一次转写，向客户端发送内部生成的 `conversation.item.*` 和 `input_audio_buffer.*` 事件。
- **麦克风直连客户端**：一些设备或浏览器端只负责持续推送音频块，不具备或不便实现本地 VAD。当服务端检测到完整的语音片段后，需自动 `commit` 并可选地推送实时/增量识别结果，使用户获得“开口-闭口”自然的交互体验。

## 技术方案

1. **公共 VAD 模块**：在 `internal/asr` 下引入可复用的 VAD 管道，封装 sherpa-onnx 的 **Silero VAD** 与 **TEN VAD** 两套实现，并对外暴露统一接口。模块职责：
- 建议将所有 VAD 模型统一存放在 `./models/vad/` 下，方便配置默认路径并保持仓库整洁。
   - 提供流式 `AcceptWaveform`/`IsSpeech` 判断；
   - 支持动态参数（阈值、最小静音/语音时长）的在线调整；
   - 根据服务端配置选择默认 provider（如未指定则使用工厂中的首个定义）；
   - 暴露回调或通道传递“检测到语音开始/结束”事件。
2. **池化与限流**：
   - `internal/asr/vad.go` 实现 `VADPool`，按 provider+覆盖参数复用 detector，并限制总实例数；
   - 通过 `pool.min_size` / `pool.max_size` 预热/限制并发，`acquire_timeout_ms` 控制阻塞时长。
2. **会话级配置合并**：
   - 在 `config/config.yaml` 中新增默认 VAD 设置（是否启用、默认 `provider`、各模型目录、默认阈值等）；
   - `session.audio.input.turn_detection` 的参数优先级高于服务端默认值，实时覆盖当前会话的检测策略；
   - 会话更新仅覆盖阈值、静音时长等参数，provider 仍由服务端配置决定；
   - 保留客户端显式 `commit` 能力，允许 VAD 与手动提交共存。
3. **音频缓冲与分段**：
   - 连接处理器维持循环缓冲区，将每个会话的音频推送到 VAD，同时保留原始 PCM；
   - 检测到“语音开始”时标记当前片段；检测到“语音结束且静音满足阈值”时，将累计的音频帧交给 `Recognizer.Recognize` 并清空缓冲；
   - 对于麦克风场景，可在语音进行中周期性输出 partial 结果（调用 sherpa incremental decode），在语音结束时产出 final 结果。
4. **事件输出**：
   - 自动 `commit` 时复用现有的事件构造逻辑，确保与手动 `commit` 相同的 `conversation.item.*` 和 `input_audio_buffer.*` 序列；
   - turn detection 触发时额外回传 `response.turn_detected`（若后续扩展协议），并记录使用的阈值和检测耗时。
5. **可观察性**：
   - 在日志中增加 VAD 启停、语音片段长度、阈值覆盖等指标；
   - 预留 Prometheus 计数器/直方图接口，便于后续对 detection 命中率与误触发率进行监控。

## 实现步骤

1. **VAD 封装**：
   - 将 sherpa-onnx Silero VAD 和 TEN VAD 引入 `internal/asr/vad.go`；
   - 定义工厂，根据配置构建 VAD 实例并暴露统一接口；
   - 实现会话独立的上下文对象，支持参数注入与热更新。
2. **配置扩展**：
   - 更新配置结构体与解析逻辑（`internal/app/config_loader.go` 等），增加 `vad.enabled`、`vad.provider`、`vad.models.*` 以及 `vad.pool.*` 字段；
   - 将默认配置写入嵌入式 `config.yaml`，并在 `validateConfig` 中校验依赖文件是否存在，同时设置池化默认值。
3. **会话状态扩展**：
   - 在 `sessionState` 中增加 VAD 实例与状态机（Idle/Speaking/Silence）；
   - 在 `applyUpdate` 时解析 `turn_detection`，更新会话的 VAD 参数（阈值、静音窗口等），必要时重建 VAD。
4. **音频处理改造**：
   - `handleAudioAppend` 中同时将浮点 PCM 推入 VAD；
   - 新增检测循环，当满足“静音超过阈值”时自动调用 `handleCommit` 的公共逻辑；
   - 为麦克风模式添加可选的 partial 解码（可先返回 TODO，后续实现）。
5. **兼容手动提交**：若客户端仍发送 `commit`，优先处理客户端请求并重置 VAD 状态，避免重复提交。
6. **文档与配置**：更新 `README.zh-CN.md` 和示例配置文件，说明如何开启 VAD、准备模型以及 turn detection 的用法。

## 测试方案

- **单元测试**：
  - 针对 VAD 封装编写 table-driven 测试，验证阈值覆盖、静音/语音判定逻辑；
  - 分别使用 Silero VAD 和 TEN VAD 的模型跑语音/静音片段，确保接口行为一致；
  - 通过录制的音频片段（语音与静音混合）模拟 turn detection，确保状态机正确切换。
- **集成测试**：
  - 扩展 `internal/app/realtime_test.go`，引入带语音与静默的测试数据，模拟自动 turn detection 的触发并验证事件顺序；
  - 针对会话更新覆盖阈值/静音窗口的场景编写回归用例，确认缓冲被正确重置且没有重复/遗漏事件；
  - 新增端到端测试脚本，使用麦克风/PCM 数据持续推流，确认服务端会自动结束轮次。
- **手动验证**：
  - 本地运行 `go run ./cmd/server`，使用浏览器/Node 客户端通过 WebRTC 或麦克风推送实时音频；
  - 调整 `turn_detection` 配置（阈值、静音时长）观察是否按预期触发；
  - 统计误触发率与漏检率，迭代默认参数。

## 后续展望

- 与 `backend-server` 的 VAD/VADPool 规划统一接口，便于未来跨服务复用。
- 在事件流中新增检测到的语音能量、噪声比等字段，为客户端 UI 提供可视化依据。

## 示例与使用

- `examples/turn-detection`：读取本地 WAV/PCM16 文件，启用 `session.audio.input.turn_detection.server_vad` 自动完成轮次并打印返回事件（包含语言/情绪/事件元数据）。
- `examples/mic-stream`：从 `stdin` 读取麦克风 PCM 流并推送到 Realtime 服务，可配合 `ffmpeg`/`arecord` 等工具捕获音频，同时输出实时元数据：

  ```bash
  ffmpeg -f alsa -i default -ac 1 -ar 24000 -f s16le - \
    | go run ./examples/mic-stream --url ws://localhost:8009/v1/realtime
  ```

  以上示例默认启用服务端 VAD，并允许通过命令行参数覆盖阈值、静音时长、语种提示等设置。

## 识别元数据

- `internal/asr/recognizer.go` 的 `Recognize` 现返回结构体，携带文本、时间戳、语言、情绪与事件等信息；
- `processAudioSegment` 会将这些元数据写入 `conversation.item` 的额外 `input_text` 内容，并在日志中输出；
- 当识别结果未提供语言时，系统会回退到会话中的 `language` 提示，以便上游业务继续使用。
