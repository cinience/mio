package state

import (
	"sync/atomic"
	"time"

	log "backend-server/internal/infrastructure/logger"
)

type Statistic struct {
	AsrStartTs           atomic.Int64 //asr开始时间
	AsrEndTs             atomic.Int64 //asr结束时间
	LlmStartTs           atomic.Int64 //llm开始时间
	LlmFirstTokenTs      atomic.Int64 //llm首token时间
	LlmFirstChunkTs      atomic.Int64 //llm首个响应片段时间
	LlmFirstTextTs       atomic.Int64 //llm首个文本片段时间
	TtsStartTs           atomic.Int64 //tts开始时间
	TtsFirstChunkTs      atomic.Int64 //tts首帧发送时间
	TtsDecoderReadyTs    atomic.Int64 //tts首帧解码完成时间
	FirstFrameTraceLogged atomic.Bool //首帧链路耗时是否已记录
	AsrToLlmLogged       atomic.Bool //asr_end->llm_start耗时是否已记录
	LlmFirstTokenLogged  atomic.Bool //llm_start->llm_first_token耗时是否已记录
	LlmFirstChunkLogged  atomic.Bool //llm_start->llm_first_chunk耗时是否已记录
	LlmFirstTextLogged   atomic.Bool //llm_start->llm_first_text耗时是否已记录
	LlmFirstTextToTtsFirstLogged atomic.Bool //llm_first_text->tts_first耗时是否已记录
	LlmToTtsFirstLogged  atomic.Bool //llm_start->tts_first耗时是否已记录
	TtsFirstToDecoderLogged atomic.Bool //tts_first->decoder_ready耗时是否已记录
}

func (s *Statistic) Reset() {
	s.AsrStartTs.Store(0)
	s.AsrEndTs.Store(0)
	s.LlmStartTs.Store(0)
	s.LlmFirstTokenTs.Store(0)
	s.LlmFirstChunkTs.Store(0)
	s.LlmFirstTextTs.Store(0)
	s.TtsStartTs.Store(0)
	s.TtsFirstChunkTs.Store(0)
	s.TtsDecoderReadyTs.Store(0)
	s.FirstFrameTraceLogged.Store(false)
	s.AsrToLlmLogged.Store(false)
	s.LlmFirstTokenLogged.Store(false)
	s.LlmFirstChunkLogged.Store(false)
	s.LlmFirstTextLogged.Store(false)
	s.LlmFirstTextToTtsFirstLogged.Store(false)
	s.LlmToTtsFirstLogged.Store(false)
	s.TtsFirstToDecoderLogged.Store(false)
}

func (c *ClientState) SetStartAsrTs() {
	c.Statistic.Reset()
	c.Statistic.AsrStartTs.Store(time.Now().UnixMilli())
}

func (c *ClientState) GetAsrDuration() int64 {
	start := c.Statistic.AsrStartTs.Load()
	if start == 0 {
		return 0
	}
	return time.Now().UnixMilli() - start
}

func (c *ClientState) GetAsrLlmTtsDuration() int64 {
	start := c.Statistic.AsrStartTs.Load()
	if start == 0 {
		return 0
	}
	return time.Now().UnixMilli() - start
}

func (c *ClientState) SetStartLlmTs() {
	c.Statistic.LlmStartTs.Store(time.Now().UnixMilli())
}

func (c *ClientState) GetLlmDuration() int64 {
	start := c.Statistic.LlmStartTs.Load()
	if start == 0 {
		return 0
	}
	return time.Now().UnixMilli() - start
}

func (c *ClientState) SetStartTtsTs() {
	c.Statistic.TtsStartTs.Store(time.Now().UnixMilli())
}

func (c *ClientState) GetTtsDuration() int64 {
	start := c.Statistic.TtsStartTs.Load()
	if start == 0 {
		return 0
	}
	return time.Now().UnixMilli() - start
}

func (c *ClientState) MarkAsrEndTs() {
	if c.Statistic.AsrEndTs.CompareAndSwap(0, time.Now().UnixMilli()) {
		c.tryLogAsrToLlm()
	}
}

func (c *ClientState) MarkLlmStartTs() {
	if c.Statistic.LlmStartTs.CompareAndSwap(0, time.Now().UnixMilli()) {
		c.tryLogAsrToLlm()
		c.tryLogLlmFirstToken()
		c.tryLogLlmFirstChunk()
		c.tryLogLlmFirstText()
		c.tryLogLlmToTtsFirst()
	}
}

func (c *ClientState) MarkLlmFirstTokenTs() {
	if c.Statistic.LlmFirstTokenTs.CompareAndSwap(0, time.Now().UnixMilli()) {
		c.tryLogLlmFirstToken()
	}
}

func (c *ClientState) MarkLlmFirstChunkTs() {
	if c.Statistic.LlmFirstChunkTs.CompareAndSwap(0, time.Now().UnixMilli()) {
		c.tryLogLlmFirstChunk()
	}
}

func (c *ClientState) MarkLlmFirstTextTs() {
	if c.Statistic.LlmFirstTextTs.CompareAndSwap(0, time.Now().UnixMilli()) {
		c.tryLogLlmFirstText()
		c.tryLogLlmFirstTextToTtsFirst()
	}
}

func (c *ClientState) MarkTtsFirstChunkTs() {
	if c.Statistic.TtsFirstChunkTs.CompareAndSwap(0, time.Now().UnixMilli()) {
		c.tryLogLlmFirstTextToTtsFirst()
		c.tryLogLlmToTtsFirst()
		c.tryLogTtsFirstToDecoder()
		c.tryLogFirstFrameTrace()
	}
}

func (c *ClientState) MarkTtsDecoderReadyTs() {
	if c.Statistic.TtsDecoderReadyTs.CompareAndSwap(0, time.Now().UnixMilli()) {
		c.tryLogTtsFirstToDecoder()
		c.tryLogFirstFrameTrace()
	}
}

func (c *ClientState) tryLogFirstFrameTrace() {
	if c == nil || c.Statistic.FirstFrameTraceLogged.Load() {
		return
	}

	asrEnd := c.Statistic.AsrEndTs.Load()
	llmStart := c.Statistic.LlmStartTs.Load()
	ttsFirst := c.Statistic.TtsFirstChunkTs.Load()
	decoderReady := c.Statistic.TtsDecoderReadyTs.Load()

	if asrEnd == 0 || llmStart == 0 || ttsFirst == 0 || decoderReady == 0 {
		return
	}
	if asrEnd > llmStart || llmStart > ttsFirst || ttsFirst > decoderReady {
		return
	}
	if !c.Statistic.FirstFrameTraceLogged.CompareAndSwap(false, true) {
		return
	}

	log.Infof(
		"首帧链路耗时: asr_end->llm_start=%dms, llm_start->tts_first=%dms, tts_first->decoder_ready=%dms, total=%dms (device=%s, session=%s)",
		llmStart-asrEnd,
		ttsFirst-llmStart,
		decoderReady-ttsFirst,
		decoderReady-asrEnd,
		c.DeviceID,
		c.SessionID,
	)
}

func (c *ClientState) tryLogAsrToLlm() {
	if c == nil || c.Statistic.AsrToLlmLogged.Load() {
		return
	}
	asrEnd := c.Statistic.AsrEndTs.Load()
	llmStart := c.Statistic.LlmStartTs.Load()
	if asrEnd == 0 || llmStart == 0 || asrEnd > llmStart {
		return
	}
	if !c.Statistic.AsrToLlmLogged.CompareAndSwap(false, true) {
		return
	}
	log.Infof(
		"首帧链路耗时片段: asr_end->llm_start=%dms (device=%s, session=%s)",
		llmStart-asrEnd,
		c.DeviceID,
		c.SessionID,
	)
}

func (c *ClientState) tryLogLlmFirstToken() {
	if c == nil || c.Statistic.LlmFirstTokenLogged.Load() {
		return
	}
	llmStart := c.Statistic.LlmStartTs.Load()
	llmFirst := c.Statistic.LlmFirstTokenTs.Load()
	if llmStart == 0 || llmFirst == 0 || llmStart > llmFirst {
		return
	}
	if !c.Statistic.LlmFirstTokenLogged.CompareAndSwap(false, true) {
		return
	}
	log.Infof(
		"首帧链路耗时片段: llm_start->llm_first_token=%dms (device=%s, session=%s)",
		llmFirst-llmStart,
		c.DeviceID,
		c.SessionID,
	)
}

func (c *ClientState) tryLogLlmFirstChunk() {
	if c == nil || c.Statistic.LlmFirstChunkLogged.Load() {
		return
	}
	llmStart := c.Statistic.LlmStartTs.Load()
	llmFirst := c.Statistic.LlmFirstChunkTs.Load()
	if llmStart == 0 || llmFirst == 0 || llmStart > llmFirst {
		return
	}
	if !c.Statistic.LlmFirstChunkLogged.CompareAndSwap(false, true) {
		return
	}
	log.Infof(
		"首帧链路耗时片段: llm_start->llm_first_chunk=%dms (device=%s, session=%s)",
		llmFirst-llmStart,
		c.DeviceID,
		c.SessionID,
	)
}

func (c *ClientState) tryLogLlmFirstText() {
	if c == nil || c.Statistic.LlmFirstTextLogged.Load() {
		return
	}
	llmStart := c.Statistic.LlmStartTs.Load()
	llmFirstText := c.Statistic.LlmFirstTextTs.Load()
	if llmStart == 0 || llmFirstText == 0 || llmStart > llmFirstText {
		return
	}
	if !c.Statistic.LlmFirstTextLogged.CompareAndSwap(false, true) {
		return
	}
	log.Infof(
		"首帧链路耗时片段: llm_start->llm_first_text=%dms (device=%s, session=%s)",
		llmFirstText-llmStart,
		c.DeviceID,
		c.SessionID,
	)
}

func (c *ClientState) tryLogLlmToTtsFirst() {
	if c == nil || c.Statistic.LlmToTtsFirstLogged.Load() {
		return
	}
	llmStart := c.Statistic.LlmStartTs.Load()
	ttsFirst := c.Statistic.TtsFirstChunkTs.Load()
	if llmStart == 0 || ttsFirst == 0 || llmStart > ttsFirst {
		return
	}
	if !c.Statistic.LlmToTtsFirstLogged.CompareAndSwap(false, true) {
		return
	}
	log.Infof(
		"首帧链路耗时片段: llm_start->tts_first=%dms (device=%s, session=%s)",
		ttsFirst-llmStart,
		c.DeviceID,
		c.SessionID,
	)
}

func (c *ClientState) tryLogLlmFirstTextToTtsFirst() {
	if c == nil || c.Statistic.LlmFirstTextToTtsFirstLogged.Load() {
		return
	}
	llmFirstText := c.Statistic.LlmFirstTextTs.Load()
	ttsFirst := c.Statistic.TtsFirstChunkTs.Load()
	if llmFirstText == 0 || ttsFirst == 0 || llmFirstText > ttsFirst {
		return
	}
	if !c.Statistic.LlmFirstTextToTtsFirstLogged.CompareAndSwap(false, true) {
		return
	}
	log.Infof(
		"首帧链路耗时片段: llm_first_text->tts_first=%dms (device=%s, session=%s)",
		ttsFirst-llmFirstText,
		c.DeviceID,
		c.SessionID,
	)
}

func (c *ClientState) tryLogTtsFirstToDecoder() {
	if c == nil || c.Statistic.TtsFirstToDecoderLogged.Load() {
		return
	}
	ttsFirst := c.Statistic.TtsFirstChunkTs.Load()
	decoderReady := c.Statistic.TtsDecoderReadyTs.Load()
	if ttsFirst == 0 || decoderReady == 0 || ttsFirst > decoderReady {
		return
	}
	if !c.Statistic.TtsFirstToDecoderLogged.CompareAndSwap(false, true) {
		return
	}
	log.Infof(
		"首帧链路耗时片段: tts_first->decoder_ready=%dms (device=%s, session=%s)",
		decoderReady-ttsFirst,
		c.DeviceID,
		c.SessionID,
	)
}
