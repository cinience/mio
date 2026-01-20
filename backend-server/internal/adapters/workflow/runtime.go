package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/adapters/manager/types"
	"backend-server/internal/infrastructure/logger"
)

// Definition represents a workflow definition loaded from manager-server.
type Definition struct {
	ID         uint64          `json:"id"`
	Name       string          `json:"name"`
	Version    int             `json:"version"`
	Status     string          `json:"status"`
	Definition json.RawMessage `json:"definition"`
	Metadata   json.RawMessage `json:"metadata"`
	Tags       json.RawMessage `json:"tags"`
}

// ExecutionLog represents a runtime log entry streamed back to callers.
type ExecutionLog struct {
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	NodeID    string         `json:"nodeId,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// NodeExecutor processes a single node and optionally overrides the next hop.
type NodeExecutor func(ctx context.Context, node *workflowNode, result *ExecutionResult) (string, error)

// ExecutionResult captures the final workflow state and diagnostic logs.
type ExecutionResult struct {
	WorkflowID uint64         `json:"workflowId,omitempty"`
	Success    bool           `json:"success"`
	State      map[string]any `json:"state"`
	Output     map[string]any `json:"output"`
	Logs       []ExecutionLog `json:"logs"`
	StartedAt  time.Time      `json:"startedAt"`
	FinishedAt time.Time      `json:"finishedAt"`
	DurationMs int64          `json:"durationMs"`
}

func newExecutionResult(workflowID uint64, initial map[string]any) *ExecutionResult {
	state := make(map[string]any, len(initial))
	for k, v := range initial {
		state[k] = v
	}
	return &ExecutionResult{
		WorkflowID: workflowID,
		State:      state,
		Output:     make(map[string]any),
		Logs:       make([]ExecutionLog, 0, 8),
	}
}

func (r *ExecutionResult) appendLog(level, message, nodeID string, payload map[string]any) {
	if r == nil {
		return
	}
	log := ExecutionLog{
		ID:        fmt.Sprintf("log-%d", len(r.Logs)+1),
		Timestamp: time.Now().UTC(),
		Level:     level,
		Message:   message,
		NodeID:    nodeID,
		Payload:   payload,
	}
	r.Logs = append(r.Logs, log)
}

// Runtime manages workflow definitions and provides basic lookup/execute helpers.
type Runtime struct {
	mu        sync.RWMutex
	items     map[uint64]Definition
	manager   manager_api.ManagerAPIService
	executors map[string]NodeExecutor
}

// NewRuntime constructs a workflow runtime backed by the manager-api service.
func NewRuntime(manager manager_api.ManagerAPIService) *Runtime {
	return &Runtime{
		items:     make(map[uint64]Definition),
		manager:   manager,
		executors: builtinNodeExecutors(),
	}
}

func builtinNodeExecutors() map[string]NodeExecutor {
	execs := map[string]NodeExecutor{}
	for k, v := range defaultExecutors {
		execs[k] = v
	}
	return execs
}

// Sync loads workflow definitions from manager-api.
func (r *Runtime) Sync(ctx context.Context) error {
	if r == nil || r.manager == nil {
		return nil
	}
	workflows, err := r.manager.ListWorkflows(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = make(map[uint64]Definition, len(workflows))
	for _, wf := range workflows {
		r.items[wf.ID] = Definition{
			ID:         wf.ID,
			Name:       wf.Name,
			Version:    wf.Version,
			Status:     wf.Status,
			Definition: wf.Definition,
			Metadata:   wf.Metadata,
			Tags:       wf.Tags,
		}
	}
	logger.Infof("同步工作流定义: %d 条", len(workflows))
	return nil
}

// List returns the currently cached workflow definitions.
func (r *Runtime) List() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Definition, 0, len(r.items))
	for _, def := range r.items {
		items = append(items, def)
	}
	return items
}

// Execute runs a cached workflow definition by ID.
func (r *Runtime) Execute(ctx context.Context, workflowID uint64, input map[string]any) (*ExecutionResult, error) {
	r.mu.RLock()
	definition, ok := r.items[workflowID]
	r.mu.RUnlock()
	if !ok {
		return nil, manager_api.ErrServiceUnavailable{Service: "workflow", Reason: "definition not loaded"}
	}
	return r.executeDefinition(ctx, workflowID, definition.Definition, input)
}

// ExecuteDefinition evaluates an ad-hoc workflow definition without caching it.
func (r *Runtime) ExecuteDefinition(ctx context.Context, raw json.RawMessage, input map[string]any) (*ExecutionResult, error) {
	return r.executeDefinition(ctx, 0, raw, input)
}

func (r *Runtime) executeDefinition(ctx context.Context, workflowID uint64, raw json.RawMessage, input map[string]any) (*ExecutionResult, error) {
	graph, err := parseDefinition(raw)
	if err != nil {
		return nil, err
	}
	result := newExecutionResult(workflowID, input)
	current := graph.startNode()
	if current == nil {
		return nil, errors.New("workflow missing start node")
	}
	visited := make(map[string]bool)
	result.StartedAt = time.Now()
	for current != nil {
		if visited[current.ID] {
			return nil, fmt.Errorf("workflow contains cycle at node %s", current.ID)
		}
		visited[current.ID] = true
		override, err := r.executeNode(ctx, current, result)
		if err != nil {
			return nil, err
		}
		nextID := override
		if nextID == "" {
			nextID = graph.next(current.ID)
		}
		if nextID == "" {
			break
		}
		current = graph.node(nextID)
	}
	result.Success = true
	result.FinishedAt = time.Now()
	result.DurationMs = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
	for k, v := range result.State {
		result.Output[k] = v
	}
	return result, nil
}

// RegisterNodeExecutor allows callers to override or extend node behaviours.
func (r *Runtime) RegisterNodeExecutor(nodeType string, executor NodeExecutor) {
	if r == nil || nodeType == "" || executor == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.executors == nil {
		r.executors = make(map[string]NodeExecutor)
	}
	r.executors[nodeType] = executor
}

// FromTypes converts manager-api types into runtime definitions.
func FromTypes(items []types.Workflow) []Definition {
	result := make([]Definition, len(items))
	for i, wf := range items {
		result[i] = Definition{
			ID:         wf.ID,
			Name:       wf.Name,
			Version:    wf.Version,
			Status:     wf.Status,
			Definition: wf.Definition,
			Metadata:   wf.Metadata,
			Tags:       wf.Tags,
		}
	}
	return result
}

type workflowGraph struct {
	nodes map[string]*workflowNode
	edges map[string][]workflowEdge
}

type workflowNode struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
	Data   map[string]any `json:"data"`
}

type workflowEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

func parseDefinition(raw json.RawMessage) (*workflowGraph, error) {
	var payload struct {
		Nodes []workflowNode `json:"nodes"`
		Edges []workflowEdge `json:"edges"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("invalid workflow definition: %w", err)
	}
	graph := &workflowGraph{
		nodes: make(map[string]*workflowNode, len(payload.Nodes)),
		edges: make(map[string][]workflowEdge),
	}
	for i := range payload.Nodes {
		nodeCopy := payload.Nodes[i]
		graph.nodes[nodeCopy.ID] = &nodeCopy
	}
	for _, edge := range payload.Edges {
		graph.edges[edge.Source] = append(graph.edges[edge.Source], edge)
	}
	return graph, nil
}

func (g *workflowGraph) node(id string) *workflowNode {
	if g == nil {
		return nil
	}
	return g.nodes[id]
}

func (g *workflowGraph) startNode() *workflowNode {
	for _, node := range g.nodes {
		if node.Type == "start" {
			return node
		}
	}
	for _, node := range g.nodes {
		return node
	}
	return nil
}

func (g *workflowGraph) next(id string) string {
	if edges, ok := g.edges[id]; ok {
		for _, edge := range edges {
			if edge.Target != "" {
				return edge.Target
			}
		}
	}
	return ""
}

func (r *Runtime) executeNode(ctx context.Context, node *workflowNode, result *ExecutionResult) (string, error) {
	if result == nil {
		return "", errors.New("execution result missing")
	}
	result.appendLog("info", fmt.Sprintf("enter node %s", node.Type), node.ID, nil)
	executor := r.executorFor(node.Type)
	if executor == nil {
		logger.Warnf("workflow node type not registered: %s", node.Type)
		return "", nil
	}
	return executor(ctx, node, result)
}

func (r *Runtime) executorFor(nodeType string) NodeExecutor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.executors != nil {
		if exec, ok := r.executors[nodeType]; ok {
			return exec
		}
	}
	return defaultExecutors[nodeType]
}
