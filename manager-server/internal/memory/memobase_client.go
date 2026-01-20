package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/spf13/viper"

	// 真实的memobase-go SDK
	"github.com/memodb-io/memobase/src/client/memobase-go/blob"
	memobase "github.com/memodb-io/memobase/src/client/memobase-go/core"

	"manager-server/internal/logger"
)

// 定义常见错误
var (
	ErrMemobaseNotInitialized = errors.New("memobase客户端未初始化")
	ErrMemobaseNotEnabled     = errors.New("memobase功能未启用")
	ErrInvalidConfig          = errors.New("memobase配置无效")
	ErrUserNotFound           = errors.New("用户未找到")
	ErrSyncFailed             = errors.New("同步失败")
)

// MemobaseClient Memobase长记忆客户端包装器
type MemobaseClient struct {
	client     MemobaseSDKInterface // 使用接口以便测试
	projectURL string
	apiKey     string
}

// MemobaseSDKInterface 定义memobase-go SDK接口，便于测试和模拟
type MemobaseSDKInterface interface {
	Ping() bool
	GetUser(userID string, noGet bool) (MemobaseUser, error)
	AddUser(data map[string]interface{}, userID string) (string, error)
	UpdateConfig(config string) error
}

// MemobaseUser 定义用户操作接口
type MemobaseUser interface {
	ID() string
	Insert(blob interface{}, sync bool) (string, error)
	Flush(blobType string, sync bool) (interface{}, error)
	Profile(preferTopics []string, onlyTopics []string, maxTokenSize *int) ([]UserProfile, error)
	Event(topk *int, maxTokenSize *int, needSummary *bool) ([]UserEvent, error)
	Context(maxTokenSize int, preferTopics []string, onlyTopics []string) (string, error)
	AddProfile(content, topic, subTopic string) (string, error)
	UpdateProfile(profileID, content, topic, subTopic string) error
	DeleteProfile(profileID string) error
}

// RealMemobaseSDK 真实SDK的适配器
type RealMemobaseSDK struct {
	client *memobase.MemoBaseClient
}

// NewRealMemobaseSDK 创建真实SDK适配器
func NewRealMemobaseSDK(client *memobase.MemoBaseClient) *RealMemobaseSDK {
	return &RealMemobaseSDK{client: client}
}

func (r *RealMemobaseSDK) Ping() bool {
	return r.client.Ping()
}

func (r *RealMemobaseSDK) GetUser(userID string, noGet bool) (MemobaseUser, error) {
	user, err := r.client.GetUser(userID, noGet)
	if err != nil {
		return nil, err
	}
	return &RealMemobaseUser{user: user}, nil
}

func (r *RealMemobaseSDK) AddUser(data map[string]interface{}, userID string) (string, error) {
	return r.client.AddUser(data, userID)
}

func (r *RealMemobaseSDK) UpdateConfig(config string) error {
	return r.client.UpdateConfig(config)
}

// RealMemobaseUser 真实用户的适配器
type RealMemobaseUser struct {
	user *memobase.User
}

func (r *RealMemobaseUser) Insert(data interface{}, sync bool) (string, error) {
	// 将interface{}转换为blob.BlobInterface
	blobInterface, err := convertToBlob(data)
	if err != nil {
		return "", fmt.Errorf("转换为Blob失败: %w", err)
	}

	return r.user.Insert(blobInterface, sync)
}

// convertToBlob 将数据转换为BlobInterface
func convertToBlob(data interface{}) (blob.BlobInterface, error) {
	switch v := data.(type) {
	case *ChatBlob:
		// 转换我们的ChatBlob为SDK的ChatBlob
		messages := make([]blob.OpenAICompatibleMessage, 0, len(v.Messages))
		for _, msg := range v.Messages {
			message := blob.OpenAICompatibleMessage{
				Role:    getString(msg, "role"),
				Content: getString(msg, "content"),
			}
			if createdAt := getString(msg, "created_at"); createdAt != "" {
				message.CreatedAt = &createdAt
			}
			messages = append(messages, message)
		}

		now := time.Now()
		return &blob.ChatBlob{
			BaseBlob: blob.BaseBlob{
				Type:      blob.ChatType,
				CreatedAt: &now,
			},
			Messages: messages,
		}, nil

	case map[string]interface{}:
		// 处理通用的map数据，转换为ChatBlob
		if msgType, ok := v["type"].(string); ok && msgType == "chat" {
			if messages, ok := v["messages"].([]map[string]interface{}); ok {
				chatMessages := make([]blob.OpenAICompatibleMessage, 0, len(messages))
				for _, msg := range messages {
					message := blob.OpenAICompatibleMessage{
						Role:    getString(msg, "role"),
						Content: getString(msg, "content"),
					}
					if createdAt := getString(msg, "created_at"); createdAt != "" {
						message.CreatedAt = &createdAt
					}
					chatMessages = append(chatMessages, message)
				}

				now := time.Now()
				return &blob.ChatBlob{
					BaseBlob: blob.BaseBlob{
						Type:      blob.ChatType,
						CreatedAt: &now,
					},
					Messages: chatMessages,
				}, nil
			}
		}

		// 处理单个消息数据
		if role := getString(v, "role"); role != "" {
			content := getString(v, "content")
			now := time.Now()
			nowStr := now.Format(time.RFC3339)

			return &blob.ChatBlob{
				BaseBlob: blob.BaseBlob{
					Type:      blob.ChatType,
					CreatedAt: &now,
				},
				Messages: []blob.OpenAICompatibleMessage{
					{
						Role:      role,
						Content:   content,
						CreatedAt: &nowStr,
					},
				},
			}, nil
		}

		return nil, fmt.Errorf("无法识别的数据格式: %+v", v)

	default:
		return nil, fmt.Errorf("不支持的数据类型: %T", data)
	}
}

// getString 安全地从map中获取字符串值
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// TestConvertToBlob 测试Blob转换功能（仅用于调试）
func TestConvertToBlob() error {
	// 测试单个消息转换
	messageData := map[string]interface{}{
		"role":      "user",
		"content":   "Hello, world!",
		"timestamp": time.Now().Unix(),
		"device_id": "test-device",
	}

	blobInterface, err := convertToBlob(messageData)
	if err != nil {
		return fmt.Errorf("转换单个消息失败: %w", err)
	}

	if blobInterface.GetType() != blob.ChatType {
		return fmt.Errorf("期望类型为ChatType，得到: %s", blobInterface.GetType())
	}

	logger.Debugf("✅ Blob转换测试成功: 类型=%s", blobInterface.GetType())
	return nil
}

// TestUserCreation 测试用户创建功能（调试用）
func (m *MemobaseClient) TestUserCreation(deviceID string) error {
	if m.client == nil {
		return fmt.Errorf("memobase客户端未初始化")
	}

	user, err := m.GetOrCreateUser(deviceID)
	if err != nil {
		return fmt.Errorf("用户创建/获取失败: %w", err)
	}

	// 测试插入一条简单消息
	messageData := map[string]interface{}{
		"role":    "user",
		"content": "测试消息",
	}

	_, err = user.Insert(messageData, true)
	if err != nil {
		return fmt.Errorf("消息插入失败: %w", err)
	}

	return nil
}

func (r *RealMemobaseUser) Flush(blobType string, sync bool) (interface{}, error) {
	// 转换字符串为BlobType
	var bt blob.BlobType
	switch blobType {
	case "chat":
		bt = blob.ChatType
	case "doc":
		bt = blob.DocType
	case "image":
		bt = blob.ImageType
	case "code":
		bt = blob.CodeType
	case "transcript":
		bt = blob.TranscriptType
	default:
		bt = blob.ChatType // 默认为chat类型
	}

	// 调用真实SDK的Flush方法
	err := r.user.Flush(bt, sync)
	if err != nil {
		return nil, err
	}

	// 返回成功标志
	return map[string]interface{}{
		"success": true,
		"type":    blobType,
	}, nil
}

func (r *RealMemobaseUser) Profile(preferTopics []string, onlyTopics []string, maxTokenSize *int) ([]UserProfile, error) {
	// 构造ProfileOptions
	options := &memobase.ProfileOptions{
		PreferTopics: preferTopics,
		OnlyTopics:   onlyTopics,
	}
	if maxTokenSize != nil {
		options.MaxTokenSize = *maxTokenSize
	}

	profiles, err := r.user.Profile(options)
	if err != nil {
		return nil, err
	}

	// 转换为我们的UserProfile格式
	var result []UserProfile
	for _, p := range profiles {
		result = append(result, UserProfile{
			ID:        p.ID.String(), // UUID转字符串
			Content:   p.Content,
			Topic:     p.Attributes.Topic,
			SubTopic:  p.Attributes.SubTopic,
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
			Attributes: map[string]interface{}{
				"topic":     p.Attributes.Topic,
				"sub_topic": p.Attributes.SubTopic,
			},
		})
	}

	return result, nil
}

func (r *RealMemobaseUser) Event(topk *int, maxTokenSize *int, needSummary *bool) ([]UserEvent, error) {
	// 处理参数
	topkVal := 10 // 默认值
	if topk != nil {
		topkVal = *topk
	}
	needSummaryVal := true // 默认值
	if needSummary != nil {
		needSummaryVal = *needSummary
	}

	events, err := r.user.Event(topkVal, maxTokenSize, needSummaryVal)
	if err != nil {
		return nil, err
	}

	// 转换为我们的UserEvent格式
	var result []UserEvent
	for _, e := range events {
		// 将EventData转换为map
		eventDataMap := map[string]interface{}{
			"profile_delta": e.EventData.ProfileDelta,
			"event_tip":     e.EventData.EventTip,
			"event_tags":    e.EventData.EventTags,
		}

		result = append(result, UserEvent{
			ID:         e.ID.String(), // UUID转字符串
			CreatedAt:  e.CreatedAt,
			Timestamp:  e.CreatedAt,
			Content:    e.EventData.EventTip, // 使用EventTip作为Content
			EventData:  eventDataMap,
			Similarity: e.Similarity,
		})
	}

	return result, nil
}

func (r *RealMemobaseUser) Context(maxTokenSize int, preferTopics []string, onlyTopics []string) (string, error) {
	// 构造ContextOptions
	options := &memobase.ContextOptions{
		MaxTokenSize: maxTokenSize,
		PreferTopics: preferTopics,
		OnlyTopics:   onlyTopics,
	}

	return r.user.Context(options)
}

func (r *RealMemobaseUser) ID() string {
	if r.user == nil {
		return ""
	}
	return r.user.UserID
}

func (r *RealMemobaseUser) AddProfile(content, topic, subTopic string) (string, error) {
	return r.user.AddProfile(content, topic, subTopic)
}

func (r *RealMemobaseUser) UpdateProfile(profileID, content, topic, subTopic string) error {
	return r.user.UpdateProfile(profileID, content, topic, subTopic)
}

func (r *RealMemobaseUser) DeleteProfile(profileID string) error {
	return r.user.DeleteProfile(profileID)
}

// UserProfile 用户画像结构
type UserProfile struct {
	ID         string                 `json:"id"`
	Content    string                 `json:"content"`
	Topic      string                 `json:"topic"`
	SubTopic   string                 `json:"sub_topic"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
	Attributes map[string]interface{} `json:"attributes"`
}

// UserEvent 用户事件结构
type UserEvent struct {
	ID         string                 `json:"id"`
	CreatedAt  time.Time              `json:"created_at"`
	Timestamp  time.Time              `json:"timestamp"`
	Content    string                 `json:"content"`
	EventData  map[string]interface{} `json:"event_data"`
	Similarity float64                `json:"similarity,omitempty"`
}

// ChatBlob 对话数据结构，对应memobase-go的ChatBlob
type ChatBlob struct {
	Type     string                   `json:"type"`
	Messages []map[string]interface{} `json:"messages"`
}

// validateMemobaseConfig 验证Memobase配置
func validateMemobaseConfig() error {
	// 检查长记忆是否启用且提供者为 memobase
	if !viper.GetBool("memory.long_term.enabled") {
		return fmt.Errorf("%w: 长记忆功能未启用", ErrMemobaseNotEnabled)
	}

	if viper.GetString("memory.long_term.provider") != "memobase" {
		return fmt.Errorf("%w: 长记忆提供者不是 memobase", ErrMemobaseNotEnabled)
	}

	projectURL := viper.GetString("memory.long_term.providers.memobase.project_url")
	apiKey := viper.GetString("memory.long_term.providers.memobase.api_key")

	if projectURL == "" || apiKey == "" {
		return fmt.Errorf("%w: project_url和api_key不能为空", ErrInvalidConfig)
	}

	// 验证URL格式
	if !strings.HasPrefix(projectURL, "http://") && !strings.HasPrefix(projectURL, "https://") {
		return fmt.Errorf("%w: project_url必须是有效的HTTP(S) URL", ErrInvalidConfig)
	}

	return nil
}

// NewMemobaseClient 创建新的Memobase客户端
func NewMemobaseClient(projectURL, apiKey string) (*MemobaseClient, error) {
	if projectURL == "" || apiKey == "" {
		return nil, fmt.Errorf("project_url和api_key不能为空")
	}

	cli, err := memobase.NewMemoBaseClient(projectURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("创建Memobase客户端失败: %w", err)
	}

	// 使用适配器包装真实SDK
	adapter := NewRealMemobaseSDK(cli)

	client := &MemobaseClient{
		projectURL: projectURL,
		apiKey:     apiKey,
		client:     adapter,
	}

	if client.client != nil && !client.client.Ping() {
		logger.Warnf("Memobase连接测试失败，但继续初始化")
	}

	logger.Infof("Memobase客户端初始化完成: %s", projectURL)
	return client, nil
}

// SyncMessage 同步消息到Memobase
func (m *MemobaseClient) SyncMessage(ctx context.Context, deviceID string, role schema.RoleType, content string) error {
	if m.client == nil {
		return ErrMemobaseNotInitialized
	}

	user, err := m.GetOrCreateUser(deviceID)
	if err != nil {
		return fmt.Errorf("获取用户失败: %w", err)
	}

	// 构造消息数据
	messageData := map[string]interface{}{
		"role":      string(role),
		"content":   content,
		"timestamp": time.Now().Unix(),
		"device_id": deviceID,
	}

	// 插入到Memobase
	_, err = user.Insert(messageData, true)
	if err != nil {
		return fmt.Errorf("同步消息失败: %w", err)
	}

	return nil
}

// ensureValidUUID 确保字符串是有效的UUID格式，如果不是则生成标准的UUIDv5
func ensureValidUUID(input string) string {
	// 首先检查是否已经是有效的UUID
	if _, err := uuid.Parse(input); err == nil {
		return input
	}

	// 如果不是有效UUID，则基于输入字符串生成确定性的UUIDv5
	// 使用DNS命名空间作为基础，确保相同输入总是生成相同的UUID
	namespace := uuid.NameSpaceDNS                  // 使用标准DNS命名空间
	name := fmt.Sprintf("xiaozhi.device.%s", input) // 创建唯一的名称

	// 生成标准的UUIDv5
	generatedUUID := uuid.NewSHA1(namespace, []byte(name))

	logger.Debugf("将用户ID '%s' 转换为UUIDv5格式: %s", input, generatedUUID.String())
	return generatedUUID.String()
}

// generateUUIDv4 生成随机的UUIDv4（如果需要完全随机的UUID）
func generateUUIDv4() string {
	return uuid.New().String()
}

func (m *MemobaseClient) GetUserID(deviceID string) string {
	return ensureValidUUID(deviceID)
}

// GetOrCreateUser 获取或创建用户
func (m *MemobaseClient) GetOrCreateUser(deviceID string) (MemobaseUser, error) {
	// 将设备ID转换为用户ID (确保是UUID格式)
	userID := m.GetUserID(deviceID)

	if m.client == nil {
		return nil, fmt.Errorf("memobase客户端未初始化")
	}

	// 首先尝试从服务器获取现有用户
	user, err := m.client.GetUser(userID, false)
	if err == nil {
		// 用户已存在，直接返回
		logger.Debugf("获取到现有Memobase用户: %s (设备: %s)", userID, deviceID)
		return user, nil
	}

	// 用户不存在，创建新用户
	logger.Infof("用户不存在，创建新用户: %s (设备: %s)", userID, deviceID)

	userData := map[string]interface{}{
		"device_id":  deviceID,
		"created_at": time.Now().Format(time.RFC3339),
		"source":     "xiaozhi-server",
		"user_type":  "device",
	}

	// 使用预生成的UUID作为用户ID
	createdUserID, err := m.client.AddUser(userData, userID)
	if err != nil {
		return nil, fmt.Errorf("创建用户失败 (userID: %s): %w", userID, err)
	}

	logger.Infof("✅ 成功创建Memobase用户: %s -> %s", deviceID, createdUserID)

	// 重新获取用户对象，使用创建时返回的用户ID
	user, err = m.client.GetUser(createdUserID, false)
	if err != nil {
		// 如果获取失败，尝试使用noGet=true创建本地用户对象
		logger.Warnf("获取新创建用户失败，使用本地对象: %v", err)
		user, err = m.client.GetUser(createdUserID, true)
		if err != nil {
			return nil, fmt.Errorf("获取新创建用户失败: %w", err)
		}
	}

	return user, nil
}

// StoreConversation 存储对话到Memobase
func (m *MemobaseClient) StoreConversation(ctx context.Context, deviceID string, messages []schema.Message) error {
	user, err := m.GetOrCreateUser(deviceID)
	if err != nil {
		return fmt.Errorf("获取用户失败: %w", err)
	}

	// 转换消息格式为Memobase格式，过滤不支持的角色类型
	chatMessages := make([]map[string]interface{}, 0, len(messages))

	filteredCount := 0
	for _, msg := range messages {
		// 检查角色是否受支持（Memobase目前只支持user和assistant角色）
		if msg.Role != schema.User && msg.Role != schema.Assistant {
			filteredCount++
			logger.Debugf("过滤不支持的消息角色: %s, 内容: %s", string(msg.Role), msg.Content)
			continue
		}

		chatMessage := map[string]interface{}{
			"role":       string(msg.Role),
			"content":    msg.Content,
			"created_at": time.Now().Format(time.RFC3339),
		}
		chatMessages = append(chatMessages, chatMessage)
	}

	if filteredCount > 0 {
		logger.Infof("Memobase存储时过滤了%d条不支持的消息角色，保留%d条有效消息", filteredCount, len(chatMessages))
	}

	// 如果没有有效消息，则跳过存储
	if len(chatMessages) == 0 {
		logger.Debugf("所有消息都被过滤，跳过Memobase存储: deviceID=%s", deviceID)
		return nil
	}

	// 创建ChatBlob
	chatBlob := &ChatBlob{
		Type:     "chat",
		Messages: chatMessages,
	}

	// 插入到Memobase
	blobID, err := user.Insert(chatBlob, true)
	if err != nil {
		return fmt.Errorf("插入对话失败: %w", err)
	}

	logger.Debugf("成功存储对话到Memobase: deviceID=%s, userID:%s, blobID=%s", deviceID, user.ID(), blobID)

	// 异步处理缓冲区
	go func() {
		if _, err := user.Flush("chat", false); err != nil {
			logger.Errorf("刷新Memobase缓冲区失败: %v", err)
		}
	}()

	return nil
}

// GetLongTermContext 获取长期记忆上下文
func (m *MemobaseClient) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	user, err := m.GetOrCreateUser(deviceID)
	if err != nil {
		return "", fmt.Errorf("获取用户失败: %w", err)
	}

	// 获取结构化上下文
	context, err := user.Context(maxTokens, nil, nil)
	if err != nil {
		return "", fmt.Errorf("获取上下文失败: %w", err)
	}

	return context, nil
}

// GetUserProfiles 获取用户画像
func (m *MemobaseClient) GetUserProfiles(ctx context.Context, deviceID string, topics []string) ([]UserProfile, error) {
	user, err := m.GetOrCreateUser(deviceID)
	if err != nil {
		return nil, fmt.Errorf("获取用户失败: %w", err)
	}

	maxTokens := 2000
	profiles, err := user.Profile(topics, nil, &maxTokens)
	if err != nil {
		return nil, fmt.Errorf("获取用户画像失败: %w", err)
	}

	return profiles, nil
}

// GetUserEvents 获取用户事件
func (m *MemobaseClient) GetUserEvents(ctx context.Context, deviceID string, limit int) ([]UserEvent, error) {
	user, err := m.GetOrCreateUser(deviceID)
	if err != nil {
		return nil, fmt.Errorf("获取用户失败: %w", err)
	}

	maxTokens := 1000
	needSummary := true
	events, err := user.Event(&limit, &maxTokens, &needSummary)
	if err != nil {
		return nil, fmt.Errorf("获取用户事件失败: %w", err)
	}

	return events, nil
}

// UpdateUserProfile 更新用户画像
func (m *MemobaseClient) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	user, err := m.GetOrCreateUser(deviceID)
	if err != nil {
		return fmt.Errorf("获取用户失败: %w", err)
	}

	// 添加新的画像条目
	profileID, err := user.AddProfile(content, topic, subTopic)
	if err != nil {
		return fmt.Errorf("添加用户画像失败: %w", err)
	}

	logger.Debugf("成功添加用户画像: deviceID=%s, profileID=%s, topic=%s/%s",
		deviceID, profileID, topic, subTopic)

	return nil
}

// IsHealthy 检查Memobase连接健康状态
func (m *MemobaseClient) IsHealthy() bool {
	if m.client == nil {
		return false
	}
	return m.client.Ping()
}

// DeleteUserProfile 删除用户画像
func (m *MemobaseClient) DeleteUserProfile(ctx context.Context, deviceID, profileID string) error {
	user, err := m.GetOrCreateUser(deviceID)
	if err != nil {
		return fmt.Errorf("获取用户失败: %w", err)
	}

	// 删除画像
	err = user.DeleteProfile(profileID)
	if err != nil {
		return fmt.Errorf("删除用户画像失败: %w", err)
	}

	logger.Debugf("成功删除用户画像: deviceID=%s, profileID=%s", deviceID, profileID)
	return nil
}

// Close 关闭客户端连接
func (m *MemobaseClient) Close() error {
	m.client = nil
	logger.Infof("Memobase客户端已关闭")
	return nil
}
