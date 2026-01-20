package service

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"manager-server/internal/models"

	"github.com/patrickmn/go-cache"
)

// 缓存存储结构，替代Redis
var (
	// 激活码到设备ID的映射缓存: activationCode -> deviceID
	activationCodeCache *cache.Cache
	// 设备ID到激活数据的映射缓存: deviceID -> activationData
	deviceActivationCache *cache.Cache
)

// 初始化缓存，24小时有效期，每小时清理一次过期项
func init() {
	activationCodeCache = cache.New(24*time.Hour, time.Hour)
	deviceActivationCache = cache.New(24*time.Hour, time.Hour)
}

// FindDeviceByActivationCode 根据激活码查找设备ID（提供给repository使用）
func FindDeviceByActivationCode(activationCode string) string {
	if deviceID, found := activationCodeCache.Get(activationCode); found {
		return deviceID.(string)
	}
	return ""
}

// GetDeviceActivationInfo 获取设备激活数据（提供给repository使用）
func GetDeviceActivationInfo(deviceID string) map[string]interface{} {
	if data, found := deviceActivationCache.Get(deviceID); found {
		// 返回数据副本避免并发问题
		original := data.(map[string]interface{})
		result := make(map[string]interface{})
		for k, v := range original {
			result[k] = v
		}
		return result
	}
	return nil
}

// ClearActivationCode 清理全局存储中的激活码（提供给repository使用）
func ClearActivationCode(activationCode string) {
	// 获取设备ID
	if deviceID, found := activationCodeCache.Get(activationCode); found {
		// 删除激活码到设备的映射
		activationCodeCache.Delete(activationCode)
		// 删除设备激活数据
		deviceActivationCache.Delete(deviceID.(string))
	}
}

// buildActivation 生成设备激活码（与Java版本逻辑一致）
func (s *deviceService) buildActivation(ctx context.Context, macAddress string, req *models.DeviceReportReqDTO) *models.Activation {
	activation := &models.Activation{}

	// 检查是否已有缓存的激活码
	cachedCode := s.getCodeByDeviceID(ctx, macAddress)

	if cachedCode != "" {
		// 使用已存在的激活码
		activation.Code = cachedCode
		frontendURL := s.getFrontendURL(ctx)
		activation.Message = fmt.Sprintf("%s\n%s", frontendURL, cachedCode)
		activation.Challenge = macAddress
	} else {
		// 生成新的6位激活码
		newCode := s.generateRandomCode()
		activation.Code = newCode
		frontendURL := s.getFrontendURL(ctx)
		activation.Message = fmt.Sprintf("%s\n%s", frontendURL, newCode)
		activation.Challenge = macAddress

		// 存储设备数据到缓存
		s.storeDeviceActivationData(ctx, macAddress, newCode, req)
	}

	return activation
}

// getCodeByDeviceID 根据设备ID获取已存在的激活码
func (s *deviceService) getCodeByDeviceID(ctx context.Context, deviceID string) string {
	// 从缓存中查找设备的激活数据
	if data, found := deviceActivationCache.Get(deviceID); found {
		if activationData, ok := data.(map[string]interface{}); ok {
			if code, ok := activationData["activation_code"].(string); ok {
				return code
			}
		}
	}
	return ""
}

// getFrontendURL 获取前端URL配置
func (s *deviceService) getFrontendURL(ctx context.Context) string {
	// 尝试从系统参数获取前端URL
	frontendURL, err := s.paramsService.GetValue(ctx, "server.frontend.url", "")
	if err != nil || frontendURL == "" {
		// 使用默认的前端URL
		return "http://localhost:3000"
	}
	return frontendURL
}

// generateRandomCode 生成6位随机激活码
func (s *deviceService) generateRandomCode() string {
	rand.Seed(time.Now().UnixNano())
	code := rand.Intn(900000) + 100000 // 生成100000-999999之间的数字
	return strconv.Itoa(code)
}

// storeDeviceActivationData 存储设备激活数据到缓存，有效期24小时
func (s *deviceService) storeDeviceActivationData(ctx context.Context, macAddress, activationCode string, req *models.DeviceReportReqDTO) {
	// 构建设备数据
	deviceData := map[string]interface{}{
		"id":              macAddress,
		"mac_address":     macAddress,
		"deviceId":        macAddress,
		"activation_code": activationCode,
	}

	// 添加板子信息
	if req.Board != nil && req.Board.Type != "" {
		deviceData["board"] = req.Board.Type
	} else if req.ChipModelName != "" {
		deviceData["board"] = req.ChipModelName
	} else {
		deviceData["board"] = "unknown"
	}

	// 添加应用版本信息
	if req.Application != nil {
		deviceData["app_version"] = req.Application.Version
	} else {
		deviceData["app_version"] = nil
	}

	// 存储到缓存中，有效期24小时
	// 1. 存储设备激活数据: deviceID -> activationData
	deviceActivationCache.Set(macAddress, deviceData, cache.DefaultExpiration)

	// 2. 存储激活码反向映射: activationCode -> deviceID
	activationCodeCache.Set(activationCode, macAddress, cache.DefaultExpiration)

	fmt.Printf("Generated activation code %s for device %s (stored in cache with 24h expiration)\n", activationCode, macAddress)
}

// getDeviceCacheKey 获取设备缓存key（与Java版本保持一致）
func (s *deviceService) getDeviceCacheKey(deviceID string) string {
	// 将MAC地址中的冒号替换为下划线，转为小写
	safeDeviceID := ""
	for _, char := range deviceID {
		if char == ':' {
			safeDeviceID += "_"
		} else {
			safeDeviceID += string(char)
		}
	}
	safeDeviceID = fmt.Sprintf("%s", safeDeviceID) // 确保是小写，Go中字符串默认保持原样
	return fmt.Sprintf("ota:activation:data:%s", safeDeviceID)
}
