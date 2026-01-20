package managers

import (
	log "backend-server/internal/infrastructure/logger"
	client "backend-server/internal/server/chat/session/state"
	chattransport "backend-server/internal/server/chat/transport"
	"backend-server/internal/server/observability"
	"fmt"
	"sync"
	"time"
)

// SessionState tracks the coarse phases of a chat session lifecycle.
type SessionState int

const (
	StateIdle SessionState = iota
	StateListening
	StateProcessingASR
	StateProcessingLLM
	StateGeneratingTTS
	StateSendingTTS
	StateWaitingTTSComplete
)

const (
	defaultFrameDurationMs = 60
	ttsStopTimerFrames     = 3
)

func (s SessionState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateListening:
		return "listening"
	case StateProcessingASR:
		return "processing_asr"
	case StateProcessingLLM:
		return "processing_llm"
	case StateGeneratingTTS:
		return "generating_tts"
	case StateSendingTTS:
		return "sending_tts"
	case StateWaitingTTSComplete:
		return "waiting_tts_complete"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// SessionStateMachine centralises chat state transitions and related side-effects.
type SessionStateMachine struct {
	mu              sync.RWMutex
	clientState     *client.ClientState
	serverTransport *chattransport.ServerTransport
	currentState    SessionState
	stateEnteredAt  time.Time

	ttsCompleteTimer *time.Timer
	timerMu          sync.Mutex

	onStateChange func(from, to SessionState)
	closed        bool
}

// NewSessionStateMachine builds a state machine bound to the provided client session.
func NewSessionStateMachine(clientState *client.ClientState, transport *chattransport.ServerTransport) *SessionStateMachine {
	return &SessionStateMachine{
		clientState:     clientState,
		serverTransport: transport,
		currentState:    StateIdle,
		stateEnteredAt:  time.Now(),
	}
}

// SetStateChangeHook registers a callback invoked after every transition.
func (sm *SessionStateMachine) SetStateChangeHook(hook func(from, to SessionState)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onStateChange = hook
}

// GetState returns the current session state.
func (sm *SessionStateMachine) GetState() SessionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState
}

// TransitionTo forces the machine into the specified state.
func (sm *SessionStateMachine) TransitionTo(newState SessionState) error {
	return sm.transitionTo(newState, "manual-transition")
}

func (sm *SessionStateMachine) transitionTo(newState SessionState, reason string) error {
	sm.mu.Lock()
	if sm.closed || sm.currentState == newState {
		sm.mu.Unlock()
		return nil
	}
	oldState := sm.currentState
	prevEntered := sm.stateEnteredAt
	sm.currentState = newState
	sm.stateEnteredAt = time.Now()
	hook := sm.onStateChange
	sm.mu.Unlock()

	deviceID := ""
	if sm.clientState != nil {
		deviceID = sm.clientState.DeviceID
	}

	log.Infof("chat session state transition: %s -> %s (device=%s, reason=%s)", oldState, newState, deviceID, reason)

	if metrics := observability.Server(); metrics != nil {
		metrics.RecordStateTransition(oldState.String(), newState.String())
		if !prevEntered.IsZero() {
			metrics.RecordStateDuration(oldState.String(), time.Since(prevEntered))
		}
	}

	if err := sm.handleStateTransition(oldState, newState); err != nil {
		log.Errorf("state transition failure (%s -> %s, device=%s): %v", oldState, newState, deviceID, err)
		return err
	}

	if hook != nil {
		hook(oldState, newState)
	}
	return nil
}

func (sm *SessionStateMachine) handleStateTransition(from, to SessionState) error {
	if sm.clientState == nil {
		return nil
	}

	switch to {
	case StateListening:
		sm.clientState.SetStatus(client.ClientStatusListening)
		sm.clientState.SetTtsStart(false)
	case StateProcessingASR:
		sm.clientState.SetStatus(client.ClientStatusListenStop)
	case StateProcessingLLM:
		sm.clientState.SetStatus(client.ClientStatusLLMStart)
	case StateGeneratingTTS, StateSendingTTS, StateWaitingTTSComplete:
		sm.clientState.SetStatus(client.ClientStatusTTSStart)
	}

	switch to {
	case StateGeneratingTTS:
		sm.cancelTTSCompleteTimer()
		if from == StateListening || from == StateProcessingASR || from == StateProcessingLLM {
			return sm.sendTtsStart()
		}
	case StateSendingTTS:
		sm.cancelTTSCompleteTimer()
		if from == StateListening || from == StateProcessingASR || from == StateProcessingLLM {
			return sm.sendTtsStart()
		}
	case StateWaitingTTSComplete:
		sm.scheduleTTSComplete()
	case StateListening:
		if from == StateGeneratingTTS || from == StateSendingTTS || from == StateWaitingTTSComplete {
			sm.cancelTTSCompleteTimer()
			return sm.sendTtsStop()
		}
	}

	return nil
}

func (sm *SessionStateMachine) sendTtsStart() error {
	if sm.serverTransport == nil {
		return nil
	}
	if err := sm.serverTransport.SendTtsStart(); err != nil {
		return fmt.Errorf("send tts start: %w", err)
	}
	return nil
}

func (sm *SessionStateMachine) sendTtsStop() error {
	if sm.serverTransport == nil {
		return nil
	}
	if err := sm.serverTransport.SendTtsStop(); err != nil {
		return fmt.Errorf("send tts stop: %w", err)
	}
	return nil
}

// OnListenStart marks the device listening state.
func (sm *SessionStateMachine) OnListenStart() {
	if err := sm.transitionTo(StateListening, "listen-start"); err != nil {
		log.Warnf("failed to enter listening state: %v", err)
	}
}

// OnASRStart marks ASR processing.
func (sm *SessionStateMachine) OnASRStart() {
	if err := sm.transitionTo(StateProcessingASR, "asr-start"); err != nil {
		log.Warnf("failed to enter asr state: %v", err)
	}
}

// OnLLMStart marks LLM processing.
func (sm *SessionStateMachine) OnLLMStart() {
	if err := sm.transitionTo(StateProcessingLLM, "llm-start"); err != nil {
		log.Warnf("failed to enter llm state: %v", err)
	}
}

// OnTTSStreamStart marks the beginning of TTS synthesis.
func (sm *SessionStateMachine) OnTTSStreamStart() {
	if err := sm.transitionTo(StateGeneratingTTS, "tts-stream-start"); err != nil {
		log.Warnf("failed to enter generating tts state: %v", err)
	}
}

// OnTTSStreamSending marks that TTS frames are being sent to the device.
func (sm *SessionStateMachine) OnTTSStreamSending() {
	if err := sm.transitionTo(StateSendingTTS, "tts-stream-sending"); err != nil {
		log.Warnf("failed to enter sending tts state: %v", err)
	}
}

// OnTTSStreamComplete finalises a TTS synthesis stream.
func (sm *SessionStateMachine) OnTTSStreamComplete(needWaitPlayback bool) {
	sm.cancelTTSCompleteTimer()
	if needWaitPlayback {
		if err := sm.transitionTo(StateWaitingTTSComplete, "tts-stream-complete-waiting"); err != nil {
			log.Warnf("failed to enter waiting tts state: %v", err)
			return
		}
		return
	}
	if err := sm.transitionTo(StateListening, "tts-stream-complete"); err != nil {
		log.Warnf("failed to return to listening after tts complete: %v", err)
	}
}

// OnTTSPlaybackComplete resumes listening once playback finishes.
func (sm *SessionStateMachine) OnTTSPlaybackComplete() {
	sm.cancelTTSCompleteTimer()
	if err := sm.transitionTo(StateListening, "tts-playback-complete"); err != nil {
		log.Warnf("failed to return to listening after playback complete: %v", err)
	}
}

// ForceReset immediately jumps to the requested state, cancelling timers.
func (sm *SessionStateMachine) ForceReset(state SessionState) {
	sm.cancelTTSCompleteTimer()
	if err := sm.transitionTo(state, "force-reset"); err != nil {
		log.Warnf("force reset transition failed: %v", err)
	}
}

// Close tears down timers and prevents future transitions.
func (sm *SessionStateMachine) Close() {
	sm.mu.Lock()
	if sm.closed {
		sm.mu.Unlock()
		return
	}
	sm.closed = true
	sm.mu.Unlock()
	sm.cancelTTSCompleteTimer()
}

func (sm *SessionStateMachine) scheduleTTSComplete() {
	delay := sm.computeTTSWaitDuration()
	frameDuration := defaultFrameDurationMs
	if sm.clientState != nil && sm.clientState.OutputAudioFormat.FrameDuration > 0 {
		frameDuration = sm.clientState.OutputAudioFormat.FrameDuration
	}
	log.Debugf("TTS playback timer scheduled: delay=%v frameDuration=%dms frames=%d device=%s", delay, frameDuration, ttsStopTimerFrames, sm.deviceID())
	sm.timerMu.Lock()
	defer sm.timerMu.Unlock()
	if sm.ttsCompleteTimer != nil {
		sm.ttsCompleteTimer.Stop()
	}
	sm.ttsCompleteTimer = time.AfterFunc(delay, func() {
		log.Warnf("TTS playback timeout for device=%s after %v", sm.deviceID(), delay)
		sm.OnTTSPlaybackComplete()
	})
}

func (sm *SessionStateMachine) cancelTTSCompleteTimer() {
	sm.timerMu.Lock()
	defer sm.timerMu.Unlock()
	if sm.ttsCompleteTimer != nil {
		sm.ttsCompleteTimer.Stop()
		sm.ttsCompleteTimer = nil
	}
}

func (sm *SessionStateMachine) computeTTSWaitDuration() time.Duration {
	if sm.clientState != nil {
		if wait := sm.clientState.ConsumeTTSPlaybackWait(); wait > 0 {
			return wait
		}
	}
	frameDuration := defaultFrameDurationMs
	if sm.clientState != nil && sm.clientState.OutputAudioFormat.FrameDuration > 0 {
		frameDuration = sm.clientState.OutputAudioFormat.FrameDuration
	}
	return time.Duration(frameDuration*ttsStopTimerFrames) * time.Millisecond
}

func (sm *SessionStateMachine) deviceID() string {
	if sm.clientState == nil {
		return ""
	}
	return sm.clientState.DeviceID
}
