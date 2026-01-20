package service

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CaptchaService 验证码服务接口
type CaptchaService interface {
	GenerateCaptcha(ctx context.Context, uuid string) (string, error)
	ValidateCaptcha(ctx context.Context, uuid, captcha string, removeAfterValidate bool) (bool, error)
	SendSMSValidateCode(ctx context.Context, phone string) error
	ValidateSMSValidateCode(ctx context.Context, phone, code string, removeAfterValidate bool) (bool, error)
}

type captchaService struct {
	captchas map[string]captchaData // 内存存储验证码
	smsCodes map[string]smsCodeData // 内存存储短信验证码
	mu       sync.RWMutex           // 保护map的读写锁
}

type captchaData struct {
	Code      string
	ExpiresAt time.Time
}

type smsCodeData struct {
	Code      string
	ExpiresAt time.Time
}

// NewCaptchaService 创建验证码服务实例
func NewCaptchaService() CaptchaService {
	return &captchaService{
		captchas: make(map[string]captchaData),
		smsCodes: make(map[string]smsCodeData),
	}
}

// GenerateCaptcha 生成图形验证码
func (s *captchaService) GenerateCaptcha(ctx context.Context, uuid string) (string, error) {
	// 生成4位随机数字验证码
	captcha := generateRandomCode(4)

	s.mu.Lock()
	defer s.mu.Unlock()

	// 清理过期的验证码
	s.cleanExpiredCaptchas()

	// 存储到内存，5分钟过期
	s.captchas[uuid] = captchaData{
		Code:      captcha,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// 添加调试日志
	fmt.Printf("[DEBUG] 验证码生成成功: uuid=%s, captcha=%s\n", uuid, captcha)

	return captcha, nil
}

// ValidateCaptcha 验证图形验证码
func (s *captchaService) ValidateCaptcha(ctx context.Context, uuid, captcha string, removeAfterValidate bool) (bool, error) {
	s.mu.RLock()
	data, exists := s.captchas[uuid]
	s.mu.RUnlock()

	if !exists {
		fmt.Printf("[DEBUG] 验证码不存在: uuid=%s\n", uuid)
		return false, nil // 验证码不存在
	}

	// 检查是否过期
	if time.Now().After(data.ExpiresAt) {
		// 删除过期的验证码
		s.mu.Lock()
		delete(s.captchas, uuid)
		s.mu.Unlock()

		fmt.Printf("[DEBUG] 验证码已过期: uuid=%s\n", uuid)
		return false, nil
	}

	// 添加调试日志
	fmt.Printf("[DEBUG] 验证码比较: uuid=%s, input=%s, stored=%s\n", uuid, captcha, data.Code)

	// 验证码比较（不区分大小写）
	valid := strings.EqualFold(captcha, data.Code)

	// 如果需要验证后删除
	if removeAfterValidate && valid {
		s.mu.Lock()
		delete(s.captchas, uuid)
		s.mu.Unlock()
	}

	return valid, nil
}

// SendSMSValidateCode 发送短信验证码
func (s *captchaService) SendSMSValidateCode(ctx context.Context, phone string) error {
	// 生成6位随机数字验证码
	code := generateRandomCode(6)

	s.mu.Lock()
	defer s.mu.Unlock()

	// 清理过期的短信验证码
	s.cleanExpiredSMSCodes()

	// 存储到内存，5分钟过期
	s.smsCodes[phone] = smsCodeData{
		Code:      code,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// TODO: 实际项目中这里应该调用短信服务商API发送短信
	// 这里只是模拟发送
	fmt.Printf("[模拟短信] 向 %s 发送验证码: %s\n", phone, code)

	return nil
}

// ValidateSMSValidateCode 验证短信验证码
func (s *captchaService) ValidateSMSValidateCode(ctx context.Context, phone, code string, removeAfterValidate bool) (bool, error) {
	s.mu.RLock()
	data, exists := s.smsCodes[phone]
	s.mu.RUnlock()

	if !exists {
		return false, nil // 验证码不存在
	}

	// 检查是否过期
	if time.Now().After(data.ExpiresAt) {
		// 删除过期的验证码
		s.mu.Lock()
		delete(s.smsCodes, phone)
		s.mu.Unlock()
		return false, nil
	}

	// 验证码比较
	valid := code == data.Code

	// 如果需要验证后删除
	if removeAfterValidate {
		s.mu.Lock()
		delete(s.smsCodes, phone)
		s.mu.Unlock()
	}

	return valid, nil
}

// generateRandomCode 生成指定位数的随机数字字符串
func generateRandomCode(length int) string {
	rand.Seed(time.Now().UnixNano())
	code := ""
	for i := 0; i < length; i++ {
		code += strconv.Itoa(rand.Intn(10))
	}
	return code
}

// cleanExpiredCaptchas 清理过期的图形验证码 (需要在调用前获取写锁)
func (s *captchaService) cleanExpiredCaptchas() {
	now := time.Now()
	for uuid, data := range s.captchas {
		if now.After(data.ExpiresAt) {
			delete(s.captchas, uuid)
		}
	}
}

// cleanExpiredSMSCodes 清理过期的短信验证码 (需要在调用前获取写锁)
func (s *captchaService) cleanExpiredSMSCodes() {
	now := time.Now()
	for phone, data := range s.smsCodes {
		if now.After(data.ExpiresAt) {
			delete(s.smsCodes, phone)
		}
	}
}
