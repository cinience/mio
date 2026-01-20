package models

import (
	"time"
)

// DeviceEntity 设备实体，对应数据库中的ai_device表
type DeviceEntity struct {
	ID              string     `gorm:"primarykey;column:id;type:varchar(32)" json:"id"`
	UserID          *uint64    `gorm:"column:user_id;comment:关联用户ID" json:"userId"`
	MacAddress      string     `gorm:"column:mac_address;type:varchar(20);comment:MAC地址" json:"macAddress"`
	LastConnectedAt *time.Time `gorm:"column:last_connected_at;comment:最后连接时间" json:"lastConnectedAt"`
	AutoUpdate      *int       `gorm:"column:auto_update;type:int;default:1;comment:自动更新开关(0关闭/1开启)" json:"autoUpdate"`
	Board           string     `gorm:"column:board;type:varchar(50);comment:设备硬件型号" json:"board"`
	Alias           string     `gorm:"column:alias;type:varchar(100);comment:设备别名" json:"alias"`
	AgentID         string     `gorm:"column:agent_id;type:varchar(32);comment:智能体ID" json:"agentId"`
	AppVersion      string     `gorm:"column:app_version;type:varchar(20);comment:固件版本号" json:"appVersion"`
	Sort            *int       `gorm:"column:sort;type:int;comment:排序" json:"sort"`
	Creator         *uint64    `gorm:"column:creator;comment:创建者" json:"creator"`
	CreateDate      time.Time  `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	Updater         *uint64    `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdateDate      time.Time  `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
	// 新增专门的使用时长统计字段
	UsageSeconds    *int64     `gorm:"column:usage_seconds;type:bigint;default:0;comment:累计使用时长(秒)" json:"usageSeconds"`
}

// TableName 指定表名
func (DeviceEntity) TableName() string {
	return "ai_device"
}

// DeviceRegisterDTO 设备注册请求
type DeviceRegisterDTO struct {
	MacAddress string `json:"macAddress" binding:"required" validate:"min=1,max=20"`
}

// DeviceUnBindDTO 设备解绑请求
type DeviceUnBindDTO struct {
	DeviceID string `json:"deviceId" binding:"required"`
}

// DeviceUpdateDTO 设备更新请求
type DeviceUpdateDTO struct {
	Alias      string `json:"alias" validate:"max=100"`
	AutoUpdate *int   `json:"autoUpdate" validate:"oneof=0 1"`
	Sort       *int   `json:"sort"`
}

// DeviceManualAddDTO 手动添加设备请求
type DeviceManualAddDTO struct {
	AgentID    string `json:"agentId" binding:"required" validate:"min=1"`
	MacAddress string `json:"macAddress" binding:"required" validate:"min=1,max=20"`
	Alias      string `json:"alias" validate:"max=100"`
	Board      string `json:"board" validate:"max=50"`
	AppVersion string `json:"appVersion" validate:"max=50"`
}

// DeviceStatusReportDTO 设备状态上报请求
type DeviceStatusReportDTO struct {
	DeviceID        string `json:"deviceId" binding:"required"`                    // 设备ID
	SessionDuration int64  `json:"sessionDuration" binding:"required,min=0"`       // 本次会话使用时长（秒）
	IsOnline        bool   `json:"isOnline"`                                       // 设备在线状态
	AppVersion      string `json:"appVersion" validate:"max=20"`                   // 固件版本号（可选更新）
}

// DeviceWithUserInfoDTO 包含用户信息的设备DTO
type DeviceWithUserInfoDTO struct {
	ID                string    `gorm:"column:id" json:"id"`
	MacAddress        string    `gorm:"column:mac_address" json:"macAddress"`
	Alias             string    `gorm:"column:alias" json:"alias"`
	Board             string    `gorm:"column:board" json:"board"`
	AppVersion        string    `gorm:"column:app_version" json:"appVersion"`
	UserID            *uint64   `gorm:"column:user_id" json:"userId"`
	AgentID           string    `gorm:"column:agent_id" json:"agentId"`
	LastConnectedAt   *time.Time `gorm:"column:last_connected_at" json:"lastConnectedAt"`
	Creator           *uint64   `gorm:"column:creator" json:"creator"`
	CreateDate        time.Time `gorm:"column:create_date" json:"createDate"`
	Updater           *uint64   `gorm:"column:updater" json:"updater"`
	UpdateDate        time.Time `gorm:"column:update_date" json:"updateDate"`
	UsageSeconds      *int64    `gorm:"column:usage_seconds" json:"usageSeconds"` // 累计使用时长(秒)
	Username          string    `gorm:"column:username" json:"username"`  // 用户名
	AgentName         string    `gorm:"column:agent_name" json:"agentName"` // 智能体名称
	// 使用时长统计字段（不映射数据库字段）
	UsageDuration     string    `gorm:"-" json:"usageDuration"`     // 累计使用时长（格式化显示）
	OfflineDuration   string    `gorm:"-" json:"offlineDuration"`   // 离线时长
	TotalDays         int       `gorm:"-" json:"totalDays"`         // 注册总天数
	IsOnline          bool      `gorm:"-" json:"isOnline"`          // 当前在线状态
}
