package constants

const (
	VadTypeWebRTCVad = "webrtc_vad"
	VadTypeSherpaVad = "sherpa_vad"
	// VadTypeSileroVad 保留旧的字符串常量以保持向后兼容，内部实现统一由 sherpa_vad 提供
	VadTypeSileroVad = "silero_vad"
)

const (
	AsrTypeFunAsr         = "funasr"
	AsrTypeDoubao         = "doubao"
	AsrTypeAliyun         = "aliyun"
	AsrTypeSherpa         = "sherpa"
	AsrTypeOpenAIRealtime = "openai_realtime"
	AsrTypeElevenLabs     = "elevenlabs"
	AsrTypeGoogleGenAI    = "google_genai"
)

const (
	LlmTypeOpenai  = "openai"
	LlmTypeOllama  = "ollama"
	LlmTypeEinoLLM = "eino_llm"
	LlmTypeEino    = "eino"
)

const (
	TtsTypeDoubao         = "doubao"
	TtsTypeDoubaoWS       = "doubao_ws"
	TtsTypeCosyvoice      = "cosyvoice"
	TtsTypeEdge           = "edge"
	TtsTypeEdgeOffline    = "edge_offline"
	TtsTypeXiaozhi        = "xiaozhi"
	TtsTypeGptSovitsV2    = "gpt_sovits_v2"
	TtsTypeGptSovitsV3    = "gpt_sovits_v3"
	TtsTypeIndexTTSStream = "indextts_stream"
	TtsTypeSherpaOnnx     = "sherpa_onnx"
	TtsTypeOpenAIRealtime = "openai_realtime"
	TtsTypeElevenLabs     = "elevenlabs"
	TtsTypeGoogleGenAI    = "google_genai"
)
