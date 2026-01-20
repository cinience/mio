package vad

import (
	"math"
	"testing"

	"backend-server/constants"
	vadinter "backend-server/internal/domain/vad/inter"
	vadregistry "backend-server/internal/registry/vad"
	client "backend-server/internal/server/chat/session/state"
	audiohelper "backend-server/pkg/audio"
)

func init() {
	vadregistry.SetCapabilities(constants.VadTypeWebRTCVad, vadregistry.ProviderCapabilities{RequiresWindow: true, ResetEachCall: true})
	vadregistry.SetCapabilities(constants.VadTypeSherpaVad, vadregistry.ProviderCapabilities{Incremental: true})
}

func TestApplyActivationStreak(t *testing.T) {
	tests := []struct {
		name            string
		initialStreak   int
		rawHaveVoice    bool
		minActiveFrames int
		clientHaveVoice bool
		wantStreak      int
		wantConfirmed   bool
	}{
		{"no_voice_reset", 1, false, 2, false, 0, false},
		{"first_voice_not_enough", 0, true, 2, false, 1, false},
		{"second_voice_confirmed", 1, true, 2, false, 2, true},
		{"client_has_voice_continues", 2, true, 2, true, 2, true},
		{"client_has_voice_silence", 2, false, 2, true, 0, false},
		{"min_active_one", 0, true, 1, false, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := newVADStrategy(constants.VadTypeWebRTCVad, 0, tt.minActiveFrames)
			strategy.activeStreak = tt.initialStreak
			gotConfirmed := strategy.confirmActivation(tt.rawHaveVoice, tt.clientHaveVoice)
			if strategy.activeStreak != tt.wantStreak {
				t.Fatalf("confirmActivation() streak got %d, want %d", strategy.activeStreak, tt.wantStreak)
			}
			if gotConfirmed != tt.wantConfirmed {
				t.Fatalf("confirmActivation() confirmed got %v, want %v", gotConfirmed, tt.wantConfirmed)
			}
		})
	}
}

func TestCalcRMS(t *testing.T) {
	if rms := calcRMS([]float32{}); rms != 0 {
		t.Fatalf("calcRMS empty expected 0, got %f", rms)
	}
	data := []float32{0.0, 0.5, -0.5, 1.0, -1.0}
	rms := calcRMS(data)
	if rms <= 0 {
		t.Fatalf("calcRMS expected positive value, got %f", rms)
	}
}

func TestPrepareInputIncremental(t *testing.T) {
	buffer := &client.AsrAudioBuffer{}
	buffer.Configure(4, 8)
	strategy := newVADStrategy(constants.VadTypeSherpaVad, 0, 1)

	data, _, ready := strategy.prepareInput(buffer, nil, false)
	if ready || len(data) != 0 {
		t.Fatalf("expected no data when buffer empty")
	}

	buffer.AddAsrAudioData([]float32{1, 2, 3, 4})
	data, _, ready = strategy.prepareInput(buffer, nil, false)
	if !ready || len(data) != 4 {
		t.Fatalf("expected 4 samples, got %d", len(data))
	}

	data, _, ready = strategy.prepareInput(buffer, nil, false)
	if ready || len(data) != 0 {
		t.Fatalf("expected no new data after consumption")
	}

	buffer.AddAsrAudioData([]float32{5, 6})
	data, _, ready = strategy.prepareInput(buffer, nil, false)
	if !ready || len(data) != 2 {
		t.Fatalf("expected 2 samples after append, got %d", len(data))
	}
}

func TestPrepareInputAggregated(t *testing.T) {
	buffer := &client.AsrAudioBuffer{}
	buffer.Configure(4, 8)
	strategy := newVADStrategy(constants.VadTypeWebRTCVad, 0, 1)

	data, _, ready := strategy.prepareInput(buffer, nil, false)
	if ready || len(data) != 0 {
		t.Fatalf("expected no data without aggregation ready")
	}

	aggregated := []float32{0.1, 0.2, 0.3}
	data, rms, ready := strategy.prepareInput(buffer, aggregated, true)
	if !ready || len(data) != len(aggregated) {
		t.Fatalf("expected aggregated data to pass through")
	}
	if rms <= 0 {
		t.Fatalf("rms should be positive")
	}
}

func TestVADPipelineEvaluateAggregated(t *testing.T) {
	buffer := &client.AsrAudioBuffer{}
	buffer.Configure(4, 8)
	mockVAD := &mockVAD{responses: []bool{true}}
	runtime := &mockRuntime{next: mockVAD}
	pipeline := newVADPipeline(runtime, buffer, vadPipelineConfig{
		Provider:        constants.VadTypeWebRTCVad,
		NoiseFloor:      0,
		MinActiveFrames: 1,
		FrameDurationMs: 20,
		FrameSize:       4,
		SampleRate:      16000,
	})

	result, ready, err := pipeline.EvaluateFrame([]float32{0.1, 0.2, 0.3, 0.4}, false)
	if err != nil {
		t.Fatalf("EvaluateFrame returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected evaluation to be ready on first frame")
	}
	if !result.Raw || !result.Confirmed {
		t.Fatalf("expected voice detected; got raw=%v confirmed=%v", result.Raw, result.Confirmed)
	}
	if !result.ShouldFlush {
		t.Fatalf("expected ShouldFlush when client inactive")
	}
	if mockVAD.callCount != 1 {
		t.Fatalf("expected one VAD call, got %d", mockVAD.callCount)
	}
	if mockVAD.resetCount == 0 {
		t.Fatalf("expected VAD Reset to be called")
	}
	if len(mockVAD.inputs) != 1 || len(mockVAD.inputs[0]) != 4 {
		t.Fatalf("expected VAD input of 4 samples, got %d entries with length %d", len(mockVAD.inputs), len(mockVAD.inputs[0]))
	}
}

func TestVADPipelineNoiseSuppression(t *testing.T) {
	buffer := &client.AsrAudioBuffer{}
	buffer.Configure(4, 8)
	mockVAD := &mockVAD{responses: []bool{false}}
	runtime := &mockRuntime{next: mockVAD}
	pipeline := newVADPipeline(runtime, buffer, vadPipelineConfig{
		Provider:        constants.VadTypeWebRTCVad,
		NoiseFloor:      0.05, // higher than the RMS of the test data
		MinActiveFrames: 1,
		FrameDurationMs: 20,
		FrameSize:       4,
		SampleRate:      16000,
	})

	result, ready, err := pipeline.EvaluateFrame([]float32{0.001, -0.001, 0.0005, -0.0005}, false)
	if err != nil {
		t.Fatalf("EvaluateFrame returned error: %v", err)
	}
	if !ready {
		t.Fatalf("expected evaluation to be ready with sufficient samples")
	}
	if result.Raw || result.Confirmed {
		t.Fatalf("expected noise suppression to prevent activation, got raw=%v confirmed=%v", result.Raw, result.Confirmed)
	}
	if mockVAD.callCount != 1 {
		t.Fatalf("expected VAD provider to be invoked exactly once, got %d calls", mockVAD.callCount)
	}
}

func TestVADServicePreservesBufferedAudioUntilConfirmation(t *testing.T) {
	frameSize := 4
	buffer := &client.AsrAudioBuffer{}
	buffer.Configure(frameSize, 32)

	mockVAD := &mockVAD{responses: []bool{false, false, false, false, false, true, true}}
	runtime := &mockRuntime{next: mockVAD}
	pipeline := newVADPipeline(runtime, buffer, vadPipelineConfig{
		Provider:        constants.VadTypeWebRTCVad,
		NoiseFloor:      0,
		MinActiveFrames: 2,
		FrameDurationMs: 20,
		FrameSize:       frameSize,
		SampleRate:      16000,
	})

	state := &client.ClientState{
		AsrAudioBuffer: buffer,
	}
	service := &vadService{
		state:    state,
		pipeline: pipeline,
		cfg: VADConfig{
			Provider:        constants.VadTypeWebRTCVad,
			MinActiveFrames: 2,
			FrameDurationMs: 20,
			FrameSize:       frameSize,
			SampleRate:      16000,
		},
		frameSize: frameSize,
	}

	silentFrame := []float32{0, 0, 0, 0}
	for i := 0; i < 5; i++ {
		pcm, raw, confirmed, skipped, err := service.evaluateDecodedPCM(silentFrame)
		if err != nil {
			t.Fatalf("silence evaluation %d failed: %v", i, err)
		}
		if pcm != nil {
			t.Fatalf("expected nil pcm for silence at %d, got len=%d", i, len(pcm))
		}
		if raw || confirmed || skipped {
			t.Fatalf("expected silence flags false, got raw=%v confirmed=%v skipped=%v", raw, confirmed, skipped)
		}
	}

	if got := service.pipeline.buffer.GetFrameCount(); got != 5 {
		t.Fatalf("expected buffered frames to remain intact, got %d", got)
	}

	voiceFrame := []float32{0.5, 0.4, 0.3, 0.2}
	pcm, raw, confirmed, skipped, err := service.evaluateDecodedPCM(voiceFrame)
	if err != nil {
		t.Fatalf("first voiced frame failed: %v", err)
	}
	if !raw || confirmed {
		t.Fatalf("expected raw=true confirmed=false on first voiced frame, got raw=%v confirmed=%v", raw, confirmed)
	}
	if len(pcm) != frameSize {
		t.Fatalf("expected first voiced frame to forward current frame only, got len=%d", len(pcm))
	}
	if skipped {
		t.Fatalf("did not expect skip flag on voiced frame")
	}

	pcm, raw, confirmed, skipped, err = service.evaluateDecodedPCM(voiceFrame)
	if err != nil {
		t.Fatalf("second voiced frame failed: %v", err)
	}
	if !raw || !confirmed {
		t.Fatalf("expected confirmation on second voiced frame, got raw=%v confirmed=%v", raw, confirmed)
	}
	if skipped {
		t.Fatalf("skip flag must remain false once VAD runs")
	}

	expectedFrames := 7 // 5 silent + 2 voiced frames drained together
	if len(pcm) != expectedFrames*frameSize {
		t.Fatalf("expected drained buffer len=%d, got %d", expectedFrames*frameSize, len(pcm))
	}
}

func TestVADServiceEvaluatePCM(t *testing.T) {
	frameSize := 4
	buffer := &client.AsrAudioBuffer{}
	buffer.Configure(frameSize, 8)

	runtime := &mockRuntime{}
	pipeline := newVADPipeline(runtime, buffer, vadPipelineConfig{
		Provider:        constants.VadTypeWebRTCVad,
		NoiseFloor:      0,
		MinActiveFrames: 1,
		FrameDurationMs: 20,
		FrameSize:       frameSize,
		SampleRate:      16000,
	})

	state := &client.ClientState{
		AsrAudioBuffer: buffer,
	}
	state.Asr.AutoEnd = true

	service := &vadService{
		state:    state,
		pipeline: pipeline,
		cfg: VADConfig{
			Provider:        constants.VadTypeWebRTCVad,
			MinActiveFrames: 1,
			FrameDurationMs: 20,
			FrameSize:       frameSize,
			SampleRate:      16000,
		},
		frameSize: frameSize,
	}

	inputSamples := []int16{0, 16384, -16384, 32767}
	pcmBytes := audiohelper.Int16SliceToBytes(inputSamples)

	pcm, raw, confirmed, skipped, err := service.EvaluatePCM(pcmBytes)
	if err != nil {
		t.Fatalf("EvaluatePCM failed: %v", err)
	}
	if !raw || !confirmed || !skipped {
		t.Fatalf("expected pcm evaluation to bypass VAD, got raw=%v confirmed=%v skipped=%v", raw, confirmed, skipped)
	}
	if len(pcm) != len(inputSamples) {
		t.Fatalf("expected %d samples, got %d", len(inputSamples), len(pcm))
	}

	want := []float32{0, 0.5, -0.5, 1.0}
	for i, v := range want {
		if math.Abs(float64(pcm[i]-v)) > 0.01 {
			t.Fatalf("sample %d mismatch: got %.3f want %.3f", i, pcm[i], v)
		}
	}
}

type mockRuntime struct {
	provider vadinter.VAD
	next     vadinter.VAD
	initErr  error
	initCall int
}

func (m *mockRuntime) IsInitialized() bool {
	return m.provider != nil
}

func (m *mockRuntime) Init(provider string, config map[string]interface{}) error {
	m.initCall++
	if m.initErr != nil {
		return m.initErr
	}
	if m.next != nil {
		m.provider = m.next
		m.next = nil
	} else if m.provider == nil {
		m.provider = &mockVAD{}
	}
	return nil
}

func (m *mockRuntime) GetProvider() vadinter.VAD {
	return m.provider
}

type mockVAD struct {
	responses  []bool
	errs       []error
	callCount  int
	resetCount int
	inputs     [][]float32
}

func (m *mockVAD) IsVAD(_ []float32) (bool, error) {
	return false, nil
}

func (m *mockVAD) IsVADExt(pcmData []float32, _ int, _ int) (bool, error) {
	copyData := append([]float32(nil), pcmData...)
	m.inputs = append(m.inputs, copyData)
	var err error
	if m.callCount < len(m.errs) && m.errs[m.callCount] != nil {
		err = m.errs[m.callCount]
	}
	var resp bool
	if m.callCount < len(m.responses) {
		resp = m.responses[m.callCount]
	}
	m.callCount++
	return resp, err
}

func (m *mockVAD) Reset() error {
	m.resetCount++
	return nil
}

func (m *mockVAD) Close() error {
	return nil
}
