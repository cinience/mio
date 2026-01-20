package models

// LoginDTO 登录表单传输对象
type LoginDTO struct {
	Username      string `json:"username" binding:"required" validate:"min=1"`
	Password      string `json:"password" binding:"required" validate:"min=1"`
	Captcha       string `json:"captcha" binding:"required" validate:"min=1"`
	MobileCaptcha string `json:"mobileCaptcha"`
	CaptchaID     string `json:"captchaId" binding:"required" validate:"min=1"`
}

// SmsVerificationDTO 短信验证码请求传输对象
type SmsVerificationDTO struct {
	Phone     string `json:"phone" binding:"required" validate:"min=1"`
	Captcha   string `json:"captcha" binding:"required" validate:"min=1"`
	CaptchaID string `json:"captchaId" binding:"required" validate:"min=1"`
}

// PasswordDTO 修改密码传输对象
type PasswordDTO struct {
	Password    string `json:"password" binding:"required" validate:"min=1"`
	NewPassword string `json:"newPassword" binding:"required" validate:"min=1"`
}

// RetrievePasswordDTO 找回密码传输对象
type RetrievePasswordDTO struct {
	Phone    string `json:"phone" binding:"required" validate:"min=1"`
	Code     string `json:"code" binding:"required" validate:"min=1"`
	Password string `json:"password" binding:"required" validate:"min=1"`
}

// TokenDTO 令牌信息传输对象
type TokenDTO struct {
	Token      string `json:"token"`
	Expire     int    `json:"expire"`
	ClientHash string `json:"clientHash"`
}

// UserDetailVO 用户详情展示对象 - 与Java版本UserDetail保持一致
type UserDetailVO struct {
	ID         uint64 `json:"id"`
	Username   string `json:"username"`
	SuperAdmin int    `json:"superAdmin"` // 超级管理员标识 0:否 1:是
	Token      string `json:"token"`      // 用户token
	Status     int    `json:"status"`     // 状态 0:停用 1:正常
}

// PublicConfigVO 公共配置展示对象
type PublicConfigVO struct {
	EnableMobileRegister bool              `json:"enableMobileRegister"`
	Version              string            `json:"version"`
	Year                 string            `json:"year"`
	AllowUserRegister    bool              `json:"allowUserRegister"`
	HasAnyUser           bool              `json:"hasAnyUser"`
	MobileAreaList       []SysDictDataItem `json:"mobileAreaList"`
	BeianIcpNum          string            `json:"beianIcpNum"`
	BeianGaNum           string            `json:"beianGaNum"`
	Name                 string            `json:"name"`
}

// 兼容旧结构的占位（若其他地方仍引用可再调整）
