# Domain Layer

Each capability package (ASR, TTS, LLM, VAD, MCP, tools, music/audio) owns a `base.go` or equivalent interface file that defines the provider contract. Runtime code only consumes those interfaces and never reaches into provider implementations directly. Cross-cutting dependencies flow downward into `internal/shared` and `internal/infrastructure`, ensuring the domain layer stays isolated from server/session packages.
