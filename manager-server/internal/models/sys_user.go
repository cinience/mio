package models

import "time"

// SysUser 系统用户模型，对应数据库中的sys_user表
type SysUser struct {
	ID         uint64    `gorm:"primarykey;column:id" json:"id"`
	Username   string    `gorm:"column:username;type:varchar(50);not null;uniqueIndex:uk_username" json:"username"`
	RealName   string    `gorm:"column:real_name;type:varchar(50)" json:"realName"`
	Password   string    `gorm:"column:password;type:varchar(100)" json:"-"`
	Email      string    `gorm:"column:email;type:varchar(100)" json:"email"`
	Mobile     string    `gorm:"column:mobile;type:varchar(20)" json:"mobile"`
	SuperAdmin *int      `gorm:"column:super_admin;default:0;comment:超级管理员 0:否 1:是" json:"superAdmin"`
	Status     int       `gorm:"column:status;comment:状态 0:停用 1:正常" json:"status"`
	Creator    *uint64   `gorm:"column:creator;comment:创建者" json:"creator"`
	CreateDate time.Time `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	Updater    *uint64   `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdateDate time.Time `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
}

// TableName 指定表名
func (SysUser) TableName() string {
	return "sys_user"
}

// SysUserDTO 用户传输对象，用于API响应
type SysUserDTO struct {
	ID         uint64    `json:"id"`
	Username   string    `json:"username"`
	RealName   string    `json:"realName"`
	Password   string    `json:"-"` // 密码不在JSON中显示，但在内部处理时需要
	Email      string    `json:"email"`
	Mobile     string    `json:"mobile"`
	SuperAdmin *int      `json:"superAdmin"`
	Status     int       `json:"status"`
	Creator    *uint64   `json:"creator"`
	CreateDate time.Time `json:"createDate"`
	Updater    *uint64   `json:"updater"`
	UpdateDate time.Time `json:"updateDate"`
}

// ToDTO 转换为DTO
func (u *SysUser) ToDTO() *SysUserDTO {
	return &SysUserDTO{
		ID:         u.ID,
		Username:   u.Username,
		RealName:   u.RealName,
		Password:   u.Password, // 内部使用，不会在JSON中显示
		Email:      u.Email,
		Mobile:     u.Mobile,
		SuperAdmin: u.SuperAdmin,
		Status:     u.Status,
		Creator:    u.Creator,
		CreateDate: u.CreateDate,
		Updater:    u.Updater,
		UpdateDate: u.UpdateDate,
	}
}

// LoginRequest 登录请求结构
type LoginRequest struct {
	Username string `json:"username" binding:"required" validate:"min=1,max=50"`
	Password string `json:"password" binding:"required" validate:"min=6,max=50"`
	Captcha  string `json:"captcha" binding:"required"`
	UUID     string `json:"uuid" binding:"required"`
}

// LoginResponse 登录响应结构
type LoginResponse struct {
	Token  string `json:"token"`
	Expire int64  `json:"expire"`
}
