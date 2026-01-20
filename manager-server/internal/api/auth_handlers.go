package api

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"manager-server/internal/constants"
	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"

	"github.com/gin-gonic/gin"
)

// AuthHandlers 认证处理器
type AuthHandlers struct {
	userService     service.SysUserService
	tokenService    service.TokenService
	captchaService  service.CaptchaService
	paramsService   service.SysParamsService
	dictDataService service.SysDictDataService
}

// NewAuthHandlers 创建认证处理器实例
func NewAuthHandlers(
	userService service.SysUserService,
	tokenService service.TokenService,
	captchaService service.CaptchaService,
	paramsService service.SysParamsService,
	dictDataService service.SysDictDataService,
) *AuthHandlers {
	return &AuthHandlers{
		userService:     userService,
		tokenService:    tokenService,
		captchaService:  captchaService,
		paramsService:   paramsService,
		dictDataService: dictDataService,
	}
}

// generateCaptcha 生成验证码
func (h *AuthHandlers) generateCaptcha(c *gin.Context) {
	uuid := c.Query("uuid")
	if uuid == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "uuid不能为空",
		})
		return
	}

	// 生成验证码（并存储到内存）
	captcha, err := h.captchaService.GenerateCaptcha(context.Background(), uuid)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "生成验证码失败: " + err.Error(),
		})
		return
	}

	// 生成PNG验证码图片
	charWidth := 14
	imgW := len(captcha)*charWidth + 20
	imgH := 40
	rgba := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	// 背景白色
	draw.Draw(rgba, rgba.Bounds(), &image.Uniform{C: color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)

	// 绘制文字
	d := &font.Drawer{
		Dst:  rgba,
		Src:  image.NewUniform(color.Black),
		Face: basicfont.Face7x13,
	}
	// 居中偏移
	x := 10
	y := (imgH / 2) + 5
	d.Dot = fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)}
	d.DrawString(captcha)

	// 输出PNG
	c.Header("Content-Type", "image/png")
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	_ = png.Encode(c.Writer, rgba)
}

// sendSmsVerification 发送短信验证码
func (h *AuthHandlers) sendSmsVerification(c *gin.Context) {
	var dto models.SmsVerificationDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	// 验证图形验证码
	valid, err := h.captchaService.ValidateCaptcha(context.Background(), dto.CaptchaID, dto.Captcha, true)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "验证码验证失败: " + err.Error(),
		})
		return
	}
	if !valid {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "图形验证码错误",
		})
		return
	}

	// 检查是否开启手机注册
	isMobileRegister, err := h.paramsService.GetBooleanValue(context.Background(), constants.SERVER_ENABLE_MOBILE_REGISTER, false)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取系统配置失败: " + err.Error(),
		})
		return
	}
	if !isMobileRegister {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "未开启手机注册功能",
		})
		return
	}

	// 发送短信验证码
	err = h.captchaService.SendSMSValidateCode(context.Background(), dto.Phone)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "发送短信验证码失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// login 用户登录
func (h *AuthHandlers) login(c *gin.Context) {
	var loginDTO models.LoginDTO
	if err := c.ShouldBindJSON(&loginDTO); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	ctx := context.Background()

	// 验证图形验证码
	valid, err := h.captchaService.ValidateCaptcha(ctx, loginDTO.CaptchaID, loginDTO.Captcha, true)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "验证码验证失败: " + err.Error(),
		})
		return
	}
	if !valid {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "图形验证码错误，请重新获取",
		})
		return
	}

	// 获取用户信息
	userDTO, err := h.userService.GetByUsername(ctx, loginDTO.Username)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取用户信息失败: " + err.Error(),
		})
		return
	}
	if userDTO == nil {
		c.JSON(http.StatusBadRequest, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请检测用户和密码是否输入错误",
		})
		return
	}

	// 验证密码
	if !h.userService.VerifyPassword(userDTO.Password, loginDTO.Password) {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请检测用户和密码是否输入错误",
		})
		return
	}

	// 创建令牌
	tokenDTO, err := h.tokenService.CreateToken(ctx, userDTO.ID)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "创建令牌失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: tokenDTO,
	})
}

// register 用户注册
func (h *AuthHandlers) register(c *gin.Context) {
	var loginDTO models.LoginDTO
	if err := c.ShouldBindJSON(&loginDTO); err != nil {
		c.JSON(http.StatusBadRequest, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	ctx := context.Background()

	// 检查是否存在超级管理员
	hasSuperAdmin, err := h.userService.HasSuperAdmin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "检查注册权限失败: " + err.Error(),
		})
		return
	}

	allowRegister := true
	if hasSuperAdmin {
		allowRegister, err = h.paramsService.GetBooleanValue(ctx, constants.SERVER_ALLOW_USER_REGISTER, true)
		if err != nil {
			c.JSON(http.StatusInternalServerError, &models.CommonResponse{
				Code: http.StatusInternalServerError,
				Msg:  "读取注册配置失败: " + err.Error(),
			})
			return
		}
	}

	if !allowRegister {
		c.JSON(http.StatusBadRequest, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "当前不允许普通用户注册",
		})
		return
	}

	// TODO: 验证手机号格式和短信验证码

	// 验证图形验证码（如果不是手机注册模式）
	valid, err := h.captchaService.ValidateCaptcha(ctx, loginDTO.CaptchaID, loginDTO.Captcha, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "验证码验证失败: " + err.Error(),
		})
		return
	}
	if !valid {
		c.JSON(http.StatusBadRequest, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "图形验证码错误，请重新获取",
		})
		return
	}

	// 检查用户是否已存在
	existingUser, err := h.userService.GetByUsername(ctx, loginDTO.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "检查用户失败: " + err.Error(),
		})
		return
	}
	if existingUser != nil {
		c.JSON(http.StatusBadRequest, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "此手机号码已经注册过",
		})
		return
	}
	superAdminInt := 1
	if hasSuperAdmin {
		superAdminInt = 0
	}
	// 注册用户
	userDTO := &models.SysUserDTO{
		Username:   loginDTO.Username,
		Password:   loginDTO.Password,
		SuperAdmin: &superAdminInt,
	}

	err = h.userService.Register(ctx, userDTO)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "用户注册失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// getUserInfo 获取用户信息
func (h *AuthHandlers) getUserInfo(c *gin.Context) {
	// 从认证中间件的上下文中获取用户信息
	userDTO, exists := middleware.GetUserFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, &models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "未找到用户身份信息",
		})
		return
	}

	// 获取原始Token（用于返回给客户端）
	token := c.GetHeader("Authorization")
	if token == "" {
		token = c.GetHeader("token")
	}
	// 去除 Bearer 前缀
	token = strings.Replace(token, "Bearer ", "", 1)

	// 需要获取完整的用户信息，包括SuperAdmin字段
	user, err := h.userService.GetUserByID(context.Background(), userDTO.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取用户信息失败: " + err.Error(),
		})
		return
	}

	// 转换为用户详情展示对象 - 与Java版本UserDetail保持一致
	superAdmin := 0
	if user.SuperAdmin != nil {
		superAdmin = *user.SuperAdmin
	}

	userDetail := &models.UserDetailVO{
		ID:         userDTO.ID,
		Username:   userDTO.Username,
		SuperAdmin: superAdmin,
		Token:      token, // 返回当前token
		Status:     userDTO.Status,
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: userDetail,
	})
}

// changePassword 修改密码
func (h *AuthHandlers) changePassword(c *gin.Context) {
	var passwordDTO models.PasswordDTO
	if err := c.ShouldBindJSON(&passwordDTO); err != nil {
		c.JSON(http.StatusBadRequest, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	// 从认证中间件的上下文中获取当前用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, &models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "未找到用户身份信息",
		})
		return
	}

	err := h.userService.ChangePassword(context.Background(), userID, &passwordDTO)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "修改密码失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// retrievePassword 找回密码
func (h *AuthHandlers) retrievePassword(c *gin.Context) {
	var dto models.RetrievePasswordDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	ctx := context.Background()

	// TODO: 检查是否开启手机注册功能

	// TODO: 验证手机号格式

	// 检查用户是否存在
	userDTO, err := h.userService.GetByUsername(ctx, dto.Phone)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取用户信息失败: " + err.Error(),
		})
		return
	}
	if userDTO == nil {
		c.JSON(http.StatusBadRequest, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "输入的手机号码未注册",
		})
		return
	}

	// 验证短信验证码
	valid, err := h.captchaService.ValidateSMSValidateCode(ctx, dto.Phone, dto.Code, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "验证短信验证码失败: " + err.Error(),
		})
		return
	}
	if !valid {
		c.JSON(http.StatusBadRequest, &models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "输入的手机验证码错误",
		})
		return
	}

	// 直接修改密码
	err = h.userService.ChangePasswordDirectly(ctx, userDTO.ID, dto.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "修改密码失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// getPublicConfig 获取公共配置
func (h *AuthHandlers) getPublicConfig(c *gin.Context) {
	ctx := context.Background()

	// 从系统参数中获取配置信息
	enableMobileRegister, _ := h.paramsService.GetBooleanValue(ctx, constants.SERVER_ENABLE_MOBILE_REGISTER, false)

	allowUserRegister := true
	if hasSuperAdmin, err := h.userService.HasSuperAdmin(ctx); err == nil {
		if hasSuperAdmin {
			if val, err := h.paramsService.GetBooleanValue(ctx, constants.SERVER_ALLOW_USER_REGISTER, true); err == nil {
				allowUserRegister = val
			}
		}
	}
	hasAnyUser := true
	if val, err := h.userService.HasAnyUser(ctx); err == nil {
		hasAnyUser = val
	}
	beianIcpNum, _ := h.paramsService.GetValue(ctx, constants.BEIAN_ICP_NUM, "")
	beianGaNum, _ := h.paramsService.GetValue(ctx, constants.BEIAN_GA_NUM, "")
	serverName, _ := h.paramsService.GetValue(ctx, constants.SERVER_NAME, "小智ESP32服务器")

	// 如果参数值是"null"，转换为空字符串
	if beianIcpNum == "null" {
		beianIcpNum = ""
	}
	if beianGaNum == "null" {
		beianGaNum = ""
	}

	// 获取手机区域字典数据
	mobileAreaList := []models.SysDictDataItem{}
	dictDataList, err := h.dictDataService.GetDictDataByType(ctx, constants.DICT_TYPE_MOBILE_AREA)
	if err == nil && dictDataList != nil {
		for _, p := range dictDataList {
			if p != nil {
				mobileAreaList = append(mobileAreaList, *p)
			}
		}
	}

	config := &models.PublicConfigVO{
		EnableMobileRegister: enableMobileRegister,
		Version:              constants.VERSION,
		Year:                 "©" + strconv.Itoa(time.Now().Year()),
		AllowUserRegister:    allowUserRegister,
		HasAnyUser:           hasAnyUser,
		MobileAreaList:       mobileAreaList,
		BeianIcpNum:          beianIcpNum,
		BeianGaNum:           beianGaNum,
		Name:                 serverName,
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: config,
	})
}
