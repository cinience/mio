package models

import "time"

// SysUserToken 系统用户Token模型，对应数据库中的sys_user_token表
type SysUserToken struct {
	ID         uint64    `gorm:"primarykey;column:id" json:"id"`
	UserID     uint64    `gorm:"column:user_id;not null;uniqueIndex:user_id" json:"userId"`
	Token      string    `gorm:"column:token;type:varchar(100);not null;uniqueIndex:token" json:"token"`
	ExpireDate time.Time `gorm:"column:expire_date;comment:过期时间" json:"expireDate"`
	CreateDate time.Time `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	UpdateDate time.Time `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
}

// TableName 指定表名
func (SysUserToken) TableName() string {
	return "sys_user_token"
}

// IsExpired 检查token是否过期
func (t *SysUserToken) IsExpired() bool {
	return time.Now().After(t.ExpireDate)
}
