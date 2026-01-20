package models

import "time"

// DeviceReportReqDTO 设备固件信息上报请求体
type DeviceReportReqDTO struct {
	Version             int          `json:"version"`                   // 板子固件版本号
	FlashSize           int          `json:"flash_size"`                // 闪存大小（单位：字节）
	MinimumFreeHeapSize int64        `json:"minimum_free_heap_size"`    // 最小空闲堆内存（字节）
	MacAddress          string       `json:"mac_address"`               // 设备 MAC 地址
	UUID                string       `json:"uuid"`                      // 设备唯一标识 UUID
	ChipModelName       string       `json:"chip_model_name"`           // 芯片型号名称
	ChipInfo            *ChipInfo    `json:"chip_info,omitempty"`       // 芯片详细信息
	Application         *Application `json:"application,omitempty"`     // 应用程序信息
	PartitionTable      []Partition  `json:"partition_table,omitempty"` // 分区表列表
	OTA                 *OTAInfo     `json:"ota,omitempty"`             // 当前运行的 OTA 分区信息
	Board               *BoardInfo   `json:"board,omitempty"`           // 板子配置信息
}

// ChipInfo 芯片信息
type ChipInfo struct {
	Model    int `json:"model"`    // 芯片模型代码
	Cores    int `json:"cores"`    // 核心数
	Revision int `json:"revision"` // 硬件修订版本
	Features int `json:"features"` // 芯片功能标志位
}

// Application 板子编译信息
type Application struct {
	Name        string `json:"name"`         // 名称
	Version     string `json:"version"`      // 应用版本号
	CompileTime string `json:"compile_time"` // 编译时间（UTC ISO格式）
	IDFVersion  string `json:"idf_version"`  // ESP-IDF 版本号
	ElfSHA256   string `json:"elf_sha256"`   // ELF 文件 SHA256 校验
}

// Partition 分区信息
type Partition struct {
	Label   string `json:"label"`   // 分区标签名
	Type    int    `json:"type"`    // 分区类型
	Subtype int    `json:"subtype"` // 子类型
	Address int    `json:"address"` // 起始地址
	Size    int    `json:"size"`    // 分区大小
}

// OTAInfo OTA信息
type OTAInfo struct {
	Label string `json:"label"` // 当前OTA标签
}

// BoardInfo 板子连接和网络信息
type BoardInfo struct {
	Type    string `json:"type"`    // 板子类型
	SSID    string `json:"ssid"`    // 连接的 Wi-Fi SSID
	RSSI    int    `json:"rssi"`    // Wi-Fi 信号强度（RSSI）
	Channel int    `json:"channel"` // Wi-Fi 信道
	IP      string `json:"ip"`      // IP 地址
	MAC     string `json:"mac"`     // MAC 地址
}

// DeviceReportRespDTO 设备OTA检测版本返回体，包含激活码要求
type DeviceReportRespDTO struct {
	ServerTime *ServerTime `json:"server_time,omitempty"` // 服务器时间
	Activation *Activation `json:"activation,omitempty"`  // 激活码
	Error      string      `json:"error,omitempty"`       // 错误信息
	Firmware   *Firmware   `json:"firmware,omitempty"`    // 固件版本信息
	Websocket  *Websocket  `json:"websocket,omitempty"`   // WebSocket配置
}

// Firmware 固件信息
type Firmware struct {
	Version string `json:"version"` // 版本号
	URL     string `json:"url"`     // 下载地址
}

// Activation 激活码信息
type Activation struct {
	Code      string `json:"code"`      // 激活码
	Message   string `json:"message"`   // 激活码信息: 激活地址
	Challenge string `json:"challenge"` // 挑战码
}

// ServerTime 服务器时间
type ServerTime struct {
	Timestamp      int64  `json:"timestamp"`       // 时间戳
	TimeZone       string `json:"timeZone"`        // 时区
	TimezoneOffset int    `json:"timezone_offset"` // 时区偏移量，单位为分钟
}

// Websocket WebSocket配置
type Websocket struct {
	URL string `json:"url"` // WebSocket服务器地址
}

// CreateErrorResponse 创建错误响应
func CreateErrorResponse(message string) *DeviceReportRespDTO {
	return &DeviceReportRespDTO{
		Error: message,
	}
}

// CreateServerTime 创建服务器时间
func CreateServerTime() *ServerTime {
	now := time.Now()
	_, offset := now.Zone()

	return &ServerTime{
		Timestamp:      now.Unix(),
		TimeZone:       "Asia/Shanghai", // 默认使用中国时区
		TimezoneOffset: offset / 60,     // 转换为分钟
	}
}
