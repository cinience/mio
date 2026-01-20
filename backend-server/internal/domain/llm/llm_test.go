package llm

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"backend-server/internal/domain/llm/common"
)

type mockLLMProvider struct {
	messages []*schema.Message
}

func (m *mockLLMProvider) ResponseWithContext(ctx context.Context, sessionID string, dialogue []*schema.Message, functions []*schema.ToolInfo) chan *schema.Message {
	ch := make(chan *schema.Message)
	go func() {
		defer close(ch)
		for _, msg := range m.messages {
			select {
			case <-ctx.Done():
				return
			case ch <- msg:
			}
		}
	}()
	return ch
}

func (m *mockLLMProvider) ResponseWithVLM(ctx context.Context, file []byte, text string, mimeType string) (string, error) {
	return "", nil
}

func (m *mockLLMProvider) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{}
}

func TestHandleLLMWithContextAndTools_SegmentsIpAndDecimals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	provider := &mockLLMProvider{
		messages: []*schema.Message{
			{Content: "公共IP是127.0.0."},
			{Content: "1，请检查。温度是3."},
			{Content: "14度。"},
		},
	}

	stream, err := HandleLLMWithContextAndTools(ctx, provider, nil, nil, "session")
	if err != nil {
		t.Fatalf("HandleLLMWithContextAndTools returned error: %v", err)
	}

	var responses []common.LLMResponseStruct
	for resp := range stream {
		responses = append(responses, resp)
	}

	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d: %#v", len(responses), responses)
	}

	first := responses[0]
	if first.Text != "公共IP是127.0.0.1，请检查。" {
		t.Fatalf("first sentence mismatch: %q", first.Text)
	}
	if !first.IsStart || first.IsEnd {
		t.Fatalf("first sentence flags unexpected: %+v", first)
	}

	second := responses[1]
	if second.Text != "温度是3.14度。" {
		t.Fatalf("second sentence mismatch: %q", second.Text)
	}
	if second.IsStart || second.IsEnd {
		t.Fatalf("second sentence flags unexpected: %+v", second)
	}

	last := responses[2]
	if last.Text != "" || !last.IsEnd {
		t.Fatalf("final response mismatch: %+v", last)
	}
}

func TestHandleLLMWithContextAndTools_FlushesWithoutTerminator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	provider := &mockLLMProvider{
		messages: []*schema.Message{
			{Content: "这是"},
			{Content: "没有标点"},
			{Content: "的回复"},
		},
	}

	stream, err := HandleLLMWithContextAndTools(ctx, provider, nil, nil, "session")
	if err != nil {
		t.Fatalf("HandleLLMWithContextAndTools returned error: %v", err)
	}

	var responses []common.LLMResponseStruct
	for resp := range stream {
		responses = append(responses, resp)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d: %#v", len(responses), responses)
	}

	final := responses[0]
	if final.Text != "这是没有标点的回复" {
		t.Fatalf("final text mismatch: %q", final.Text)
	}
	if !final.IsEnd {
		t.Fatalf("final response IsEnd should be true: %+v", final)
	}
}

func TestHandleLLMWithContextAndTools_TimeExpression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	provider := &mockLLMProvider{
		messages: []*schema.Message{
			{Content: "现在是北京时间2023年10月6日15:23:"},
			{Content: "45啦。"},
		},
	}

	stream, err := HandleLLMWithContextAndTools(ctx, provider, nil, nil, "session")
	if err != nil {
		t.Fatalf("HandleLLMWithContextAndTools returned error: %v", err)
	}

	var responses []common.LLMResponseStruct
	for resp := range stream {
		responses = append(responses, resp)
	}

	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d: %#v", len(responses), responses)
	}

	first := responses[0]
	if first.Text != "现在是北京时间2023年10月6日15:23:45啦。" {
		t.Fatalf("first sentence mismatch: %q", first.Text)
	}

	last := responses[1]
	if last.Text != "" || !last.IsEnd {
		t.Fatalf("final response mismatch: %+v", last)
	}
}
