package videogen

import (
	"context"
	"fmt"

	"backend-server/pkg/confighelper"
)

// GenerateRequest 封装生视频生成请求参数
// 字段与 DashScope 视频生成能力保持一致
// 参考文档: https://dashscope.aliyuncs.com
//
// Prompt 必填，其余参数由配置或调用方覆盖
// AudioURL 可选，用于音频驱动生成
// PromptExtend 为空时使用配置默认值
// Duration 单位秒
// Size 格式示例: 1280*720
// ShotType 示例: multi/single
type GenerateRequest struct {
	Prompt       string
	Model        string
	Size         string
	Duration     int
	ShotType     string
	PromptExtend *bool
	AudioURL     string
	User         string
}

// VideoData 表示生成结果中的单条视频数据
// URL 为可访问的视频地址
type VideoData struct {
	URL string `json:"url,omitempty"`
}

// GenerateResponse 封装生视频生成响应数据
// TaskID 和 Status 用于任务追踪
type GenerateResponse struct {
	Created int64       `json:"created,omitempty"`
	TaskID  string      `json:"task_id,omitempty"`
	Status  string      `json:"status,omitempty"`
	Data    []VideoData `json:"data,omitempty"`
}

// Provider 生视频模型提供者接口
// Generate 需要负责提交任务并返回最终结果
type Provider interface {
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
}

// GetProvider 根据提供者配置创建生视频提供者实例
func GetProvider(providerName string, config map[string]interface{}) (Provider, error) {
	if providerName == "" {
		return nil, fmt.Errorf("provider不能为空")
	}

	cfgHelper := confighelper.New(config)
	providerType := cfgHelper.GetString("type", providerName)

	switch providerType {
	case "dashscope":
		return newDashscopeProvider(config)
	default:
		return nil, fmt.Errorf("不支持的生视频提供者类型: %s", providerType)
	}
}
