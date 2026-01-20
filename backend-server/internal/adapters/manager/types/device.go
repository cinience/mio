package types

import "time"

// DeviceStatusReportRequest 设备状态上报请求
type DeviceStatusReportRequest struct {
	DeviceID        string `json:"deviceId" validate:"required"`     // 设备ID
	SessionDuration int64  `json:"sessionDuration" validate:"min=0"` // 本次会话使用时长（秒）
	IsOnline        bool   `json:"isOnline"`                         // 设备在线状态
	AppVersion      string `json:"appVersion,omitempty"`             // 固件版本号（可选）
}

// DeviceStatusReportResponse 设备状态上报响应
type DeviceStatusReportResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// DeviceInfo 设备信息
type DeviceInfo struct {
	ID              string     `json:"id"`
	MacAddress      string     `json:"macAddress"`
	Alias           string     `json:"alias"`
	Board           string     `json:"board"`
	AppVersion      string     `json:"appVersion"`
	UserID          *uint64    `json:"userId"`
	AgentID         string     `json:"agentId"`
	LastConnectedAt *time.Time `json:"lastConnectedAt"`
	Creator         *uint64    `json:"creator"`
	CreateDate      time.Time  `json:"createDate"`
	Updater         *uint64    `json:"updater"`
	UpdateDate      time.Time  `json:"updateDate"`
	UsageSeconds    *int64     `json:"usageSeconds"`    // 累计使用时长(秒)
	Username        string     `json:"username"`        // 用户名
	AgentName       string     `json:"agentName"`       // 智能体名称
	UsageDuration   string     `json:"usageDuration"`   // 累计使用时长（格式化显示）
	OfflineDuration string     `json:"offlineDuration"` // 离线时长
	TotalDays       int        `json:"totalDays"`       // 注册总天数
	IsOnline        bool       `json:"isOnline"`        // 当前在线状态
}

// DeviceInfoResponse 设备信息响应
type DeviceInfoResponse struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data *DeviceInfo `json:"data,omitempty"`
}

// DeviceSessionTracker 设备会话跟踪器
type DeviceSessionTracker struct {
	DeviceID    string    `json:"deviceId"`
	StartTime   time.Time `json:"startTime"`   // 会话开始时间
	LastReport  time.Time `json:"lastReport"`  // 最后上报时间
	IsActive    bool      `json:"isActive"`    // 会话是否活跃
	AppVersion  string    `json:"appVersion"`  // 固件版本
	TotalTime   int64     `json:"totalTime"`   // 累计时长（秒）
	ReportCount int64     `json:"reportCount"` // 上报次数
}

// NewDeviceSessionTracker 创建新的设备会话跟踪器
func NewDeviceSessionTracker(deviceID, appVersion string) *DeviceSessionTracker {
	now := time.Now()
	return &DeviceSessionTracker{
		DeviceID:    deviceID,
		StartTime:   now,
		LastReport:  now,
		IsActive:    true,
		AppVersion:  appVersion,
		TotalTime:   0,
		ReportCount: 0,
	}
}

// UpdateSession 更新会话信息
func (dst *DeviceSessionTracker) UpdateSession(duration int64) {
	dst.LastReport = time.Now()
	dst.TotalTime += duration
	dst.ReportCount++
}

// GetSessionDuration 获取当前会话总时长
func (dst *DeviceSessionTracker) GetSessionDuration() int64 {
	if !dst.IsActive {
		return dst.TotalTime
	}
	return dst.TotalTime + int64(time.Since(dst.LastReport).Seconds())
}

// Close 关闭会话
func (dst *DeviceSessionTracker) Close() {
	dst.IsActive = false
}
