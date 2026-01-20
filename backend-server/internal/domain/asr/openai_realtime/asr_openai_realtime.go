package openai_realtime

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/gorilla/websocket"

	"backend-server/internal/domain/asr/types"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/asr"
	"backend-server/pkg/audio"
	"backend-server/pkg/confighelper"
)

const (
	defaultAsrSampleRate    = 16000
	defaultAsrFrameDuration = 20
	defaultResponseTimeout  = 10 // 默认响应超时时间（秒）
	defaultCommitTimeout    = 5  // 默认 commit 超时时间（秒）
)

type providerConfig struct {
	URL             string
	AuthToken       string
	SampleRate      int
	FrameDuration   int
	ResponseTimeout int // 响应超时时间（秒）
	CommitTimeout   int // commit 超时时间（秒）
}

// Provider 实现基于 speech-server 的 OpenAI Realtime ASR
type Provider struct {
	config providerConfig
}

// NewProvider 创建新的 OpenAI Realtime ASR Provider
func NewProvider(config map[string]interface{}) (*Provider, error) {
	helper := confighelper.New(config)
	validator := confighelper.NewValidator()

	validator.RequireString(helper, "url", "OpenAI Realtime ASR 服务地址")
	if validator.HasErrors() {
		return nil, validator.FirstError()
	}

	cfg := providerConfig{
		URL:             helper.GetString("url"),
		AuthToken:       helper.GetString("auth_token"),
		SampleRate:      helper.GetInt("sample_rate", defaultAsrSampleRate),
		FrameDuration:   helper.GetInt("frame_duration", defaultAsrFrameDuration),
		ResponseTimeout: helper.GetInt("response_timeout", defaultResponseTimeout),
		CommitTimeout:   helper.GetInt("commit_timeout", defaultCommitTimeout),
	}

	if cfg.SampleRate <= 0 {
		cfg.SampleRate = defaultAsrSampleRate
	}
	if cfg.FrameDuration == 0 {
		cfg.FrameDuration = defaultAsrFrameDuration
	}
	if cfg.ResponseTimeout <= 0 {
		cfg.ResponseTimeout = defaultResponseTimeout
	}
	if cfg.CommitTimeout <= 0 {
		cfg.CommitTimeout = defaultCommitTimeout
	}

	return &Provider{config: cfg}, nil
}

// Process 暂未实现一次性识别
func (p *Provider) Process(pcmData []float32) (string, error) {
	return "", fmt.Errorf("OpenAI Realtime ASR 暂不支持一次性识别，请使用流式接口")
}

// StreamingRecognize 实现流式语音识别
func (p *Provider) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	streamCtx, cancel, conn, err := p.connectWithFallback(ctx, openairt.WithIntent())
	if err != nil {
		return nil, fmt.Errorf("连接OpenAI Realtime ASR失败: %w", err)
	}

	if err := p.sendSessionUpdate(streamCtx, conn); err != nil {
		_ = conn.Close()
		cancel()
		return nil, fmt.Errorf("初始化OpenAI Realtime ASR会话失败: %w", err)
	}

	resultChan := make(chan types.StreamingResult, 10)
	sendErrCh := make(chan error, 1)
	var once sync.Once
	closeConnection := func() {
		once.Do(func() {
			cancel()
			_ = conn.Close()
		})
	}

	go p.consumeAudio(streamCtx, conn, audioStream, sendErrCh)
	go p.receiveResults(streamCtx, conn, resultChan, sendErrCh, closeConnection)

	return resultChan, nil
}

func (p *Provider) sendSessionUpdate(ctx context.Context, conn *openairt.Conn) error {
	update := openairt.SessionUpdateEvent{
		Session: openairt.SessionUnion{
			Transcription: &openairt.TranscriptionSession{
				Audio: &openairt.TranscriptionSessionAudio{
					Input: &openairt.SessionAudioInput{
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: p.config.SampleRate},
						},
						Transcription: &openairt.AudioTranscription{
							Language: "auto",
						},
					},
				},
			},
		},
	}
	return conn.SendMessage(ctx, update)
}

func (p *Provider) consumeAudio(ctx context.Context, conn *openairt.Conn, audioStream <-chan []float32, errCh chan<- error) {
	defer close(errCh)

	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-audioStream:
			if !ok {
				// 音频结束，提交缓冲区（使用超时机制）
				log.Debug("OpenAI Realtime ASR 音频流结束，提交缓冲区")

				commitCtx, cancel := context.WithTimeout(ctx, time.Duration(p.config.CommitTimeout)*time.Second)
				defer cancel()

				err := conn.SendMessage(commitCtx, openairt.InputAudioBufferCommitEvent{})
				if err != nil {
					if commitCtx.Err() == context.DeadlineExceeded {
						errCh <- fmt.Errorf("提交OpenAI Realtime ASR音频超时(%ds)，可能服务端无响应", p.config.CommitTimeout)
					} else {
						errCh <- fmt.Errorf("提交OpenAI Realtime ASR音频失败: %w", err)
					}
				}
				return
			}
			if len(frame) == 0 {
				continue
			}

			pcmBytes := p.float32ToPCMBytes(frame)
			if len(pcmBytes) == 0 {
				continue
			}

			payload := base64.StdEncoding.EncodeToString(pcmBytes)
			err := conn.SendMessage(ctx, openairt.InputAudioBufferAppendEvent{
				Audio: payload,
			})
			if err != nil {
				errCh <- fmt.Errorf("发送OpenAI Realtime ASR音频块失败: %w", err)
				return
			}
		}
	}
}

func (p *Provider) receiveResults(ctx context.Context, conn *openairt.Conn, resultChan chan types.StreamingResult, errCh <-chan error, closeFn func()) {
	defer closeFn()
	defer close(resultChan)

	// 使用超时定时器防止无限等待
	responseTimeout := time.Duration(p.config.ResponseTimeout) * time.Second
	timer := time.NewTimer(responseTimeout)
	defer timer.Stop()

	// 用于跟踪是否正在等待 commit 响应
	waitingForCommit := false
	commitTimer := time.NewTimer(time.Duration(p.config.CommitTimeout) * time.Second)
	commitTimer.Stop() // 初始状态不启动
	defer commitTimer.Stop()

	for {
		// 重置超时定时器
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(responseTimeout)

		select {
		case <-ctx.Done():
			return
		case err, ok := <-errCh:
			if ok && err != nil {
				resultChan <- types.StreamingResult{Error: err}
				return
			}
			if !ok {
				// 音频流结束，开始等待 commit 响应
				waitingForCommit = true
				commitTimer.Reset(time.Duration(p.config.CommitTimeout) * time.Second)
				errCh = nil
			}
		case <-timer.C:
			// 响应超时，返回空结果让系统继续
			log.Warnf("OpenAI Realtime ASR 响应超时(%ds)，可能服务端无响应，返回空结果", p.config.ResponseTimeout)
			resultChan <- types.StreamingResult{
				Text:    "",
				IsFinal: true,
			}
			return
		case <-commitTimer.C:
			if waitingForCommit {
				// commit 超时，服务端可能过滤了音频或出现问题，返回空结果
				log.Warnf("OpenAI Realtime ASR commit 响应超时(%ds)，服务端可能过滤了音频，返回空结果", p.config.CommitTimeout)
				resultChan <- types.StreamingResult{
					Text:    "",
					IsFinal: true,
				}
				return
			}
		default:
		}

		// 直接读取消息，使用 select 的超时机制控制
		event, err := conn.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				// Context 被取消，正常退出
				return
			}
			if isNormalRealtimeClose(err) {
				log.Infof("OpenAI Realtime ASR 连接已关闭: %v", err)
				resultChan <- types.StreamingResult{
					Text:    "",
					IsFinal: true,
				}
				return
			}
			// 其他错误，返回错误并关闭连接
			resultChan <- types.StreamingResult{Error: fmt.Errorf("读取OpenAI Realtime ASR事件失败: %w", err)}
			return
		}

		switch evt := event.(type) {
		case openairt.ConversationItemInputAudioTranscriptionDeltaEvent:
			delta := strings.TrimSpace(evt.Delta)
			if delta == "" {
				continue
			}
			resultChan <- types.StreamingResult{
				Text:    delta,
				IsFinal: false,
			}
		case openairt.ConversationItemInputAudioTranscriptionCompletedEvent:
			transcript := strings.TrimSpace(evt.Transcript)
			resultChan <- types.StreamingResult{
				Text:    transcript,
				IsFinal: true,
			}
			return
		case openairt.InputAudioBufferClearedEvent:
			// 收到 cleared 事件说明服务端过滤了音频或处理完成
			log.Debug("OpenAI Realtime ASR 收到 InputAudioBufferCleared 事件")
			if waitingForCommit {
				// 返回空结果，让系统继续
				resultChan <- types.StreamingResult{
					Text:    "",
					IsFinal: true,
				}
				return
			}
		case openairt.ErrorEvent:
			message := evt.Error.Message
			if message == "" {
				message = "speech-server 返回未知错误"
			}
			resultChan <- types.StreamingResult{
				Error: fmt.Errorf("OpenAI Realtime ASR 错误: %s", message),
			}
			return
		case openairt.InputAudioBufferCommittedEvent:
			log.Debugf("OpenAI Realtime ASR 缓冲提交成功, itemID=%s", evt.ItemID)
			waitingForCommit = false
			commitTimer.Stop()
		default:
			// 其他事件仅记录调试日志
			log.Debugf("OpenAI Realtime ASR 忽略事件: %s", evt.ServerEventType())
		}
	}
}

func (p *Provider) connectWithFallback(ctx context.Context, opts ...openairt.ConnectOption) (context.Context, context.CancelFunc, *openairt.Conn, error) {
	streamCtx, cancel, conn, err := p.connectOnce(ctx, p.config.URL, opts...)
	if err == nil {
		return streamCtx, cancel, conn, nil
	}

	if !shouldFallbackToLocalhost(p.config.URL) {
		return nil, nil, nil, err
	}

	fallbackURL, parseErr := switchHostToLocalhost(p.config.URL)
	if parseErr != nil {
		return nil, nil, nil, err
	}

	log.Warnf("连接OpenAI Realtime ASR地址 %s 失败，自动替换 host 为 localhost 重试: %v", p.config.URL, err)

	streamCtx, cancel, conn, retryErr := p.connectOnce(ctx, fallbackURL, opts...)
	if retryErr != nil {
		return nil, nil, nil, fmt.Errorf("fallback to localhost failed: %w (original error: %v)", retryErr, err)
	}

	return streamCtx, cancel, conn, nil
}

func isNormalRealtimeClose(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		switch closeErr.Code {
		case websocket.CloseNormalClosure,
			websocket.CloseGoingAway,
			websocket.CloseAbnormalClosure,
			websocket.CloseNoStatusReceived:
			return true
		}
	}
	return false
}

func (p *Provider) connectOnce(ctx context.Context, baseURL string, opts ...openairt.ConnectOption) (context.Context, context.CancelFunc, *openairt.Conn, error) {
	streamCtx, cancel := context.WithCancel(ctx)

	clientCfg := openairt.DefaultConfig(p.config.AuthToken)
	clientCfg.BaseURL = baseURL
	client := openairt.NewClientWithConfig(clientCfg)

	conn, err := client.Connect(streamCtx, opts...)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}

	return streamCtx, cancel, conn, nil
}

func shouldFallbackToLocalhost(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Hostname() == "example.localhost"
}

func switchHostToLocalhost(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	port := parsed.Port()
	if port != "" {
		parsed.Host = net.JoinHostPort("localhost", port)
	} else {
		parsed.Host = "localhost"
	}
	return parsed.String(), nil
}

func (p *Provider) float32ToPCMBytes(samples []float32) []byte {
	int16Samples := audio.Float32SliceToInt16Slice(samples)
	return audio.Int16SliceToBytes(int16Samples)
}

// 确保 Provider 实现 asr.BaseASRProvider 接口
var _ asr.BaseASRProvider = (*Provider)(nil)
