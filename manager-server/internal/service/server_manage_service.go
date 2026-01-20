package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"manager-server/internal/constants"
	"manager-server/internal/models"
	"manager-server/internal/repository"
)

// ServerManageService 服务端管理业务逻辑接口
type ServerManageService interface {
	GetWsServerList(ctx context.Context) ([]string, error)
	EmitServerAction(ctx context.Context, dto *models.EmitServerActionDTO) (bool, error)
}

type serverManageService struct {
	paramsRepo repository.SysParamsRepository
}

// NewServerManageService 创建服务端管理业务逻辑实例
func NewServerManageService(paramsRepo repository.SysParamsRepository) ServerManageService {
	return &serverManageService{
		paramsRepo: paramsRepo,
	}
}

// GetWsServerList 获取WebSocket服务端列表
func (s *serverManageService) GetWsServerList(ctx context.Context) ([]string, error) {
	// 从系统参数中获取WebSocket服务端配置
	wsParam, err := s.paramsRepo.GetByCode(ctx, constants.SERVER_WEBSOCKET)
	if err != nil {
		return []string{}, nil
	}

	if wsParam == nil || wsParam.ParamValue == "" || wsParam.ParamValue == "null" {
		return []string{}, nil
	}

	// 按分号分割WebSocket地址
	servers := strings.Split(wsParam.ParamValue, ";")
	var result []string
	for _, server := range servers {
		if trimmed := strings.TrimSpace(server); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result, nil
}

// EmitServerAction 通知Python服务端更新配置
func (s *serverManageService) EmitServerAction(ctx context.Context, dto *models.EmitServerActionDTO) (bool, error) {
	if dto.Action == "" {
		return false, errors.New("无效服务端操作")
	}

	// 获取WebSocket服务端列表
	servers, err := s.GetWsServerList(ctx)
	if err != nil {
		return false, err
	}

	if len(servers) == 0 {
		return false, errors.New("未配置服务端WebSocket地址")
	}

	// 检查目标WebSocket地址是否存在
	targetExists := false
	for _, server := range servers {
		if server == dto.TargetWs {
			targetExists = true
			break
		}
	}

	if !targetExists {
		return false, errors.New("目标WebSocket地址不存在")
	}

	// 获取服务端密钥
	secretParam, err := s.paramsRepo.GetByCode(ctx, "SERVER_SECRET")
	if err != nil {
		return false, errors.New("获取服务端密钥失败")
	}

	serverSecret := ""
	if secretParam != nil {
		serverSecret = secretParam.ParamValue
	}

	// 调用WebSocket客户端发送消息
	return s.emitServerActionByWs(ctx, dto.TargetWs, dto.Action, serverSecret)
}

// emitServerActionByWs 通过WebSocket发送服务端动作
func (s *serverManageService) emitServerActionByWs(ctx context.Context, targetWsUri string, action models.ServerActionEnum, serverSecret string) (bool, error) {
	if targetWsUri == "" || action == "" {
		return false, errors.New("参数不能为空")
	}

	// TODO: 实现WebSocket客户端连接和通信
	// 这里简化实现，仅返回模拟结果
	// 在实际项目中，需要实现WebSocket客户端功能

	fmt.Printf("[模拟] 准备连接WebSocket服务器: %s\n", targetWsUri)
	fmt.Printf("[模拟] 发送动作: %s\n", action)

	// 模拟构建请求载荷
	payload := models.BuildServerActionPayload(action, map[string]interface{}{
		"secret": serverSecret,
	})

	payloadJSON, _ := json.Marshal(payload)
	fmt.Printf("[模拟] 发送载荷: %s\n", string(payloadJSON))

	// 模拟WebSocket客户端操作
	// 实际实现需要：
	// 1. 创建WebSocket连接
	// 2. 设置连接头部 (device-id, client-id)
	// 3. 发送JSON消息
	// 4. 等待服务端响应
	// 5. 解析响应并判断是否成功

	// 模拟成功响应
	time.Sleep(100 * time.Millisecond) // 模拟网络延迟
	fmt.Printf("[模拟] 收到服务端响应: 操作成功\n")

	// 模拟构建响应
	response := &models.ServerActionResponseDTO{
		Status:  models.ServerActionResponseSuccess,
		Message: "操作成功",
		Type:    "server",
		Content: map[string]interface{}{
			"action": action,
			"result": "success",
		},
	}

	return response.IsSuccess(), nil
}

// 以下是WebSocket客户端的伪代码实现思路：
/*
func (s *serverManageService) emitServerActionByWsReal(ctx context.Context, targetWsUri string, action models.ServerActionEnum, serverSecret string) (bool, error) {
	// 1. 创建WebSocket拨号器
	dialer := websocket.Dialer{
		HandshakeTimeout: 3 * time.Second,
	}

	// 2. 设置请求头
	headers := http.Header{}
	headers.Set("device-id", uuid.New().String())
	headers.Set("client-id", uuid.New().String())

	// 3. 连接WebSocket服务器
	conn, _, err := dialer.Dial(targetWsUri, headers)
	if err != nil {
		return false, fmt.Errorf("WebSocket连接失败: %v", err)
	}
	defer conn.Close()

	// 4. 设置超时
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	conn.SetReadDeadline(time.Now().Add(120 * time.Second))

	// 5. 构建并发送消息
	payload := models.BuildServerActionPayload(action, map[string]interface{}{
		"secret": serverSecret,
	})

	if err := conn.WriteJSON(payload); err != nil {
		return false, fmt.Errorf("发送消息失败: %v", err)
	}

	// 6. 等待响应
	var response models.ServerActionResponseDTO
	if err := conn.ReadJSON(&response); err != nil {
		return false, fmt.Errorf("读取响应失败: %v", err)
	}

	// 7. 判断响应是否成功
	return response.IsSuccess(), nil
}
*/
