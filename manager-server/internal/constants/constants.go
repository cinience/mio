package constants

// 系统参数常量定义
const (
	// 服务器基础参数
	SERVER_NAME      = "server.name"
	BEIAN_ICP_NUM    = "server.beian_icp_num"
	BEIAN_GA_NUM     = "server.beian_ga_num"
	SERVER_SECRET    = "server.secret"
	SERVER_WEBSOCKET = "server.websocket"
	SERVER_OTA       = "server.ota"

	// 用户注册相关参数
	SERVER_ALLOW_USER_REGISTER    = "server.allow_user_register"
	SERVER_ENABLE_MOBILE_REGISTER = "server.enable_mobile_register"

	// 短信相关参数
	SERVER_SMS_MAX_SEND_COUNT         = "server.sms_max_send_count"
	ALIYUN_SMS_ACCESS_KEY_ID          = "aliyun.sms.access_key_id"
	ALIYUN_SMS_ACCESS_KEY_SECRET      = "aliyun.sms.access_key_secret"
	ALIYUN_SMS_SIGN_NAME              = "aliyun.sms.sign_name"
	ALIYUN_SMS_SMS_CODE_TEMPLATE_CODE = "aliyun.sms.code_template_code"

	// MCP相关参数
	SERVER_MCP_ENDPOINT = "server.mcp_endpoint"
	SERVER_VOICE_PRINT  = "server.voice_print"

	// 系统版本
	VERSION = "1.0.0"

	// 字典类型
	DICT_TYPE_MOBILE_AREA = "MOBILE_AREA"

	// 模型类型常量
	INTENT_NO_INTENT = "Intent_nointent"
	MEMORY_NO_MEM    = "Memory_nomem"
)

// 系统参数默认值
var DefaultParams = map[string]string{
	SERVER_NAME:                   "小智ESP32服务器",
	BEIAN_ICP_NUM:                 "null",
	BEIAN_GA_NUM:                  "null",
	SERVER_ALLOW_USER_REGISTER:    "true",
	SERVER_ENABLE_MOBILE_REGISTER: "false",
	SERVER_SMS_MAX_SEND_COUNT:     "10",
}
