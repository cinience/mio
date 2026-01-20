package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	log "backend-server/internal/infrastructure/logger"
	"backend-server/pkg/confighelper"
)

type dashscopeProvider struct {
	client                *http.Client
	apiKey                string
	baseURL               string
	requestPath           string
	defaultModel          string
	defaultSize           string
	defaultNegativePrompt string
	defaultPromptExtend   bool
	defaultWatermark      bool
	hasNegativePrompt     bool
	hasPromptExtend       bool
	hasWatermark          bool
}

type dashscopeMessage struct {
	Role    string             `json:"role"`
	Content []dashscopeContent `json:"content"`
}

type dashscopeContent struct {
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
	Mask  string `json:"mask,omitempty"`
}

type dashscopeRequest struct {
	Model      string              `json:"model"`
	Input      dashscopeInput      `json:"input"`
	Parameters dashscopeParameters `json:"parameters,omitempty"`
}

type dashscopeInput struct {
	Messages []dashscopeMessage `json:"messages"`
}

type dashscopeParameters struct {
	NegativePrompt string `json:"negative_prompt,omitempty"`
	PromptExtend   *bool  `json:"prompt_extend,omitempty"`
	Watermark      *bool  `json:"watermark,omitempty"`
	Size           string `json:"size,omitempty"`
}

type dashscopeResponse struct {
	Output struct {
		Results []struct {
			URL   string `json:"url"`
			Image string `json:"image"`
		} `json:"results"`
		Choices []struct {
			Message struct {
				Content []struct {
					Image string `json:"image"`
					URL   string `json:"url"`
				} `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	} `json:"output"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func newDashscopeProvider(config map[string]interface{}) (Provider, error) {
	cfgHelper := confighelper.New(config)

	apiKey := cfgHelper.GetString("api_key")
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("dashscope 生图提供者需要配置 api_key")
	}

	model := cfgHelper.GetString("model")
	if model == "" {
		model = cfgHelper.GetString("model_name")
	}
	if model == "" {
		return nil, fmt.Errorf("dashscope 生图提供者需要配置 model")
	}

	baseURL := cfgHelper.GetString("base_url", "https://dashscope.aliyuncs.com/api/v1")
	requestPath := cfgHelper.GetString("request_path", "/services/aigc/multimodal-generation/generation")
	timeoutSeconds := cfgHelper.GetInt("timeout", 60)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}

	return &dashscopeProvider{
		client: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
		apiKey:                apiKey,
		baseURL:               strings.TrimRight(baseURL, "/"),
		requestPath:           requestPath,
		defaultModel:          model,
		defaultSize:           cfgHelper.GetString("size"),
		defaultNegativePrompt: cfgHelper.GetString("negative_prompt"),
		defaultPromptExtend:   cfgHelper.GetBool("prompt_extend"),
		defaultWatermark:      cfgHelper.GetBool("watermark"),
		hasNegativePrompt:     cfgHelper.HasKey("negative_prompt"),
		hasPromptExtend:       cfgHelper.HasKey("prompt_extend"),
		hasWatermark:          cfgHelper.HasKey("watermark"),
	}, nil
}

func (p *dashscopeProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("生成请求不能为空")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt不能为空")
	}

	parameters := dashscopeParameters{
		Size: firstNonEmpty(req.Size, p.defaultSize),
	}
	if p.hasNegativePrompt {
		parameters.NegativePrompt = strings.TrimSpace(p.defaultNegativePrompt)
	}
	if p.hasPromptExtend {
		value := p.defaultPromptExtend
		parameters.PromptExtend = &value
	}
	if p.hasWatermark {
		value := p.defaultWatermark
		parameters.Watermark = &value
	}

	request := dashscopeRequest{
		Model: firstNonEmpty(req.Model, p.defaultModel),
		Input: dashscopeInput{
			Messages: []dashscopeMessage{
				{
					Role: "user",
					Content: []dashscopeContent{
						{Text: prompt},
					},
				},
			},
		},
		Parameters: parameters,
	}
	return p.doRequest(ctx, request)
}

func (p *dashscopeProvider) Edit(ctx context.Context, req *EditRequest) (*GenerateResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("编辑请求不能为空")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt不能为空")
	}

	imageValue := strings.TrimSpace(req.ImageURL)
	if imageValue == "" {
		imageValue = strings.TrimSpace(req.ImageBase64)
	}
	if imageValue == "" {
		return nil, fmt.Errorf("image不能为空")
	}
	if strings.HasPrefix(imageValue, "data:") == false && req.ImageBase64 != "" {
		imageValue = "data:image/png;base64," + imageValue
	}

	parameters := dashscopeParameters{
		Size: firstNonEmpty(req.Size, p.defaultSize),
	}
	if req.NegativePrompt != "" {
		parameters.NegativePrompt = strings.TrimSpace(req.NegativePrompt)
	} else if p.hasNegativePrompt {
		parameters.NegativePrompt = strings.TrimSpace(p.defaultNegativePrompt)
	}
	if req.PromptExtend != nil {
		parameters.PromptExtend = req.PromptExtend
	} else if p.hasPromptExtend {
		value := p.defaultPromptExtend
		parameters.PromptExtend = &value
	}
	if req.Watermark != nil {
		parameters.Watermark = req.Watermark
	} else if p.hasWatermark {
		value := p.defaultWatermark
		parameters.Watermark = &value
	}

	contents := []dashscopeContent{
		{Image: imageValue},
		{Text: prompt},
	}
	if mask := strings.TrimSpace(req.MaskURL); mask != "" {
		contents = append([]dashscopeContent{{Mask: mask}}, contents...)
	}

	request := dashscopeRequest{
		Model: firstNonEmpty(req.Model, p.defaultModel),
		Input: dashscopeInput{
			Messages: []dashscopeMessage{
				{
					Role:    "user",
					Content: contents,
				},
			},
		},
		Parameters: parameters,
	}

	return p.doRequest(ctx, request)
}

func (p *dashscopeProvider) doRequest(ctx context.Context, request dashscopeRequest) (*GenerateResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("序列化dashscope请求失败: %w", err)
	}

	endpoint := p.baseURL + p.requestPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("创建dashscope请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", strings.TrimSpace(p.apiKey)))

	resp, err := p.client.Do(httpReq)
	if err != nil {
		log.Errorf("Dashscope生图请求失败: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取dashscope响应失败: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("dashscope请求失败: %s", strings.TrimSpace(string(body)))
	}

	var response dashscopeResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("解析dashscope响应失败: %w", err)
	}

	if response.Code != "" {
		return nil, fmt.Errorf("dashscope错误: %s", response.Message)
	}

	images := make([]ImageData, 0)
	for _, item := range response.Output.Results {
		if item.URL != "" {
			images = append(images, ImageData{URL: item.URL})
			continue
		}
		if item.Image != "" {
			images = append(images, ImageData{URL: item.Image})
		}
	}
	if len(images) == 0 {
		for _, choice := range response.Output.Choices {
			for _, content := range choice.Message.Content {
				if content.URL != "" {
					images = append(images, ImageData{URL: content.URL})
					continue
				}
				if content.Image != "" {
					images = append(images, ImageData{URL: content.Image})
				}
			}
		}
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("dashscope未返回可用图片")
	}

	return &GenerateResponse{
		Created: time.Now().Unix(),
		Data:    images,
	}, nil
}
