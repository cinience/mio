package imagegen

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	openai "github.com/meguminnnnnnnnn/go-openai"

	log "backend-server/internal/infrastructure/logger"
	"backend-server/pkg/confighelper"
)

type openAIProvider struct {
	client                   *openai.Client
	defaultModel             string
	defaultN                 int
	defaultQuality           string
	defaultSize              string
	defaultStyle             string
	defaultResponseFormat    string
	defaultUser              string
	defaultBackground        string
	defaultModeration        string
	defaultOutputCompression int
	defaultOutputFormat      string
}

func newOpenAIProvider(config map[string]interface{}) (Provider, error) {
	cfgHelper := confighelper.New(config)

	apiKey := cfgHelper.GetString("api_key")
	if apiKey == "" {
		return nil, fmt.Errorf("openai 生图提供者需要配置 api_key")
	}

	model := cfgHelper.GetString("model")
	if model == "" {
		model = cfgHelper.GetString("model_name")
	}
	if model == "" {
		return nil, fmt.Errorf("openai 生图提供者需要配置 model")
	}

	baseURL := cfgHelper.GetString("base_url")
	timeoutSeconds := cfgHelper.GetInt("timeout", 60)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}

	httpClient := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}

	clientCfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		clientCfg.BaseURL = strings.TrimRight(baseURL, "/")
	}
	clientCfg.HTTPClient = httpClient

	provider := &openAIProvider{
		client:                   openai.NewClientWithConfig(clientCfg),
		defaultModel:             model,
		defaultN:                 cfgHelper.GetInt("n", 1),
		defaultQuality:           cfgHelper.GetString("quality"),
		defaultSize:              cfgHelper.GetString("size"),
		defaultStyle:             cfgHelper.GetString("style"),
		defaultResponseFormat:    cfgHelper.GetString("response_format"),
		defaultUser:              cfgHelper.GetString("user"),
		defaultBackground:        cfgHelper.GetString("background"),
		defaultModeration:        cfgHelper.GetString("moderation"),
		defaultOutputCompression: cfgHelper.GetInt("output_compression"),
		defaultOutputFormat:      cfgHelper.GetString("output_format"),
	}

	return provider, nil
}

func (p *openAIProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("生成请求不能为空")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt不能为空")
	}

	imageReq := openai.ImageRequest{
		Prompt:         prompt,
		Model:          firstNonEmpty(req.Model, p.defaultModel),
		Quality:        firstNonEmpty(req.Quality, p.defaultQuality),
		Size:           firstNonEmpty(req.Size, p.defaultSize),
		Style:          firstNonEmpty(req.Style, p.defaultStyle),
		ResponseFormat: firstNonEmpty(req.ResponseFormat, p.defaultResponseFormat),
		User:           firstNonEmpty(req.User, p.defaultUser),
		Background:     firstNonEmpty(req.Background, p.defaultBackground),
		Moderation:     firstNonEmpty(req.Moderation, p.defaultModeration),
		OutputFormat:   firstNonEmpty(req.OutputFormat, p.defaultOutputFormat),
	}

	if compression := firstPositive(req.OutputCompression, p.defaultOutputCompression); compression > 0 {
		imageReq.OutputCompression = compression
	}

	if n := firstPositive(req.N, p.defaultN); n > 0 {
		imageReq.N = n
	}

	resp, err := p.client.CreateImage(ctx, imageReq)
	if err != nil {
		log.Errorf("OpenAI生图请求失败: %v", err)
		return nil, err
	}

	return convertOpenAIResponse(&resp), nil
}

func convertOpenAIResponse(resp *openai.ImageResponse) *GenerateResponse {
	if resp == nil {
		return &GenerateResponse{}
	}

	result := &GenerateResponse{Created: resp.Created}

	if len(resp.Data) > 0 {
		result.Data = make([]ImageData, 0, len(resp.Data))
		for _, item := range resp.Data {
			result.Data = append(result.Data, ImageData{
				URL:           item.URL,
				B64JSON:       item.B64JSON,
				RevisedPrompt: item.RevisedPrompt,
			})
		}
	}

	if resp.Usage.TotalTokens != 0 || resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		usage := &Usage{
			TotalTokens:  resp.Usage.TotalTokens,
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		}
		usage.TextTokens = resp.Usage.InputTokensDetails.TextTokens
		usage.ImageTokens = resp.Usage.InputTokensDetails.ImageTokens
		result.Usage = usage
	}

	return result
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
