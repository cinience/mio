package models

import "time"

// SysDictType 字典类型模型，对应数据库中的sys_dict_type表
type SysDictType struct {
	ID         uint64    `gorm:"primarykey;column:id" json:"id"`
	DictType   string    `gorm:"column:dict_type;type:varchar(100);not null;uniqueIndex" json:"dictType"`
	DictName   string    `gorm:"column:dict_name;type:varchar(255);not null" json:"dictName"`
	Remark     string    `gorm:"column:remark;type:varchar(255)" json:"remark"`
	Sort       *int      `gorm:"column:sort" json:"sort"`
	Creator    *uint64   `gorm:"column:creator;comment:创建者" json:"creator"`
	CreateDate time.Time `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	Updater    *uint64   `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdateDate time.Time `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
}

// TableName 指定表名
func (SysDictType) TableName() string {
	return "sys_dict_type"
}

// SysDictData 字典数据模型，对应数据库中的sys_dict_data表
type SysDictData struct {
	ID         uint64    `gorm:"primarykey;column:id" json:"id"`
	DictTypeID uint64    `gorm:"column:dict_type_id;not null;uniqueIndex:uk_dict_type_value,priority:1" json:"dictTypeId"`
	DictLabel  string    `gorm:"column:dict_label;type:varchar(255);not null" json:"dictLabel"`
	DictValue  string    `gorm:"column:dict_value;type:varchar(255);uniqueIndex:uk_dict_type_value,priority:2" json:"dictValue"`
	Remark     string    `gorm:"column:remark;type:varchar(255)" json:"remark"`
	Sort       *int      `gorm:"column:sort;index:idx_sort" json:"sort"`
	Creator    *uint64   `gorm:"column:creator;comment:创建者" json:"creator"`
	CreateDate time.Time `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	Updater    *uint64   `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdateDate time.Time `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
}

// TableName 指定表名
func (SysDictData) TableName() string {
	return "sys_dict_data"
}

// SysDictDataVO 字典数据视图对象
type SysDictDataVO struct {
	ID         uint64    `json:"id"`
	DictTypeID uint64    `json:"dictTypeId"`
	DictLabel  string    `json:"dictLabel"`
	DictValue  string    `json:"dictValue"`
	Remark     string    `json:"remark"`
	Sort       *int      `json:"sort"`
	Creator    *uint64   `json:"creator"`
	CreateDate time.Time `json:"createDate"`
	Updater    *uint64   `json:"updater"`
	UpdateDate time.Time `json:"updateDate"`
}

// SysDictDataItem 字典数据项，用于前端下拉选择等
type SysDictDataItem struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ToVO 转换为VO
func (d *SysDictData) ToVO() *SysDictDataVO {
	return &SysDictDataVO{
		ID:         d.ID,
		DictTypeID: d.DictTypeID,
		DictLabel:  d.DictLabel,
		DictValue:  d.DictValue,
		Remark:     d.Remark,
		Sort:       d.Sort,
		Creator:    d.Creator,
		CreateDate: d.CreateDate,
		Updater:    d.Updater,
		UpdateDate: d.UpdateDate,
	}
}

// SysParamsDTO 系统参数传输对象
type SysParamsDTO struct {
	ID         uint64  `json:"id"`
	ParamCode  string  `json:"paramCode" binding:"required" validate:"min=1,max=32"`
	ParamValue string  `json:"paramValue" validate:"max=2000"`
	ValueType  string  `json:"valueType" validate:"max=255"`
	ParamType  *int    `json:"paramType" validate:"oneof=0 1"`
	Remark     string  `json:"remark" validate:"max=200"`
	Creator    *uint64 `json:"creator"`
}

// SysDictDataDTO 字典数据传输对象
type SysDictDataDTO struct {
	ID         uint64  `json:"id"`
	DictTypeID uint64  `json:"dictTypeId" binding:"required"`
	DictLabel  string  `json:"dictLabel" binding:"required" validate:"min=1,max=255"`
	DictValue  string  `json:"dictValue" validate:"max=255"`
	Remark     string  `json:"remark" validate:"max=255"`
	Sort       *int    `json:"sort"`
	Creator    *uint64 `json:"creator"`
}

// SysDictTypeDTO 字典类型传输对象
type SysDictTypeDTO struct {
	ID       uint64  `json:"id"`
	DictType string  `json:"dictType" binding:"required" validate:"min=1,max=100"`
	DictName string  `json:"dictName" binding:"required" validate:"min=1,max=255"`
	Remark   string  `json:"remark" validate:"max=255"`
	Sort     *int    `json:"sort"`
	Creator  *uint64 `json:"creator"`
}
