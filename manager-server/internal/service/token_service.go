package service

import (
	"context"
	"crypto/md5"
	"fmt"
	"math/rand"
	"time"

	"manager-server/internal/models"
	"manager-server/internal/repository"
)

// TokenService 令牌服务接口
type TokenService interface {
	CreateToken(ctx context.Context, userID uint64) (*models.TokenDTO, error)
	GetUserByToken(ctx context.Context, token string) (*models.SysUserDTO, error)
	InvalidateToken(ctx context.Context, token string) error
}

type tokenService struct {
	userRepo      repository.SysUserRepository
	userTokenRepo repository.SysUserTokenRepository
}

// NewTokenService 创建令牌服务实例
func NewTokenService(userRepo repository.SysUserRepository, userTokenRepo repository.SysUserTokenRepository) TokenService {
	return &tokenService{
		userRepo:      userRepo,
		userTokenRepo: userTokenRepo,
	}
}

// CreateToken 创建令牌
func (s *tokenService) CreateToken(ctx context.Context, userID uint64) (*models.TokenDTO, error) {
	// 生成令牌
	token := s.generateToken(userID)

	// 设置过期时间（7天）
	expireHours := 24 * 7
	expireTime := time.Now().Add(time.Duration(expireHours) * time.Hour)

	// 检查是否已有令牌，如果有则更新，否则创建
	existingToken, err := s.userTokenRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if existingToken != nil {
		// 更新现有令牌
		existingToken.Token = token
		existingToken.ExpireDate = expireTime
		existingToken.UpdateDate = time.Now()
		err = s.userTokenRepo.Update(ctx, existingToken)
	} else {
		// 创建新令牌
		userToken := &models.SysUserToken{
			UserID:     userID,
			Token:      token,
			ExpireDate: expireTime,
			CreateDate: time.Now(),
			UpdateDate: time.Now(),
		}
		err = s.userTokenRepo.Create(ctx, userToken)
	}

	if err != nil {
		return nil, err
	}

	return &models.TokenDTO{
		Token:      token,
		Expire:     expireHours * 3600, // 转换为秒
		ClientHash: "",                 // 暂不实现客户端指纹
	}, nil
}

// GetUserByToken 根据令牌获取用户信息
func (s *tokenService) GetUserByToken(ctx context.Context, token string) (*models.SysUserDTO, error) {
	// 根据令牌查找用户令牌记录
	userToken, err := s.userTokenRepo.FindByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if userToken == nil {
		return nil, fmt.Errorf("无效的令牌")
	}

	// 检查令牌是否过期
	if time.Now().After(userToken.ExpireDate) {
		return nil, fmt.Errorf("令牌已过期")
	}

	// 获取用户信息
	user, err := s.userRepo.FindByID(ctx, userToken.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("用户不存在")
	}

	return &models.SysUserDTO{
		ID:         user.ID,
		Username:   user.Username,
		RealName:   user.RealName,
		Email:      user.Email,
		Mobile:     user.Mobile,
		SuperAdmin: user.SuperAdmin, // 添加SuperAdmin字段
		Status:     user.Status,
		CreateDate: user.CreateDate,
		UpdateDate: user.UpdateDate,
	}, nil
}

// InvalidateToken 使令牌失效
func (s *tokenService) InvalidateToken(ctx context.Context, token string) error {
	// 根据令牌查找用户令牌记录
	userToken, err := s.userTokenRepo.FindByToken(ctx, token)
	if err != nil {
		return err
	}
	if userToken == nil {
		return nil // 令牌不存在，视为已失效
	}

	// 删除令牌记录
	return s.userTokenRepo.Delete(ctx, userToken.ID)
}

// generateToken 生成令牌
func (s *tokenService) generateToken(userID uint64) string {
	timestamp := time.Now().UnixNano()
	random := rand.Int63()
	source := fmt.Sprintf("token_%d_%d_%d", userID, timestamp, random)
	hash := md5.Sum([]byte(source))
	return fmt.Sprintf("%x", hash)
}
