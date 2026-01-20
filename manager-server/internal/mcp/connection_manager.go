package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"manager-server/internal/logger"
)

// RobotConnection 小智端连接信息
type RobotConnection struct {
	WebSocket      *websocket.Conn `json:"-"`
	AgentID        string          `json:"agentId"`
	ConnectionUUID string          `json:"connectionUUID"`
	Timestamp      int64           `json:"timestamp"`
}

// ToolConnection 工具端连接信息
type ToolConnection struct {
	WebSocket    *websocket.Conn `json:"-"`
	AgentID      string          `json:"agentId"`
	ConnectionID string          `json:"connectionId"`
	Timestamp    int64           `json:"timestamp"`
	RemoteAddr   string          `json:"remoteAddr"`
}

// ToolInfo 工具信息
type ToolInfo struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"inputSchema"`
	UpdatedAt    int64                  `json:"updatedAt"`
	ConnectionID string                 `json:"connectionId"`
}

type ToolConnectionSnapshot struct {
	ConnectionID string
	RemoteAddr   string
	ConnectedAt  int64
}

type ToolInfoSnapshot struct {
	Name         string
	Description  string
	InputSchema  map[string]interface{}
	UpdatedAt    int64
	ConnectionID string
	ConnectedAt  int64
	RemoteAddr   string
}

type pendingRequest struct {
	connectionUUID   string
	originalID       interface{}
	responseChan     chan map[string]interface{}
	method           string
	toolName         string
	arguments        interface{}
	toolConnectionID string
}

type pendingHTTPRequest struct {
	originalID   interface{}
	responseChan chan map[string]interface{}
	method       string
	toolName     string
	arguments    interface{}
}

// ConnectionManager 连接管理器
type ConnectionManager struct {
	// 工具端连接: {agentId: {toolConnectionId: ToolConnection}}
	toolConnections map[string]map[string]*ToolConnection
	// 小智端连接: {connection_uuid: RobotConnection}
	robotConnections map[string]*RobotConnection
	// 连接时间戳: {agentId: timestamp}
	connectionTimestamps map[string]int64
	// 工具缓存: {agentId: {toolName: ToolInfo}}
	toolsCache map[string]map[string]*ToolInfo
	// 工具列表更新时间: {agentId: timestamp}
	toolsUpdatedAt map[string]int64
	// WebSocket写入锁: {agentId: {toolConnectionId: mutex}}
	writeMutexes map[string]map[string]*sync.Mutex
	// 工具列表请求状态: {agentId: {toolConnectionId: bool}} 防止重复请求
	toolsRequesting map[string]map[string]bool
	// 连接锁
	mutex sync.RWMutex
	// 每个agent的自增序列计数器
	sequenceCounters map[string]int64
	// 待恢复的请求映射: {agentId: {generatedId: pendingRequest}}
	pendingRequests map[string]map[string]*pendingRequest
	// 待返回的HTTP请求: {agentId: {generatedId: pendingHTTPRequest}}
	pendingHTTPRequests map[string]map[string]*pendingHTTPRequest
}

// NewConnectionManager 创建新的连接管理器
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		toolConnections:      make(map[string]map[string]*ToolConnection),
		robotConnections:     make(map[string]*RobotConnection),
		connectionTimestamps: make(map[string]int64),
		toolsCache:           make(map[string]map[string]*ToolInfo),
		toolsUpdatedAt:       make(map[string]int64),
		writeMutexes:         make(map[string]map[string]*sync.Mutex),
		toolsRequesting:      make(map[string]map[string]bool),
		sequenceCounters:     make(map[string]int64),
		pendingRequests:      make(map[string]map[string]*pendingRequest),
		pendingHTTPRequests:  make(map[string]map[string]*pendingHTTPRequest),
	}
}

// getToolConnection returns the tool websocket connection and write mutex for the agent.
func (cm *ConnectionManager) getToolConnection(agentID, connectionID string) (*ToolConnection, *sync.Mutex, error) {
	cm.mutex.RLock()
	connections := cm.toolConnections[agentID]
	writeLocks := cm.writeMutexes[agentID]
	cm.mutex.RUnlock()

	if connections == nil {
		return nil, nil, fmt.Errorf("工具端连接不存在: %s", agentID)
	}

	conn := connections[connectionID]
	if conn == nil {
		return nil, nil, fmt.Errorf("工具端连接不存在: agent=%s connection_id=%s", agentID, connectionID)
	}

	if writeLocks == nil {
		return nil, nil, fmt.Errorf("写入锁不存在: agent=%s connection_id=%s", agentID, connectionID)
	}

	writeMutex := writeLocks[connectionID]
	if writeMutex == nil {
		return nil, nil, fmt.Errorf("写入锁不存在: agent=%s connection_id=%s", agentID, connectionID)
	}

	return conn, writeMutex, nil
}

// getDefaultToolConnectionID returns the first available tool connection ID for the agent.
func (cm *ConnectionManager) getDefaultToolConnectionID(agentID string) (string, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	connections := cm.toolConnections[agentID]
	if len(connections) == 0 {
		return "", fmt.Errorf("工具端连接不存在: %s", agentID)
	}

	for connectionID := range connections {
		return connectionID, nil
	}

	return "", fmt.Errorf("工具端连接不存在: %s", agentID)
}

// listToolConnectionIDs returns all tool connection IDs for the agent.
func (cm *ConnectionManager) listToolConnectionIDs(agentID string) []string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	connections := cm.toolConnections[agentID]
	if len(connections) == 0 {
		return nil
	}

	result := make([]string, 0, len(connections))
	for connectionID := range connections {
		result = append(result, connectionID)
	}
	return result
}

// resolveToolConnectionByName finds the tool connection responsible for a specific tool.
func (cm *ConnectionManager) resolveToolConnectionByName(agentID, toolName string) (string, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if toolsMap, exists := cm.toolsCache[agentID]; exists {
		if info, ok := toolsMap[toolName]; ok && info != nil && info.ConnectionID != "" {
			return info.ConnectionID, nil
		}
	}
	return "", fmt.Errorf("未找到工具所属连接: agent=%s tool=%s", agentID, toolName)
}

// assignPendingRequestToolConnection attaches a tool connection ID to a pending request.
func (cm *ConnectionManager) assignPendingRequestToolConnection(agentID string, generatedID interface{}, toolConnectionID string) {
	if generatedID == nil || toolConnectionID == "" {
		return
	}

	key := fmt.Sprintf("%v", generatedID)

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if requests, exists := cm.pendingRequests[agentID]; exists {
		if info, ok := requests[key]; ok && info != nil {
			info.toolConnectionID = toolConnectionID
		}
	}
}

// getRobotConnection returns the robot websocket connection for the UUID.
func (cm *ConnectionManager) getRobotConnection(connectionUUID string) (*RobotConnection, error) {
	cm.mutex.RLock()
	robotConn, exists := cm.robotConnections[connectionUUID]
	cm.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("小智端连接不存在: %s", connectionUUID)
	}

	return robotConn, nil
}

// stringifyMessage normalises outgoing payload to a JSON string.
func (cm *ConnectionManager) stringifyMessage(message interface{}) (string, error) {
	switch msg := message.(type) {
	case string:
		return msg, nil
	case []byte:
		return string(msg), nil
	case json.RawMessage:
		return string(msg), nil
	case map[string]interface{}:
		data, err := json.Marshal(msg)
		if err != nil {
			return "", fmt.Errorf("消息序列化失败: %w", err)
		}
		return string(data), nil
	default:
		data, err := json.Marshal(msg)
		if err != nil {
			return "", fmt.Errorf("消息序列化失败: %w", err)
		}
		return string(data), nil
	}
}

// writeToTool sends payload to the tool websocket while guarding the write mutex.
func (cm *ConnectionManager) writeToTool(agentID, connectionID, payload string) error {
	conn, writeMutex, err := cm.getToolConnection(agentID, connectionID)
	if err != nil {
		return err
	}

	writeMutex.Lock()
	defer writeMutex.Unlock()

	if err := conn.WebSocket.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
		cm.UnregisterToolConnection(agentID, connectionID)
		return fmt.Errorf("发送消息失败: %w", err)
	}

	return nil
}

// writeToRobot sends payload to the robot websocket and cleans up on failure.
func (cm *ConnectionManager) writeToRobot(connectionUUID string, robotConn *RobotConnection, payload string) error {
	if err := robotConn.WebSocket.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
		cm.UnregisterRobotConnection(connectionUUID)
		return fmt.Errorf("发送消息失败: %w", err)
	}

	return nil
}

func truncateForLog(message string) string {
	if len(message) <= 100 {
		return message
	}
	return message[:100]
}

func extractJSONRPCMeta(payload string) (interface{}, string) {
	var jsonPayload map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &jsonPayload); err != nil {
		return nil, ""
	}

	method, _ := jsonPayload["method"].(string)
	return jsonPayload["id"], method
}

func (cm *ConnectionManager) storePendingRequest(agentID, connectionUUID string, originalID interface{}, responseChan chan map[string]interface{}, method string, toolName string, arguments interface{}) (int64, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	seq := cm.sequenceCounters[agentID] + 1
	cm.sequenceCounters[agentID] = seq

	if cm.pendingRequests[agentID] == nil {
		cm.pendingRequests[agentID] = make(map[string]*pendingRequest)
	}

	cm.pendingRequests[agentID][fmt.Sprintf("%d", seq)] = &pendingRequest{
		connectionUUID: connectionUUID,
		originalID:     originalID,
		responseChan:   responseChan,
		method:         method,
		toolName:       toolName,
		arguments:      arguments,
	}

	return seq, nil
}

func (cm *ConnectionManager) storePendingHTTPRequest(agentID string, seq int64, originalID interface{}, responseChan chan map[string]interface{}, method string, toolName string, arguments interface{}) {
	if cm.pendingHTTPRequests[agentID] == nil {
		cm.pendingHTTPRequests[agentID] = make(map[string]*pendingHTTPRequest)
	}

	cm.pendingHTTPRequests[agentID][fmt.Sprintf("%d", seq)] = &pendingHTTPRequest{
		originalID:   originalID,
		responseChan: responseChan,
		method:       method,
		toolName:     toolName,
		arguments:    arguments,
	}
}

func (cm *ConnectionManager) popPendingRequest(agentID string, transformedID interface{}, toolConnectionID string) (string, interface{}, chan map[string]interface{}, string, string, interface{}, error) {
	key := fmt.Sprintf("%v", transformedID)

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	requests := cm.pendingRequests[agentID]
	if requests == nil {
		return "", nil, nil, "", "", nil, fmt.Errorf("agent %s 没有待恢复的请求", agentID)
	}

	info, exists := requests[key]
	if !exists {
		return "", nil, nil, "", "", nil, fmt.Errorf("agent %s 未找到待恢复的请求 id=%s", agentID, key)
	}

	if info.toolConnectionID != "" && toolConnectionID != "" && info.toolConnectionID != toolConnectionID {
		return "", nil, nil, "", "", nil, fmt.Errorf("agent %s 待恢复请求工具连接不匹配: expected=%s actual=%s id=%s", agentID, info.toolConnectionID, toolConnectionID, key)
	}

	delete(requests, key)
	if len(requests) == 0 {
		delete(cm.pendingRequests, agentID)
	}

	return info.connectionUUID, info.originalID, info.responseChan, info.method, info.toolName, info.arguments, nil
}

// cancelPendingRequest removes a pending request for the given agent and generated ID.
func (cm *ConnectionManager) cancelPendingRequest(agentID string, generatedID int64) {
	key := fmt.Sprintf("%d", generatedID)

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if requests, exists := cm.pendingRequests[agentID]; exists {
		delete(requests, key)
		if len(requests) == 0 {
			delete(cm.pendingRequests, agentID)
		}
	}
}

func (cm *ConnectionManager) popPendingHTTPRequest(agentID string, transformedID interface{}) (interface{}, chan map[string]interface{}, string, string, interface{}, bool) {
	key := fmt.Sprintf("%v", transformedID)

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	requests := cm.pendingHTTPRequests[agentID]
	if requests == nil {
		return nil, nil, "", "", nil, false
	}

	info, exists := requests[key]
	if !exists {
		return nil, nil, "", "", nil, false
	}

	delete(requests, key)
	if len(requests) == 0 {
		delete(cm.pendingHTTPRequests, agentID)
	}

	return info.originalID, info.responseChan, info.method, info.toolName, info.arguments, true
}

// RegisterPendingHTTPRequest stores a pending tool call originating from the HTTP transport.
func (cm *ConnectionManager) RegisterPendingHTTPRequest(agentID string, originalID interface{}, responseChan chan map[string]interface{}, method string, toolName string, arguments interface{}) (int64, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	seq := cm.sequenceCounters[agentID] + 1
	cm.sequenceCounters[agentID] = seq
	cm.storePendingHTTPRequest(agentID, seq, originalID, responseChan, method, toolName, arguments)

	return seq, nil
}

// CancelPendingHTTPRequest removes a pending HTTP tool call by its generated ID.
func (cm *ConnectionManager) CancelPendingHTTPRequest(agentID string, generatedID int64) {
	key := fmt.Sprintf("%d", generatedID)

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if requests, exists := cm.pendingHTTPRequests[agentID]; exists {
		delete(requests, key)
		if len(requests) == 0 {
			delete(cm.pendingHTTPRequests, agentID)
		}
	}
}

// RegisterToolConnection 注册工具端连接，返回连接ID
func (cm *ConnectionManager) RegisterToolConnection(agentID string, conn *websocket.Conn) string {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	connectionID := uuid.New().String()
	if cm.toolConnections[agentID] == nil {
		cm.toolConnections[agentID] = make(map[string]*ToolConnection)
	}

	remoteAddr := ""
	if conn != nil && conn.RemoteAddr() != nil {
		remoteAddr = conn.RemoteAddr().String()
	}

	cm.toolConnections[agentID][connectionID] = &ToolConnection{
		WebSocket:    conn,
		AgentID:      agentID,
		ConnectionID: connectionID,
		Timestamp:    time.Now().Unix(),
		RemoteAddr:   remoteAddr,
	}

	if cm.writeMutexes[agentID] == nil {
		cm.writeMutexes[agentID] = make(map[string]*sync.Mutex)
	}
	cm.writeMutexes[agentID][connectionID] = &sync.Mutex{}

	if cm.toolsRequesting[agentID] == nil {
		cm.toolsRequesting[agentID] = make(map[string]bool)
	}

	if cm.pendingRequests[agentID] == nil {
		cm.pendingRequests[agentID] = make(map[string]*pendingRequest)
	}
	// 注意：不要重置pendingHTTPRequests，因为可能有正在进行的HTTP请求
	if cm.pendingHTTPRequests[agentID] == nil {
		cm.pendingHTTPRequests[agentID] = make(map[string]*pendingHTTPRequest)
	}

	cm.connectionTimestamps[agentID] = time.Now().Unix()

	logger.Infof("agent=%s connection_id=%s msg=register_tool_connection", agentID, connectionID)

	// 新连接建立后，按照协议主动发送 initialize 请求
	go func(aid, cid string) {
		if err := cm.SendInitializeRequest(aid, cid); err != nil {
			logger.Errorf("agent=%s connection_id=%s err=%v msg=send_initialize_after_register_failed", aid, cid, err)
		}
	}(agentID, connectionID)

	return connectionID
}

// RegisterRobotConnection 注册小智端连接，返回分配的UUID
func (cm *ConnectionManager) RegisterRobotConnection(agentID string, conn *websocket.Conn) string {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	robotConn := &RobotConnection{
		WebSocket:      conn,
		AgentID:        agentID,
		ConnectionUUID: uuid.New().String(),
		Timestamp:      time.Now().Unix(),
	}

	cm.robotConnections[robotConn.ConnectionUUID] = robotConn
	cm.connectionTimestamps[agentID] = time.Now().Unix()
	logger.Infof("agent=%s uuid=%s msg=register_robot_connection", agentID, robotConn.ConnectionUUID)

	return robotConn.ConnectionUUID
}

// UnregisterToolConnection 注销工具端连接
func (cm *ConnectionManager) UnregisterToolConnection(agentID string, connectionID string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	connections := cm.toolConnections[agentID]
	if connections == nil {
		return
	}

	if toolConn, exists := connections[connectionID]; exists {
		toolConn.WebSocket.Close()
		delete(connections, connectionID)

		// 清理写入锁和请求状态
		if locks := cm.writeMutexes[agentID]; locks != nil {
			delete(locks, connectionID)
			if len(locks) == 0 {
				delete(cm.writeMutexes, agentID)
			}
		}

		if requesting := cm.toolsRequesting[agentID]; requesting != nil {
			delete(requesting, connectionID)
			if len(requesting) == 0 {
				delete(cm.toolsRequesting, agentID)
			}
		}

		// 移除属于该工具连接的缓存工具
		if toolsMap, exists := cm.toolsCache[agentID]; exists {
			for name, info := range toolsMap {
				if info.ConnectionID == connectionID {
					delete(toolsMap, name)
				}
			}
			if len(toolsMap) == 0 {
				delete(cm.toolsCache, agentID)
			}
		}

		// 清理待处理请求
		if requests, exists := cm.pendingRequests[agentID]; exists {
			for id, info := range requests {
				if info.toolConnectionID == connectionID {
					delete(requests, id)
				}
			}
			if len(requests) == 0 {
				delete(cm.pendingRequests, agentID)
			}
		}

		logger.Infof("agent=%s connection_id=%s msg=unregister_tool_connection", agentID, connectionID)
	}

	if len(connections) == 0 {
		delete(cm.toolConnections, agentID)
		delete(cm.connectionTimestamps, agentID)
		delete(cm.toolsUpdatedAt, agentID)
		delete(cm.pendingHTTPRequests, agentID)
		delete(cm.sequenceCounters, agentID)
	}
}

// UnregisterRobotConnection 注销小智端连接
func (cm *ConnectionManager) UnregisterRobotConnection(connectionUUID string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if robotConn, exists := cm.robotConnections[connectionUUID]; exists {
		delete(cm.robotConnections, connectionUUID)
		for agentID, requests := range cm.pendingRequests {
			for generatedID, info := range requests {
				if info.connectionUUID == connectionUUID {
					delete(requests, generatedID)
				}
			}
			if len(requests) == 0 {
				delete(cm.pendingRequests, agentID)
			}
		}
		logger.Infof("agent=%s uuid=%s msg=unregister_robot_connection", robotConn.AgentID, connectionUUID)
	}
}

// TransformJSONRPCMessage 转换JSON-RPC消息的ID
func (cm *ConnectionManager) TransformJSONRPCMessage(message map[string]interface{}, connectionUUID string) map[string]interface{} {
	// 创建消息副本
	transformedMessage := make(map[string]interface{})
	for k, v := range message {
		transformedMessage[k] = v
	}

	// 转换ID
	if id, exists := transformedMessage["id"]; exists && id != nil {
		method, _ := message["method"].(string)
		var toolName string
		var arguments interface{}
		if method == "tools/call" {
			if params, ok := message["params"].(map[string]interface{}); ok {
				if name, ok := params["name"].(string); ok {
					toolName = name
				}
				arguments = params["arguments"]
			}
		}
		if robotConn, err := cm.getRobotConnection(connectionUUID); err == nil {
			if seq, storeErr := cm.storePendingRequest(robotConn.AgentID, connectionUUID, id, nil, method, toolName, arguments); storeErr == nil {
				transformedMessage["id"] = seq
			}
		}
	}

	return transformedMessage
}

// RestoreJSONRPCMessage 还原JSON-RPC消息的ID，返回(connection_uuid, restored_message)
func (cm *ConnectionManager) RestoreJSONRPCMessage(agentID, toolConnectionID string, message map[string]interface{}) (string, chan map[string]interface{}, map[string]interface{}, string, string, interface{}, error) {
	// 创建消息副本
	restoredMessage := make(map[string]interface{})
	for k, v := range message {
		restoredMessage[k] = v
	}

	// 还原ID
	if id, exists := restoredMessage["id"]; exists && id != nil {
		if originalID, responseChan, method, toolName, arguments, ok := cm.popPendingHTTPRequest(agentID, id); ok {
			restoredMessage["id"] = originalID
			return "", responseChan, restoredMessage, method, toolName, arguments, nil
		}
		connectionUUID, originalID, responseChan, method, toolName, arguments, err := cm.popPendingRequest(agentID, id, toolConnectionID)
		if err == nil {
			restoredMessage["id"] = originalID
			return connectionUUID, responseChan, restoredMessage, method, toolName, arguments, nil
		}
		return connectionUUID, nil, restoredMessage, method, toolName, arguments, err
	}

	return "", nil, restoredMessage, "", "", nil, fmt.Errorf("no msg id found msg:%v", restoredMessage)
}

// ForwardToTool 转发消息给工具端
func (cm *ConnectionManager) ForwardToTool(agentID, connectionID string, message interface{}) error {
	messageStr, err := cm.stringifyMessage(message)
	if err != nil {
		return err
	}

	id, method := extractJSONRPCMeta(messageStr)
	var toolName string
	var arguments interface{}

	if method == "tools/call" {
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(messageStr), &payload); err != nil {
			logger.Warnf("agent=%s id=%v err=%v msg=tool_call_log_parse_failed", agentID, id, err)
		} else if params, ok := payload["params"].(map[string]interface{}); ok {
			toolName, _ = params["name"].(string)
			arguments = params["arguments"]
			logger.Infof("agent=%s tool=%s args=%v msg=tool_call_request", agentID, toolName, arguments)
		}
	}

	targetConnectionID := connectionID
	if targetConnectionID == "" {
		if method == "tools/call" && toolName != "" {
			if resolvedID, resolveErr := cm.resolveToolConnectionByName(agentID, toolName); resolveErr == nil {
				targetConnectionID = resolvedID
			} else {
				logger.Warnf("agent=%s tool=%s err=%v msg=resolve_tool_connection_failed", agentID, toolName, resolveErr)
			}
		}

		if targetConnectionID == "" {
			resolvedID, defaultErr := cm.getDefaultToolConnectionID(agentID)
			if defaultErr != nil {
				logger.Errorf("agent=%s id=%v method=%s err=%v msg=no_available_tool_connection", agentID, id, method, defaultErr)
				return defaultErr
			}
			targetConnectionID = resolvedID
		}
	}

	logger.Infof("agent=%s connection_id=%s id=%v method=%s payload=%s msg=forward_tool_request", agentID, targetConnectionID, id, method, truncateForLog(messageStr))

	if id != nil {
		cm.assignPendingRequestToolConnection(agentID, id, targetConnectionID)
	}

	if err := cm.writeToTool(agentID, targetConnectionID, messageStr); err != nil {
		logger.Errorf("agent=%s connection_id=%s id=%v method=%s err=%v msg=forward_tool_request_failed", agentID, targetConnectionID, id, method, err)
		return err
	}

	logger.Infof("agent=%s connection_id=%s id=%v method=%s msg=forward_tool_request_success", agentID, targetConnectionID, id, method)
	return nil
}

// ForwardToRobotByUUID 根据UUID转发消息给特定的小智端连接
func (cm *ConnectionManager) ForwardToRobotByUUID(connectionUUID string, message interface{}) error {
	messageStr, err := cm.stringifyMessage(message)
	if err != nil {
		return err
	}

	robotConn, err := cm.getRobotConnection(connectionUUID)
	if err != nil {
		return err
	}

	id, method := extractJSONRPCMeta(messageStr)
	logger.Infof("agent=%s uuid=%s id=%v method=%s payload=%s msg=forward_robot_response", robotConn.AgentID, connectionUUID, id, method, truncateForLog(messageStr))

	if err := cm.writeToRobot(connectionUUID, robotConn, messageStr); err != nil {
		logger.Errorf("agent=%s uuid=%s id=%v method=%s err=%v msg=forward_robot_response_failed", robotConn.AgentID, connectionUUID, id, method, err)
		return err
	}

	logger.Infof("agent=%s uuid=%s id=%v method=%s msg=forward_robot_response_success", robotConn.AgentID, connectionUUID, id, method)
	return nil
}

// GetConnectionStats 获取连接统计信息
func (cm *ConnectionManager) GetConnectionStats() map[string]interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// 统计每个agent_id的连接数
	agentConnectionCounts := make(map[string]int)
	for _, robotConn := range cm.robotConnections {
		agentConnectionCounts[robotConn.AgentID]++
	}

	toolConnectionCounts := make(map[string]int)
	totalToolConnections := 0
	for agentID, connections := range cm.toolConnections {
		count := len(connections)
		if count > 0 {
			toolConnectionCounts[agentID] = count
			totalToolConnections += count
		}
	}

	return map[string]interface{}{
		"tool_connections":           totalToolConnections,
		"robot_connections":          len(cm.robotConnections),
		"total_connections":          totalToolConnections + len(cm.robotConnections),
		"tool_connections_by_agent":  toolConnectionCounts,
		"robot_connections_by_agent": agentConnectionCounts,
	}
}

// IsToolConnected 检查工具端是否已连接
func (cm *ConnectionManager) IsToolConnected(agentID string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	if connections, exists := cm.toolConnections[agentID]; exists {
		return len(connections) > 0
	}
	return false
}

// IsRobotConnected 检查小智端是否已连接
func (cm *ConnectionManager) IsRobotConnected(agentID string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for _, conn := range cm.robotConnections {
		if conn.AgentID == agentID {
			return true
		}
	}
	return false
}

// GetRobotConnectionsByAgent 获取指定agent_id的所有小智端连接
func (cm *ConnectionManager) GetRobotConnectionsByAgent(agentID string) []*RobotConnection {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var connections []*RobotConnection
	for _, conn := range cm.robotConnections {
		if conn.AgentID == agentID {
			connections = append(connections, conn)
		}
	}
	return connections
}

// GetCachedToolsForAgent 获取指定agent的缓存工具列表（仅用于HTTP同步响应）
func (cm *ConnectionManager) GetCachedToolsForAgent(agentID string) []string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// 只返回缓存的工具，不触发异步请求
	if toolsMap, exists := cm.toolsCache[agentID]; exists {
		tools := make([]string, 0, len(toolsMap))
		for toolName := range toolsMap {
			tools = append(tools, toolName)
		}
		return tools
	}

	// 没有缓存，返回空列表
	return []string{}
}

// GetCachedMCPToolsForAgent 获取指定agent的缓存MCP工具对象列表（用于MCP协议）
func (cm *ConnectionManager) GetCachedMCPToolsForAgent(agentID string) []map[string]interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// 只返回缓存的工具，不触发异步请求
	if toolsMap, exists := cm.toolsCache[agentID]; exists {
		tools := make([]map[string]interface{}, 0, len(toolsMap))
		for _, toolInfo := range toolsMap {
			tool := map[string]interface{}{
				"name":        toolInfo.Name,
				"description": toolInfo.Description,
				"inputSchema": toolInfo.InputSchema,
			}
			tools = append(tools, tool)
		}
		return tools
	}

	// 没有缓存，返回空列表
	return []map[string]interface{}{}
}

// GetToolsForAgent 获取指定agent的MCP工具列表（可能触发异步请求）
func (cm *ConnectionManager) GetToolsForAgent(agentID string) []string {
	cm.mutex.RLock()
	cachedTools, hasCachedTools := cm.toolsCache[agentID]
	lastUpdated, hasUpdatedTime := cm.toolsUpdatedAt[agentID]
	connections := cm.toolConnections[agentID]
	connectionIDs := make([]string, 0, len(connections))
	for connectionID := range connections {
		connectionIDs = append(connectionIDs, connectionID)
	}
	tools := make([]string, 0, len(cachedTools))
	if hasCachedTools {
		for toolName := range cachedTools {
			tools = append(tools, toolName)
		}
	}
	cm.mutex.RUnlock()

	hasConnection := len(connectionIDs) > 0
	if hasCachedTools {
		if hasUpdatedTime && (time.Now().Unix()-lastUpdated < 300) {
			logger.Infof("agent=%s tools=%v msg=return_tools_cache_fresh", agentID, tools)
			return tools
		}

		if hasConnection {
			cm.startToolsListRequest(agentID, connectionIDs...)
		}

		logger.Infof("agent=%s tools=%v msg=return_tools_cache", agentID, tools)
		return tools
	}

	if hasConnection {
		cm.startToolsListRequest(agentID, connectionIDs...)
	}

	logger.Infof("agent=%s msg=no_tool_cache", agentID)
	return []string{}
}

// BroadcastToolsListChanged notifies connected tool clients that capabilities changed.
func (cm *ConnectionManager) BroadcastToolsListChanged(agentID string) {
	cm.mutex.RLock()
	connections := cm.toolConnections[agentID]
	connectionIDs := make([]string, 0, len(connections))
	for connectionID := range connections {
		connectionIDs = append(connectionIDs, connectionID)
	}
	cm.mutex.RUnlock()

	if len(connectionIDs) == 0 {
		return
	}

	notification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/tools/list_changed",
	}

	payload, err := json.Marshal(notification)
	if err != nil {
		logger.Warnf("agent=%s err=%v msg=marshal_tools_list_changed_failed", agentID, err)
		return
	}

	for _, connectionID := range connectionIDs {
		if err := cm.writeToTool(agentID, connectionID, string(payload)); err != nil {
			logger.Warnf("agent=%s connection_id=%s err=%v msg=broadcast_tools_list_changed_failed", agentID, connectionID, err)
		}
	}

	cm.startToolsListRequest(agentID, connectionIDs...)
}

// startToolsListRequest 尝试异步刷新工具列表
func (cm *ConnectionManager) startToolsListRequest(agentID string, connectionIDs ...string) {
	if len(connectionIDs) == 0 {
		connectionIDs = cm.listToolConnectionIDs(agentID)
	}

	for _, connectionID := range connectionIDs {
		cid := connectionID
		go func() {
			if !cm.trySetToolsRequesting(agentID, cid) {
				return
			}
			defer cm.clearToolsRequesting(agentID, cid)

			if err := cm.SendToolsListRequest(agentID, cid); err != nil {
				logger.Errorf("agent=%s connection_id=%s err=%v msg=request_tools_list_failed", agentID, cid, err)
			}
		}()
	}
}

// trySetToolsRequesting 在锁保护下标记正在请求工具列表
func (cm *ConnectionManager) trySetToolsRequesting(agentID, connectionID string) bool {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if cm.toolsRequesting[agentID] == nil {
		cm.toolsRequesting[agentID] = make(map[string]bool)
	}

	if cm.toolsRequesting[agentID][connectionID] {
		return false
	}

	cm.toolsRequesting[agentID][connectionID] = true
	return true
}

// clearToolsRequesting 清理工具请求状态
func (cm *ConnectionManager) clearToolsRequesting(agentID, connectionID string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if agentRequesting, exists := cm.toolsRequesting[agentID]; exists {
		delete(agentRequesting, connectionID)
		if len(agentRequesting) == 0 {
			delete(cm.toolsRequesting, agentID)
		}
	}
}

// SendToolsListRequest 向指定agent的工具端请求工具列表
func (cm *ConnectionManager) SendToolsListRequest(agentID, connectionID string) error {
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}

	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	id, method := extractJSONRPCMeta(string(data))
	logger.Infof("agent=%s connection_id=%s id=%v method=%s msg=send_tools_list_request", agentID, connectionID, id, method)

	if err := cm.writeToTool(agentID, connectionID, string(data)); err != nil {
		logger.Errorf("agent=%s connection_id=%s id=%v method=%s err=%v msg=send_tools_list_request_failed", agentID, connectionID, id, method, err)
		return err
	}

	logger.Infof("agent=%s connection_id=%s id=%v method=%s msg=send_tools_list_request_success", agentID, connectionID, id, method)
	return nil
}

// SendInitializeRequest 向指定agent的工具端发送初始化请求
func (cm *ConnectionManager) SendInitializeRequest(agentID, connectionID string) error {
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{},
				"resources": map[string]interface{}{},
				"prompts":   map[string]interface{}{},
				"logging":   map[string]interface{}{},
			},
			"clientInfo": map[string]interface{}{
				"name":    "xiaozhi-mcp-server",
				"version": "1.0.0",
			},
		},
	}

	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	id, method := extractJSONRPCMeta(string(data))
	logger.Infof("agent=%s connection_id=%s id=%v method=%s msg=send_initialize_request", agentID, connectionID, id, method)

	if err := cm.writeToTool(agentID, connectionID, string(data)); err != nil {
		logger.Errorf("agent=%s connection_id=%s id=%v method=%s err=%v msg=send_initialize_request_failed", agentID, connectionID, id, method, err)
		return err
	}

	logger.Infof("agent=%s connection_id=%s id=%v method=%s msg=send_initialize_request_success", agentID, connectionID, id, method)
	return nil
}

// UpdateToolsCache 更新工具缓存
func (cm *ConnectionManager) UpdateToolsCache(agentID, connectionID string, tools []map[string]interface{}) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if cm.toolsCache[agentID] == nil {
		cm.toolsCache[agentID] = make(map[string]*ToolInfo)
	}

	toolsMap := cm.toolsCache[agentID]
	currentTime := time.Now().Unix()
	seenNames := make(map[string]struct{}, len(tools))

	for _, tool := range tools {
		name, ok := tool["name"].(string)
		if !ok {
			continue
		}

		description, _ := tool["description"].(string)
		inputSchema, _ := tool["inputSchema"].(map[string]interface{})

		toolsMap[name] = &ToolInfo{
			Name:         name,
			Description:  description,
			InputSchema:  inputSchema,
			UpdatedAt:    currentTime,
			ConnectionID: connectionID,
		}
		seenNames[name] = struct{}{}
	}

	for name, info := range toolsMap {
		if info.ConnectionID == connectionID {
			if _, exists := seenNames[name]; !exists {
				delete(toolsMap, name)
			}
		}
	}

	cm.toolsCache[agentID] = toolsMap
	cm.toolsUpdatedAt[agentID] = currentTime

	toolNames := make([]string, 0, len(toolsMap))
	for name := range toolsMap {
		toolNames = append(toolNames, name)
	}

	logger.Infof("agent=%s connection_id=%s tools=%v count=%d msg=update_tools_cache", agentID, connectionID, toolNames, len(toolsMap))
}

// GetToolDetails 获取工具详细信息
func (cm *ConnectionManager) GetToolDetails(agentID, toolName string) *ToolInfo {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if agentTools, exists := cm.toolsCache[agentID]; exists {
		if tool, found := agentTools[toolName]; found {
			return tool
		}
	}
	return nil
}

// GetToolConnectionSnapshots 返回指定 agent 的工具连接快照
func (cm *ConnectionManager) GetToolConnectionSnapshots(agentID string) []ToolConnectionSnapshot {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	connections := cm.toolConnections[agentID]
	if len(connections) == 0 {
		return nil
	}

	result := make([]ToolConnectionSnapshot, 0, len(connections))
	for connectionID, conn := range connections {
		if conn == nil {
			continue
		}
		result = append(result, ToolConnectionSnapshot{
			ConnectionID: connectionID,
			RemoteAddr:   conn.RemoteAddr,
			ConnectedAt:  conn.Timestamp,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ConnectionID < result[j].ConnectionID
	})
	return result
}

// GetToolInfoSnapshots 返回指定 agent 的工具信息快照
func (cm *ConnectionManager) GetToolInfoSnapshots(agentID string) []ToolInfoSnapshot {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	toolsMap := cm.toolsCache[agentID]
	if len(toolsMap) == 0 {
		return nil
	}

	result := make([]ToolInfoSnapshot, 0, len(toolsMap))
	for _, info := range toolsMap {
		if info == nil {
			continue
		}
		snapshot := ToolInfoSnapshot{
			Name:         info.Name,
			Description:  info.Description,
			ConnectionID: info.ConnectionID,
			UpdatedAt:    info.UpdatedAt,
		}
		if len(info.InputSchema) > 0 {
			copied := make(map[string]interface{}, len(info.InputSchema))
			for key, value := range info.InputSchema {
				copied[key] = value
			}
			snapshot.InputSchema = copied
		}

		if connections := cm.toolConnections[agentID]; len(connections) > 0 {
			if conn := connections[info.ConnectionID]; conn != nil {
				snapshot.ConnectedAt = conn.Timestamp
				snapshot.RemoteAddr = conn.RemoteAddr
			}
		}
		result = append(result, snapshot)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// GetAgentToolsCount 获取agent的工具数量
func (cm *ConnectionManager) GetAgentToolsCount(agentID string) int {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if agentTools, exists := cm.toolsCache[agentID]; exists {
		return len(agentTools)
	}
	return 0
}

// 全局连接管理器实例
var GlobalConnectionManager = NewConnectionManager()
