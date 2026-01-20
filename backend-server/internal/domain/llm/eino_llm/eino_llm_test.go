package eino_llm

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

func TestEinoResponseWithToolsReturnsClosedChannelWhenMessagesEmpty(t *testing.T) {
	t.Parallel()
	provider := &EinoLLMProvider{streamable: true}
	ch := provider.EinoResponseWithTools(context.Background(), "session", nil, nil)
	require.NotNil(t, ch)
	_, ok := <-ch
	require.False(t, ok, "expected closed channel for empty message slice")
}

func TestEinoResponseWithToolsDoesNotMutateSharedChatModel(t *testing.T) {
	t.Parallel()
	toolModel := &mockToolCallingChatModel{
		streamMessages: []*schema.Message{{Role: schema.Assistant, Content: "hello"}},
	}
	baseModel := &mockToolCallingChatModel{withToolsReturn: toolModel}
	provider := &EinoLLMProvider{
		chatModel:  baseModel,
		streamable: true,
		maxTokens:  128,
	}
	messages := []*schema.Message{
		{Role: schema.User, Content: "ping"},
	}
	tools := []*schema.ToolInfo{{Name: "echo"}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch := provider.EinoResponseWithTools(ctx, "session", messages, tools)
	require.NotNil(t, ch)
	var contents []string
	for msg := range ch {
		contents = append(contents, msg.Content)
	}
	require.Equal(t, []string{"hello"}, contents)
	require.Equal(t, baseModel, provider.chatModel)
	require.Equal(t, 1, baseModel.withToolsCalls)
}

func TestIsValidJSON(t *testing.T) {
	t.Parallel()
	require.True(t, isValidJSON(`{"foo":1}`))
	require.False(t, isValidJSON(`{"foo":`))
}

type mockToolCallingChatModel struct {
	streamMessages  []*schema.Message
	streamErr       error
	generateMsg     *schema.Message
	generateErr     error
	withToolsReturn model.ToolCallingChatModel
	withToolsErr    error
	withToolsCalls  int
}

func (m *mockToolCallingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.generateMsg, m.generateErr
}

func (m *mockToolCallingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	reader, writer := schema.Pipe[*schema.Message](len(m.streamMessages) + 1)
	go func() {
		defer writer.Close()
		for _, msg := range m.streamMessages {
			writer.Send(msg, nil)
		}
	}()
	return reader, nil
}

func (m *mockToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.withToolsCalls++
	if m.withToolsErr != nil {
		return nil, m.withToolsErr
	}
	if m.withToolsReturn != nil {
		return m.withToolsReturn, nil
	}
	return m, nil
}
