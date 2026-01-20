package videogen

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

type dashscopeVideoProvider struct {
	client              *http.Client
	apiKey              string
	baseURL             string
	requestPath         string
	taskPath            string
	defaultModel        string
	defaultSize         string
	defaultDuration     int
	defaultShotType     string
	defaultPromptExtend bool
	hasPromptExtend     bool
	pollInterval        time.Duration
	pollTimeout         time.Duration
}

type dashscopeVideoRequest struct {
	Model      string               `json:"model"`
	Input      dashscopeVideoInput  `json:"input"`
	Parameters dashscopeVideoParams `json:"parameters,omitempty"`
}

type dashscopeVideoInput struct {
	Prompt   string `json:"prompt"`
	AudioURL string `json:"audio_url,omitempty"`
}

type dashscopeVideoParams struct {
	Size         string `json:"size,omitempty"`
	PromptExtend *bool  `json:"prompt_extend,omitempty"`
	Duration     int    `json:"duration,omitempty"`
	ShotType     string `json:"shot_type,omitempty"`
}

type dashscopeVideoResponse struct {
	Output struct {
		TaskID     string `json:"task_id"`
		TaskStatus string `json:"task_status"`
		VideoURL   string `json:"video_url"`
		Results    []struct {
			URL string `json:"url"`
		} `json:"results"`
	} `json:"output"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func newDashscopeProvider(config map[string]interface{}) (Provider, error) {
	cfgHelper := confighelper.New(config)

	apiKey := cfgHelper.GetString("api_key")
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("dashscope 生视频提供者需要配置 api_key")
	}

	model := cfgHelper.GetString("model")
	if model == "" {
		model = cfgHelper.GetString("model_name")
	}
	if model == "" {
		return nil, fmt.Errorf("dashscope 生视频提供者需要配置 model")
	}

	baseURL := cfgHelper.GetString("base_url", "https://dashscope.aliyuncs.com/api/v1")
	requestPath := cfgHelper.GetString("request_path", "/services/aigc/video-generation/video-synthesis")
	taskPath := cfgHelper.GetString("task_path", "/tasks/%s")
	timeoutSeconds := cfgHelper.GetInt("timeout", 120)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}

	pollIntervalSeconds := cfgHelper.GetInt("poll_interval_seconds", 5)
	if pollIntervalSeconds <= 0 {
		pollIntervalSeconds = 5
	}
	pollTimeoutSeconds := cfgHelper.GetInt("poll_timeout_seconds", 180)
	if pollTimeoutSeconds <= 0 {
		pollTimeoutSeconds = 180
	}

	provider := &dashscopeVideoProvider{
		client: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
		apiKey:              apiKey,
		baseURL:             strings.TrimRight(baseURL, "/"),
		requestPath:         requestPath,
		taskPath:            taskPath,
		defaultModel:        model,
		defaultSize:         cfgHelper.GetString("size"),
		defaultDuration:     cfgHelper.GetInt("duration"),
		defaultShotType:     cfgHelper.GetString("shot_type"),
		defaultPromptExtend: cfgHelper.GetBool("prompt_extend"),
		hasPromptExtend:     cfgHelper.HasKey("prompt_extend"),
		pollInterval:        time.Duration(pollIntervalSeconds) * time.Second,
		pollTimeout:         time.Duration(pollTimeoutSeconds) * time.Second,
	}
	return provider, nil
}

func (p *dashscopeVideoProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("生成请求不能为空")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt不能为空")
	}

	parameters := dashscopeVideoParams{
		Size:     firstNonEmpty(req.Size, p.defaultSize),
		Duration: firstNonZero(req.Duration, p.defaultDuration),
		ShotType: firstNonEmpty(req.ShotType, p.defaultShotType),
	}
	if req.PromptExtend != nil {
		parameters.PromptExtend = req.PromptExtend
	} else if p.hasPromptExtend {
		value := p.defaultPromptExtend
		parameters.PromptExtend = &value
	}

	request := dashscopeVideoRequest{
		Model: firstNonEmpty(req.Model, p.defaultModel),
		Input: dashscopeVideoInput{
			Prompt:   prompt,
			AudioURL: strings.TrimSpace(req.AudioURL),
		},
		Parameters: parameters,
	}

	resp, err := p.submit(ctx, request)
	if err != nil {
		return nil, err
	}

	videoURL := extractVideoURL(resp)
	if videoURL != "" {
		return buildVideoResponse(resp, videoURL), nil
	}

	taskID := strings.TrimSpace(resp.Output.TaskID)
	if taskID == "" {
		return nil, fmt.Errorf("dashscope 未返回任务ID")
	}

	return p.pollResult(ctx, taskID)
}

func (p *dashscopeVideoProvider) submit(ctx context.Context, request dashscopeVideoRequest) (*dashscopeVideoResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("序列化dashscope视频请求失败: %w", err)
	}

	endpoint := p.baseURL + p.requestPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("创建dashscope视频请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", strings.TrimSpace(p.apiKey)))
	httpReq.Header.Set("X-DashScope-Async", "enable")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		log.Errorf("Dashscope生视频请求失败: %v", err)
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

	var response dashscopeVideoResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("解析dashscope响应失败: %w", err)
	}

	if response.Code != "" {
		return nil, fmt.Errorf("dashscope错误: %s", response.Message)
	}
	return &response, nil
}

func (p *dashscopeVideoProvider) pollResult(ctx context.Context, taskID string) (*GenerateResponse, error) {
	deadline := time.Now().Add(p.pollTimeout)

	for {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("dashscope视频任务超时")
		}

		resp, err := p.fetchTask(ctx, taskID)
		if err != nil {
			return nil, err
		}

		status := strings.ToUpper(strings.TrimSpace(resp.Output.TaskStatus))
		switch status {
		case "SUCCEEDED", "SUCCESS", "DONE":
			videoURL := extractVideoURL(resp)
			if videoURL == "" {
				return nil, fmt.Errorf("dashscope未返回视频地址")
			}
			return buildVideoResponse(resp, videoURL), nil
		case "FAILED", "ERROR", "CANCELED", "CANCELLED":
			if resp.Message != "" {
				return nil, fmt.Errorf("dashscope视频任务失败: %s", resp.Message)
			}
			return nil, fmt.Errorf("dashscope视频任务失败")
		default:
			// keep waiting
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.pollInterval):
		}
	}
}

func (p *dashscopeVideoProvider) fetchTask(ctx context.Context, taskID string) (*dashscopeVideoResponse, error) {
	endpoint := p.baseURL + fmt.Sprintf(p.taskPath, strings.TrimSpace(taskID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("创建dashscope任务查询失败: %w", err)
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", strings.TrimSpace(p.apiKey)))

	resp, err := p.client.Do(httpReq)
	if err != nil {
		log.Errorf("Dashscope任务查询失败: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取dashscope任务响应失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("dashscope任务查询失败: %s", strings.TrimSpace(string(body)))
	}

	var response dashscopeVideoResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("解析dashscope任务响应失败: %w", err)
	}

	if response.Code != "" {
		return nil, fmt.Errorf("dashscope错误: %s", response.Message)
	}
	return &response, nil
}

func extractVideoURL(resp *dashscopeVideoResponse) string {
	if resp == nil {
		return ""
	}
	if url := strings.TrimSpace(resp.Output.VideoURL); url != "" {
		return url
	}
	for _, item := range resp.Output.Results {
		if url := strings.TrimSpace(item.URL); url != "" {
			return url
		}
	}
	return ""
}

func buildVideoResponse(resp *dashscopeVideoResponse, videoURL string) *GenerateResponse {
	result := &GenerateResponse{
		Created: time.Now().Unix(),
		TaskID:  strings.TrimSpace(resp.Output.TaskID),
		Status:  strings.TrimSpace(resp.Output.TaskStatus),
		Data: []VideoData{
			{URL: videoURL},
		},
	}
	if result.Status == "" {
		result.Status = "SUCCEEDED"
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

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
