# MCP Domain Refactor TODOs (Phase 1)

- [x] Align global MCP manager runtime configuration with `config.MCP` values and surface transport creation failures (`internal/domain/mcp/global_manage.go`:70-218).
- [x] Fix global tool lookup prefixing bug and guard lookups against nil transports (`internal/domain/mcp/global_manage.go`:527-552).
- [x] Rename `sendInitlize` to `sendInitialize`, handle initialization errors, and sequence client start correctly (`internal/domain/mcp/device_manager.go`:65-420).
- [x] Convert the MCP client pool offline sweep into a periodic watcher with context awareness (`internal/domain/mcp/mcp_pool.go`:18-66).
- [x] Ensure custom transports actually forward notifications to remote peers instead of short-circuiting locally (`internal/domain/mcp/websocket_transport.go`:33-78, `internal/domain/mcp/iot_over_mcp_transport.go`:19-63).
