package repository

import (
	"context"
	"math/rand"
	"time"

	"gorm.io/gorm"

	"manager-server/internal/models"
)

// SysUserRepository 用户仓储接口
type SysUserRepository interface {
	FindByID(ctx context.Context, id uint64) (*models.SysUser, error)
	FindByUsername(ctx context.Context, username string) (*models.SysUser, error)
	Create(ctx context.Context, user *models.SysUser) error
	Update(ctx context.Context, user *models.SysUser) error
	DeleteByID(ctx context.Context, id uint64) error
	List(ctx context.Context, offset, limit int, mobile string) ([]*models.SysUser, int64, error)
	ChangeStatus(ctx context.Context, status int, userIds []string) error
	ResetPassword(ctx context.Context, id uint64, newPassword string) error
	HasSuperAdmin(ctx context.Context) (bool, error)
	HasAnyUser(ctx context.Context) (bool, error)
}

// sysUserRepository 用户仓储实现
type sysUserRepository struct {
	db *gorm.DB
}

// NewSysUserRepository 创建用户仓储实例
func NewSysUserRepository(db *gorm.DB) SysUserRepository {
	return &sysUserRepository{
		db: db,
	}
}

// FindByID 根据ID查找用户
func (r *sysUserRepository) FindByID(ctx context.Context, id uint64) (*models.SysUser, error) {
	var user models.SysUser
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// FindByUsername 根据用户名查找用户
func (r *sysUserRepository) FindByUsername(ctx context.Context, username string) (*models.SysUser, error) {
	var user models.SysUser
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// Create 创建用户
func (r *sysUserRepository) Create(ctx context.Context, user *models.SysUser) error {
	// 如果ID为空，自动生成ID（模拟Java版本的ASSIGN_ID策略）
	if user.ID == 0 {
		user.ID = generateID()
	}
	user.CreateDate = time.Now()
	user.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(user).Error
}

// generateID 生成ID，模拟MyBatis-Plus的ASSIGN_ID策略
func generateID() uint64 {
	// 使用时间戳（毫秒）+ 随机数的方式生成ID，确保唯一性
	timestamp := time.Now().UnixMilli()
	random := rand.Intn(9999) // 4位随机数

	// 组合成一个唯一ID: 时间戳 * 10000 + 随机数
	id := uint64(timestamp)*10000 + uint64(random)
	return id
}

// Update 更新用户
func (r *sysUserRepository) Update(ctx context.Context, user *models.SysUser) error {
	// 只更新数据库中实际存在的字段，避免 real_name、email、mobile 等字段导致 1054 错误
	updates := map[string]interface{}{
		"username":    user.Username,
		"super_admin": user.SuperAdmin,
		"status":      user.Status,
		"updater":     user.Updater,
		"update_date": time.Now(),
	}
	return r.db.WithContext(ctx).Model(&models.SysUser{}).Where("id = ?", user.ID).Updates(updates).Error
}

// DeleteByID 根据ID删除用户
func (r *sysUserRepository) DeleteByID(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&models.SysUser{}, id).Error
}

// List 分页查询用户列表
func (r *sysUserRepository) List(ctx context.Context, offset, limit int, mobile string) ([]*models.SysUser, int64, error) {
	var users []*models.SysUser
	var total int64

	query := r.db.WithContext(ctx).Model(&models.SysUser{})

	// 如果有手机号过滤条件（这里先预留，原表结构中没有mobile字段）
	if mobile != "" {
		// query = query.Where("mobile LIKE ?", "%"+mobile+"%")
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	err := query.Offset(offset).Limit(limit).Order("id DESC").Find(&users).Error
	return users, total, err
}

// ChangeStatus 批量修改用户状态
func (r *sysUserRepository) ChangeStatus(ctx context.Context, status int, userIds []string) error {
	return r.db.WithContext(ctx).Model(&models.SysUser{}).
		Where("id IN ?", userIds).
		Updates(map[string]interface{}{
			"status":      status,
			"update_date": time.Now(),
		}).Error
}

// ResetPassword 重置用户密码
func (r *sysUserRepository) ResetPassword(ctx context.Context, id uint64, newPassword string) error {
	return r.db.WithContext(ctx).Model(&models.SysUser{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"password":    newPassword,
			"update_date": time.Now(),
		}).Error
}

// HasSuperAdmin 检查是否存在超级管理员
func (r *sysUserRepository) HasSuperAdmin(ctx context.Context) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.SysUser{}).
		Where("super_admin = ?", 1).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasAnyUser 检查是否存在任意用户
func (r *sysUserRepository) HasAnyUser(ctx context.Context) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.SysUser{}).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
