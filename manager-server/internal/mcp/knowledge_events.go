package mcp

import (
	"context"
	"fmt"

	"manager-server/internal/models"
	"manager-server/internal/service"
)

type kbAgentNotifier struct {
	cm *ConnectionManager
}

// NewKBAgentNotifier wires KB capability change events into the MCP connection manager.
func NewKBAgentNotifier(cm *ConnectionManager) service.KBEventSink {
	return &kbAgentNotifier{cm: cm}
}

func (n *kbAgentNotifier) AgentsCapabilitiesChanged(_ context.Context, agentIDs []uint64) {
	if n == nil || n.cm == nil {
		return
	}
	for _, id := range agentIDs {
		agentID := fmt.Sprintf("%d", id)
		n.cm.BroadcastToolsListChanged(agentID)
	}
}

func (n *kbAgentNotifier) JobStatusChanged(context.Context, *models.KBJob) {
	// no-op: MCP only cares about capability changes
}
