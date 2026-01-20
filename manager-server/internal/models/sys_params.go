package models

import "time"

// SysParams 参数管理模型，对应数据库中的sys_params表
type SysParams struct {
	ID         uint64    `gorm:"primarykey;column:id" json:"id"`
	ParamCode  string    `gorm:"column:param_code;type:varchar(64);uniqueIndex:uk_param_code" json:"paramCode"`
	ParamValue string    `gorm:"column:param_value;type:varchar(2000)" json:"paramValue"`
	ValueType  string    `gorm:"column:value_type;type:varchar(20)" json:"valueType"` // 值类型：string-字符串，number-数字，boolean-布尔，array-数组，json-JSON
	ParamType  *int      `gorm:"column:param_type;default:1;comment:类型 0:系统参数 1:非系统参数" json:"paramType"`
	Remark     string    `gorm:"column:remark;type:text" json:"remark"`
	Creator    *uint64   `gorm:"column:creator;comment:创建者" json:"creator"`
	CreateDate time.Time `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	Updater    *uint64   `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdateDate time.Time `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
}

// TableName 指定表名
func (SysParams) TableName() string {
	return "sys_params"
}
