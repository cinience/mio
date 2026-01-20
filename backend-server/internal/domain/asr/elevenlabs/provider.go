package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/LinkedDestiny/elevenlabs-golang/pkg/elevenlabs"

	"backend-server/constants"
	"backend-server/internal/domain/asr/types"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/asr"
	"backend-server/pkg/confighelper"
)

const (
	defaultAsrModelID     = "scribe_v1"
	defaultAsrSampleRate  = 16000
	defaultAsrTimeoutSec  = 60
	fileFormatPCM16LE16   = "pcm_s16le_16"
	speechToTextEndpoint  = "/v1/speech-to-text"
	defaultStreamingFmt   = fileFormatPCM16LE16
	defaultTimestampsMode = "word"
)

type providerConfig struct {
	APIKey         string
	BaseURL        string
	Region         string
	ModelID        string
	LanguageCode   string
	FileFormat     string
	TimestampsMode string
	EnableLogging  bool
	Timeout        time.Duration
	SampleRate     int
}

type Provider struct {
	cfg        providerConfig
	httpClient *http.Client
	baseURL    string
}

// init registers the ElevenLabs ASR provider.
func init() {
	asr.Register([]string{"elevenlabs", constants.AsrTypeElevenLabs}, "ElevenLabs speech-to-text (scribe)", func(config map[string]interface{}) (asr.BaseASRProvider, error) {
		return NewProvider(config)
	})
}

func NewProvider(config map[string]interface{}) (*Provider, error) {
	helper := confighelper.New(config)
	validator := confighelper.NewValidator()

	apiKey := helper.GetString("api_key")
	validator.RequireString(helper, "api_key", "api_key")

	modelID := helper.GetString("model_id", defaultAsrModelID)
	languageCode := helper.GetString("language_code")
	fileFormat := helper.GetString("file_format", defaultStreamingFmt)
	timestampsMode := helper.GetString("timestamps_granularity", defaultTimestampsMode)
	if timestampsMode == "" {
		timestampsMode = defaultTimestampsMode
	}

	timeoutSeconds := helper.GetInt("timeout_seconds", defaultAsrTimeoutSec)
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultAsrTimeoutSec
	}

	sampleRate := helper.GetInt("sample_rate", defaultAsrSampleRate)
	if sampleRate <= 0 {
		sampleRate = defaultAsrSampleRate
	}

	if validator.HasErrors() {
		return nil, validator.FirstError()
	}

	baseURL, wsURL := resolveBaseURLs(helper.GetString("base_url"), helper.GetString("region"))
	_ = wsURL // reserved for future realtime usage

	cfg := providerConfig{
		APIKey:         apiKey,
		BaseURL:        baseURL,
		Region:         helper.GetString("region"),
		ModelID:        modelID,
		LanguageCode:   languageCode,
		FileFormat:     fileFormat,
		TimestampsMode: timestampsMode,
		EnableLogging:  helper.GetBool("enable_logging", true),
		Timeout:        time.Duration(timeoutSeconds) * time.Second,
		SampleRate:     sampleRate,
	}

	return &Provider{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: cfg.Timeout},
		baseURL:    baseURL,
	}, nil
}

// Process performs a one-shot transcription request.
func (p *Provider) Process(pcmData []float32) (string, error) {
	if len(pcmData) == 0 {
		return "", fmt.Errorf("elevenlabs asr: no pcm data provided")
	}

	pcmBytes := p.float32ToPCMBytes(pcmData)
	text, err := p.doTranscribe(context.Background(), pcmBytes)
	if err != nil {
		return "", err
	}
	return text, nil
}

// StreamingRecognize buffers the incoming frames then posts them to ElevenLabs STT.
func (p *Provider) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	resultChan := make(chan types.StreamingResult, 1)

	go func() {
		defer close(resultChan)

		var pcmBuffer bytes.Buffer
		for {
			select {
			case <-ctx.Done():
				resultChan <- types.StreamingResult{Error: ctx.Err()}
				return
			case frame, ok := <-audioStream:
				if !ok {
					if pcmBuffer.Len() == 0 {
						resultChan <- types.StreamingResult{Error: fmt.Errorf("elevenlabs asr: empty audio stream")}
						return
					}

					text, err := p.doTranscribe(ctx, pcmBuffer.Bytes())
					if err != nil {
						resultChan <- types.StreamingResult{Error: err}
						return
					}

					resultChan <- types.StreamingResult{
						Text:    text,
						IsFinal: true,
					}
					return
				}

				if len(frame) == 0 {
					continue
				}

				pcmBytes := p.float32ToPCMBytes(frame)
				if len(pcmBytes) > 0 {
					if _, err := pcmBuffer.Write(pcmBytes); err != nil {
						resultChan <- types.StreamingResult{Error: fmt.Errorf("elevenlabs asr: buffer write failed: %w", err)}
						return
					}
				}
			}
		}
	}()

	return resultChan, nil
}

func (p *Provider) doTranscribe(ctx context.Context, pcmBytes []byte) (string, error) {
	reqCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, p.cfg.Timeout)
		defer cancel()
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if err := writer.WriteField("model_id", p.cfg.ModelID); err != nil {
		return "", fmt.Errorf("elevenlabs asr: write model_id failed: %w", err)
	}
	if p.cfg.LanguageCode != "" {
		_ = writer.WriteField("language_code", p.cfg.LanguageCode)
	}
	if p.cfg.FileFormat != "" {
		_ = writer.WriteField("file_format", p.cfg.FileFormat)
	}
	if p.cfg.TimestampsMode != "" {
		_ = writer.WriteField("timestamps_granularity", p.cfg.TimestampsMode)
	}
	_ = writer.WriteField("enable_logging", fmt.Sprintf("%t", p.cfg.EnableLogging))

	filePart, err := writer.CreateFormFile("file", "audio.pcm")
	if err != nil {
		return "", fmt.Errorf("elevenlabs asr: create form file failed: %w", err)
	}
	if _, err := filePart.Write(pcmBytes); err != nil {
		return "", fmt.Errorf("elevenlabs asr: write audio payload failed: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("elevenlabs asr: close multipart writer failed: %w", err)
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.baseURL+speechToTextEndpoint, body)
	if err != nil {
		return "", fmt.Errorf("elevenlabs asr: create request failed: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("xi-api-key", p.cfg.APIKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("elevenlabs asr: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("elevenlabs asr: read response failed: %w", err)
	}

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("elevenlabs asr: http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	text := extractTranscript(respBytes)
	if text == "" {
		log.Warnf("elevenlabs asr: empty transcript, raw response: %s", strings.TrimSpace(string(respBytes)))
	}

	return text, nil
}

func extractTranscript(resp []byte) string {
	var parsed struct {
		Text        string `json:"text"`
		Transcripts []struct {
			Text string `json:"text"`
		} `json:"transcripts"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return ""
	}

	if parsed.Text != "" {
		return parsed.Text
	}
	if len(parsed.Transcripts) > 0 {
		for _, t := range parsed.Transcripts {
			if t.Text != "" {
				return t.Text
			}
		}
	}
	return ""
}

func (p *Provider) float32ToPCMBytes(samples []float32) []byte {
	pcm := make([]byte, len(samples)*2)
	for i, sample := range samples {
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}

		intSample := int16(sample * 32767)
		pcm[2*i] = byte(intSample)
		pcm[2*i+1] = byte(intSample >> 8)
	}
	return pcm
}

func resolveBaseURLs(customBaseURL, region string) (string, string) {
	if customBaseURL != "" {
		return strings.TrimSuffix(customBaseURL, "/"), toWSURL(customBaseURL)
	}

	switch strings.ToLower(region) {
	case "us":
		return elevenlabs.ProductionUSEnv.BaseURL, elevenlabs.ProductionUSEnv.WebSocketURL
	case "eu":
		return elevenlabs.ProductionEUEnv.BaseURL, elevenlabs.ProductionEUEnv.WebSocketURL
	default:
		return elevenlabs.ProductionEnv.BaseURL, elevenlabs.ProductionEnv.WebSocketURL
	}
}

func toWSURL(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	}
	if strings.HasPrefix(httpURL, "http://") {
		return "ws://" + strings.TrimPrefix(httpURL, "http://")
	}
	return httpURL
}
