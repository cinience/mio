package webrtc_vad

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebRTCVAD_MultiFrameSupport 测试通用多帧支持（借鉴 v0 设计）
// 验证新实现能自动支持任意帧长度
func TestWebRTCVAD_MultiFrameSupport(t *testing.T) {
	tests := []struct {
		name          string
		sampleRate    int
		frameDuration int
		description   string
	}{
		// 标准帧长
		{
			name:          "标准10ms",
			sampleRate:    16000,
			frameDuration: 10,
			description:   "原生支持，直接处理",
		},
		{
			name:          "标准20ms",
			sampleRate:    16000,
			frameDuration: 20,
			description:   "原生支持，直接处理",
		},
		{
			name:          "标准30ms",
			sampleRate:    16000,
			frameDuration: 30,
			description:   "原生支持，直接处理",
		},
		// 非标准帧长 - 自动分割处理
		{
			name:          "40ms",
			sampleRate:    16000,
			frameDuration: 40,
			description:   "自动分割成多个子帧",
		},
		{
			name:          "50ms",
			sampleRate:    16000,
			frameDuration: 50,
			description:   "自动分割成多个子帧",
		},
		{
			name:          "60ms",
			sampleRate:    16000,
			frameDuration: 60,
			description:   "自动分割成 2×30ms",
		},
		{
			name:          "90ms",
			sampleRate:    16000,
			frameDuration: 90,
			description:   "自动分割成 3×30ms",
		},
		{
			name:          "100ms",
			sampleRate:    16000,
			frameDuration: 100,
			description:   "自动分割成多个子帧",
		},
		{
			name:          "120ms",
			sampleRate:    16000,
			frameDuration: 120,
			description:   "自动分割成 4×30ms",
		},
		{
			name:          "150ms",
			sampleRate:    16000,
			frameDuration: 150,
			description:   "自动分割成 5×30ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建 VAD 实例
			vad, err := NewWebRTCVADWithConfig(tt.sampleRate, DefaultMode)
			require.NoError(t, err, "创建 VAD 实例失败")
			require.NotNil(t, vad)
			t.Cleanup(func() {
				require.NoError(t, vad.Close())
			})

			// 计算帧大小
			frameSize := tt.sampleRate * tt.frameDuration / 1000
			t.Logf("测试 %s (%s): 采样率=%d Hz, 帧时长=%d ms, frameSize=%d samples",
				tt.name, tt.description, tt.sampleRate, tt.frameDuration, frameSize)

			// 生成测试数据（语音信号）
			pcmData := generateSineWave(tt.sampleRate, 300.0, float64(tt.frameDuration)/1000.0, 0.5)
			require.Equal(t, frameSize, len(pcmData), "数据长度应该匹配")

			// 执行 VAD 检测
			isVoice, err := vad.IsVADExt(pcmData, tt.sampleRate, frameSize)
			assert.NoError(t, err, "VAD 检测应该成功")

			t.Logf("  结果: isVoice=%v", isVoice)

			// 对于语音信号，应该检测到语音
			assert.True(t, isVoice, "语音信号应该被检测到")
		})
	}
}

// TestWebRTCVAD_MultiFrameStrategy 测试多帧处理的投票策略
func TestWebRTCVAD_MultiFrameStrategy(t *testing.T) {
	sampleRate := 16000
	vad, err := NewWebRTCVADWithConfig(sampleRate, DefaultMode)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, vad.Close())
	})

	t.Run("60ms_全语音", func(t *testing.T) {
		// 60ms 全是语音 (2×30ms)
		pcmData := generateSineWave(sampleRate, 300.0, 0.06, 0.5)
		frameSize := sampleRate * 60 / 1000

		isVoice, err := vad.IsVADExt(pcmData, sampleRate, frameSize)
		assert.NoError(t, err)
		t.Logf("60ms 全语音: %v", isVoice)
		assert.True(t, isVoice, "应该检测到语音")
	})

	t.Run("90ms_全语音", func(t *testing.T) {
		// 90ms 全是语音 (3×30ms)
		pcmData := generateSineWave(sampleRate, 300.0, 0.09, 0.5)
		frameSize := sampleRate * 90 / 1000

		isVoice, err := vad.IsVADExt(pcmData, sampleRate, frameSize)
		assert.NoError(t, err)
		t.Logf("90ms 全语音: %v", isVoice)
		assert.True(t, isVoice, "应该检测到语音")
	})

	t.Run("120ms_全语音", func(t *testing.T) {
		// 120ms 全是语音 (4×30ms)
		pcmData := generateSineWave(sampleRate, 300.0, 0.12, 0.5)
		frameSize := sampleRate * 120 / 1000

		isVoice, err := vad.IsVADExt(pcmData, sampleRate, frameSize)
		assert.NoError(t, err)
		t.Logf("120ms 全语音: %v", isVoice)
		assert.True(t, isVoice, "应该检测到语音")
	})
}

// TestWebRTCVAD_AllSampleRatesMultiFrame 测试所有采样率的多帧支持
func TestWebRTCVAD_AllSampleRatesMultiFrame(t *testing.T) {
	sampleRates := []int{8000, 16000, 32000, 48000}
	frameDurations := []int{60, 90, 120}

	for _, sampleRate := range sampleRates {
		for _, frameDuration := range frameDurations {
			testName := formatTestName(sampleRate, frameDuration)
			t.Run(testName, func(t *testing.T) {
				vad, err := NewWebRTCVADWithConfig(sampleRate, DefaultMode)
				require.NoError(t, err)
				t.Cleanup(func() {
					require.NoError(t, vad.Close())
				})

				frameSize := sampleRate * frameDuration / 1000
				pcmData := generateSineWave(sampleRate, 300.0, float64(frameDuration)/1000.0, 0.5)

				isVoice, err := vad.IsVADExt(pcmData, sampleRate, frameSize)
				assert.NoError(t, err)

				t.Logf("采样率=%d Hz, 帧时长=%d ms: 结果=%v", sampleRate, frameDuration, isVoice)
				assert.True(t, isVoice, "应该检测到语音")
			})
		}
	}
}

// TestWebRTCVAD_MultiFrameEdgeCases 测试多帧处理的边界情况
func TestWebRTCVAD_MultiFrameEdgeCases(t *testing.T) {
	sampleRate := 16000
	vad, err := NewWebRTCVADWithConfig(sampleRate, DefaultMode)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, vad.Close())
	})

	t.Run("极短帧_5ms", func(t *testing.T) {
		// 5ms 帧（小于最小标准帧 10ms）
		pcmData := generateSineWave(sampleRate, 300.0, 0.005, 0.5)
		frameSize := sampleRate * 5 / 1000

		// 应该能处理，但可能返回 false（数据不足）
		_, err := vad.IsVADExt(pcmData, sampleRate, frameSize)
		// 不应该出错
		assert.NoError(t, err)
	})

	t.Run("超长帧_500ms", func(t *testing.T) {
		// 500ms 超长帧
		pcmData := generateSineWave(sampleRate, 300.0, 0.5, 0.5)
		frameSize := sampleRate * 500 / 1000

		isVoice, err := vad.IsVADExt(pcmData, sampleRate, frameSize)
		assert.NoError(t, err)
		t.Logf("500ms 超长帧: %v", isVoice)
		// 应该能正常处理
	})

	t.Run("空数据", func(t *testing.T) {
		pcmData := []float32{}
		frameSize := sampleRate * 20 / 1000

		isVoice, err := vad.IsVADExt(pcmData, sampleRate, frameSize)
		assert.NoError(t, err)
		assert.False(t, isVoice, "空数据应该返回 false")
	})

	t.Run("数据长度不足一帧", func(t *testing.T) {
		// 只有 5 个样本，但要求 60ms (960 samples)
		pcmData := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
		frameSize := sampleRate * 60 / 1000

		isVoice, err := vad.IsVADExt(pcmData, sampleRate, frameSize)
		assert.NoError(t, err)
		assert.False(t, isVoice, "数据不足应该返回 false")
	})
}

// TestWebRTCVAD_CompareStrategies 对比不同帧长的处理效果
func TestWebRTCVAD_CompareStrategies(t *testing.T) {
	sampleRate := 16000
	vad, err := NewWebRTCVADWithConfig(sampleRate, DefaultMode)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, vad.Close())
	})

	// 生成相同的测试数据
	pcmData180ms := generateSineWave(sampleRate, 300.0, 0.18, 0.5)

	t.Run("180ms_直接处理", func(t *testing.T) {
		frameSize := sampleRate * 180 / 1000
		isVoice, err := vad.IsVADExt(pcmData180ms, sampleRate, frameSize)
		assert.NoError(t, err)
		t.Logf("180ms 直接处理: %v", isVoice)
	})

	t.Run("180ms_分3次60ms", func(t *testing.T) {
		// 分成 3 个 60ms 处理
		frameSize := sampleRate * 60 / 1000
		results := []bool{}

		for i := 0; i < 3; i++ {
			start := i * frameSize
			end := start + frameSize
			frame := pcmData180ms[start:end]

			isVoice, err := vad.IsVADExt(frame, sampleRate, frameSize)
			assert.NoError(t, err)
			results = append(results, isVoice)
		}

		t.Logf("180ms 分3次60ms: %v", results)
	})

	t.Run("180ms_分9次20ms", func(t *testing.T) {
		// 分成 9 个 20ms 处理
		frameSize := sampleRate * 20 / 1000
		results := []bool{}

		for i := 0; i < 9; i++ {
			start := i * frameSize
			end := start + frameSize
			frame := pcmData180ms[start:end]

			isVoice, err := vad.IsVADExt(frame, sampleRate, frameSize)
			assert.NoError(t, err)
			results = append(results, isVoice)
		}

		t.Logf("180ms 分9次20ms: %v", results)
	})
}

// Benchmark: 对比不同帧长的性能
func BenchmarkWebRTCVAD_MultiFrame_60ms(b *testing.B) {
	vad, _ := NewWebRTCVADWithConfig(16000, DefaultMode)
	b.Cleanup(func() {
		_ = vad.Close()
	})
	pcmData := generateSineWave(16000, 300.0, 0.06, 0.5)
	frameSize := 16000 * 60 / 1000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vad.IsVADExt(pcmData, 16000, frameSize)
	}
}

func BenchmarkWebRTCVAD_MultiFrame_90ms(b *testing.B) {
	vad, _ := NewWebRTCVADWithConfig(16000, DefaultMode)
	b.Cleanup(func() {
		_ = vad.Close()
	})
	pcmData := generateSineWave(16000, 300.0, 0.09, 0.5)
	frameSize := 16000 * 90 / 1000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vad.IsVADExt(pcmData, 16000, frameSize)
	}
}

func BenchmarkWebRTCVAD_MultiFrame_120ms(b *testing.B) {
	vad, _ := NewWebRTCVADWithConfig(16000, DefaultMode)
	b.Cleanup(func() {
		_ = vad.Close()
	})
	pcmData := generateSineWave(16000, 300.0, 0.12, 0.5)
	frameSize := 16000 * 120 / 1000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vad.IsVADExt(pcmData, 16000, frameSize)
	}
}

// 辅助函数
func formatTestName(sampleRate, frameDuration int) string {
	return formatSampleRate(sampleRate) + "_" + formatDuration(frameDuration)
}

func formatDuration(duration int) string {
	return formatSampleRate(duration) + "ms"
}

// 辅助函数：格式化采样率
func formatSampleRate(sampleRate int) string {
	switch sampleRate {
	case 8000:
		return "8kHz"
	case 16000:
		return "16kHz"
	case 32000:
		return "32kHz"
	case 48000:
		return "48kHz"
	default:
		return "Unknown"
	}
}
