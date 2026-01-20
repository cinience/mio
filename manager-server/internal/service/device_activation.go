package service

import (
	"context"
	"fmt"
	"time"

	"manager-server/internal/models"
)

// activateDeviceFromOTA 从OTA激活码创建并绑定设备
func (s *deviceService) activateDeviceFromOTA(ctx context.Context, userID uint64, agentID, deviceCode, macAddress string) error {
	// 检查设备是否已存在
	existingDevice, err := s.deviceRepo.FindByMacAddress(ctx, macAddress)
	if err != nil {
		return fmt.Errorf("查询设备失败: %v", err)
	}

	var device *models.DeviceEntity

	if existingDevice == nil {
		// 设备不存在，从激活数据创建设备
		activationData := GetDeviceActivationInfo(macAddress)
		if activationData == nil {
			return fmt.Errorf("找不到设备激活数据")
		}

		// 创建新设备 - 使用MAC地址作为设备ID（参考Java版本实现）
		userIDPtr := uint64(userID)
		autoUpdateVal := 1

		device = &models.DeviceEntity{
			ID:         macAddress,
			MacAddress: macAddress,
			UserID:     &userIDPtr, // 设置用户ID
			AgentID:    agentID,
			AutoUpdate: &autoUpdateVal, // 默认开启自动更新
			Creator:    &userIDPtr,     // 设置创建者
			Updater:    &userIDPtr,     // 设置更新者
			CreateDate: time.Now(),
			UpdateDate: time.Now(),
		}

		// 设置板子信息
		if board, ok := activationData["board"].(string); ok && board != "" {
			device.Board = board
		}

		// 设置应用版本
		if appVersion, ok := activationData["app_version"].(string); ok && appVersion != "" {
			device.AppVersion = appVersion
		}

		// 保存设备到数据库
		if err := s.deviceRepo.Create(ctx, device); err != nil {
			return fmt.Errorf("创建设备记录失败: %v", err)
		}
	} else {
		// 设备已存在，更新其agentID和用户信息
		device = existingDevice
		userIDPtr := uint64(userID)
		device.UserID = &userIDPtr // 更新用户ID
		device.AgentID = agentID
		device.Updater = &userIDPtr // 设置更新者
		device.UpdateDate = time.Now()
		if err := s.deviceRepo.Update(ctx, device); err != nil {
			return fmt.Errorf("更新设备记录失败: %v", err)
		}
	}

	// 清理激活码
	ClearActivationCode(deviceCode)

	return nil
}
