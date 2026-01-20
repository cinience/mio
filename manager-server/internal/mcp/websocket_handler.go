package mcp

import (
	"encoding/json"

	"manager-server/internal/logger"
	"manager-server/internal/utils"
)

// WebSocketHandler WebSocket处理器
type WebSocketHandler struct {
	connectionManager *ConnectionManager
}

// NewWebSocketHandler 创建新的WebSocket处理器
func NewWebSocketHandler(connectionManager *ConnectionManager) *WebSocketHandler {
	return &WebSocketHandler{
		connectionManager: connectionManager,
	}
}

// parseJSONMessage attempts to decode a JSON-RPC payload into a generic map.
func (wh *WebSocketHandler) parseJSONMessage(raw string) (map[string]interface{}, error) {
	var messageData map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &messageData); err != nil {
		return nil, err
	}
	return messageData, nil
}

// handleToolSideControlFlow consumes internal control messages from the tool connection.
func (wh *WebSocketHandler) handleToolSideControlFlow(agentID, connectionID string, messageData map[string]interface{}) bool {
	if wh.isToolsListResponse(messageData) {
		logger.Infof("agent=%s id=%v msg=tool_list_response_detected", agentID, messageData["id"])
		wh.handleToolsListResponse(agentID, connectionID, messageData)
		return true
	}

	if wh.isToolsListChangedNotification(messageData) {
		logger.Infof("agent=%s connection_id=%s msg=tool_list_changed_notification", agentID, connectionID)
		wh.handleToolsListChangedNotification(agentID, connectionID)
		return true
	}

	if wh.isInitializeResponse(messageData) {
		logger.Infof("agent=%s connection_id=%s msg=initialize_response_detected", agentID, connectionID)
		wh.handleInitializeResponse(agentID, connectionID)
		return true
	}

	return false
}

// HandleToolMessage 处理工具端消息
func (wh *WebSocketHandler) HandleToolMessage(agentID, connectionID string, message string) error {
	logger.Infof("agent=%s connection_id=%s payload=%s msg=tool_message_received", agentID, connectionID, message)

	messageData, err := wh.parseJSONMessage(message)
	if err != nil {
		logger.Warnf("payload=%s msg=tool_message_invalid_json", message)
		return nil
	}

	logger.Infof("agent=%s connection_id=%s id=%v method=%s msg=tool_message_parsed", agentID, connectionID, messageData["id"], messageData["method"])

	if wh.handleToolSideControlFlow(agentID, connectionID, messageData) {
		return nil
	}

	// 还原JSON-RPC ID并获取目标连接UUID
	connectionUUID, responseChan, restoredMessage, method, toolName, arguments, restoreErr := wh.connectionManager.RestoreJSONRPCMessage(agentID, connectionID, messageData)
	if restoreErr != nil {
		logger.Warnf("err=%v msg=no_target_for_tool_message", restoreErr)
		// 对于找不到对应请求的情况，记录警告但不返回错误，避免影响其他消息处理
		return nil
	}

	if method == "tools/call" {
		if result, ok := restoredMessage["result"]; ok {
			logger.Infof("agent=%s tool=%s args=%v result=%v msg=tool_call_response_success", agentID, toolName, arguments, result)
		} else if errPayload, ok := restoredMessage["error"]; ok {
			logger.Warnf("agent=%s tool=%s args=%v error=%v msg=tool_call_response_error", agentID, toolName, arguments, errPayload)
		} else {
			logger.Infof("agent=%s tool=%s args=%v payload=%v msg=tool_call_response_payload", agentID, toolName, arguments, restoredMessage)
		}
	}

	logger.Infof("agent=%s uuid=%s id=%v msg=forward_tool_message_to_robot", agentID, connectionUUID, restoredMessage["id"])

	// 如果存在HTTP等待通道，优先返回给该通道
	if responseChan != nil {
		defer func() {
			if r := recover(); r != nil {
				logger.Warnf("agent=%s id=%v err=%v msg=http_response_channel_unavailable", agentID, restoredMessage["id"], r)
			}
		}()

		responseChan <- restoredMessage
		logger.Infof("agent=%s connection_id=%s id=%v msg=return_tool_message_to_http", agentID, connectionID, restoredMessage["id"])
		return nil
	}

	// 有特定的目标连接，发送给该连接
	if err := wh.connectionManager.ForwardToRobotByUUID(connectionUUID, restoredMessage); err != nil {
		logger.Errorf("uuid=%s err=%v msg=forward_tool_message_to_robot_failed", connectionUUID, err)
		return err
	}

	return nil
}

// HandleRobotMessage 处理小智端消息
func (wh *WebSocketHandler) HandleRobotMessage(agentID string, message string, connectionUUID string) error {
	logger.Infof("agent=%s uuid=%s payload=%s msg=robot_message_received", agentID, connectionUUID, message)

	requestID, outboundMessage := wh.prepareRobotMessage(message, connectionUUID)
	if transformedMap, ok := outboundMessage.(map[string]interface{}); ok {
		logger.Infof("agent=%s uuid=%s id=%v method=%s msg=robot_message_transformed", agentID, connectionUUID, transformedMap["id"], transformedMap["method"])
	}

	// 检查是否有对应的工具端连接
	if !wh.connectionManager.IsToolConnected(agentID) {
		logger.Warnf("agent=%s msg=tool_not_connected", agentID)
		// 发送JSON-RPC格式的错误消息给小智端
		errorResponse := utils.CreateToolNotConnectedError(requestID, agentID)
		return wh.connectionManager.ForwardToRobotByUUID(connectionUUID, errorResponse)
	}

	// 转发转换后的消息给工具端
	if err := wh.connectionManager.ForwardToTool(agentID, "", outboundMessage); err != nil {
		logger.Errorf("agent=%s err=%v msg=forward_robot_message_to_tool_failed", agentID, err)
		// 发送JSON-RPC格式的错误消息给小智端
		errorResponse := utils.CreateForwardFailedError(requestID, agentID)
		return wh.connectionManager.ForwardToRobotByUUID(connectionUUID, errorResponse)
	}

	return nil
}

func (wh *WebSocketHandler) prepareRobotMessage(message, connectionUUID string) (interface{}, interface{}) {
	messageData, err := wh.parseJSONMessage(message)
	if err != nil {
		logger.Warnf("payload=%s msg=robot_message_invalid_json", message)
		return nil, message
	}

	requestID := messageData["id"]
	transformed := wh.connectionManager.TransformJSONRPCMessage(messageData, connectionUUID)
	logger.Infof("original_id=%v transformed_id=%v method=%s msg=robot_message_id_transformed", messageData["id"], transformed["id"], messageData["method"])

	return requestID, transformed
}

func (wh *WebSocketHandler) isInitializeResponse(messageData map[string]interface{}) bool {
	result, ok := messageData["result"].(map[string]interface{})
	if !ok {
		return false
	}

	_, hasPV := result["protocolVersion"]
	return hasPV
}

func (wh *WebSocketHandler) handleInitializeResponse(agentID, connectionID string) {
	logger.Infof("agent=%s connection_id=%s msg=process_initialize_response", agentID, connectionID)
	go func() {
		notification := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "notifications/initialized",
		}
		if err := wh.connectionManager.ForwardToTool(agentID, connectionID, notification); err != nil {
			logger.Errorf("agent=%s connection_id=%s err=%v msg=send_initialized_notification_failed", agentID, connectionID, err)
		}

		if err := wh.connectionManager.SendToolsListRequest(agentID, connectionID); err != nil {
			logger.Errorf("agent=%s connection_id=%s err=%v msg=request_tools_list_after_initialize_failed", agentID, connectionID, err)
		}
	}()
}

// isToolsListResponse 检查是否为工具列表响应
func (wh *WebSocketHandler) isToolsListResponse(messageData map[string]interface{}) bool {
	// 检查是否为JSON-RPC响应
	if _, hasResult := messageData["result"]; !hasResult {
		return false
	}

	// 检查ID字段，如果ID为1且没有error，则认为是工具列表响应
	// （这是一个简化的判断，实际实现中可能需要更复杂的逻辑）
	if id, hasID := messageData["id"]; hasID {
		if idNum, ok := id.(float64); ok && idNum == 1 {
			if _, hasError := messageData["error"]; !hasError {
				return true
			}
		}
	}

	return false
}

// isToolsListChangedNotification 检查是否为工具列表变更通知
func (wh *WebSocketHandler) isToolsListChangedNotification(messageData map[string]interface{}) bool {
	// 检查是否为通知
	method, hasMethod := messageData["method"].(string)
	if !hasMethod {
		return false
	}

	// 检查是否为工具列表变更通知
	return method == "notifications/tools/list_changed"
}

// handleToolsListChangedNotification 处理工具列表变更通知
func (wh *WebSocketHandler) handleToolsListChangedNotification(agentID, connectionID string) {
	logger.Infof("agent=%s connection_id=%s msg=tool_list_changed_handling", agentID, connectionID)

	// 异步请求工具列表
	go func() {
		if err := wh.connectionManager.SendToolsListRequest(agentID, connectionID); err != nil {
			logger.Errorf("agent=%s connection_id=%s err=%v msg=request_tools_list_after_change_failed", agentID, connectionID, err)
		}
	}()
}

// handleToolsListResponse 处理工具列表响应
func (wh *WebSocketHandler) handleToolsListResponse(agentID, connectionID string, messageData map[string]interface{}) {
	result, ok := messageData["result"].(map[string]interface{})
	if !ok {
		logger.Warnf("agent=%s msg=tool_list_response_invalid", agentID)
		return
	}

	tools, ok := result["tools"].([]interface{})
	if !ok {
		logger.Warnf("agent=%s msg=tool_list_field_invalid", agentID)
		return
	}

	// 转换为所需格式
	toolsData := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		if toolMap, ok := tool.(map[string]interface{}); ok {
			toolsData = append(toolsData, toolMap)
		}
	}

	// 更新工具缓存
	wh.connectionManager.UpdateToolsCache(agentID, connectionID, toolsData)
	logger.Infof("agent=%s connection_id=%s count=%d msg=tool_list_processed", agentID, connectionID, len(toolsData))
}

// 全局WebSocket处理器实例
var GlobalWebSocketHandler = NewWebSocketHandler(GlobalConnectionManager)
