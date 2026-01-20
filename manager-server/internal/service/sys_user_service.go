package service

import (
	"context"
	"crypto/md5"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"manager-server/internal/models"
	"manager-server/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// SysUserService 用户服务接口
type SysUserService interface {
	Login(ctx context.Context, req *models.LoginRequest) (*models.LoginResponse, error)
	Logout(ctx context.Context, token string) error
	GetUserInfo(ctx context.Context, token string) (*models.SysUserDTO, error)
	GetUserByID(ctx context.Context, id uint64) (*models.SysUser, error)
	PageUsers(ctx context.Context, page, limit int, mobile string) (*models.PageResponse, error)
	ResetPassword(ctx context.Context, id uint64) (string, error)
	DeleteByID(ctx context.Context, id uint64) error
	ChangeStatus(ctx context.Context, status int, userIds []string) error
	// 新增的登录相关方法
	GetByUsername(ctx context.Context, username string) (*models.SysUserDTO, error)
	Register(ctx context.Context, user *models.SysUserDTO) error
	ChangePassword(ctx context.Context, userID uint64, passwordDTO *models.PasswordDTO) error
	ChangePasswordDirectly(ctx context.Context, userID uint64, newPassword string) error
	HasSuperAdmin(ctx context.Context) (bool, error)
	HasAnyUser(ctx context.Context) (bool, error)
	VerifyPassword(hashedPassword, plainPassword string) bool // 验证密码
}

// sysUserService 用户服务实现
type sysUserService struct {
	userRepo      repository.SysUserRepository
	userTokenRepo repository.SysUserTokenRepository
}

// NewSysUserService 创建用户服务实例
func NewSysUserService(userRepo repository.SysUserRepository, userTokenRepo repository.SysUserTokenRepository) SysUserService {
	return &sysUserService{
		userRepo:      userRepo,
		userTokenRepo: userTokenRepo,
	}
}

// Login 用户登录
func (s *sysUserService) Login(ctx context.Context, req *models.LoginRequest) (*models.LoginResponse, error) {
	// TODO: 验证验证码

	// 查找用户
	user, err := s.userRepo.FindByUsername(ctx, req.Username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("用户不存在")
	}

	// 验证密码（优先使用BCrypt，兼容旧的MD5）
	if !s.VerifyPassword(user.Password, req.Password) {
		return nil, fmt.Errorf("密码错误")
	}

	// 检查用户状态
	if user.Status == 0 {
		return nil, fmt.Errorf("账号已停用")
	}

	// 生成Token
	token := s.generateToken(user.ID)
	expireTime := time.Now().Add(2 * time.Hour) // 2小时过期

	// 删除旧Token
	s.userTokenRepo.DeleteByUserID(ctx, user.ID)

	// 创建新Token
	userToken := &models.SysUserToken{
		UserID:     user.ID,
		Token:      token,
		ExpireDate: expireTime,
	}
	if err := s.userTokenRepo.Create(ctx, userToken); err != nil {
		return nil, err
	}

	return &models.LoginResponse{
		Token:  token,
		Expire: expireTime.Unix(),
	}, nil
}

// Logout 用户登出
func (s *sysUserService) Logout(ctx context.Context, token string) error {
	userToken, err := s.userTokenRepo.FindByToken(ctx, token)
	if err != nil {
		return err
	}
	if userToken != nil {
		return s.userTokenRepo.DeleteByUserID(ctx, userToken.UserID)
	}
	return nil
}

// GetUserInfo 获取用户信息
func (s *sysUserService) GetUserInfo(ctx context.Context, token string) (*models.SysUserDTO, error) {
	userToken, err := s.userTokenRepo.FindByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if userToken == nil || userToken.IsExpired() {
		return nil, fmt.Errorf("token无效或已过期")
	}

	user, err := s.userRepo.FindByID(ctx, userToken.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("用户不存在")
	}

	return user.ToDTO(), nil
}

// GetUserByID 根据ID获取用户信息
func (s *sysUserService) GetUserByID(ctx context.Context, id uint64) (*models.SysUser, error) {
	return s.userRepo.FindByID(ctx, id)
}

// PageUsers 分页查询用户
func (s *sysUserService) PageUsers(ctx context.Context, page, limit int, mobile string) (*models.PageResponse, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	offset := (page - 1) * limit
	users, total, err := s.userRepo.List(ctx, offset, limit, mobile)
	if err != nil {
		return nil, err
	}

	// 转换为DTO
	var userDTOs []*models.SysUserDTO
	for _, user := range users {
		userDTOs = append(userDTOs, user.ToDTO())
	}

	totalPage := int((total + int64(limit) - 1) / int64(limit))

	return &models.PageResponse{
		List:       userDTOs,
		TotalCount: total,
		PageSize:   limit,
		CurrPage:   page,
		TotalPage:  totalPage,
	}, nil
}

// ResetPassword 重置密码
func (s *sysUserService) ResetPassword(ctx context.Context, id uint64) (string, error) {
	// 生成随机密码
	newPassword := s.generateRandomPassword(8)

	// 加密密码（使用BCrypt）
	hashedPassword, err := s.HashPassword(newPassword)
	if err != nil {
		return "", err
	}
	// 更新数据库
	err = s.userRepo.ResetPassword(ctx, id, hashedPassword)
	if err != nil {
		return "", err
	}

	// 返回明文密码（实际应该通过其他安全方式通知用户）
	return newPassword, nil
}

// DeleteByID 删除用户
func (s *sysUserService) DeleteByID(ctx context.Context, id uint64) error {
	// 删除用户Token
	s.userTokenRepo.DeleteByUserID(ctx, id)

	// 删除用户
	return s.userRepo.DeleteByID(ctx, id)
}

// ChangeStatus 批量修改用户状态
func (s *sysUserService) ChangeStatus(ctx context.Context, status int, userIds []string) error {
	return s.userRepo.ChangeStatus(ctx, status, userIds)
}

// verifyPassword 验证密码（简化实现）
func (s *sysUserService) verifyPassword(plaintext, hashed string) bool {
	return s.hashPassword(plaintext) == hashed
}

// hashPassword 密码哈希（简化实现，实际应该使用bcrypt）
func (s *sysUserService) hashPassword(password string) string {
	hash := md5.Sum([]byte(password))
	return fmt.Sprintf("%x", hash)
}

// generateToken 生成Token
func (s *sysUserService) generateToken(userID uint64) string {
	timestamp := time.Now().Unix()
	random := rand.Int63()
	source := fmt.Sprintf("%d_%d_%d", userID, timestamp, random)
	hash := md5.Sum([]byte(source))
	return fmt.Sprintf("%x", hash)
}

// generateRandomPassword 生成随机密码
func (s *sysUserService) generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var password strings.Builder
	for i := 0; i < length; i++ {
		password.WriteByte(charset[rand.Intn(len(charset))])
	}
	return password.String()
}

// VerifyPassword 验证密码 - 使用BCrypt算法验证
func (s *sysUserService) VerifyPassword(hashedPassword, plainPassword string) bool {
	if hashedPassword == "" || plainPassword == "" {
		return false
	}

	// 首先尝试BCrypt验证
	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(plainPassword)); err == nil {
		return true
	}

	// 如果BCrypt验证失败，尝试使用旧的MD5方式（向后兼容）
	return s.verifyPassword(plainPassword, hashedPassword)
}

// HashPassword 加密密码 - 使用BCrypt算法
func (s *sysUserService) HashPassword(plainPassword string) (string, error) {
	if plainPassword == "" {
		return "", fmt.Errorf("密码不能为空")
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return string(hashedBytes), nil
}

// GetByUsername 根据用户名获取用户
func (s *sysUserService) GetByUsername(ctx context.Context, username string) (*models.SysUserDTO, error) {
	user, err := s.userRepo.FindByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	return &models.SysUserDTO{
		ID:         user.ID,
		Username:   user.Username,
		RealName:   user.RealName,
		Password:   user.Password,
		Email:      user.Email,
		Mobile:     user.Mobile,
		Status:     user.Status,
		CreateDate: user.CreateDate,
		UpdateDate: user.UpdateDate,
	}, nil
}

// Register 用户注册
func (s *sysUserService) Register(ctx context.Context, user *models.SysUserDTO) error {
	// 检查用户名是否已存在
	existingUser, err := s.userRepo.FindByUsername(ctx, user.Username)
	if err != nil {
		return err
	}
	if existingUser != nil {
		return fmt.Errorf("用户名已存在")
	}

	// 密码加密（使用BCrypt）
	hashedPassword, err := s.HashPassword(user.Password)
	if err != nil {
		return err
	}

	// 创建新用户
	newUser := &models.SysUser{
		Username:   user.Username,
		RealName:   user.RealName,
		Password:   hashedPassword,
		Email:      user.Email,
		Mobile:     user.Mobile,
		Status:     1, // 默认启用状态
		CreateDate: time.Now(),
		UpdateDate: time.Now(),
		SuperAdmin: user.SuperAdmin,
	}

	return s.userRepo.Create(ctx, newUser)
}

// ChangePassword 修改用户密码
func (s *sysUserService) ChangePassword(ctx context.Context, userID uint64, passwordDTO *models.PasswordDTO) error {
	// 获取用户信息
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("用户不存在")
	}

	// 验证原密码（优先BCrypt，兼容MD5）
	if !s.VerifyPassword(user.Password, passwordDTO.Password) {
		return fmt.Errorf("旧密码输入错误")
	}

	// 更新密码
	newHashedPassword, err := s.HashPassword(passwordDTO.NewPassword)
	if err != nil {
		return err
	}
	// 仅更新密码，避免不必要的字段更新
	return s.userRepo.ResetPassword(ctx, userID, newHashedPassword)
}

// ChangePasswordDirectly 直接修改用户密码（不验证原密码）
func (s *sysUserService) ChangePasswordDirectly(ctx context.Context, userID uint64, newPassword string) error {
	// 获取用户信息
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("用户不存在")
	}

	// 更新密码（使用BCrypt）
	hashedPassword, err := s.HashPassword(newPassword)
	if err != nil {
		return err
	}
	// 仅更新密码，避免不必要的字段更新
	return s.userRepo.ResetPassword(ctx, userID, hashedPassword)
}

// HasSuperAdmin 检查是否存在超级管理员
func (s *sysUserService) HasSuperAdmin(ctx context.Context) (bool, error) {
	return s.userRepo.HasSuperAdmin(ctx)
}

// HasAnyUser 检查是否存在任意用户
func (s *sysUserService) HasAnyUser(ctx context.Context) (bool, error) {
	return s.userRepo.HasAnyUser(ctx)
}
