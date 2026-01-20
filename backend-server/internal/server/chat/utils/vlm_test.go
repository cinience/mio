package utils

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"backend-server/internal/domain/llm"
)

type mockLLMProvider struct {
	response string
	err      error

	lastFile []byte
	lastText string
	lastMime string
}

func (m *mockLLMProvider) ResponseWithContext(ctx context.Context, sessionID string, dialogue []*schema.Message, tools []*schema.ToolInfo) chan *schema.Message {
	return nil
}

func (m *mockLLMProvider) ResponseWithVLM(ctx context.Context, file []byte, text string, mimeType string) (string, error) {
	m.lastFile = append([]byte(nil), file...)
	m.lastText = text
	m.lastMime = mimeType
	return m.response, m.err
}

func (m *mockLLMProvider) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{}
}

func TestHandleVllmSuccess(t *testing.T) {
	originalProvider := getLLMProvider
	t.Cleanup(func() {
		getLLMProvider = originalProvider
	})

	mockProvider := &mockLLMProvider{
		response: "vision-response",
	}
	var receivedProvider string
	var receivedConfig map[string]interface{}
	getLLMProvider = func(provider string, cfg map[string]interface{}) (llm.LLMProvider, error) {
		receivedProvider = provider
		receivedConfig = cfg
		return mockProvider, nil
	}

	overrideConfig := map[string]interface{}{
		"type": "test-provider",
		"key":  "value",
	}

	imageData := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x00}, 700)...)
	result, err := HandleVLM("device-1", imageData, "describe image", "", overrideConfig)
	require.NoError(t, err)
	assert.Equal(t, "vision-response", result)
	assert.Equal(t, "test-provider", receivedProvider)
	assert.Equal(t, overrideConfig["key"], receivedConfig["key"])

	assert.Equal(t, len(imageData), len(mockProvider.lastFile))
	assert.Equal(t, "describe image", mockProvider.lastText)
	assert.Equal(t, "image/png", mockProvider.lastMime)
}

func TestHandleVllmMissingProvider(t *testing.T) {
	_, err := HandleVLM("device-1", []byte("data"), "text", "", nil)
	require.Error(t, err)
	assert.EqualError(t, err, "未找到可用的VLLM provider")
}

func TestHandleVllmMissingConfig(t *testing.T) {
	_, err := HandleVLM("device-1", []byte("data"), "text", "test-provider", nil)
	require.Error(t, err)
	assert.EqualError(t, err, "未找到VLLM配置")
}

func TestHandleVllmProviderError(t *testing.T) {
	originalProvider := getLLMProvider
	t.Cleanup(func() {
		getLLMProvider = originalProvider
	})

	getLLMProvider = func(provider string, cfg map[string]interface{}) (llm.LLMProvider, error) {
		return nil, errors.New("provider error")
	}

	overrideConfig := map[string]interface{}{
		"type": "test-provider",
	}

	_, err := HandleVLM("device-1", []byte("data"), "text", "", overrideConfig)
	require.Error(t, err)
	assert.EqualError(t, err, "provider error")
}
