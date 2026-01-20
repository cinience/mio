package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BaseModel 基础模型，对应Java项目中的通用字段
type BaseModel struct {
	ID         uint64         `gorm:"primarykey;column:id" json:"id"`
	CreateTime time.Time      `gorm:"column:create_time;autoCreateTime" json:"createTime"`
	UpdateTime time.Time      `gorm:"column:update_time;autoUpdateTime" json:"updateTime"`
	DeletedAt  gorm.DeletedAt `gorm:"index;column:deleted_at" json:"-"`
}

// BaseModelUUID mirrors BaseModel but stores identifiers as UUID strings.
type BaseModelUUID struct {
	ID         string         `gorm:"type:uuid;primarykey;column:id" json:"id"`
	CreateTime time.Time      `gorm:"column:create_time;autoCreateTime" json:"createTime"`
	UpdateTime time.Time      `gorm:"column:update_time;autoUpdateTime" json:"updateTime"`
	DeletedAt  gorm.DeletedAt `gorm:"index;column:deleted_at" json:"-"`
}

// BeforeCreate ensures UUID fields are populated when database defaults are unavailable.
func (b *BaseModelUUID) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	return nil
}

// CommonResponse 通用响应结构，与Java项目的响应格式保持一致
type CommonResponse struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

// PageResponse 分页响应结构
type PageResponse struct {
	List       interface{} `json:"list"`
	TotalCount int64       `json:"totalCount"`
	PageSize   int         `json:"pageSize"`
	CurrPage   int         `json:"currPage"`
	TotalPage  int         `json:"totalPage"`
}

// PageResult 分页结果结构（用于Swagger文档）
type PageResult struct {
	List       []interface{} `json:"list"`
	TotalCount int64         `json:"totalCount"`
	PageSize   int           `json:"pageSize"`
	CurrPage   int           `json:"currPage"`
	TotalPage  int           `json:"totalPage"`
}

// PageRequest 分页请求结构
type PageRequest struct {
	Page  int `form:"page" json:"page" validate:"min=1"`
	Limit int `form:"limit" json:"limit" validate:"min=1,max=100"`
}

// GetOffset 获取偏移量
func (p *PageRequest) GetOffset() int {
	if p.Page <= 0 {
		p.Page = 1
	}
	return (p.Page - 1) * p.Limit
}

// GetLimit 获取限制数量
func (p *PageRequest) GetLimit() int {
	if p.Limit <= 0 {
		p.Limit = 10
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	return p.Limit
}
