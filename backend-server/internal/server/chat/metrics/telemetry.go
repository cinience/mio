package metrics

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ConversationMetrics struct {
	mu                sync.Mutex
	roundID           string
	span              trace.Span
	inputText         string
	start             time.Time
	lastUpstreamAudio time.Time
	firstDownAudio    time.Time
	audioDuration     time.Duration
	audioBytes        int
	partialCount      int
	interruptCount    int
	asrRestartCount   int
	autoResumeCount   int
	llmDuration       time.Duration
	toolDuration      time.Duration
	toolCalls         int
	ttsDuration       time.Duration
	audioDownDuration time.Duration
	audioDownBytes    int
	audioDownFrames   int
	errors            []string
	outputBuilder     strings.Builder
	outputSegments    []string
}

type metricsContextKey struct{}

var conversationMetricsKey = metricsContextKey{}

const outputSentenceSeparator = "#"

func NewConversationMetrics(roundID, inputText string, start time.Time) *ConversationMetrics {
	if start.IsZero() {
		start = time.Now()
	}
	return &ConversationMetrics{
		roundID:   roundID,
		inputText: inputText,
		start:     start,
	}
}

type Snapshot struct {
	RoundID           string
	InputPreview      string
	TotalDuration     time.Duration
	AudioDuration     time.Duration
	LastUpAudio       time.Time
	FirstDownAudio    time.Time
	ResponseLatency   time.Duration
	AudioBytes        int
	LLMDuration       time.Duration
	ToolDuration      time.Duration
	ToolCalls         int
	TTSDuration       time.Duration
	AudioDownDuration time.Duration
	AudioDownBytes    int
	AudioDownFrames   int
	Errors            []string
	OutputText        string
	OutputPreview     string
	PartialCount      int
	InterruptCount    int
	AsrRestartCount   int
	AutoResumeCount   int
}

func WithConversationMetrics(ctx context.Context, metrics *ConversationMetrics) context.Context {
	return context.WithValue(ctx, conversationMetricsKey, metrics)
}

func GetConversationMetrics(ctx context.Context) *ConversationMetrics {
	if v := ctx.Value(conversationMetricsKey); v != nil {
		if metrics, ok := v.(*ConversationMetrics); ok {
			return metrics
		}
	}
	return nil
}

func (m *ConversationMetrics) AddError(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, err.Error())
}

func (m *ConversationMetrics) SetAudioStats(duration time.Duration, bytes int) {
	m.mu.Lock()
	m.audioDuration = duration
	m.audioBytes = bytes
	m.mu.Unlock()
}

func (m *ConversationMetrics) SetLastUpstreamAudio(ts time.Time) {
	if ts.IsZero() {
		return
	}
	m.mu.Lock()
	m.lastUpstreamAudio = ts
	m.mu.Unlock()
}

func (m *ConversationMetrics) MarkFirstDownstreamAudio(ts time.Time) {
	if ts.IsZero() {
		return
	}
	m.mu.Lock()
	if m.firstDownAudio.IsZero() {
		m.firstDownAudio = ts
	}
	m.mu.Unlock()
}

func (m *ConversationMetrics) AddLLMDuration(d time.Duration) {
	if d <= 0 {
		return
	}
	m.mu.Lock()
	m.llmDuration += d
	m.mu.Unlock()
}

func (m *ConversationMetrics) AddToolDuration(d time.Duration, count int) {
	if d <= 0 && count == 0 {
		return
	}
	m.mu.Lock()
	m.toolDuration += d
	m.toolCalls += count
	m.mu.Unlock()
}

func (m *ConversationMetrics) AddTTSDuration(d time.Duration) {
	if d <= 0 {
		return
	}
	m.mu.Lock()
	m.ttsDuration += d
	m.mu.Unlock()
}

func (m *ConversationMetrics) IncPartialCount() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.partialCount++
	m.mu.Unlock()
}

func (m *ConversationMetrics) IncInterruptCount() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.interruptCount++
	m.mu.Unlock()
}

func (m *ConversationMetrics) IncAsrRestartCount() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.asrRestartCount++
	m.mu.Unlock()
}

func (m *ConversationMetrics) IncAutoResumeCount() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.autoResumeCount++
	m.mu.Unlock()
}

func (m *ConversationMetrics) AddAudioDownStats(duration time.Duration, frames, bytes int) {
	if duration <= 0 && frames == 0 && bytes == 0 {
		return
	}
	m.mu.Lock()
	m.audioDownDuration += duration
	m.audioDownFrames += frames
	m.audioDownBytes += bytes
	m.mu.Unlock()
}

func (m *ConversationMetrics) AddOutput(text string) {
	if text == "" {
		return
	}
	m.mu.Lock()
	if m.outputBuilder.Len() > 0 {
		m.outputBuilder.WriteString(" ")
	}
	m.outputBuilder.WriteString(text)
	m.outputSegments = append(m.outputSegments, text)
	m.mu.Unlock()
}

func (m *ConversationMetrics) SetSpan(span trace.Span) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.span = span
	m.mu.Unlock()
}

func (m *ConversationMetrics) ApplySpanAttributes() {
	if m == nil || m.span == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	attrs := []attribute.KeyValue{
		attribute.String("conversation.round_id", m.roundID),
		attribute.Int64("conversation.duration_ms", time.Since(m.start).Milliseconds()),
		attribute.Int64("stage.audio.capture_duration_ms", m.audioDuration.Milliseconds()),
		attribute.Int("stage.audio.capture_bytes", m.audioBytes),
		attribute.Int64("stage.llm.duration_ms", m.llmDuration.Milliseconds()),
		attribute.Int("stage.function.calls", m.toolCalls),
		attribute.Int64("stage.function.duration_ms", m.toolDuration.Milliseconds()),
		attribute.Int64("stage.tts.duration_ms", m.ttsDuration.Milliseconds()),
		attribute.Int64("stage.audio.downstream_duration_ms", m.audioDownDuration.Milliseconds()),
		attribute.Int("stage.audio.downstream_bytes", m.audioDownBytes),
		attribute.Int("stage.audio.downstream_frames", m.audioDownFrames),
		attribute.Int("stage.input.text_length", len([]rune(m.inputText))),
		attribute.Int("realtime.partial.count", m.partialCount),
		attribute.Int("realtime.interrupt.count", m.interruptCount),
		attribute.Int("realtime.asr_restart.count", m.asrRestartCount),
		attribute.Int("realtime.auto_resume.count", m.autoResumeCount),
	}

	if len(m.inputText) > 0 {
		const maxPreview = 120
		preview := m.inputText
		if len([]rune(preview)) > maxPreview {
			runes := []rune(preview)
			preview = string(runes[:maxPreview]) + "..."
		}
		attrs = append(attrs, attribute.String("stage.input.preview", preview))
	}

	output := m.outputBuilder.String()
	if output != "" {
		attrs = append(attrs,
			attribute.Int("stage.output.text_length", len([]rune(output))),
			attribute.String("stage.output.preview", truncateForEvent(output, 200)),
		)
	}

	if !m.lastUpstreamAudio.IsZero() {
		attrs = append(attrs, attribute.String("stage.audio.last_upstream_at", m.lastUpstreamAudio.Format(time.RFC3339Nano)))
	}
	if !m.firstDownAudio.IsZero() {
		attrs = append(attrs, attribute.String("stage.audio.first_downstream_at", m.firstDownAudio.Format(time.RFC3339Nano)))
	}
	if !m.lastUpstreamAudio.IsZero() && !m.firstDownAudio.IsZero() {
		latency := m.firstDownAudio.Sub(m.lastUpstreamAudio)
		if latency < 0 {
			latency = 0
		}
		attrs = append(attrs, attribute.Int64("stage.audio.response_latency_ms", latency.Milliseconds()))
	}

	m.span.SetAttributes(attrs...)

	for _, errMsg := range m.errors {
		m.span.AddEvent("stage.error", trace.WithAttributes(attribute.String("error.message", errMsg)))
	}
}

func (m *ConversationMetrics) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	errorsCopy := make([]string, len(m.errors))
	copy(errorsCopy, m.errors)

	output := m.outputBuilder.String()
	partialCount := m.partialCount
	interruptCount := m.interruptCount
	asrRestartCount := m.asrRestartCount
	autoResumeCount := m.autoResumeCount
	segmentedOutput := ""
	if len(m.outputSegments) > 0 {
		segmentedOutput = strings.Join(m.outputSegments, outputSentenceSeparator)
	}

	latency := time.Duration(0)
	if !m.lastUpstreamAudio.IsZero() && !m.firstDownAudio.IsZero() {
		latency = m.firstDownAudio.Sub(m.lastUpstreamAudio)
		if latency < 0 {
			latency = 0
		}
	}

	return Snapshot{
		RoundID:           m.roundID,
		InputPreview:      truncateForEvent(m.inputText, 60),
		TotalDuration:     time.Since(m.start),
		AudioDuration:     m.audioDuration,
		LastUpAudio:       m.lastUpstreamAudio,
		FirstDownAudio:    m.firstDownAudio,
		ResponseLatency:   latency,
		AudioBytes:        m.audioBytes,
		LLMDuration:       m.llmDuration,
		ToolDuration:      m.toolDuration,
		ToolCalls:         m.toolCalls,
		TTSDuration:       m.ttsDuration,
		AudioDownDuration: m.audioDownDuration,
		AudioDownBytes:    m.audioDownBytes,
		AudioDownFrames:   m.audioDownFrames,
		Errors:            errorsCopy,
		OutputText:        segmentedOutput,
		OutputPreview:     truncateForEvent(output, 200),
		PartialCount:      partialCount,
		InterruptCount:    interruptCount,
		AsrRestartCount:   asrRestartCount,
		AutoResumeCount:   autoResumeCount,
	}
}

func truncateForEvent(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
