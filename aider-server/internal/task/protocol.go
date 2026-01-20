package task

import (
	"fmt"
	"strings"

	"aider-server/internal/taskmodel"
)

func ResolveProtocol(client string, requested taskmodel.Protocol, defaultProtocol string) (taskmodel.Protocol, error) {
	client = strings.TrimSpace(client)
	raw := strings.TrimSpace(string(requested))
	if !strings.EqualFold(client, "codex") {
		if raw != "" {
			return "", fmt.Errorf("protocol is only supported for codex client")
		}
		return taskmodel.ProtocolPTY, nil
	}
	if raw == "" {
		raw = strings.TrimSpace(defaultProtocol)
	}
	if raw == "" {
		raw = string(taskmodel.ProtocolPTY)
	}
	switch taskmodel.Protocol(raw) {
	case taskmodel.ProtocolPTY, taskmodel.ProtocolAppServer:
		return taskmodel.Protocol(raw), nil
	default:
		return "", fmt.Errorf("unsupported protocol %q", raw)
	}
}
