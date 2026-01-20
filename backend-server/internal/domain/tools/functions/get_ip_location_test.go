package functions

import (
	"context"
	"log/slog"
	"testing"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/domain/meetingminutes"
	"backend-server/internal/domain/tools/types"
	ttsdomain "backend-server/internal/domain/tts"
)

type mockConnection struct {
	remoteAddr string
	metadata   map[string]string
}

func newMockConnection(remoteAddr string) *mockConnection {
	return &mockConnection{
		remoteAddr: remoteAddr,
		metadata:   make(map[string]string),
	}
}

func (m *mockConnection) GetRemoteAddr() string {
	return m.remoteAddr
}

func (m *mockConnection) GetDeviceID() string {
	return "test-device"
}

func (m *mockConnection) GetMetadata() map[string]string {
	return m.metadata
}

func (m *mockConnection) GetAudioFormat() (int, int, string) {
	return 16000, 20, "pcm"
}

func (m *mockConnection) GetUserAudio() ([]byte, string) {
	return nil, ""
}

func (m *mockConnection) StreamAudio(context.Context, string, chan []byte) (<-chan error, error) {
	done := make(chan error, 1)
	done <- nil
	close(done)
	return done, nil
}

func (m *mockConnection) GetLogger() *slog.Logger {
	return slog.Default()
}

func (m *mockConnection) ChangeSystemPrompt(string) error {
	return nil
}

func (m *mockConnection) TemporaryChangeVoice(context.Context, *ttsdomain.ChangeVoiceOptions) error {
	return nil
}

func (m *mockConnection) SetCloseAfterChat(bool, string) {}

func (m *mockConnection) SetLastNewsLink(map[string]interface{}) {}

func (m *mockConnection) GetManagerAPIService() manager_api.ManagerAPIService { return nil }

func (m *mockConnection) StartMeetingMinutes() error { return nil }

func (m *mockConnection) StopMeetingMinutes() (*meetingminutes.Snapshot, error) { return nil, nil }

func TestGetIPLocationFunction_Execute(t *testing.T) {
	// Create a new function instance
	function := NewGetIPLocationFunction(nil)

	// Test with a mock connection
	mockConn := newMockConnection("8.8.8.8:12345")

	// Test case 1: Query current IP (from connection)
	args := map[string]interface{}{
		"format": "simple",
	}

	response, err := function.Execute(context.Background(), mockConn, args)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if response == nil {
		t.Error("Response is nil")
	}

	// Test case 2: Query specific IP
	args2 := map[string]interface{}{
		"ip":     "8.8.8.8",
		"format": "detailed",
	}

	response2, err := function.Execute(context.Background(), mockConn, args2)
	if err != nil {
		t.Errorf("Execute with specific IP failed: %v", err)
	}

	if response2 == nil {
		t.Error("Response2 is nil")
	}

	// Test case 3: Query without connection (should fail gracefully)
	response3, err := function.Execute(context.Background(), nil, args)
	if err != nil {
		t.Errorf("Execute without connection failed: %v", err)
	}

	if response3 == nil {
		t.Error("Response3 is nil")
	}
}

func TestGetIPLocationFunction_GetInfo(t *testing.T) {
	function := NewGetIPLocationFunction(nil)

	info := function.GetInfo()
	if info == nil {
		t.Error("GetInfo returned nil")
	}
}

func TestGetIPLocationFunction_GetName(t *testing.T) {
	function := NewGetIPLocationFunction(nil)

	name := function.GetName()
	expected := GetIPLocationFunctionName
	if name != expected {
		t.Errorf("GetName returned %s, expected %s", name, expected)
	}
}

func TestGetIPLocationFunction_GetType(t *testing.T) {
	function := NewGetIPLocationFunction(nil)

	toolType := function.GetType()
	if toolType != types.ToolTypeWait {
		t.Errorf("GetType returned %v, expected %v", toolType, types.ToolTypeWait)
	}
}

func TestGetIPLocationFunction_GetDescription(t *testing.T) {
	function := NewGetIPLocationFunction(nil)

	desc := function.GetDescription()
	if desc == nil {
		t.Error("GetDescription returned nil")
	}
}
