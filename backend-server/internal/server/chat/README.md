# Chat 模块

会话管理与多模态消息处理的核心模块。该目录负责连接 ASR、LLM、TTS、VAD、MCP 工具以及管理端设备配置。

## 目录结构

- `manager.go` / `session.go`：ChatManager 与 ChatSession 入口
- `config/`：设备配置解析、Provider 映射、配置转换
- `managers/`：ASR/LLM/TTS 管理器、对话管线及 LLM 子模块
- `session/`：会话生命周期、消息/音频/命令/LLM/TTS 处理器
- `transport/`：传输层与 server transport
- `vad/`：VAD 服务与配置
- `mcp/`：MCP 连接与类型
- `monitoring/`：会话监控、日志与遥测
- `utils/`：通用工具、会话 ID、VLM 辅助函数

在重构过程中，请保持对外公开接口兼容，并确保所有子模块拥有独立测试。

## ASR 环形缓冲配置

- `ring_buffer_frames`（可选，默认 500）控制 `RingAudioChannel` 可保存的帧数（20ms/帧）；值越大越能抵抗 ASR 慢速或重连时的抖动，但也会增加内存占用与重连后延迟。
- 该参数位于设备的 `asr.config` 下，日志会在缓冲区关闭时输出写入/读取/丢弃统计，并在出现丢帧时以 WARN 级别提示便于调优。
