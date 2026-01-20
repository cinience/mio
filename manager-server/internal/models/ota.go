package models

import (
	"time"
)

// OtaEntity OTA固件信息实体
type OtaEntity struct {
	ID           string     `json:"id" gorm:"column:id;primaryKey"`
	FirmwareName string     `json:"firmwareName" gorm:"column:firmware_name"`
	Type         string     `json:"type" gorm:"column:type"`
	Version      string     `json:"version" gorm:"column:version"`
	Size         *int64     `json:"size,omitempty" gorm:"column:size"`
	Remark       string     `json:"remark,omitempty" gorm:"column:remark"`
	FirmwarePath string     `json:"firmwarePath,omitempty" gorm:"column:firmware_path"`
	Sort         *int       `json:"sort,omitempty" gorm:"column:sort"`
	Updater      *uint64    `json:"updater,omitempty" gorm:"column:updater"`
	UpdateDate   *time.Time `json:"updateDate,omitempty" gorm:"column:update_date"`
	Creator      *uint64    `json:"creator,omitempty" gorm:"column:creator"`
	CreateDate   *time.Time `json:"createDate,omitempty" gorm:"column:create_date"`
}

// TableName 指定表名
func (OtaEntity) TableName() string {
	return "ai_ota"
}

// OtaPageReqDTO OTA分页查询请求DTO
type OtaPageReqDTO struct {
	PageNum      int    `json:"pageNum" form:"pageNum"`
	PageSize     int    `json:"pageSize" form:"pageSize"`
	FirmwareName string `json:"firmwareName" form:"firmwareName"`
	OrderField   string `json:"orderField" form:"orderField"`
	Order        string `json:"order" form:"order"`
}

// OtaCreateReqDTO OTA创建请求DTO
type OtaCreateReqDTO struct {
	FirmwareName string `json:"firmwareName" binding:"required"`
	Type         string `json:"type" binding:"required"`
	Version      string `json:"version" binding:"required"`
	Size         *int64 `json:"size,omitempty"`
	Remark       string `json:"remark,omitempty"`
	FirmwarePath string `json:"firmwarePath,omitempty"`
	Sort         *int   `json:"sort,omitempty"`
}

// OtaUpdateReqDTO OTA更新请求DTO
type OtaUpdateReqDTO struct {
	ID           string `json:"id"`
	FirmwareName string `json:"firmwareName" binding:"required"`
	Type         string `json:"type" binding:"required"`
	Version      string `json:"version" binding:"required"`
	Size         *int64 `json:"size,omitempty"`
	Remark       string `json:"remark,omitempty"`
	FirmwarePath string `json:"firmwarePath,omitempty"`
	Sort         *int   `json:"sort,omitempty"`
}
