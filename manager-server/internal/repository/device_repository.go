package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"manager-server/internal/models"
)

// DeviceRepository 设备仓储接口
type DeviceRepository interface {
	FindByID(ctx context.Context, id string) (*models.DeviceEntity, error)
	GetDeviceWithUserInfo(ctx context.Context, deviceID string) (*models.DeviceWithUserInfoDTO, error) // 获取设备详细信息（包含使用时长统计）
	FindByMacAddress(ctx context.Context, macAddress string) (*models.DeviceEntity, error)
	FindByAgentID(ctx context.Context, agentID string) ([]*models.DeviceEntity, error)
	FindUserDevices(ctx context.Context, userID uint64, agentID string) ([]*models.DeviceEntity, error)
	FindUserDevicesPage(ctx context.Context, userID uint64, agentID string, offset, limit int, keywords string) ([]*models.DeviceEntity, int64, error)
	FindAllDevices(ctx context.Context) ([]*models.DeviceEntity, error) // 获取所有设备（系统调用）
	Create(ctx context.Context, device *models.DeviceEntity) error
	Update(ctx context.Context, device *models.DeviceEntity) error
	DeleteByID(ctx context.Context, id string) error
	ActivateDevice(ctx context.Context, userID uint64, agentID, deviceCode string) error
	UnbindDevice(ctx context.Context, userID uint64, deviceID string) error
	PageDevices(ctx context.Context, offset, limit int, keywords string) ([]*models.DeviceEntity, int64, error)
	PageDevicesWithUserInfo(ctx context.Context, offset, limit int, keywords string) ([]*models.DeviceWithUserInfoDTO, int64, error)
	CountByAgentID(ctx context.Context, agentID string) (int64, error)                   // 根据智能体ID获取设备数量
	GetLatestLastConnectionTime(ctx context.Context, agentID string) (*time.Time, error) // 获取智能体最后连接时间
	StoreActivationCode(ctx context.Context, code, macAddress string) error              // 存储激活码到内存
	GetActivationCode(ctx context.Context, code string) (string, error)                  // 获取激活码对应的MAC地址
	CountByUserID(ctx context.Context, userID uint64) (int64, error)                     // 根据用户ID获取设备数量
	ReportDeviceStatus(ctx context.Context, req *models.DeviceStatusReportDTO) error     // 上报设备状态和使用时长
}

// deviceRepository 设备仓储实现
type deviceRepository struct {
	db              *gorm.DB
	activationCodes map[string]activationCodeData // 内存存储激活码
	mu              sync.RWMutex                  // 保护激活码map的读写锁
	usageColumnMu   sync.Mutex                    // 保护usage_seconds列检查的互斥锁
}

// activationCodeData 激活码数据
type activationCodeData struct {
	MacAddress string
	ExpiresAt  time.Time
}

// NewDeviceRepository 创建设备仓储实例
func NewDeviceRepository(db *gorm.DB) DeviceRepository {
	return &deviceRepository{
		db:              db,
		activationCodes: make(map[string]activationCodeData),
	}
}

// FindByID 根据ID查找设备
func (r *deviceRepository) FindByID(ctx context.Context, id string) (*models.DeviceEntity, error) {
	var device models.DeviceEntity
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&device).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

// GetDeviceWithUserInfo 获取设备详细信息（包含使用时长统计）
func (r *deviceRepository) GetDeviceWithUserInfo(ctx context.Context, deviceID string) (*models.DeviceWithUserInfoDTO, error) {
	var result models.DeviceWithUserInfoDTO

	// 构建查询
	err := r.db.WithContext(ctx).Table("ai_device d").
		Select("d.*, u.username, a.agent_name").
		Joins("LEFT JOIN sys_user u ON d.user_id = u.id").
		Joins("LEFT JOIN ai_agent a ON d.agent_id = a.id").
		Where("d.id = ?", deviceID).
		Scan(&result).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	// 计算使用时长统计
	r.calculateUsageStats(&result)

	return &result, nil
}

// FindByMacAddress 根据MAC地址查找设备
func (r *deviceRepository) FindByMacAddress(ctx context.Context, macAddress string) (*models.DeviceEntity, error) {
	var device models.DeviceEntity
	err := r.db.WithContext(ctx).Where("mac_address = ?", macAddress).First(&device).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

// FindUserDevices 查找用户的设备列表
func (r *deviceRepository) FindUserDevices(ctx context.Context, userID uint64, agentID string) ([]*models.DeviceEntity, error) {
	var devices []*models.DeviceEntity
	query := r.db.WithContext(ctx)

	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}

	if agentID != "" {
		query = query.Where("agent_id = ?", agentID)
	}

	err := query.Order("sort ASC, create_date DESC").Find(&devices).Error
	return devices, err
}

// FindUserDevicesPage 分页查询用户设备列表
func (r *deviceRepository) FindUserDevicesPage(ctx context.Context, userID uint64, agentID string, offset, limit int, keywords string) ([]*models.DeviceEntity, int64, error) {
	var devices []*models.DeviceEntity
	query := r.db.WithContext(ctx).Model(&models.DeviceEntity{})

	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}

	if agentID != "" {
		query = query.Where("agent_id = ?", agentID)
	}
	if strings.TrimSpace(keywords) != "" {
		like := "%" + strings.TrimSpace(keywords) + "%"
		query = query.Where("mac_address LIKE ? OR alias LIKE ?", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("sort ASC, create_date DESC").Offset(offset).Limit(limit).Find(&devices).Error; err != nil {
		return nil, 0, err
	}
	return devices, total, nil
}

// FindAllDevices 获取所有设备（系统调用）
func (r *deviceRepository) FindAllDevices(ctx context.Context) ([]*models.DeviceEntity, error) {
	var devices []*models.DeviceEntity
	err := r.db.WithContext(ctx).
		Order("create_date DESC").
		Find(&devices).Error
	return devices, err
}

// FindByAgentID 根据智能体ID查找设备列表
func (r *deviceRepository) FindByAgentID(ctx context.Context, agentID string) ([]*models.DeviceEntity, error) {
	var devices []*models.DeviceEntity
	err := r.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("create_date DESC").
		Find(&devices).Error
	return devices, err
}

// Create 创建设备
func (r *deviceRepository) Create(ctx context.Context, device *models.DeviceEntity) error {
	device.CreateDate = time.Now()
	device.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(device).Error
}

// Update 更新设备
func (r *deviceRepository) Update(ctx context.Context, device *models.DeviceEntity) error {
	updates := map[string]interface{}{
		"user_id":     device.UserID,
		"agent_id":    device.AgentID,
		"mac_address": device.MacAddress,
		"alias":       device.Alias,
		"board":       device.Board,
		"auto_update": device.AutoUpdate,
		"sort":        device.Sort,
		"updater":     device.Updater,
		"update_date": time.Now(),
	}
	return r.db.WithContext(ctx).Model(&models.DeviceEntity{}).Where("id = ?", device.ID).Updates(updates).Error
}

// DeleteByID 根据ID删除设备
func (r *deviceRepository) DeleteByID(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.DeviceEntity{}, "id = ?", id).Error
}

// ActivateDevice 激活设备 - 绑定设备到智能体
func (r *deviceRepository) ActivateDevice(ctx context.Context, userID uint64, agentID, deviceCode string) error {
	// 从内存获取MAC地址
	macAddress, err := r.GetActivationCode(ctx, deviceCode)
	if err != nil {
		return err
	}

	// 查找设备
	device, err := r.FindByMacAddress(ctx, macAddress)
	if err != nil {
		return err
	}
	if device == nil {
		return gorm.ErrRecordNotFound
	}

	// 更新设备的智能体ID和用户ID
	err = r.db.WithContext(ctx).Model(&models.DeviceEntity{}).Where("id = ?", device.ID).Updates(map[string]interface{}{
		"user_id":     userID,
		"agent_id":    agentID,
		"updater":     userID,
		"update_date": time.Now(),
	}).Error
	if err != nil {
		return err
	}

	// 删除内存中的验证码
	r.mu.Lock()
	delete(r.activationCodes, deviceCode)
	r.mu.Unlock()

	return nil
}

// UnbindDevice 解绑设备
func (r *deviceRepository) UnbindDevice(ctx context.Context, userID uint64, deviceID string) error {
	//return r.db.WithContext(ctx).Model(&models.DeviceEntity{}).
	//	Where("id = ? AND user_id = ?", deviceID, userID).
	//	Updates(map[string]interface{}{
	//		"agent_id":    "",
	//		"update_date": time.Now(),
	//	}).Error
	if userID == 0 {
		return r.db.WithContext(ctx).Delete(&models.DeviceEntity{}, "id = ?", deviceID).Error
	}

	return r.db.WithContext(ctx).Delete(&models.DeviceEntity{}, "id = ? AND user_id = ?", deviceID, userID).Error
}

// PageDevices 分页查询所有设备（管理员功能）
func (r *deviceRepository) PageDevices(ctx context.Context, offset, limit int, keywords string) ([]*models.DeviceEntity, int64, error) {
	var devices []*models.DeviceEntity
	var total int64

	query := r.db.WithContext(ctx).Model(&models.DeviceEntity{})

	// 关键词搜索
	if keywords != "" {
		query = query.Where("mac_address LIKE ? OR alias LIKE ? OR board LIKE ?",
			"%"+keywords+"%", "%"+keywords+"%", "%"+keywords+"%")
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	err := query.Offset(offset).Limit(limit).Order("create_date DESC").Find(&devices).Error
	return devices, total, err
}

// PageDevicesWithUserInfo 分页查询所有设备（包含用户信息）
func (r *deviceRepository) PageDevicesWithUserInfo(ctx context.Context, offset, limit int, keywords string) ([]*models.DeviceWithUserInfoDTO, int64, error) {
	var results []*models.DeviceWithUserInfoDTO
	var total int64

	// 构建查询
	query := r.db.WithContext(ctx).Table("ai_device d").
		Select("d.*, u.username, a.agent_name").
		Joins("LEFT JOIN sys_user u ON d.user_id = u.id").
		Joins("LEFT JOIN ai_agent a ON d.agent_id = a.id")

	// 关键词搜索
	if keywords != "" {
		query = query.Where("d.mac_address LIKE ? OR d.alias LIKE ? OR d.board LIKE ? OR u.username LIKE ? OR a.agent_name LIKE ?",
			"%"+keywords+"%", "%"+keywords+"%", "%"+keywords+"%", "%"+keywords+"%", "%"+keywords+"%")
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	err := query.Offset(offset).Limit(limit).Order("d.create_date DESC").Scan(&results).Error
	if err != nil {
		return nil, 0, err
	}

	// 计算使用时长统计
	for _, device := range results {
		r.calculateUsageStats(device)
	}

	return results, total, err
}

// CountByAgentID 根据智能体ID获取设备数量
func (r *deviceRepository) CountByAgentID(ctx context.Context, agentID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.DeviceEntity{}).
		Where("agent_id = ?", agentID).
		Count(&count).Error
	return count, err
}

// CountByUserID 根据用户ID获取设备数量
func (r *deviceRepository) CountByUserID(ctx context.Context, userID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.DeviceEntity{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	return count, err
}

// GetLatestLastConnectionTime 获取智能体最后连接时间
func (r *deviceRepository) GetLatestLastConnectionTime(ctx context.Context, agentID string) (*time.Time, error) {
	var device models.DeviceEntity
	err := r.db.WithContext(ctx).
		Where("agent_id = ? AND last_connected_at IS NOT NULL", agentID).
		Order("last_connected_at DESC").
		First(&device).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // 没有连接记录
		}
		return nil, err
	}

	return device.LastConnectedAt, nil
}

// StoreActivationCode 存储激活码到内存
func (r *deviceRepository) StoreActivationCode(ctx context.Context, code, macAddress string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 清理过期的激活码
	r.cleanExpiredActivationCodes()

	// 存储激活码，设置5分钟过期时间
	r.activationCodes[code] = activationCodeData{
		MacAddress: macAddress,
		ExpiresAt:  time.Now().Add(5 * time.Minute),
	}

	return nil
}

// GetActivationCode 获取激活码对应的MAC地址
func (r *deviceRepository) GetActivationCode(ctx context.Context, code string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, exists := r.activationCodes[code]
	if !exists {
		return "", gorm.ErrRecordNotFound
	}

	// 检查是否过期
	if time.Now().After(data.ExpiresAt) {
		return "", gorm.ErrRecordNotFound
	}

	return data.MacAddress, nil
}

// cleanExpiredActivationCodes 清理过期的激活码 (需要在调用前获取写锁)
func (r *deviceRepository) cleanExpiredActivationCodes() {
	now := time.Now()
	for code, data := range r.activationCodes {
		if now.After(data.ExpiresAt) {
			delete(r.activationCodes, code)
		}
	}
}

// calculateUsageStats 计算设备使用时长统计
func (r *deviceRepository) calculateUsageStats(device *models.DeviceWithUserInfoDTO) {
	now := time.Now()

	// 计算注册总天数
	totalDuration := now.Sub(device.CreateDate)
	device.TotalDays = int(totalDuration.Hours() / 24)

	// 基于UsageSeconds字段计算累计使用时长
	if device.UsageSeconds != nil && *device.UsageSeconds > 0 {
		cumulativeDuration := time.Duration(*device.UsageSeconds) * time.Second
		device.UsageDuration = r.formatDuration(cumulativeDuration)
	} else {
		device.UsageDuration = "0分钟"
	}

	// 计算离线时长
	if device.LastConnectedAt != nil {
		offlineDuration := now.Sub(*device.LastConnectedAt)
		device.OfflineDuration = r.formatDuration(offlineDuration)
		// 判断设备是否在线（5分钟内有连接视为在线）
		device.IsOnline = offlineDuration.Minutes() <= 5
	} else {
		device.OfflineDuration = "从未连接"
		device.IsOnline = false
	}
}

// ReportDeviceStatus 上报设备状态和使用时长
func (r *deviceRepository) ReportDeviceStatus(ctx context.Context, req *models.DeviceStatusReportDTO) error {
	if err := r.ensureUsageSecondsColumn(ctx); err != nil {
		return fmt.Errorf("准备usage_seconds字段失败: %w", err)
	}

	// 查找设备
	device, err := r.FindByID(ctx, req.DeviceID)
	if err != nil {
		return fmt.Errorf("设备不存在: %w", err)
	}
	if device == nil {
		return fmt.Errorf("设备不存在: %s", req.DeviceID)
	}

	// 计算新的累计使用时长
	var currentUsageSeconds int64 = 0
	if device.UsageSeconds != nil {
		currentUsageSeconds = *device.UsageSeconds
	}

	// 新的累计时长 = 当前累计时长 + 本次会话时长
	newUsageSeconds := currentUsageSeconds + req.SessionDuration

	// 更新设备信息
	updates := map[string]interface{}{
		"usage_seconds": newUsageSeconds,
		"update_date":   time.Now(), // 正常更新修改时间
	}

	// 如果设备在线，更新最后连接时间
	if req.IsOnline {
		updates["last_connected_at"] = time.Now()
	}

	// 如果提供了固件版本，更新固件版本
	if req.AppVersion != "" {
		updates["app_version"] = req.AppVersion
	}

	// 执行更新
	result := r.db.WithContext(ctx).Model(&models.DeviceEntity{}).
		Where("id = ?", req.DeviceID).
		Updates(updates)

	if result.Error != nil {
		return fmt.Errorf("更新设备状态失败: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("设备不存在或无权限更新")
	}

	return nil
}

func (r *deviceRepository) ensureUsageSecondsColumn(ctx context.Context) error {
	r.usageColumnMu.Lock()
	defer r.usageColumnMu.Unlock()

	migrator := r.db.Migrator()
	if migrator.HasColumn(&models.DeviceEntity{}, "usage_seconds") {
		return nil
	}

	if err := migrator.AddColumn(&models.DeviceEntity{}, "UsageSeconds"); err != nil {
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "duplicate column") {
			alterErr := r.db.WithContext(ctx).Exec("ALTER TABLE ai_device ADD COLUMN usage_seconds BIGINT DEFAULT 0 COMMENT '累计使用时长(秒)'").Error
			if alterErr != nil && !strings.Contains(strings.ToLower(alterErr.Error()), "duplicate column") {
				return fmt.Errorf("添加usage_seconds列失败: %w", err)
			}
		}
	}

	if !migrator.HasColumn(&models.DeviceEntity{}, "usage_seconds") {
		return fmt.Errorf("usage_seconds列缺失，请检查数据库结构")
	}

	if err := r.db.WithContext(ctx).Model(&models.DeviceEntity{}).Where("usage_seconds IS NULL").Update("usage_seconds", 0).Error; err != nil {
		return fmt.Errorf("初始化usage_seconds列失败: %w", err)
	}

	return nil
}

// formatDuration 格式化时长显示
func (r *deviceRepository) formatDuration(duration time.Duration) string {
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%d天%d小时", days, hours)
	} else if hours > 0 {
		return fmt.Sprintf("%d小时%d分钟", hours, minutes)
	} else if minutes > 0 {
		return fmt.Sprintf("%d分钟", minutes)
	} else {
		return "不到1分钟"
	}
}
