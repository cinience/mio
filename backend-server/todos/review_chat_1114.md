# Chat 目录重构计划

## 📋 目标

将 `internal/server/chat` 目录从 22 个文件、6445 行代码重构为更合理的模块化结构。

## 🎯 新目录结构

```
internal/server/chat/
├── README.md                    # 模块说明
│
├── manager.go                   # ChatManager 核心
├── session.go                   # ChatSession 核心
│
├── config/                      # 配置管理
│   ├── device_config.go        # 设备配置获取
│   ├── converter.go            # 配置转换
│   └── provider_mapping.go     # Provider 映射
│
├── managers/                    # 业务管理器
│   ├── asr_manager.go
│   ├── asr_manager_test.go
│   ├── llm_manager.go
│   ├── tts_manager.go
│   ├── pipeline.go
│   └── llm/                    # LLM 子模块
│       ├── handler.go
│       ├── tool_executor.go
│       ├── response_processor.go
│       └── context_builder.go
│
├── session/                     # Session 处理器
│   ├── lifecycle.go
│   ├── message_handler.go
│   ├── audio_handler.go
│   ├── command_handler.go
│   ├── llm_handler.go
│   └── tts_handler.go
│
├── transport/                   # 传输层
│   └── server_transport.go
│
├── vad/                        # VAD 服务
│   ├── service.go
│   └── config.go
│
├── mcp/                        # MCP 集成
│   ├── connection.go
│   └── types.go
│
├── monitoring/                  # 监控和日志
│   ├── session_monitor.go
│   ├── session_logger.go
│   ├── telemetry.go
│   └── telemetry_test.go
│
└── utils/                       # 工具函数
    ├── common.go
    ├── common_test.go
    ├── vlm.go
    ├── vlm_test.go
    ├── session_id.go
    └── common_types.go
```

## 📊 文件映射表

### 当前文件 → 新位置

| 当前文件 | 大小(行) | 新位置 | 操作 |
|---------|---------|--------|------|
| chat.go | 812 | → config/device_config.go<br>→ config/converter.go<br>→ config/provider_mapping.go<br>→ manager.go | 拆分 |
| session.go | 1524 | → session.go (核心)<br>→ session/lifecycle.go<br>→ session/message_handler.go<br>→ session/audio_handler.go<br>→ session/command_handler.go<br>→ session/llm_handler.go<br>→ session/tts_handler.go | 拆分 |
| manager_llm.go | 861 | → managers/llm_manager.go<br>→ managers/llm/handler.go<br>→ managers/llm/tool_executor.go<br>→ managers/llm/response_processor.go<br>→ managers/llm/context_builder.go | 拆分 |
| manager_asr.go | 162 | → managers/asr_manager.go | 移动 |
| manager_asr_test.go | 238 | → managers/asr_manager_test.go | 移动 |
| manager_tts.go | 297 | → managers/tts_manager.go | 移动 |
| conversation_pipeline.go | 96 | → managers/pipeline.go | 移动 |
| transport_server.go | 355 | → transport/server_transport.go | 移动 |
| vad_service.go | 423 | → vad/service.go | 移动 |
| vad_config.go | 182 | → vad/config.go | 移动 |
| mcp_connection.go | 124 | → mcp/connection.go | 移动 |
| mcp_types.go | 264 | → mcp/types.go | 移动 |
| session_monitor.go | 125 | → monitoring/session_monitor.go | 移动 |
| session_logger.go | 255 | → monitoring/session_logger.go | 移动 |
| telemetry.go | 277 | → monitoring/telemetry.go | 移动 |
| telemetry_test.go | 17 | → monitoring/telemetry_test.go | 移动 |
| utils_common.go | 170 | → utils/common.go | 移动 |
| utils_common_test.go | 55 | → utils/common_test.go | 移动 |
| utils_vlm.go | 59 | → utils/vlm.go | 移动 |
| utils_vlm_test.go | 103 | → utils/vlm_test.go | 移动 |
| session_id.go | 28 | → utils/session_id.go | 移动 |
| common.go | 18 | → utils/common_types.go | 移动 |

## 🚀 实施步骤

### Phase 1: 准备（0.5天）

```bash
# 1. 创建分支
git checkout -b refactor/chat-directory-structure

# 2. 备份当前代码
cd backend-server/internal/server
cp -r chat chat.backup.$(date +%Y%m%d)

# 3. 创建新目录
cd chat
mkdir -p config managers/llm session transport vad mcp monitoring utils

# 4. 创建 README
cat > README.md << 'EOF'
# Chat 模块

会话管理和消息处理的核心模块。

## 目录结构

- `config/` - 设备配置管理
- `managers/` - 业务管理器（ASR、LLM、TTS）
- `session/` - 会话处理逻辑
- `transport/` - 传输层
- `vad/` - VAD 服务
- `mcp/` - MCP 集成
- `monitoring/` - 监控和日志
- `utils/` - 工具函数

## 核心类型

- `ChatManager` - 管理设备会话的生命周期
- `ChatSession` - 处理单个会话的消息流
EOF
```

### Phase 2: 移动独立文件（1天）

**优先级：高，风险：低**

```bash
#!/bin/bash
# move_simple_files.sh

# 移动 managers
git mv manager_asr.go managers/asr_manager.go
git mv manager_asr_test.go managers/asr_manager_test.go
git mv manager_tts.go managers/tts_manager.go
git mv conversation_pipeline.go managers/pipeline.go

# 移动 transport
git mv transport_server.go transport/server_transport.go

# 移动 vad
git mv vad_service.go vad/service.go
git mv vad_config.go vad/config.go

# 移动 mcp
git mv mcp_connection.go mcp/connection.go
git mv mcp_types.go mcp/types.go

# 移动 monitoring
git mv session_monitor.go monitoring/session_monitor.go
git mv session_logger.go monitoring/session_logger.go
git mv telemetry.go monitoring/telemetry.go
git mv telemetry_test.go monitoring/telemetry_test.go

# 移动 utils
git mv utils_common.go utils/common.go
git mv utils_common_test.go utils/common_test.go
git mv utils_vlm.go utils/vlm.go
git mv utils_vlm_test.go utils/vlm_test.go
git mv session_id.go utils/session_id.go
git mv common.go utils/common_types.go

# 更新包名
find managers transport vad mcp monitoring utils -name "*.go" -exec sed -i '' 's/package chat$/package \$(basename \$(dirname {}))/g' {} \;
```

**验证步骤**：
```bash
# 编译检查
go build ./internal/server/chat/...

# 运行测试
go test ./internal/server/chat/...
```

### Phase 3: 拆分 chat.go (2天)

**优先级：高，风险：中**

#### 3.1 提取配置相关代码

```bash
# config/device_config.go - 设备配置获取
# 包含函数:
# - GenClientState (部分)
# - getDeviceConfigWithManagerAPI
# - buildDefaultDeviceConfig
# - getSelectedModules
# - notifyDeviceConfigFailure

# config/converter.go - 配置转换
# 包含函数:
# - convertAgentConfigToUConfig
# - mergeMetadata
# - ensureSubsonicMetadata
# - buildSubsonicMetadataURL
# - structToMap

# config/provider_mapping.go - Provider 映射
# 包含函数:
# - getModelProvider
# - getModelConfig
# - selectVADConfig
# - selectASRConfig
# - selectTTSConfig
# - selectLLMConfig
# - applyTTSOutputFormat
# - extractPositiveInt
# - parseInt
```

**创建文件示例**：

```go
// config/device_config.go
package config

import (
    "context"
    "backend-server/internal/data/client"
    // ...
)

// GetDeviceConfig 获取设备配置（主入口）
func GetDeviceConfig(ctx context.Context, managerAPI manager_api.ManagerAPIService, deviceID string, remoteAddr string) (*client.ClientState, error) {
    // 从 GenClientState 移动核心逻辑
}

// getDeviceConfigWithManagerAPI 从 manager-api 获取配置
func getDeviceConfigWithManagerAPI(...) (utypes.UConfig, error) {
    // 移动原有逻辑
}
```

```go
// manager.go (精简后的 chat.go)
package chat

import (
    "backend-server/internal/server/chat/config"
    "backend-server/internal/server/chat/monitoring"
    // ...
)

type ChatManager struct {
    DeviceID  string
    transport types_conn.IConn
    
    clientState *client.ClientState
    session     *ChatSession
    ctx         context.Context
    cancel      context.CancelFunc
    monitor     *monitoring.SessionMonitor
    closeOnce   sync.Once
    services    *service.Registry
}

func NewChatManager(deviceID string, transport types_conn.IConn, managerAPI manager_api.ManagerAPIService, options ...ChatManagerOption) (*ChatManager, error) {
    // 使用 config 包
    clientState, err := config.GetDeviceConfig(cm.ctx, managerAPI, cm.DeviceID, transport.GetRemoteAddr())
    // ...
}
```

**测试验证**：
```bash
# 单元测试
go test ./internal/server/chat/config/...

# 集成测试
go test ./internal/server/chat/... -run TestChatManager
```

### Phase 4: 拆分 session.go (3天)

**优先级：高，风险：高**

#### 4.1 识别职责

```go
// session.go 当前包含:
// 1. ChatSession 结构体定义
// 2. 初始化和生命周期管理
// 3. 消息循环 (cmd_message_loop, audio_message_loop, chat_processing_loop, llm_manager_loop, tts_manager_loop)
// 4. 命令处理
// 5. 音频处理
// 6. LLM 处理
// 7. TTS 处理
// 8. 各种辅助函数
```

#### 4.2 创建新文件

```go
// session.go (核心，保留约 300 行)
package chat

type ChatSession struct { ... }
func NewChatSession(...) *ChatSession { ... }
func (s *ChatSession) Start(pctx context.Context) error { ... }
func (s *ChatSession) Close() { ... }

// session/lifecycle.go
package session

type Lifecycle struct {
    session *chat.ChatSession
}

func (l *Lifecycle) Initialize() error { ... }
func (l *Lifecycle) StartLoops() error { ... }
func (l *Lifecycle) Shutdown(ctx context.Context) error { ... }

// session/message_handler.go
package session

type MessageHandler struct {
    session *chat.ChatSession
}

func (h *MessageHandler) HandleCmdLoop(ctx context.Context) error { ... }
func (h *MessageHandler) ProcessClientMessage(msg *client.ClientMessage) { ... }

// session/audio_handler.go
package session

type AudioHandler struct {
    session *chat.ChatSession
}

func (h *AudioHandler) HandleAudioLoop(ctx context.Context) error { ... }
func (h *AudioHandler) ProcessAudioFrame(frame []byte) error { ... }

// session/command_handler.go
package session

type CommandHandler struct {
    session *chat.ChatSession
}

func (h *CommandHandler) HandleTextCommand(text string) { ... }
func (h *CommandHandler) HandleAbortCommand() { ... }

// session/llm_handler.go
package session

type LLMHandler struct {
    session *chat.ChatSession
}

func (h *LLMHandler) RunLLMLoop(ctx context.Context) error { ... }
func (h *LLMHandler) ProcessLLMRequest(item AsrResponseChannelItem) { ... }

// session/tts_handler.go
package session

type TTSHandler struct {
    session *chat.ChatSession
}

func (h *TTSHandler) RunTTSLoop(ctx context.Context) error { ... }
func (h *TTSHandler) ProcessTTSRequest(text string) { ... }
```

#### 4.3 重构 ChatSession

```go
// session.go (重构后)
package chat

import (
    "backend-server/internal/server/chat/session"
)

type ChatSession struct {
    // 核心字段
    clientState     *client.ClientState
    serverTransport *transport.ServerTransport
    
    // 处理器 (使用组合模式)
    lifecycle      *session.Lifecycle
    messageHandler *session.MessageHandler
    audioHandler   *session.AudioHandler
    commandHandler *session.CommandHandler
    llmHandler     *session.LLMHandler
    ttsHandler     *session.TTSHandler
    
    // 管理器
    asrManager *managers.ASRManager
    ttsManager *managers.TTSManager
    llmManager *managers.LLMManager
    
    // ... 其他字段
}

func NewChatSession(clientState *client.ClientState, serverTransport *transport.ServerTransport, opts ...ChatSessionOption) *ChatSession {
    s := &ChatSession{
        clientState:     clientState,
        serverTransport: serverTransport,
    }
    
    // 初始化处理器
    s.lifecycle = session.NewLifecycle(s)
    s.messageHandler = session.NewMessageHandler(s)
    s.audioHandler = session.NewAudioHandler(s)
    s.commandHandler = session.NewCommandHandler(s)
    s.llmHandler = session.NewLLMHandler(s)
    s.ttsHandler = session.NewTTSHandler(s)
    
    return s
}

func (s *ChatSession) Start(pctx context.Context) error {
    return s.lifecycle.Initialize(pctx)
}
```

**渐进式迁移**：
```bash
# 1. 先创建新文件和接口，保留旧代码
# 2. 逐个迁移功能，双写一段时间
# 3. 切换到新实现
# 4. 删除旧代码
```

### Phase 5: 拆分 manager_llm.go (2天)

**优先级：中，风险：中**

```go
// managers/llm_manager.go (核心，约 200 行)
package managers

type LLMManager struct {
    clientState     *client.ClientState
    serverTransport *transport.ServerTransport
    ttsManager      *TTSManager
    session         SessionInterface
    
    // 子模块
    handler          *llm.Handler
    toolExecutor     *llm.ToolExecutor
    responseProcessor *llm.ResponseProcessor
    contextBuilder   *llm.ContextBuilder
}

// managers/llm/handler.go
package llm

func (h *Handler) ProcessRequest(ctx context.Context, asrText string) error { ... }

// managers/llm/tool_executor.go
package llm

func (e *ToolExecutor) ExecuteTools(ctx context.Context, toolCalls []schema.ToolCall) ([]schema.Message, error) { ... }

// managers/llm/response_processor.go
package llm

func (p *ResponseProcessor) ProcessLLMResponse(ctx context.Context, response <-chan common.LLMResponseStruct) error { ... }

// managers/llm/context_builder.go
package llm

func (b *ContextBuilder) BuildContext(dialogue []*schema.Message, systemPrompt string) []*schema.Message { ... }
```

### Phase 6: 更新导入和测试 (2天)

```bash
# 1. 更新所有导入路径
find . -name "*.go" -exec sed -i '' 's|"backend-server/internal/server/chat"|"backend-server/internal/server/chat/xxx"|g' {} \;

# 2. 运行完整测试套件
go test ./internal/server/chat/... -v

# 3. 运行竞态检测
go test ./internal/server/chat/... -race

# 4. 检查测试覆盖率
go test ./internal/server/chat/... -cover
```

### Phase 7: 清理和文档 (1天)

```bash
# 1. 删除备份文件
rm -rf chat.backup.*

# 2. 生成文档
gomarkdoc ./internal/server/chat/...

# 3. 更新 README
# 4. 提交代码审查
```

## 📝 重构检查清单

### 每个 Phase 完成后

- [ ] 代码编译通过 (`go build`)
- [ ] 测试全部通过 (`go test`)
- [ ] 无竞态条件 (`go test -race`)
- [ ] 导入路径正确
- [ ] 包名正确
- [ ] 文档已更新
- [ ] Git 提交已完成

### 整体完成后

- [ ] 所有文件已移动到新位置
- [ ] 没有超过 500 行的大文件
- [ ] 包的职责清晰
- [ ] 循环依赖检查 (`go list -json ./... | jq`)
- [ ] 性能测试无退化
- [ ] 文档完整
- [ ] Code Review 通过

## ⚠️ 风险和注意事项

### 高风险操作

1. **session.go 拆分**
   - 风险：文件太大，逻辑复杂
   - 缓解：渐进式重构，保持双写
   - 测试：重点测试消息循环

2. **manager_llm.go 拆分**
   - 风险：工具调用逻辑复杂
   - 缓解：先提取独立函数
   - 测试：工具执行的集成测试

3. **导入路径更新**
   - 风险：可能遗漏某些文件
   - 缓解：使用脚本批量更新
   - 验证：编译检查

### 注意事项

1. **包循环依赖**
   ```bash
   # 检查循环依赖
   go list -json ./internal/server/chat/... | jq -r 'select(.ImportPath != null) | .ImportPath + " imports " + (.Imports | join(", "))'
   ```

2. **接口兼容性**
   - 保持公开接口不变
   - 内部重构可以大胆调整

3. **测试隔离**
   - 每个子包都应该有独立的测试
   - 使用 mock 减少依赖

## 🎯 成功标准

### 定量指标

- [ ] 最大文件不超过 500 行
- [ ] 测试覆盖率 > 70%
- [ ] 包的数量从 1 个增加到 8 个
- [ ] 平均文件大小从 293 行降低到 < 200 行

### 定性指标

- [ ] 职责清晰，每个包只做一件事
- [ ] 易于理解和维护
- [ ] 便于新人快速上手
- [ ] 方便单元测试

## 📚 参考资料

- [Go 项目结构最佳实践](https://github.com/golang-standards/project-layout)
- [Clean Architecture in Go](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)
- [Package Oriented Design](https://www.ardanlabs.com/blog/2017/02/package-oriented-design.html)

## 📅 时间线

| Phase | 任务 | 时间 | 负责人 |
|-------|------|------|--------|
| 1 | 准备 | 0.5天 | - |
| 2 | 移动独立文件 | 1天 | - |
| 3 | 拆分 chat.go | 2天 | - |
| 4 | 拆分 session.go | 3天 | - |
| 5 | 拆分 manager_llm.go | 2天 | - |
| 6 | 更新导入和测试 | 2天 | - |
| 7 | 清理和文档 | 1天 | - |
| **总计** | | **11.5天** | |

## 🔄 回滚计划

如果重构失败，执行以下步骤：

```bash
# 1. 恢复备份
git checkout main
rm -rf internal/server/chat
cp -r internal/server/chat.backup.YYYYMMDD internal/server/chat

# 2. 验证功能
go test ./internal/server/chat/...

# 3. 分析失败原因
# 4. 调整计划后重试
```

---

**最后更新**: 2025-11-14
**版本**: 1.0


