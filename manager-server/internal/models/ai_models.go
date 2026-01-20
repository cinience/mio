package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
)

// JSONMap 用于处理JSON字段
type JSONMap map[string]interface{}

// Value 实现driver.Valuer接口
func (j JSONMap) Value() (driver.Value, error) {
	return json.Marshal(j)
}

// Scan 实现sql.Scanner接口
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = make(JSONMap)
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(bytes, j)
}

// String 将JSONMap转换为JSON字符串
func (j JSONMap) String() string {
	if j == nil {
		return "{}"
	}
	bytes, err := json.Marshal(j)
	if err != nil {
		return "{}"
	}
	return string(bytes)
}

// AIModelProvider AI模型供应器表
type AIModelProvider struct {
	ID           string         `gorm:"primarykey;column:id;type:varchar(32)" json:"id"`
	ModelType    string         `gorm:"column:model_type;type:varchar(20);index:idx_ai_model_provider_model_type" json:"modelType"`
	ProviderCode string         `gorm:"column:provider_code;type:varchar(50)" json:"providerCode"`
	Name         string         `gorm:"column:name;type:varchar(50)" json:"name"`
	Fields       datatypes.JSON `gorm:"column:fields;type:json" json:"fields"`
	Sort         *int           `gorm:"column:sort;default:0" json:"sort"`
	Creator      *uint64        `gorm:"column:creator;comment:创建者" json:"creator"`
	CreateDate   time.Time      `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	Updater      *uint64        `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdateDate   time.Time      `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
}

// TableName 指定表名
func (AIModelProvider) TableName() string {
	return "ai_model_provider"
}

// AIModelConfig AI模型配置表
type AIModelConfig struct {
	ID         string    `gorm:"primarykey;column:id;type:varchar(32)" json:"id"`
	ModelType  string    `gorm:"column:model_type;type:varchar(20);index:idx_ai_model_config_model_type;collate:case_insensitive" json:"modelType"`
	ModelCode  string    `gorm:"column:model_code;type:varchar(50)" json:"modelCode"`
	ModelName  string    `gorm:"column:model_name;type:varchar(50)" json:"modelName"`
	IsDefault  *int      `gorm:"column:is_default;default:0;comment:是否默认配置 0否 1是" json:"isDefault"`
	IsEnabled  *int      `gorm:"column:is_enabled;default:0;comment:是否启用" json:"isEnabled"`
	ConfigJSON JSONMap   `gorm:"column:config_json;type:json" json:"configJson"`
	DocLink    string    `gorm:"column:doc_link;type:varchar(200)" json:"docLink"`
	Remark     string    `gorm:"column:remark;type:text" json:"remark"`
	Sort       *int      `gorm:"column:sort;default:0" json:"sort"`
	Creator    *uint64   `gorm:"column:creator;comment:创建者" json:"creator"`
	CreateDate time.Time `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	Updater    *uint64   `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdateDate time.Time `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
}

// TableName 指定表名
func (AIModelConfig) TableName() string {
	return "ai_model_config"
}

// AITTSVoice TTS音色表
type AITTSVoice struct {
	ID             string    `gorm:"primarykey;column:id;type:varchar(32)" json:"id"`
	TTSModelID     string    `gorm:"column:tts_model_id;type:varchar(32)" json:"ttsModelId"`
	Name           string    `gorm:"column:name;type:varchar(50)" json:"name"`
	TTSVoice       string    `gorm:"column:tts_voice;type:varchar(50)" json:"ttsVoice"`
	Languages      string    `gorm:"column:languages;type:varchar(50)" json:"languages"`
	VoiceDemo      string    `gorm:"column:voice_demo;type:varchar(500)" json:"voiceDemo"`
	ReferenceAudio string    `gorm:"column:reference_audio;type:varchar(500)" json:"referenceAudio"`
	ReferenceText  string    `gorm:"column:reference_text;type:varchar(1000)" json:"referenceText"`
	Remark         string    `gorm:"column:remark;type:text" json:"remark"`
	Sort           *int64    `gorm:"column:sort;type:bigint;default:0" json:"sort"`
	Creator        *uint64   `gorm:"column:creator;comment:创建者" json:"creator"`
	CreateDate     time.Time `gorm:"column:create_date;comment:创建时间" json:"createDate"`
	Updater        *uint64   `gorm:"column:updater;comment:更新者" json:"updater"`
	UpdateDate     time.Time `gorm:"column:update_date;comment:更新时间" json:"updateDate"`
}

// TableName 指定表名
func (AITTSVoice) TableName() string {
	return "ai_tts_voice"
}

// DTO对象

// ModelProviderDTO 模型供应器传输对象
type ModelProviderDTO struct {
	ID           string    `json:"id"`
	ModelType    string    `json:"modelType" binding:"required" validate:"min=1,max=20"`
	ProviderCode string    `json:"providerCode" binding:"required" validate:"min=1,max=50"`
	Name         string    `json:"name" binding:"required" validate:"min=1,max=50"`
	Fields       string    `json:"fields" binding:"required"`
	Sort         *int      `json:"sort" binding:"required"`
	Creator      *uint64   `json:"creator"`
	CreateDate   time.Time `json:"createDate"`
	Updater      *uint64   `json:"updater"`
	UpdateDate   time.Time `json:"updateDate"`
}

// ModelConfigDTO 模型配置传输对象
type ModelConfigDTO struct {
	ID         string  `json:"id"`
	ModelType  string  `json:"modelType"`
	ModelCode  string  `json:"modelCode"`
	ModelName  string  `json:"modelName"`
	IsDefault  *int    `json:"isDefault"`
	IsEnabled  *int    `json:"isEnabled"`
	ConfigJSON JSONMap `json:"configJson"`
	DocLink    string  `json:"docLink"`
	Remark     string  `json:"remark"`
	Sort       *int    `json:"sort"`
}

// ModelConfigBodyDTO 模型配置请求体
type ModelConfigBodyDTO struct {
	ModelName  string  `json:"modelName" binding:"required" validate:"min=1,max=50"`
	ConfigJSON JSONMap `json:"configJson" binding:"required"`
	DocLink    string  `json:"docLink" validate:"max=200"`
	Remark     string  `json:"remark" validate:"max=255"`
	Sort       *int    `json:"sort"`
}

// ModelBasicInfoDTO 模型基本信息（与Java一致）
type ModelBasicInfoDTO struct {
	ID        string `json:"id"`
	ModelName string `json:"modelName"`
}

// LlmModelBasicInfoDTO LLM模型基本信息（与Java版本保持一致）
type LlmModelBasicInfoDTO struct {
	ID        string `json:"id"`        // 继承自ModelBasicInfoDTO
	ModelName string `json:"modelName"` // 继承自ModelBasicInfoDTO
	Type      string `json:"type"`      // LlmModelBasicInfoDTO特有字段
}

// VoiceDTO 音色传输对象
type VoiceDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TTSVoice  string `json:"ttsVoice"`
	Languages string `json:"languages"`
	VoiceDemo string `json:"voiceDemo"`
}

// 音色管理相关DTO

// TimbreDetailsVO 音色详情展示对象
type TimbreDetailsVO struct {
	ID             string `json:"id"`
	Languages      string `json:"languages"`
	Name           string `json:"name"`
	Remark         string `json:"remark"`
	ReferenceAudio string `json:"referenceAudio"`
	ReferenceText  string `json:"referenceText"`
	Sort           int64  `json:"sort"`
	TTSModelID     string `json:"ttsModelId"`
	TTSVoice       string `json:"ttsVoice"`
	VoiceDemo      string `json:"voiceDemo"`
}

// TimbreDataDTO 音色表数据传输对象
type TimbreDataDTO struct {
	Languages      string `json:"languages" binding:"required" validate:"min=1"`
	Name           string `json:"name" binding:"required" validate:"min=1"`
	Remark         string `json:"remark"`
	ReferenceAudio string `json:"referenceAudio"`
	ReferenceText  string `json:"referenceText"`
	Sort           int64  `json:"sort" binding:"min=0"`
	TTSModelID     string `json:"ttsModelId" binding:"required" validate:"min=1"`
	TTSVoice       string `json:"ttsVoice" binding:"required" validate:"min=1"`
	VoiceDemo      string `json:"voiceDemo"`
}

// TimbrePageDTO 音色分页参数传输对象
type TimbrePageDTO struct {
	TTSModelID string `json:"ttsModelId" binding:"required" validate:"min=1"`
	Name       string `json:"name"`
	Page       string `json:"page"`
	Limit      string `json:"limit"`
}
