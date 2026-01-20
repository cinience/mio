package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"gorm.io/gorm"

	"manager-server/internal/constants"
	"manager-server/internal/logger"
	"manager-server/internal/models"
	"manager-server/internal/repository"
)

// DeviceService 设备服务接口
type DeviceService interface {
	RegisterDevice(ctx context.Context, req *models.DeviceRegisterDTO) (string, error)
	ActivateDevice(ctx context.Context, userID uint64, agentID, deviceCode string) error
	GetUserDevices(ctx context.Context, userID uint64, agentID string) ([]*models.DeviceEntity, error)
	GetAllDevices(ctx context.Context) ([]*models.DeviceEntity, error) // 获取所有设备（系统调用）
	UnbindDevice(ctx context.Context, userID uint64, deviceID string) error
	UpdateDevice(ctx context.Context, userID uint64, deviceID string, req *models.DeviceUpdateDTO) error
	ManualAddDevice(ctx context.Context, userID uint64, req *models.DeviceManualAddDTO) error
	GetDeviceByID(ctx context.Context, id string) (*models.DeviceEntity, error)
	GetDeviceWithUserInfo(ctx context.Context, deviceID string) (*models.DeviceWithUserInfoDTO, error) // 获取设备详细信息（包含使用时长统计）
	PageDevices(ctx context.Context, page, limit int, keywords string) (*models.PageResponse, error)
	PageDevicesWithUserInfo(ctx context.Context, page, limit int, keywords string) (*models.PageResponse, error)
	GetDeviceCountByAgentID(ctx context.Context, agentID string) (int, error)            // 根据智能体ID获取设备数量
	GetLatestLastConnectionTime(ctx context.Context, agentID string) (*time.Time, error) // 获取智能体最后连接时间
	// OTA相关方法
	CheckDeviceActive(ctx context.Context, macAddress, clientID string, req *models.DeviceReportReqDTO) (*models.DeviceReportRespDTO, error)
	GetDeviceByMacAddress(ctx context.Context, macAddress string) (*models.DeviceEntity, error)
	GetDeviceCountByUserID(ctx context.Context, userID uint64) (int, error)
	ReportDeviceStatus(ctx context.Context, req *models.DeviceStatusReportDTO) error // 上报设备状态和使用时长
	// 仅当设备未绑定用户与智能体时删除
	DeleteIfUnbound(ctx context.Context, deviceID string) error
	// 用户设备分页
	PageUserDevices(ctx context.Context, userID uint64, agentID string, page, limit int, keywords string) (*models.PageResponse, error)
}

// deviceService 设备服务实现
type deviceService struct {
	deviceRepo    repository.DeviceRepository
	paramsService SysParamsService
}

func isBlankAgentIdentifier(value string) bool {
	normalized := strings.TrimSpace(strings.ToLower(value))
	if normalized == "" {
		return true
	}
	switch normalized {
	case "0", "null", "undefined":
		return true
	default:
		return false
	}
}

// NewDeviceService 创建设备服务实例
func NewDeviceService(deviceRepo repository.DeviceRepository, paramsService SysParamsService) DeviceService {
	return &deviceService{
		deviceRepo:    deviceRepo,
		paramsService: paramsService,
	}
}

// RegisterDevice 注册设备 - 生成激活码并存储到Redis
func (s *deviceService) RegisterDevice(ctx context.Context, req *models.DeviceRegisterDTO) (string, error) {
	if req.MacAddress == "" {
		return "", fmt.Errorf("MAC地址不能为空")
	}

	// 检查设备是否已存在
	existingDevice, err := s.deviceRepo.FindByMacAddress(ctx, req.MacAddress)
	if err != nil {
		return "", err
	}

	// 如果设备已存在且已绑定智能体，返回错误
	if existingDevice != nil && existingDevice.AgentID != "" {
		return "", fmt.Errorf("设备已激活")
	}

	// 如果设备不存在，先创建设备记录（使用MAC地址作为设备ID，参考Java版本）
	if existingDevice == nil {
		device := &models.DeviceEntity{
			ID:         req.MacAddress, // 使用MAC地址作为设备ID
			MacAddress: req.MacAddress,
			CreateDate: time.Now(),
			UpdateDate: time.Now(),
		}
		if err := s.deviceRepo.Create(ctx, device); err != nil {
			return "", fmt.Errorf("创建设备记录失败: %v", err)
		}
	}

	// 生成6位数验证码
	rand.Seed(time.Now().UnixNano())
	code := fmt.Sprintf("%06d", rand.Intn(900000)+100000)

	// 存储验证码到repository层处理，因为Redis实例在那里
	return code, s.deviceRepo.StoreActivationCode(ctx, code, req.MacAddress)
}

// ActivateDevice 激活设备 - 绑定设备到智能体
func (s *deviceService) ActivateDevice(ctx context.Context, userID uint64, agentID, deviceCode string) error {
	// 先从全局激活码存储中查找（OTA接口生成的）
	macAddress := FindDeviceByActivationCode(deviceCode)
	if macAddress != "" {
		// 找到OTA接口生成的激活码，需要创建设备并绑定
		return s.activateDeviceFromOTA(ctx, userID, agentID, deviceCode, macAddress)
	}

	// 如果全局存储中没有，使用传统的激活流程（repository存储）
	return s.deviceRepo.ActivateDevice(ctx, userID, agentID, deviceCode)
}

// GetUserDevices 获取用户设备列表
func (s *deviceService) GetUserDevices(ctx context.Context, userID uint64, agentID string) ([]*models.DeviceEntity, error) {
	return s.deviceRepo.FindUserDevices(ctx, userID, agentID)
}

// GetAllDevices 获取所有设备（系统调用）
func (s *deviceService) GetAllDevices(ctx context.Context) ([]*models.DeviceEntity, error) {
	return s.deviceRepo.FindAllDevices(ctx)
}

// UnbindDevice 解绑设备
func (s *deviceService) UnbindDevice(ctx context.Context, userID uint64, deviceID string) error {
	// 验证设备是否属于该用户
	device, err := s.deviceRepo.FindByID(ctx, deviceID)
	if err != nil {
		return err
	}
	if device == nil {
		return fmt.Errorf("设备不存在")
	}
	if device.UserID == nil || (userID != 0 && *device.UserID != userID) {
		return fmt.Errorf("设备不存在")
	}

	return s.deviceRepo.UnbindDevice(ctx, userID, deviceID)
}

// UpdateDevice 更新设备信息
func (s *deviceService) UpdateDevice(ctx context.Context, userID uint64, deviceID string, req *models.DeviceUpdateDTO) error {
	// 验证设备是否属于该用户
	device, err := s.deviceRepo.FindByID(ctx, deviceID)
	if err != nil {
		return err
	}
	if device == nil {
		return fmt.Errorf("设备不存在")
	}
	if device.UserID == nil || *device.UserID != userID {
		return fmt.Errorf("设备不存在")
	}

	// 更新设备信息
	if req.Alias != "" {
		device.Alias = req.Alias
	}
	if req.AutoUpdate != nil {
		device.AutoUpdate = req.AutoUpdate
	}
	if req.Sort != nil {
		device.Sort = req.Sort
	}

	return s.deviceRepo.Update(ctx, device)
}

// ManualAddDevice 手动添加设备
func (s *deviceService) ManualAddDevice(ctx context.Context, userID uint64, req *models.DeviceManualAddDTO) error {
	// 检查设备是否已存在
	existingDevice, err := s.deviceRepo.FindByMacAddress(ctx, req.MacAddress)
	if err != nil {
		return err
	}
	if existingDevice != nil {
		return fmt.Errorf("该MAC地址的设备已存在")
	}

	// 创建新设备（直接使用MAC地址作为ID）
	autoUpdateVal := 1
	device := &models.DeviceEntity{
		ID:              req.MacAddress,
		UserID:          &userID,
		MacAddress:      req.MacAddress,
		AgentID:         req.AgentID,
		Alias:           req.Alias,
		Board:           req.Board,
		AppVersion:      req.AppVersion,
		AutoUpdate:      &autoUpdateVal,
		Creator:         &userID,
		Updater:         &userID,
		CreateDate:      time.Now(),
		UpdateDate:      time.Now(),
		LastConnectedAt: func() *time.Time { t := time.Now(); return &t }(),
	}

	return s.deviceRepo.Create(ctx, device)
}

// GetDeviceByID 根据ID获取设备信息
func (s *deviceService) GetDeviceByID(ctx context.Context, id string) (*models.DeviceEntity, error) {
	return s.deviceRepo.FindByID(ctx, id)
}

// GetDeviceWithUserInfo 获取设备详细信息（包含使用时长统计）
func (s *deviceService) GetDeviceWithUserInfo(ctx context.Context, deviceID string) (*models.DeviceWithUserInfoDTO, error) {
	return s.deviceRepo.GetDeviceWithUserInfo(ctx, deviceID)
}

// PageDevices 分页查询设备（管理员功能）
func (s *deviceService) PageDevices(ctx context.Context, page, limit int, keywords string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit
	devices, total, err := s.deviceRepo.PageDevices(ctx, offset, limit, keywords)
	if err != nil {
		return nil, err
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       devices,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// PageDevicesWithUserInfo 分页查询设备（包含用户信息，管理员功能）
func (s *deviceService) PageDevicesWithUserInfo(ctx context.Context, page, limit int, keywords string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit
	devices, total, err := s.deviceRepo.PageDevicesWithUserInfo(ctx, offset, limit, keywords)
	if err != nil {
		return nil, err
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       devices,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// GetDeviceCountByAgentID 根据智能体ID获取设备数量
func (s *deviceService) GetDeviceCountByAgentID(ctx context.Context, agentID string) (int, error) {
	if agentID == "" {
		return 0, nil
	}

	count, err := s.deviceRepo.CountByAgentID(ctx, agentID)
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

// GetLatestLastConnectionTime 获取智能体最后连接时间
func (s *deviceService) GetLatestLastConnectionTime(ctx context.Context, agentID string) (*time.Time, error) {
	if agentID == "" {
		return nil, nil
	}

	// 获取该智能体下所有设备的最后连接时间，返回最新的一个
	return s.deviceRepo.GetLatestLastConnectionTime(ctx, agentID)
}

// GetDeviceCountByUserID 根据用户ID获取设备数量
func (s *deviceService) GetDeviceCountByUserID(ctx context.Context, userID uint64) (int, error) {
	count, err := s.deviceRepo.CountByUserID(ctx, userID)
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

// CheckDeviceActive 检查设备激活状态
func (s *deviceService) CheckDeviceActive(ctx context.Context, macAddress, clientID string, req *models.DeviceReportReqDTO) (*models.DeviceReportRespDTO, error) {
	response := &models.DeviceReportRespDTO{
		ServerTime: models.CreateServerTime(),
	}

	// 先检查设备是否存在
	device, err := s.GetDeviceByMacAddress(ctx, macAddress)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.CreateErrorResponse("数据库查询失败"), nil
	}
	// 设备未绑定，返回当前上传的固件信息（不更新）以兼容旧固件版本
	if device == nil {
		// 检查必要字段：application字段必须存在
		if req.Application == nil {
			//return models.CreateErrorResponse("application字段不能为空"), nil
			req.Application = &models.Application{
				Version: "0.0",
			}
		}

		firmware := &models.Firmware{
			Version: req.Application.Version,
			URL:     "INVALID_FIRMWARE_URL", // 无效固件URL，表示不需要更新
		}
		response.Firmware = firmware
	} else {
		// 只有在设备已绑定且autoUpdate不为0的情况下才返回固件升级信息
		if device.AutoUpdate != nil && *device.AutoUpdate != 0 {
			// 检查必要字段：application字段必须存在
			if req.Application == nil {
				//return models.CreateErrorResponse("application字段不能为空"), nil
				req.Application = &models.Application{
					Version: "0.0",
				}
			}
			// 这里可以添加固件版本检查逻辑
			// 暂时返回当前版本，表示不需要升级
			firmware := &models.Firmware{
				Version: req.Application.Version,
				URL:     "INVALID_FIRMWARE_URL",
			}
			response.Firmware = firmware
		}
	}

	// 添加WebSocket配置
	websocket := &models.Websocket{}
	// 从系统参数获取WebSocket URL，如果未配置则使用默认值
	wsUrl, err := s.paramsService.GetValue(ctx, constants.SERVER_WEBSOCKET, "")
	if err != nil || strings.TrimSpace(wsUrl) == "" || wsUrl == "null" {
		logger.Warnf("WebSocket地址未配置或获取失败，请登录智控台，在参数管理找到【server.websocket】配置，error: %v", err)
		wsUrl = "ws://xiaozhi.server.com:8000/xiaozhi/v1/"
		websocket.URL = wsUrl
	} else {
		// 处理多个WebSocket URL（用分号分隔）
		wsUrls := strings.Split(wsUrl, ";")
		if len(wsUrls) > 0 {
			// 过滤掉空字符串
			validUrls := make([]string, 0, len(wsUrls))
			for _, url := range wsUrls {
				if trimmed := strings.TrimSpace(url); trimmed != "" {
					validUrls = append(validUrls, trimmed)
				}
			}

			if len(validUrls) > 0 {
				// 随机选择一个WebSocket URL
				selectedUrl := validUrls[rand.Intn(len(validUrls))]
				websocket.URL = selectedUrl
			} else {
				logger.Warnf("WebSocket地址配置为空，请登录智控台，在参数管理找到【server.websocket】配置")
				websocket.URL = "ws://xiaozhi.server.com:8000/xiaozhi/v1/"
			}
		} else {
			logger.Warnf("WebSocket地址未配置，请登录智控台，在参数管理找到【server.websocket】配置")
			websocket.URL = "ws://xiaozhi.server.com:8000/xiaozhi/v1/"
		}
	}
	response.Websocket = websocket

	// 如果设备存在，异步更新上次连接时间和版本信息
	if device != nil {
		// 这里可以添加异步更新逻辑
		// TODO: 实现异步更新设备连接信息
	} else {
		// 如果设备不存在，则生成激活码
		activation := s.buildActivation(ctx, macAddress, req)
		response.Activation = activation
	}

	return response, nil
}

// GetDeviceByMacAddress 根据MAC地址获取设备信息
func (s *deviceService) GetDeviceByMacAddress(ctx context.Context, macAddress string) (*models.DeviceEntity, error) {
	return s.deviceRepo.FindByMacAddress(ctx, macAddress)
}

// ReportDeviceStatus 上报设备状态和使用时长
func (s *deviceService) ReportDeviceStatus(ctx context.Context, req *models.DeviceStatusReportDTO) error {
	// 验证请求参数
	if req.DeviceID == "" {
		return fmt.Errorf("设备ID不能为空")
	}

	if req.SessionDuration < 0 {
		return fmt.Errorf("会话时长不能为负数")
	}

	// 调用repository层进行状态上报
	return s.deviceRepo.ReportDeviceStatus(ctx, req)
}

// DeleteIfUnbound 当设备未绑定用户与智能体时，允许从数据库删除
func (s *deviceService) DeleteIfUnbound(ctx context.Context, deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("设备ID不能为空")
	}

	device, err := s.deviceRepo.FindByID(ctx, deviceID)
	if err != nil {
		return err
	}
	if device == nil {
		return fmt.Errorf("设备不存在")
	}

	// 仅当未绑定智能体（AgentID为空）才允许删除
	if !isBlankAgentIdentifier(device.AgentID) {
		return fmt.Errorf("仅允许删除未绑定的设备")
	}

	return s.deviceRepo.DeleteByID(ctx, deviceID)
}

// PageUserDevices 用户设备分页
func (s *deviceService) PageUserDevices(ctx context.Context, userID uint64, agentID string, page, limit int, keywords string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	list, total, err := s.deviceRepo.FindUserDevicesPage(ctx, userID, agentID, offset, limit, keywords)
	if err != nil {
		return nil, err
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))
	return &models.PageResponse{
		List:       list,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}
