package eino_llm

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubChatModel struct {
	generateResp *schema.Message
	generateErr  error

	streamResp *schema.StreamReader[*schema.Message]
	streamErr  error

	lastGenerateInput []*schema.Message
	lastStreamInput   []*schema.Message
}

func (s *stubChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	s.lastGenerateInput = cloneMessages(input)
	return s.generateResp, s.generateErr
}

func (s *stubChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	s.lastStreamInput = cloneMessages(input)
	return s.streamResp, s.streamErr
}

func (s *stubChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, len(messages))
	for i, msg := range messages {
		if msg == nil {
			continue
		}
		copyMsg := *msg
		if len(msg.MultiContent) > 0 {
			copyMsg.MultiContent = make([]schema.ChatMessagePart, len(msg.MultiContent))
			copy(copyMsg.MultiContent, msg.MultiContent)
			for idx, part := range copyMsg.MultiContent {
				if part.ImageURL != nil {
					copyURL := *part.ImageURL
					copyMsg.MultiContent[idx].ImageURL = &copyURL
				}
			}
		}
		if len(msg.UserInputMultiContent) > 0 {
			copyMsg.UserInputMultiContent = make([]schema.MessageInputPart, len(msg.UserInputMultiContent))
			for idx, part := range msg.UserInputMultiContent {
				copyPart := part
				if part.Image != nil {
					copyImage := *part.Image
					if part.Image.URL != nil {
						urlCopy := *part.Image.URL
						copyImage.URL = &urlCopy
					}
					if part.Image.Base64Data != nil {
						base64Copy := *part.Image.Base64Data
						copyImage.Base64Data = &base64Copy
					}
					copyPart.Image = &copyImage
				}
				if part.Audio != nil {
					copyAudio := *part.Audio
					if part.Audio.URL != nil {
						urlCopy := *part.Audio.URL
						copyAudio.URL = &urlCopy
					}
					if part.Audio.Base64Data != nil {
						base64Copy := *part.Audio.Base64Data
						copyAudio.Base64Data = &base64Copy
					}
					copyPart.Audio = &copyAudio
				}
				if part.Video != nil {
					copyVideo := *part.Video
					if part.Video.URL != nil {
						urlCopy := *part.Video.URL
						copyVideo.URL = &urlCopy
					}
					if part.Video.Base64Data != nil {
						base64Copy := *part.Video.Base64Data
						copyVideo.Base64Data = &base64Copy
					}
					copyPart.Video = &copyVideo
				}
				if part.File != nil {
					copyFile := *part.File
					if part.File.URL != nil {
						urlCopy := *part.File.URL
						copyFile.URL = &urlCopy
					}
					if part.File.Base64Data != nil {
						base64Copy := *part.File.Base64Data
						copyFile.Base64Data = &base64Copy
					}
					copyPart.File = &copyFile
				}
				copyMsg.UserInputMultiContent[idx] = copyPart
			}
		}
		if len(msg.ToolCalls) > 0 {
			copyMsg.ToolCalls = make([]schema.ToolCall, len(msg.ToolCalls))
			copy(copyMsg.ToolCalls, msg.ToolCalls)
		}
		out[i] = &copyMsg
	}
	return out
}

func TestResponseWithVllm_Generate(t *testing.T) {
	stub := &stubChatModel{
		generateResp: &schema.Message{Content: "stubbed response"},
	}

	provider := &EinoLLMProvider{
		chatModel:  stub,
		streamable: false,
	}

	imageData := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 16)...)

	result, err := provider.ResponseWithVLM(context.Background(), imageData, "请描述图片", "image/png")
	require.NoError(t, err)
	assert.Equal(t, "stubbed response", result)

	require.Len(t, stub.lastGenerateInput, 2)

	systemMsg := stub.lastGenerateInput[0]
	require.NotNil(t, systemMsg)
	assert.Equal(t, schema.System, systemMsg.Role)
	assert.Contains(t, systemMsg.Content, "图片识别专家")

	userMsg := stub.lastGenerateInput[1]
	require.NotNil(t, userMsg)
	require.Len(t, userMsg.UserInputMultiContent, 2)

	textPart := userMsg.UserInputMultiContent[0]
	assert.Equal(t, schema.ChatMessagePartTypeText, textPart.Type)
	assert.Equal(t, "请描述图片", textPart.Text)

	imagePart := userMsg.UserInputMultiContent[1]
	assert.Equal(t, schema.ChatMessagePartTypeImageURL, imagePart.Type)
	require.NotNil(t, imagePart.Image)
	require.NotNil(t, imagePart.Image.URL)
	require.True(t, strings.HasPrefix(*imagePart.Image.URL, "data:image/png;base64,"))

	payload := strings.TrimPrefix(*imagePart.Image.URL, "data:image/png;base64,")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, err)
	assert.Equal(t, imageData, decoded)
}

func TestResponseWithVllm_Stream(t *testing.T) {
	stub := &stubChatModel{
		streamResp: schema.StreamReaderFromArray([]*schema.Message{
			schema.AssistantMessage("partial-1", nil),
			schema.AssistantMessage("partial-2", nil),
		}),
	}

	provider := &EinoLLMProvider{
		chatModel:  stub,
		streamable: true,
	}

	imageData := []byte("gif89a")

	result, err := provider.ResponseWithVLM(context.Background(), imageData, "识别内容", "image/gif")
	require.NoError(t, err)
	assert.Equal(t, "partial-1partial-2", result)

	require.Len(t, stub.lastStreamInput, 2)
	assert.Nil(t, stub.lastGenerateInput)
}

func TestResponseWithVllm_StreamErrorStopWithoutFallback(t *testing.T) {
	stub := &stubChatModel{
		streamErr:    errors.New("stream not supported"),
		generateResp: &schema.Message{Content: "fallback response"},
	}

	provider := &EinoLLMProvider{
		chatModel:  stub,
		streamable: true,
	}

	result, err := provider.ResponseWithVLM(context.Background(), []byte("jpeg-data"), "识别内容", "image/jpeg")
	require.NoError(t, err)
	assert.Equal(t, "", result)

	require.NotNil(t, stub.lastStreamInput)
	require.Nil(t, stub.lastGenerateInput)
}
