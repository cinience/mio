package manager_api

import (
	"context"
	"strings"
	"sync"
	"time"

	"backend-server/internal/adapters/manager/types"
	"backend-server/internal/infrastructure/logger"
)

// DeviceStatusManager 设备状态管理器
type DeviceStatusManager struct {
	client   *HTTPClient
	sessions map[string]*types.DeviceSessionTracker
	mutex    sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewDeviceStatusManager 创建设备状态管理器
func NewDeviceStatusManager(client *HTTPClient) *DeviceStatusManager {
	ctx, cancel := context.WithCancel(context.Background())

	manager := &DeviceStatusManager{
		client:   client,
		sessions: make(map[string]*types.DeviceSessionTracker),
		ctx:      ctx,
		cancel:   cancel,
	}

	// 启动定期上报goroutine
	manager.wg.Add(1)
	go manager.periodicReportLoop()

	return manager
}

// StartSession 开始设备会话
func (dsm *DeviceStatusManager) StartSession(deviceID, appVersion string) {
	dsm.mutex.Lock()
	defer dsm.mutex.Unlock()

	// 如果已存在会话，先结束旧会话
	if existingSession, exists := dsm.sessions[deviceID]; exists {
		logger.Infof("设备 %s 已存在会话，结束旧会话", deviceID)
		existingSession.Close()
	}

	// 创建新会话
	session := types.NewDeviceSessionTracker(deviceID, appVersion)
	dsm.sessions[deviceID] = session

	logger.Infof("设备 %s 开始新会话，固件版本: %s", deviceID, appVersion)
}

// EndSession 结束设备会话并上报最终状态
func (dsm *DeviceStatusManager) EndSession(deviceID string) {
	dsm.mutex.Lock()
	session, exists := dsm.sessions[deviceID]
	if !exists {
		dsm.mutex.Unlock()
		logger.Warnf("设备 %s 会话不存在，无法结束", deviceID)
		return
	}

	// 关闭会话
	session.Close()
	sessionDuration := session.GetSessionDuration()
	appVersion := session.AppVersion

	// 从管理器中移除会话
	delete(dsm.sessions, deviceID)
	dsm.mutex.Unlock()

	// 上报最终状态
	if sessionDuration > 0 {
		req := &types.DeviceStatusReportRequest{
			DeviceID:        deviceID,
			SessionDuration: sessionDuration,
			IsOnline:        false, // 会话结束，设备离线
			AppVersion:      appVersion,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := dsm.client.ReportDeviceStatus(ctx, req)
		if err != nil {
			logger.Errorf("设备 %s 会话结束时上报状态失败: %v", deviceID, err)
		} else {
			logger.Infof("设备 %s 会话结束，上报使用时长: %d秒", deviceID, sessionDuration)
		}
	}
}

// UpdateSessionActivity 更新会话活动（可选，用于记录中间活动）
func (dsm *DeviceStatusManager) UpdateSessionActivity(deviceID string) {
	dsm.mutex.RLock()
	session, exists := dsm.sessions[deviceID]
	dsm.mutex.RUnlock()

	if exists && session.IsActive {
		session.LastReport = time.Now()
	}
}

// periodicReportLoop 定期上报循环
func (dsm *DeviceStatusManager) periodicReportLoop() {
	defer dsm.wg.Done()

	ticker := time.NewTicker(60 * time.Second) // 增加到每60秒上报一次，减少频率
	defer ticker.Stop()

	for {
		select {
		case <-dsm.ctx.Done():
			logger.Infof("设备状态管理器停止定期上报")
			return
		case <-ticker.C:
			// 添加 recover 防止 panic
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("设备状态上报出现panic: %v", r)
					}
				}()
				dsm.reportActiveSessions()
			}()
		}
	}
}

// reportActiveSessions 上报所有活跃会话的状态
func (dsm *DeviceStatusManager) reportActiveSessions() {
	dsm.mutex.RLock()
	activeSessions := make([]*types.DeviceSessionTracker, 0, len(dsm.sessions))
	for _, session := range dsm.sessions {
		if session.IsActive {
			activeSessions = append(activeSessions, session)
		}
	}
	dsm.mutex.RUnlock()

	if len(activeSessions) == 0 {
		return
	}

	logger.Debugf("开始定期上报 %d 个活跃设备会话状态", len(activeSessions))

	// 使用信号量控制并发数，避免同时发起太多请求
	maxConcurrent := 5 // 最多同时5个请求
	semaphore := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for _, session := range activeSessions {
		// 计算自上次上报以来的时长
		now := time.Now()
		duration := int64(now.Sub(session.LastReport).Seconds())

		if duration < 10 {
			continue // 跳过时长太短的上报，改为至少10秒
		}

		wg.Add(1)
		go func(s *types.DeviceSessionTracker, dur int64) {
			defer wg.Done()

			// 获取信号量
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-dsm.ctx.Done():
				return
			}

			dsm.reportSingleSession(s, dur)
		}(session, duration)
	}

	// 等待所有上报完成，但设置超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Debugf("所有设备状态上报完成")
	case <-time.After(30 * time.Second):
		logger.Warnf("设备状态上报超时，部分请求可能未完成")
	case <-dsm.ctx.Done():
		logger.Infof("设备状态管理器正在关闭，取消上报")
	}
}

// reportSingleSession 上报单个设备会话状态
func (dsm *DeviceStatusManager) reportSingleSession(session *types.DeviceSessionTracker, duration int64) {
	req := &types.DeviceStatusReportRequest{
		DeviceID:        session.DeviceID,
		SessionDuration: duration,
		IsOnline:        true,
		AppVersion:      session.AppVersion,
	}

	// 使用较长的超时时间，避免连接建立失败
	ctx, cancel := context.WithTimeout(dsm.ctx, 15*time.Second)
	defer cancel()

	err := dsm.client.ReportDeviceStatus(ctx, req)

	if err != nil {
		// 根据错误类型决定是否记录为错误还是警告
		if strings.Contains(err.Error(), "cannot allocate memory") ||
			strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "timeout") {
			logger.Warnf("设备 %s 定期上报状态失败(网络问题): %v", session.DeviceID, err)
		} else {
			logger.Errorf("设备 %s 定期上报状态失败: %v", session.DeviceID, err)
		}
		return
	}

	// 更新会话信息
	dsm.mutex.Lock()
	if currentSession, exists := dsm.sessions[session.DeviceID]; exists && currentSession.IsActive {
		currentSession.UpdateSession(duration)
		logger.Debugf("设备 %s 定期上报成功，时长: %d秒", session.DeviceID, duration)
	}
	dsm.mutex.Unlock()
}

// GetSessionInfo 获取设备会话信息
func (dsm *DeviceStatusManager) GetSessionInfo(deviceID string) (*types.DeviceSessionTracker, bool) {
	dsm.mutex.RLock()
	defer dsm.mutex.RUnlock()

	session, exists := dsm.sessions[deviceID]
	if !exists {
		return nil, false
	}

	// 返回会话的副本
	sessionCopy := *session
	return &sessionCopy, true
}

// GetAllActiveSessions 获取所有活跃会话信息
func (dsm *DeviceStatusManager) GetAllActiveSessions() map[string]*types.DeviceSessionTracker {
	dsm.mutex.RLock()
	defer dsm.mutex.RUnlock()

	result := make(map[string]*types.DeviceSessionTracker)
	for deviceID, session := range dsm.sessions {
		if session.IsActive {
			sessionCopy := *session
			result[deviceID] = &sessionCopy
		}
	}

	return result
}

// Close 关闭设备状态管理器
func (dsm *DeviceStatusManager) Close() {
	logger.Infof("正在关闭设备状态管理器...")

	// 结束所有活跃会话
	dsm.mutex.Lock()
	for deviceID := range dsm.sessions {
		logger.Infof("结束设备 %s 的会话", deviceID)
	}
	activeSessions := make([]string, 0, len(dsm.sessions))
	for deviceID := range dsm.sessions {
		activeSessions = append(activeSessions, deviceID)
	}
	dsm.mutex.Unlock()

	// 逐个结束会话
	for _, deviceID := range activeSessions {
		dsm.EndSession(deviceID)
	}

	// 停止定期上报
	dsm.cancel()
	dsm.wg.Wait()

	logger.Infof("设备状态管理器已关闭")
}

// GetStats 获取管理器统计信息
func (dsm *DeviceStatusManager) GetStats() map[string]interface{} {
	dsm.mutex.RLock()
	defer dsm.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_sessions":  len(dsm.sessions),
		"active_sessions": 0,
		"devices":         make([]string, 0, len(dsm.sessions)),
	}

	activeCount := 0
	devices := make([]string, 0, len(dsm.sessions))

	for deviceID, session := range dsm.sessions {
		devices = append(devices, deviceID)
		if session.IsActive {
			activeCount++
		}
	}

	stats["active_sessions"] = activeCount
	stats["devices"] = devices

	return stats
}
