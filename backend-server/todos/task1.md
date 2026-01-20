# Task 1: internal/app/server Package Review

## Key Findings
- `internal/app/server/app.go:25` keeps unused `services`, `serviceFactory`, `goroutineManager`, and `workerPool`, signalling unfinished DI work and making lifecycle orchestration opaque.
- Transport adapters (`websocket`, `mqtt_udp`) directly call back into `chat` creating tight coupling and making per-transport policy/testing difficult (`internal/app/server/app.go:209`, `internal/app/server/websocket/websocket_server.go:101`).
- The `chat` package mixes session state, ASR/TTS/VAD orchestration, manager API access, and MCP tooling, leading to >12 files with cross-cutting concerns (`internal/app/server/chat/*`).
- `ProcessVadAudio` allocates per-frame buffers and never releases acquired VAD instances, risking goroutine leaks and VAD pool exhaustion (`internal/app/server/chat/asr.go:31`, `internal/data/client/vad.go:31`, `internal/app/server/chat/asr.go:137`).
- `AsrAudioBuffer.RemoveAsrAudioData` slices without bounds checks and `AddAudioData` holds a lock while writing to a possibly blocking channel (`internal/data/client/asr.go:67`, `internal/data/client/asr.go:124`).

## Optimization Plan

### 1. Restructure Server Bootstrap & Lifecycles
- Extract a dedicated `internal/server/bootstrap` (app wiring) that returns explicit `Run(ctx)` / `Shutdown(ctx)` and remove unused factory fields.
- Introduce interface-driven registries for transports (`transport.Manager`) decoupled from chat, letting transports register themselves and share health/state reporting.
- Replace global `http.HandleFunc` usage with an owned `http.Server` and context-aware goroutine management; surface readiness/metrics endpoints.

### 2. Refactor Chat Pipeline Modules
- Split chat into subpackages: `session`, `pipeline`, `llm`, `tts`, `asr`, `tools` with clear dependency flow; move manager-api config loading into a thin service layer.
- Create a `SessionSupervisor` responsible for lifecycle, device status tracking, and telemetry to keep `ChatManager` focused on dialogue orchestration.
- Standardize configuration structs and validation (e.g. unify VAD/ASR/TTS configs, defaulting, and error propagation) before session start.

### 3. Audio & VAD Improvements
- Build a streaming audio pipeline with pooled buffers (`sync.Pool`) and reusable decoder instances; avoid per-iteration `make([]float32, frameSize)` allocations.
- Ensure VAD providers acquired via `vad.AcquireVAD` are released during session teardown; add guard rails around initialization failures.
- Introduce configurable VAD hysteresis/threshold profiles per device and support switching between silero/energy-based VAD; log structured metrics (activation latency, silence timeout).
- Fix `AsrAudioBuffer.RemoveAsrAudioData` to clamp slice indices and move blocking channel writes outside locks (use buffered channels or context cancellation).

### 4. Reliability & Observability
- Wrap long-running goroutines (`processMessage`, `ProcessVadAudio`, TTS/LLM workers) with context cancellation, panic recovery, and structured logging.
- Add per-transport connection limits, heartbeat tracking, and unit tests mocking `types.IConn` to validate lifecycle edge cases.
- Provide integration tests for end-to-end audio pipeline with synthetic silence/speech patterns to verify VAD-driven state transitions.

### 5. Security & Auth
- Extend `auth.AuthManager` with token TTLs and pluggable stores, and surface authentication errors to transports without panics.
- Audit inbound payload sizes before decoding (MQTT/WebSocket) and enforce limits to protect audio/command channels.

## Immediate TODOs
- [ ] Draft a redesign doc for `internal/app/server` layering (bootstrap, transport, session, domain).
- [ ] Prototype a pooled audio decoder/VAD manager and benchmark allocation reductions.
- [ ] Harden `AsrAudioBuffer` operations and add unit tests covering buffer underflow/overflow.
- [ ] Implement graceful shutdown hooks for WebSocket/MQTT transports with shared tracing/metrics.
- [ ] Extend auth/token handling with TTL and integration tests for expiry paths.
