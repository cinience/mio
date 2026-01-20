<!-- OPENSPEC:START -->
# OpenSpec Instructions

These instructions are for AI assistants working in this project.

Always open `@/openspec/AGENTS.md` when the request:
- Mentions planning or proposals (words like proposal, spec, change, plan)
- Introduces new capabilities, breaking changes, architecture shifts, or big performance/security work
- Sounds ambiguous and you need the authoritative spec before coding

Use `@/openspec/AGENTS.md` to learn:
- How to create and apply change proposals
- Spec format and conventions
- Project structure and guidelines

Keep this managed block so 'openspec update' can refresh the instructions.

<!-- OPENSPEC:END -->

# Repository Guidelines

## Project Structure

- `backend-server/` — streaming runtime (Go 1.21+) with `cmd/`, `internal/` domains for ASR/LLM/TTS/VAD/MCP, plus shared utilities in `pkg/`; default configs live in `config/`.
- `manager-server/` — admin API and embedded UI bundles; `start.sh` syncs assets from `dist/web/**` or rebuilds the `web/manager-*` packages into `webassets/`.
- `aio-server/` — all-in-one launcher wiring backend, manager, and optional embedded Redis via CLI flags.
- `web/manager-web`, `web/manager-console`, `web/manager-mobile` — Vue 2, Vite + React 19, and UniApp consoles (pnpm-only for UniApp); shared assets are staged under `dist/`.
- `desktop-app/` — Wails-based desktop shell (`xiaozhi-desktop/`) with helper scripts (`dev.sh`, `quick-build.sh`); `docs/` holds architecture notes, RAG guidance, and deployment docs.

## Development Workflow

```
cd aio-server && go run ./cmd/server --service backend --service manager
cd backend-server && go test ./...                                  # unit + domain regressions
cd manager-server && go build ./cmd/server && ./start.sh            # rebuilds & serves embedded web
cd web/manager-web && npm install && npm run serve                  # Vue dev server on :8080
cd web/manager-console && pnpm install && pnpm dev                  # Vite dev server + React console
cd web/manager-console && pnpm lint && pnpm test -- --coverage
cd web/manager-mobile && pnpm install && pnpm dev:h5                # UniApp H5 preview
cd desktop-app && ./dev.sh                                          # runs wails dev with frontend hot reload
```

Prefer `npm ci`/`pnpm install --frozen-lockfile` in CI. Commit the `dist/web/**` artifacts only when intentionally refreshing the embedded UI consumed by Go services.

## Coding Standards

- **Go**: Format with `gofmt`/`goimports`, lint using `golangci-lint run`. Exported symbols stay PascalCase, private helpers use camelCase, and configuration structs mirror YAML keys verbatim.
- **TypeScript/JavaScript**: Follow the ESLint configs provided in each package (`@antfu/eslint-config` for UniApp, `eslint-config-prettier` for React). Vue code keeps two-space indentation; elsewhere tabs are disabled. Component files use PascalCase, hooks/services reside in kebab-case directories with camelCase filenames.
- **Styling**: Align CSS/Uno class naming with existing patterns—BEM for Vue legacy pages, utility-first in UniApp.
- **Reference**: Consolidated coding guide lives at `docs/coding-standards.zh.md`.

## Testing Expectations

- **Go modules**: Use table-driven tests (`*_test.go` under `backend-server/internal/**`, `manager-server/internal/**`). Run `go test ./... -race` for concurrency-heavy changes and maintain ≥70% coverage for touched packages.
- **React console**: Rely on Vitest + Testing Library. Place tests under `web/manager-console/src/__tests__` and keep coverage green via `pnpm test -- --coverage`.
- **UniApp**: Combine ESLint with Uni Automator. Smoke test primary flows with `pnpm dev:h5` until automation matures.
- **Fixtures**: Store device/LLM fixtures in `docs/others/` instead of embedding large constants directly in tests.

## Commits & Pull Requests

- Follow the repository’s Conventional Commit convention (`feat:`, `fix:`, `chore:`, `refactor:`, …) with subjects under 72 characters.
- Each PR should state impacted services (backend, manager, console, mobile, desktop), reference tracking issues, note migrations/config toggles, and attach UI screenshots or CLI logs for user-facing changes.
- Keep stacks logical—split infra, backend, frontend, and docs updates so reviewers can cherry-pick easily.

## Security & Configuration

- Never commit real secrets. Use `backend-server/config/*.local.yaml`, `manager-server/config/*.local.yaml`, or `.env.local` overrides that stay ignored by git.
- `manager-server/start.sh` copies built frontends into `webassets/`; always verify the output to avoid shipping stale bundles.
- Document new connectors (LLM, TTS, storage, etc.) in `docs/`, including required environment variables. Scrub PII from `backend-server/internal/server/chat` telemetry and other logs.
- When adding ASR or TTS providers, follow `docs/asr-tts-provider-integration.md` to ensure backend, config, and manager UI changes are complete.
