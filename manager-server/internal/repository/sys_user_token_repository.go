package repository

import (
	"context"
	"math/rand"
	"time"

	"gorm.io/gorm"

	"manager-server/internal/models"
)

// SysUserTokenRepository 用户Token仓储接口
type SysUserTokenRepository interface {
	FindByToken(ctx context.Context, token string) (*models.SysUserToken, error)
	FindByUserID(ctx context.Context, userID uint64) (*models.SysUserToken, error)
	Create(ctx context.Context, userToken *models.SysUserToken) error
	Update(ctx context.Context, userToken *models.SysUserToken) error
	Delete(ctx context.Context, id uint64) error
	DeleteByUserID(ctx context.Context, userID uint64) error
	DeleteExpiredTokens(ctx context.Context) error
}

// sysUserTokenRepository 用户Token仓储实现
type sysUserTokenRepository struct {
	db *gorm.DB
}

// NewSysUserTokenRepository 创建用户Token仓储实例
func NewSysUserTokenRepository(db *gorm.DB) SysUserTokenRepository {
	return &sysUserTokenRepository{
		db: db,
	}
}

// FindByToken 根据Token查找用户Token记录
func (r *sysUserTokenRepository) FindByToken(ctx context.Context, token string) (*models.SysUserToken, error) {
	var userToken models.SysUserToken
	err := r.db.WithContext(ctx).Where("token = ?", token).First(&userToken).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &userToken, nil
}

// FindByUserID 根据用户ID查找用户Token记录
func (r *sysUserTokenRepository) FindByUserID(ctx context.Context, userID uint64) (*models.SysUserToken, error) {
	var userToken models.SysUserToken
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&userToken).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &userToken, nil
}

// Create 创建用户Token记录
func (r *sysUserTokenRepository) Create(ctx context.Context, userToken *models.SysUserToken) error {
	// 如果ID为空，自动生成ID（模拟Java版本的ASSIGN_ID策略）
	if userToken.ID == 0 {
		userToken.ID = generateTokenID()
	}
	userToken.CreateDate = time.Now()
	userToken.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Create(userToken).Error
}

// generateTokenID 生成TokenID，模拟MyBatis-Plus的ASSIGN_ID策略
func generateTokenID() uint64 {
	// 使用时间戳（毫秒）+ 随机数的方式生成ID，确保唯一性
	timestamp := time.Now().UnixMilli()
	random := rand.Intn(9999) // 4位随机数

	// 组合成一个唯一ID: 时间戳 * 10000 + 随机数
	id := uint64(timestamp)*10000 + uint64(random)
	return id
}

// Update 更新用户Token记录
func (r *sysUserTokenRepository) Update(ctx context.Context, userToken *models.SysUserToken) error {
	userToken.UpdateDate = time.Now()
	return r.db.WithContext(ctx).Save(userToken).Error
}

// Delete 根据ID删除Token记录
func (r *sysUserTokenRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.SysUserToken{}).Error
}

// DeleteByUserID 根据用户ID删除Token记录
func (r *sysUserTokenRepository) DeleteByUserID(ctx context.Context, userID uint64) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&models.SysUserToken{}).Error
}

// DeleteExpiredTokens 删除过期的Token记录
func (r *sysUserTokenRepository) DeleteExpiredTokens(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("expire_date < ?", time.Now()).Delete(&models.SysUserToken{}).Error
}
