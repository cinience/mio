package managers

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"backend-server/internal/domain/audio"
	client "backend-server/internal/server/chat/session/state"
	chattransport "backend-server/internal/server/chat/transport"
	transporttypes "backend-server/internal/server/transport/types"

	"github.com/stretchr/testify/require"
)

func TestSessionStateMachineNormalTransitions(t *testing.T) {
	sm, conn := createTestStateMachine(t)

	sm.OnListenStart()
	require.Equal(t, StateListening, sm.GetState())

	sm.OnASRStart()
	require.Equal(t, StateProcessingASR, sm.GetState())

	sm.OnLLMStart()
	require.Equal(t, StateProcessingLLM, sm.GetState())

	sm.OnTTSStreamStart()
	require.Equal(t, StateGeneratingTTS, sm.GetState())

	sm.OnTTSStreamSending()
	require.Equal(t, StateSendingTTS, sm.GetState())

	sm.OnTTSStreamComplete(true)
	require.Equal(t, StateWaitingTTSComplete, sm.GetState())

	sm.OnTTSPlaybackComplete()
	require.Equal(t, StateListening, sm.GetState())

	states := conn.sentStates()
	require.Equal(t, []string{
		transporttypes.MessageStateStart,
		transporttypes.MessageStateStop,
	}, states)
}

func TestSessionStateMachineTTSTimeout(t *testing.T) {
	sm, conn := createTestStateMachine(t)

	sm.OnListenStart()
	sm.OnLLMStart()
	sm.OnTTSStreamStart()
	sm.OnTTSStreamComplete(true)

	require.Equal(t, StateWaitingTTSComplete, sm.GetState())

	require.Eventually(t, func() bool {
		return sm.GetState() == StateListening
	}, 200*time.Millisecond, 10*time.Millisecond)

	states := conn.sentStates()
	require.Equal(t, []string{
		transporttypes.MessageStateStart,
		transporttypes.MessageStateStop,
	}, states)
}

func TestSessionStateMachineForceReset(t *testing.T) {
	sm, conn := createTestStateMachine(t)

	sm.OnListenStart()
	sm.OnTTSStreamStart()
	sm.OnTTSStreamComplete(true)
	require.Equal(t, StateWaitingTTSComplete, sm.GetState())

	sm.ForceReset(StateListening)
	require.Equal(t, StateListening, sm.GetState())

	states := conn.sentStates()
	require.Equal(t, []string{
		transporttypes.MessageStateStart,
		transporttypes.MessageStateStop,
	}, states)
}

func TestSessionStateMachineConcurrentAccess(t *testing.T) {
	sm, _ := createTestStateMachine(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			sm.OnListenStart()
			sm.OnASRStart()
			sm.OnLLMStart()
			sm.OnTTSStreamStart()
			sm.OnTTSStreamComplete(false)
		}()

		go func() {
			defer wg.Done()
			_ = sm.GetState()
		}()
	}
	wg.Wait()

	require.NotEqual(t, StateIdle, sm.GetState())
}

func createTestStateMachine(t *testing.T) (*SessionStateMachine, *mockConn) {
	t.Helper()

	clientState := &client.ClientState{
		DeviceID: "device-test",
		OutputAudioFormat: audio.AudioFormat{
			FrameDuration: 5,
		},
		ListenMode: "realtime",
	}

	conn := &mockConn{}
	transport := chattransport.NewServerTransport(conn, clientState, false)
	sm := NewSessionStateMachine(clientState, transport)
	sm.OnListenStart()
	conn.reset()
	return sm, conn
}

type mockConn struct {
	mu          sync.Mutex
	cmdMessages [][]byte
}

func (m *mockConn) SendCmd(msg []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cmdMessages = append(m.cmdMessages, append([]byte(nil), msg...))
	return nil
}

func (m *mockConn) RecvCmd(ctx context.Context, timeout int) ([]byte, error) {
	return nil, context.DeadlineExceeded
}

func (m *mockConn) SendAudio(audio []byte) error {
	return nil
}

func (m *mockConn) RecvAudio(ctx context.Context, timeout int) ([]byte, error) {
	return nil, context.DeadlineExceeded
}

func (m *mockConn) GetDeviceID() string {
	return "device-test"
}

func (m *mockConn) GetRemoteAddr() string {
	return "remote"
}

func (m *mockConn) GetExternalAddr() string {
	return "external"
}

func (m *mockConn) Close() error {
	return nil
}

func (m *mockConn) OnClose(func(deviceId string)) {}

func (m *mockConn) CloseAudioChannel() error {
	return nil
}

func (m *mockConn) GetTransportType() string {
	return transporttypes.TransportTypeWebsocket
}

func (m *mockConn) GetData(key string) (interface{}, error) {
	return nil, nil
}

func (m *mockConn) sentStates() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	states := make([]string, 0, len(m.cmdMessages))
	for _, raw := range m.cmdMessages {
		var msg transporttypes.ServerMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			states = append(states, msg.State)
		}
	}
	return states
}

func (m *mockConn) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cmdMessages = nil
}
