package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

// MemuClient 封装与 MemU 服务交互的 HTTP 客户端
type MemuClient struct {
	baseURL         string
	apiKey          string
	agentID         string
	agentName       string
	userNamePrefix  string
	httpClient      *http.Client
	memorizeTimeout time.Duration
	retrieveTimeout time.Duration
	healthCheckPath string
}

// MemuConfig 客户端初始化配置
type MemuConfig struct {
	BaseURL        string
	APIKey         string
	AgentID        string
	AgentName      string
	UserNamePrefix string
	Timeout        time.Duration
}

// NewMemuClient 创建新的 MemU 客户端
func NewMemuClient(cfg MemuConfig) (*MemuClient, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("memu baseURL 不能为空")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("memu apiKey 不能为空")
	}
	if cfg.AgentID == "" {
		return nil, fmt.Errorf("memu agentID 不能为空")
	}
	if cfg.AgentName == "" {
		return nil, fmt.Errorf("memu agentName 不能为空")
	}

	baseURL := ensureTrailingSlash(cfg.BaseURL)

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}

	client := &MemuClient{
		baseURL:         baseURL,
		apiKey:          cfg.APIKey,
		agentID:         cfg.AgentID,
		agentName:       cfg.AgentName,
		userNamePrefix:  cfg.UserNamePrefix,
		httpClient:      &http.Client{Timeout: timeout},
		memorizeTimeout: timeout,
		retrieveTimeout: timeout,
		healthCheckPath: "health",
	}

	return client, nil
}

// Close 关闭底层客户端
func (c *MemuClient) Close() error {
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}

// HealthCheck 检查 MemU 服务健康状态
func (c *MemuClient) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	respBody, status, err := c.doRequest(ctx, http.MethodGet, c.healthCheckPath, nil)
	if err != nil {
		return err
	}

	if status >= 200 && status < 300 {
		_ = respBody
		return nil
	}

	return fmt.Errorf("memu health check 失败, status=%d, body=%s", status, string(respBody))
}

// StoreMessages 将对话消息同步到 MemU
func (c *MemuClient) StoreMessages(ctx context.Context, deviceID string, messages []schema.Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	conversation := make([]memuConversationMessage, 0, len(messages))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, msg := range messages {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		conversation = append(conversation, memuConversationMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
			Time:    now,
		})
	}

	if len(conversation) == 0 {
		return "", nil
	}

	reqBody := memuMemorizeRequest{
		Conversation: conversation,
		UserID:       deviceID,
		UserName:     c.buildUserName(deviceID),
		AgentID:      c.agentID,
		AgentName:    c.agentName,
		SessionDate:  time.Now().UTC().Format(time.RFC3339),
	}

	ctx, cancel := context.WithTimeout(ctx, c.memorizeTimeout)
	defer cancel()

	respBytes, statusCode, err := c.doRequest(ctx, http.MethodPost, "api/v2/memory/memorize", reqBody)
	if err != nil {
		return "", err
	}

	if statusCode < 200 || statusCode >= 300 {
		return "", fmt.Errorf("memu memorize 请求失败, status=%d, body=%s", statusCode, string(respBytes))
	}

	var resp memuMemorizeResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return "", fmt.Errorf("解析 memu memorize 响应失败: %w", err)
	}

	return resp.TaskID, nil
}

// RetrieveDefaultCategories 获取默认分类记忆
func (c *MemuClient) RetrieveDefaultCategories(ctx context.Context, deviceID string, withMemoryItems bool) (*memuDefaultCategoriesResponse, error) {
	reqBody := memuDefaultCategoriesRequest{
		UserID:          deviceID,
		AgentID:         c.agentID,
		WantMemoryItems: withMemoryItems,
	}

	ctx, cancel := context.WithTimeout(ctx, c.retrieveTimeout)
	defer cancel()

	respBytes, statusCode, err := c.doRequest(ctx, http.MethodPost, "api/v2/memory/retrieve/default-categories", reqBody)
	if err != nil {
		return nil, err
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("memu retrieve default categories 失败, status=%d, body=%s", statusCode, string(respBytes))
	}

	var resp memuDefaultCategoriesResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("解析 memu default categories 响应失败: %w", err)
	}

	return &resp, nil
}

// RetrieveRelatedMemoryItems 根据查询检索记忆
func (c *MemuClient) RetrieveRelatedMemoryItems(ctx context.Context, deviceID, query string, topK int) (*memuRelatedMemoryItemsResponse, error) {
	reqBody := memuRelatedMemoryItemsRequest{
		UserID:        deviceID,
		AgentID:       c.agentID,
		Query:         query,
		TopK:          topK,
		MinSimilarity: 0.3,
	}

	ctx, cancel := context.WithTimeout(ctx, c.retrieveTimeout)
	defer cancel()

	respBytes, statusCode, err := c.doRequest(ctx, http.MethodPost, "api/v2/memory/retrieve/related-memory-items", reqBody)
	if err != nil {
		return nil, err
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("memu retrieve related memory items 失败, status=%d, body=%s", statusCode, string(respBytes))
	}

	var resp memuRelatedMemoryItemsResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("解析 memu related memory items 响应失败: %w", err)
	}

	return &resp, nil
}

func (c *MemuClient) buildUserName(deviceID string) string {
	if c.userNamePrefix == "" {
		return deviceID
	}
	return fmt.Sprintf("%s%s", c.userNamePrefix, deviceID)
}

func (c *MemuClient) doRequest(ctx context.Context, method, path string, payload interface{}) ([]byte, int, error) {
	var body io.Reader

	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("序列化请求体失败: %w", err)
		}
		body = bytes.NewReader(data)
	}

	fullURL, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		fullURL = c.baseURL + strings.TrimPrefix(path, "/")
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, 0, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("请求 memu 失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取 memu 响应失败: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

func ensureTrailingSlash(u string) string {
	if strings.HasSuffix(u, "/") {
		return u
	}
	return u + "/"
}

type memuConversationMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Time    string `json:"time,omitempty"`
}

type memuMemorizeRequest struct {
	Conversation []memuConversationMessage `json:"conversation,omitempty"`
	UserID       string                    `json:"user_id"`
	UserName     string                    `json:"user_name"`
	AgentID      string                    `json:"agent_id"`
	AgentName    string                    `json:"agent_name"`
	SessionDate  string                    `json:"session_date"`
}

type memuMemorizeResponse struct {
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type memuDefaultCategoriesRequest struct {
	UserID          string `json:"user_id"`
	AgentID         string `json:"agent_id,omitempty"`
	WantMemoryItems bool   `json:"want_memory_items"`
}

type memuDefaultCategoriesResponse struct {
	Categories      []memuCategory `json:"categories"`
	TotalCategories int            `json:"total_categories"`
}

type memuCategory struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Description string             `json:"description"`
	Summary     string             `json:"summary,omitempty"`
	MemoryItems *memuCategoryItems `json:"memory_items,omitempty"`
}

type memuCategoryItems struct {
	Memories    []memuMemoryItem `json:"memories"`
	MemoryCount int              `json:"memory_count"`
}

type memuMemoryItem struct {
	MemoryID   string     `json:"memory_id"`
	Category   string     `json:"category"`
	Content    string     `json:"content"`
	HappenedAt *time.Time `json:"happened_at,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

type memuRelatedMemoryItemsRequest struct {
	UserID        string  `json:"user_id"`
	AgentID       string  `json:"agent_id"`
	Query         string  `json:"query"`
	TopK          int     `json:"top_k"`
	MinSimilarity float64 `json:"min_similarity"`
}

type memuRelatedMemoryItemsResponse struct {
	Memories []memuRelatedMemoryItem `json:"memories"`
}

type memuRelatedMemoryItem struct {
	ID         string  `json:"id"`
	Content    string  `json:"content"`
	Similarity float64 `json:"similarity"`
	CreatedAt  string  `json:"created_at"`
}
