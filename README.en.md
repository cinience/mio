# xiaozhi-server

## Introduction

xiaozhi-server is an end-to-end multimodal assistant platform. The Go backend delivers low-latency streaming ASR, LLM orchestration, TTS/VAD, MCP tooling, and device management, while the frontend layer provides Vue 2, React 19 (Vite), UniApp, and a Wails-based desktop client to support operations centers and field devices.

## Key Capabilities

- **Streaming voice pipeline**: Integrates OpenAI, FunASR, EdgeTTS, CosyVoice, and more across WebSocket, MQTT, and UDP transports with server-side VAD for latency-sensitive interactions.
- **MCP tool hub**: Bundles global and device-scoped MCP hosts with built-in tools (weather, Home Assistant, podcasts/music) and hooks for custom integrations.
- **Management toolkit**: `manager-server` exposes Go APIs and ships embedded web bundles; Vue/React/UniApp consoles and the Wails desktop app cover administration and device governance.
- **Knowledge & media services**: Works with Elasticsearch, pgvector, and in-memory vector stores, plus podcast center, Subsonic, and local audio pipelines for RAG-ready content.
- **Turnkey deployment**: Offers the `aio-server` launcher and Docker Compose samples with optional embedded Redis or speech-server.

## Repository Layout

```
aio-server/          Go all-in-one launcher with optional embedded Redis
backend-server/      Streaming runtime (cmd/, internal/, config/, pkg/)
manager-server/      Admin API, asset sync scripts, migrations, docs
web/manager-web/     Vue 2 admin portal (npm)
web/manager-console/ React 19 + Vite console (pnpm)
web/manager-mobile/  UniApp mobile console (pnpm)
desktop-app/         Wails desktop client and build helpers
dist/                Prebuilt web assets consumed by Go services
docs/                Architecture briefs, RAG notes, deployment guides
```

Refer to `AGENTS.md` and `docs/` for deeper architectural and contributor guidance.

## Quick Start

```bash
# Backend + manager bundle
cd aio-server && go run ./cmd/server --service backend --service manager

# Backend runtime only
cd backend-server && go test ./... && go run ./cmd/server

# Manager API with embedded UI
cd manager-server && go build ./cmd/server && ./start.sh
```

Frontend and client development entry points:

```bash
cd web/manager-web && npm install && npm run serve
cd web/manager-console && pnpm install && pnpm dev
cd web/manager-mobile && pnpm install && pnpm dev:h5
cd desktop-app && ./dev.sh
```

For containerized setups, see `quickstart.md` and launch with `docker compose up -d` (manager, backend, and optional speech-server).

## Development & Testing

- **Go services**: Require Go 1.21+. Run `gofmt`, `goimports`, `golangci-lint run`, and `go test ./...` (add `-race` for concurrency-heavy changes) before sending patches.
- **Frontend packages**: Each project has its own ESLint/Prettier/UnoCSS rules; prefer `npm ci` or `pnpm install --frozen-lockfile` in CI. Only commit `dist/web/**` when intentionally refreshing the embedded bundles used by `manager-server`.
- **Configuration & security**: Store sensitive overrides in `*.local.yaml` or `.env.local`. Document new MCP connectors or toggles under `docs/` and scrub PII from `backend-server/internal/server/chat` telemetry.

## Documentation & Resources

- Architecture & RAG: `docs/multi-project-knowledge-base*.md`, `docs/others/README_en.md`
- MCP & tooling: `backend-server/internal/domain/mcp/README.md`, `backend-server/internal/fc_tools/README.md`
- Deployment: `quickstart.md` and service-specific notes under `docs/`

## Contributing

Follow Conventional Commits (`feat:`, `fix:`, `chore:`, etc.). PRs should enumerate affected services, mention config or migration changes, and attach UI screenshots or CLI logs for user-visible updates. See `AGENTS.md` for detailed coding, testing, and security practices.

## License

This project uses the repository-level `LICENSE`.
