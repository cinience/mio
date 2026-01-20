package aliyun

import (
	"time"
)

// Config 阿里云ASR配置
type Config struct {
	AccessKeyID        string `json:"access_key_id"`
	AccessKeySecret    string `json:"access_key_secret"`
	AppKey             string `json:"appkey"`
	Token              string `json:"token"`
	Host               string `json:"host"`
	MaxSentenceSilence int    `json:"max_sentence_silence"`
}

// TokenInfo Token信息
type TokenInfo struct {
	Token      string
	ExpireTime time.Time
}

// ASRRequest 阿里云ASR请求
type ASRRequest struct {
	Header  RequestHeader  `json:"header"`
	Payload RequestPayload `json:"payload"`
}

// RequestHeader 请求头
type RequestHeader struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Status     int    `json:"status"`
	MessageID  string `json:"message_id"`
	TaskID     string `json:"task_id"`
	StatusText string `json:"status_text"`
	AppKey     string `json:"appkey"`
}

// RequestPayload 请求载荷
type RequestPayload struct {
	Format                         string `json:"format"`
	SampleRate                     int    `json:"sample_rate"`
	EnableIntermediateResult       bool   `json:"enable_intermediate_result"`
	EnablePunctuationPrediction    bool   `json:"enable_punctuation_prediction"`
	EnableInverseTextNormalization bool   `json:"enable_inverse_text_normalization"`
	MaxSentenceSilence             int    `json:"max_sentence_silence"`
	EnableVoiceDetection           bool   `json:"enable_voice_detection"`
}

// ASRResponse 阿里云ASR响应
type ASRResponse struct {
	Header  ResponseHeader  `json:"header"`
	Payload ResponsePayload `json:"payload"`
}

// ResponseHeader 响应头
type ResponseHeader struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Status     int    `json:"status"`
	MessageID  string `json:"message_id"`
	TaskID     string `json:"task_id"`
	StatusText string `json:"status_text"`
}

// ResponsePayload 响应载荷
type ResponsePayload struct {
	Result     string  `json:"result"`
	Index      int     `json:"index"`
	Time       int64   `json:"time"`
	Stash      string  `json:"stash"`
	Confidence float64 `json:"confidence"`
}

// ConnectionState 连接状态
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateReady
	StateProcessing
	StateClosing
)

// ASRClient 阿里云ASR客户端接口
type ASRClient interface {
	Connect() error
	Disconnect() error
	SendAudio(data []byte) error
	GetResult() <-chan string
	GetError() <-chan error
	IsReady() bool
	Close() error
}
