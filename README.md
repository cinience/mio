# xiaozhi-server

xiaozhi-server 是一套端到端的多模态语音助手基座，涵盖实时语音识别（ASR）、大模型推理、文本转语音（TTS）、语音端点检测（VAD）、MCP 工具编排以及设备管理。Go 服务负责低延迟的流式处理，前端则提供 Vue、React、UniApp 与 Wails 桌面端，满足运营中台与现场设备的多场景需求。

## 能力总览

| 场景                | 说明 |
| ------------------- | ---- |
| **实时语音流水线**  | OpenAI、FunASR、EdgeTTS、CosyVoice 等多模态模型 + WebSocket / MQTT / UDP 通道，结合 server-side VAD，实现毫秒级转写、对话与播报。 |
| **独立语音服务**    | `speech-server` 基于 sherpa-onnx，兼容 OpenAI Realtime 协议，内置 ASR / TTS / VAD 池化和模型自动下载，亦可被 `aio-server` 无缝托管。 |
| **MCP 工具编排**    | 提供全局与设备级 MCP Host，预置 Home Assistant、天气、播客等工具，支持自定义插件与设备隔离。 |
| **管理与运维套件**  | `manager-server` 暴露 API 与嵌入式前端，配合 Vue/React/UniApp/Wails 多端控制台覆盖中台、现场和桌面。 |
| **知识与媒体中心**  | 支持 Elasticsearch、pgvector、内存 VectorStore，集成播客中心、Subsonic、本地音频流水线，满足 RAG 与知识增强需求。 |
| **一体化部署**      | `aio-server` 将 backend / manager / Redis / speech-server 聚合为单进程守护，可通过 Docker、Compose 或裸机方式快速落地。 |

## 仓库结构

```
aio-server/          Go 一体化启动器（可选嵌入式 Redis）
backend-server/      流式语音/LLM 运行时（cmd、internal、config、pkg）
manager-server/      管理 API、静态资源打包与发布脚本
web/manager-web/     Vue 2 管理门户（npm）
web/manager-console/ React 19 + Vite 控制台（pnpm）
web/manager-mobile/  UniApp 移动端控制台（pnpm）
desktop-app/         基于 Wails 的桌面客户端
dist/                构建好的前端资源，供 Go 服务嵌入
docs/                架构说明、RAG 指南、部署文档
```

更多服务说明可参考 `AGENTS.md` 与 `docs/` 目录。

### 核心组件速览

- `backend-server`：流式语音/LLM 运行时，负责 ASR/LLM/TTS/VAD/MCP 等域服务。
- `speech-server`：OpenAI Realtime 兼容的语音服务，默认自带 sherpa-onnx 模型与 server-side VAD。
- `manager-server`：管理 API + 嵌入式前端，负责端点管理、账号体系和运维工作台。
- `aio-server`：一把梭启动器，可选嵌入式 Redis，并在 `sherpa_onnx` 标签下自动托管 speech-server。
- `web/*`、`desktop-app/`：运营端的 Vue/React/UniApp/Wails 客户端。

## 快速体验

```bash
# 后端 + 管理端（推荐）
cd aio-server && go run ./cmd/server --service backend --service manager

# 仅启动流式后端
cd backend-server && go test ./... && go run ./cmd/server

# 管理 API + 嵌入式前端
cd manager-server && go build ./cmd/server && ./start.sh
```

前端与客户端开发入口：

```bash
cd web/manager-web && npm install && npm run serve
cd web/manager-console && pnpm install && pnpm dev
cd web/manager-mobile && pnpm install && pnpm dev:h5
cd desktop-app && ./dev.sh
```

## Docker & Compose 部署

1. **一体化镜像（默认）**

   ```bash
   docker compose -f docker/docker-compose.aio.yaml up --build
   ```

   该 Compose 会基于 `docker/Dockerfile_aio` 构建镜像：包含 backend、manager、Redis 及默认启用的 speech-server（自动拉取 sherpa-onnx 模型）。容器映射 8002/8007/8009 对应 API、控制台、实时语音端点，并挂载 `db/`、`logs/`、`speech-server/models/` 以持久化数据。

2. **细粒度部署**

   - 传统多服务镜像仍使用 `docker/Dockerfile`，仅在需要时通过 GitHub Actions 的 `workflow_dispatch` 输入 `build_legacy=true` 构建。
   - 若需单独运行 `speech-server`，可参考 `docker/Dockerfile_speech_server`；对应 Compose 示例见 `quickstart.md`。

2. **使用预构建镜像（免构建）**

   ```bash
   docker compose -f docker/docker-compose.aio.prebuilt.yaml up -d
   ```

   该 Compose 直接拉取 GitHub Actions 推送到 `ghcr.io/xiaozhi-labs/xiaozhi-server:main` 的镜像，省去本地构建流程。若更偏好单条命令，也可以在仓库根目录执行：

   ```bash
   docker run -d --name xiaozhi-aio -p 8002:8002 -v ./db:/app/db ghcr.io/xiaozhi-labs/xiaozhi-server:latest
   ```

   如需自定义存储路径或端口，可按需调整 `-v` 与 `-p` 参数。

> GitHub Actions `Docker Build` workflow 现默认构建并推送 `aio-server` 镜像，其余镜像按需手动触发即可。

更多容器化说明与示例请参阅 `quickstart.md`。

## 开发与测试

- **Go 服务**：要求 Go 1.21+。提交前运行 `gofmt`、`goimports`、`golangci-lint run` 以及 `go test ./...`（并在并发相关改动中附加 `-race`）。
- **前端项目**：各子包使用独立的 ESLint/Prettier/UnoCSS 配置；CI 中优先使用 `npm ci` 或 `pnpm install --frozen-lockfile`。React 控制台的单测与覆盖率命令为 `pnpm lint`、`pnpm test -- --coverage`。
- **嵌入式资源**：仅在需要更新嵌入式 UI 时提交 `dist/web/**`，否则请使用 `manager-server/start.sh` 重新生成。
- **配置与安全**：敏感变量放置在 `*.local.yaml` 或 `.env.local`，避免泄露到仓库；新增连接器需在 `docs/` 中补充配置说明并排除 PII 日志。

## 文档资源

- 架构与知识库：`docs/multi-project-knowledge-base*.md`、`docs/others/README_en.md`
- 编码规范：`docs/coding-standards.zh.md`
- MCP 与工具：`backend-server/internal/domain/mcp/README.md`、`backend-server/internal/fc_tools/README.md`
- 部署指南：`quickstart.md`、`docs/` 下的环境/脚本说明

## 贡献指南

遵循仓库内的 Conventional Commits（`feat:`、`fix:`、`chore:` 等），在 PR 中说明受影响的服务、配置变更与可视化结果。更多编码规范、测试要求和安全提示见 `AGENTS.md`。

## 许可证

项目遵循仓库根目录的 `LICENSE`。
### Async RAG Pipeline & Knowledge Center Plan

See `docs/design/async-rag-adapter-plan.md` for the roadmap covering:

- MQ-backed asynchronous document pipeline adapters and worker process.
- Knowledge center UX alignment (wizard, monitoring, webhook logs).
