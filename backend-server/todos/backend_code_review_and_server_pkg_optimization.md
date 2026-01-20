## backend-server 代码审阅与优化建议（含 internal/server 包结构重构方案）

本文档汇总对 backend-server 的整体代码审阅结论与优先级改进清单，并提供 `internal/server`（现有实现位于 `internal/app/server`）的包结构与目录组织优化方案，旨在提升稳定性、可维护性与扩展性。

---

### 总览评价

- **优点**: 架构模块化清晰（传输、会话、VAD/ASR/LLM/TTS、MCP、管理端对接）、多协议接入（WebSocket/MQTT/UDP）、端到端语音链路完整；并发与资源池抽象具备良好基础；文档、构建脚本与示例较齐全。
- **风险**: 关停流程未完全优雅（部分组件缺少 Stop/Shutdown）、通道背压策略不统一、VAD/ASR 边界条件需要加固、外部依赖超时/重试/熔断不足、日志与指标粒度可提升、配置校验与热更新不完善、安全限流与输入校验欠缺。
- **方向**: 补齐“关停一致性、背压与容错、观测与容量管理、安全与配置治理、测试与基准”五个工程化面向。

---

### 优先级路线

- **P0（立即）**
  - 为 `WebSocketServer`、`mqttUdpAdapter`、`UdpServer`（以及其他长生命周期服务）实现 `Stop/Shutdown(ctx)` 并纳入 `App.Shutdown`，消除 `select {}` 永久阻塞。
  - 统一通道/队列容量、丢弃策略与超时（配置化），对关键链路添加指标（队列长度、丢弃数、处理耗时）。
  - 外部依赖（ASR/TTS/LLM/管理API）全面补齐 `ctx+timeout`、指数退避重试、错误分级。

- **P1（近期）**
  - VAD/ASR 状态机与边界增强，性能基准与回归样本；日志结构化与降噪；Prometheus/Grafana 指标与告警。
  - 设备级限流与鉴权；输入/输出严格校验；日志脱敏。

- **P2（中期）**
  - 配置热更新与动态调参；资源池自适应扩缩；熔断/降级策略完善。
  - 完整端到端测试集与容量规划报告。

---

### 生命周期与关停

- 在 `cmd/server/main.go` 使用 `errgroup.WithContext` 管理 goroutine，监听错误与 `ctx.Done()`；
- 所有服务（WebSocket、MQTT、UDP、ManagerAPI 等）实现 `Start(ctx) / Stop() / Shutdown(ctx)`；
- 顶层仅在 panic/退出路径调用一次全局 `cleanup.Cleanup()`（幂等）。

---

### 配置治理（viper）

- 启动期强校验：端口、超时、缓冲、服务地址、密钥等缺失或非法直接拒绝启动；
- 无损参数支持热更新（日志级别、限流阈值、欢迎语等），易损参数需重启；
- 敏感字段（密钥/令牌）支持环境变量覆盖、日志脱敏、错误信息脱敏。

---

### 并发与资源管理

- 统一通道/队列策略：容量配置化、阻塞/丢弃策略可选、操作设置超时；
- 资源池化：音频编解码器、VAD/ASR/TTS 客户端、HTTP 连接等通过对象池复用；
- 上下文传播：所有外部调用必须携带 `ctx` 与超时，内部 goroutine 继承会话级 `ctx`。

---

### 传输层与会话

- `ChatSession.Close()` 采用 `cancel()` + 等待 `errgroup` 安全退出，替代固定 `sleep`；
- 传输 `ServerTransport` 的 `Close()` 幂等，并在 `ctx.Done()` 与 `GoodBye` 两路径都能正确释放资源；
- `CmdMessageLoop`/`AudioMessageLoop` 的错误分类与退避策略下沉为通用工具，减少字符串匹配与重复逻辑；
- 设备级限流（文本指令/音频帧/工具调用）与短期隔离机制。

---

### 音频链路：VAD / ASR / TTS

- VAD：统一不同引擎（silero/webrtc）最小帧聚合；提供“暖启动”降低漏检；静默阈值与最大空闲时长配置化；
- ASR：流式识别引入部分结果/最终结果状态机；音频上行超时主动 `Finish`；解码与帧缓冲引入 `sync.Pool` 减少 GC；
- TTS：`SendTtsStart/Stop` 成对保障；播放队列长度与等待时延指标；中断策略与冲刷。

---

### LLM 与 MCP 工具链

- 工具转换失败详细记录并按设备维度打点；
- 工具调用并发/速率配额（全局与会话级）；
- 会话记忆水位控制（轮换淘汰/摘要压缩）；欢迎语/唤醒词策略集中化入口。

---

### 错误处理、日志与观测

- 统一错误类型与严重性分级、抽样日志；关键路径结构化字段（deviceID/sessionID/transport）；
- 指标：队列长度、处理时延直方图、通道丢弃、外部依赖错误率、会话寿命分布；
- pprof 默认关闭，生产按需开启；健康检查 `/healthz` `/readyz`。

---

### 性能优化

- 零拷贝与缓冲池；CPU 绑定任务使用固定 `WorkerPool`，IO 任务 goroutine + 限流；
- VAD 输入帧聚合、管理 API 批量上报；
- 基准覆盖“设备数×帧长×码率”，输出 QPS/尾延时/CPU/内存曲线与推荐配置。

---

### 稳定性与容错

- 指数退避 + 抖动重试；
- 熔断与降级（错误率/时延窗口触发，ASR/TTS 回退次级引擎或文本提示）；
- 心跳与 TTL：无活动自动回收，会话与连接维护。

---

### 安全与合规

- 设备鉴权与速率限制；
- 所有外部输入严格校验（长度、类型、schema）；
- 日志脱敏与安全审计。

---

### 测试与 CI

- 单测：VAD 状态机、ASR 重启、断连/超时恢复；
- 端到端：伪造 WS/MQTT/UDP 客户端回放音频，Mock ASR/TTS/LLM 后端与工具链；
- 基准与回归：样本集覆盖噪声/口音/静默；CI 输出覆盖率与性能趋势。

---

### 构建与发布

- `-ldflags` 注入版本/commit/构建时间，提供 `/version`；
- Docker 多平台构建说明与精简运行时镜像；
- 与管理端/Java 老接口契约保持完全一致（响应与库表一致）。

---

## internal/server 包结构优化方案（现位于 internal/app/server）

为清晰的职责分层与更易维护的依赖方向，建议将现有 `internal/app/server` 抽取为 `internal/server`，并按“生命周期/传输/会话/服务/观测/配置”的维度组织：

```
internal/server/
  lifecycle/                 # 进程生命周期、启动/关停、errgroup、信号处理
    app.go
    shutdown.go

  transport/                 # 传输与连接抽象（协议无关接口 + 各协议实现）
    types/                   # 协议无关接口与公共类型（IConn、消息体）
      conn.go
      message.go
    websocket/
      server.go
      connection.go
    mqtt_udp/
      adapter.go
      udp_server.go
    mqtt/                     # 如有独立 MQTT broker/客户端封装
      server.go

  session/                   # 会话/对话编排（不依赖具体协议）
    chat/
      session.go             # ChatSession
      server_transport.go    # 协议到会话的桥接（对 transport/types 依赖）
      asr_manager.go
      tts_manager.go
      llm_manager.go
      handlers/              # 文本/音频/控制消息处理
        cmd_handler.go
        audio_handler.go

  service/                   # 外部服务整合（manager-api、设备状态等）
    manager_api_service.go
    device_status_manager.go

  auth/
    auth.go

  mcp/
    mcp_bridge.go            # MCP 工具注册与调度适配

  observability/             # 观测：日志适配、metrics、tracing
    logging_adapter.go
    metrics.go

  config/
    config.go                # 读取、校验、默认值、热更新回调
```

### 设计要点

- `session` 仅依赖 `transport/types` 与 `domain/*` 能力，不反向依赖具体协议实现；
- `transport/*` 通过回调统一接入 `OnNewConnection(types.IConn)`；
- `lifecycle` 负责装配与启动顺序管理、Shutdown 链；
- `observability` 提供统一的指标/日志接口，避免在业务层散落 Prometheus/日志细节；
- `config` 负责强校验与默认值注入，导出清晰的结构体供各层使用。

### 迁移步骤（增量、可回滚）

1) 在 `internal/server` 按上述结构创建空包与接口，将 `interfaces` 与 `types` 先抽到 `transport/types`；
2) 迁移 `internal/app/server/chat/*` 至 `internal/server/session/chat/*`，重命名文件：`asr.go -> asr_manager.go`、`tts.go -> tts_manager.go`、`llm.go -> llm_manager.go`；
3) 迁移 `websocket/*`、`mqtt_udp/*` 至 `internal/server/transport/...`，保留对外回调签名不变；
4) 将应用装配与启动逻辑从 `internal/app/server/app.go` 拆分到 `lifecycle/app.go` 与 `lifecycle/shutdown.go`；
5) 引入 `observability/metrics.go`，在核心路径埋点；
6) 更新引用路径与构建脚本，编译并跑基础冒烟测试（WS/MQTT/UDP 回放样例）；
7) 分阶段删除旧路径，保持 tag 与分支可回滚。

### 依赖方向

`lifecycle -> transport/types -> (transport/*, session/*) -> service/* -> domain/*`

确保高层不依赖具体实现，便于替换与扩展。

---

### 附：目录与命名建议

- 文件名统一使用下划线蛇形（Go 默认习惯），保持动词/名词清晰表达职责：`*_manager.go`、`*_handler.go`；
- 避免“杂项 util”收纳，优先将工具函数靠近使用者或沉淀为跨层能力（如 `observability`、`config`）。

---

如需，我可以按 P0 事项直接提交首批改动（关停/通道配置化/超时重试/基础指标），并创建 `internal/server` 骨架以便增量迁移。


