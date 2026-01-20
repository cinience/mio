package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
)

func main() {
	var (
		urlFlag        = flag.String("url", "ws://localhost:8009/v1/realtime", "Realtime websocket URL")
		audioPath      = flag.String("file", "", "Path to WAV/PCM16 audio to stream")
		chunkDurFlag   = flag.Duration("chunk", 120*time.Millisecond, "Chunk duration for streaming")
		threshold      = flag.Float64("threshold", 0.55, "Server VAD activation threshold (0-1)")
		silence        = flag.Duration("silence", 600*time.Millisecond, "Silence duration to finalise a turn")
		prefixPadding  = flag.Duration("prefix-padding", 300*time.Millisecond, "Audio padding forwarded to the detector before speech start")
		language       = flag.String("language", "", "Optional ISO-639-1 language hint")
		vadModel       = flag.String("vad-model", "", "Target VAD model name (matches config.vad.models key); default uses server configuration")
		createResponse = flag.Bool("create-response", false, "Set server_vad.create_response so the server auto-generates replies on speech end")
		interruptResp  = flag.Bool("interrupt-response", false, "Set server_vad.interrupt_response so new speech interrupts ongoing replies")
	)
	flag.Parse()

	if *audioPath == "" {
		log.Fatal("-file is required")
	}
	if *chunkDurFlag <= 0 {
		log.Fatal("chunk duration must be positive")
	}
	if *threshold < 0 || *threshold > 1 {
		log.Fatal("threshold must be between 0 and 1")
	}
	if *prefixPadding < 0 {
		log.Fatal("prefix-padding must be non-negative")
	}

	audio, sampleRate, err := loadAudioClip(*audioPath)
	if err != nil {
		log.Fatalf("load audio: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg := openairt.DefaultConfig("")
	cfg.BaseURL = *urlFlag
	client := openairt.NewClientWithConfig(cfg)

	conn, err := client.Connect(ctx, openairt.WithIntent())
	if err != nil {
		log.Fatalf("connect realtime server: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("close websocket: %v", err)
		}
	}()

	go logEvents(ctx, conn)

	if err := sendTurnDetectionUpdate(ctx, conn, sampleRate, *language, *vadModel, *threshold, *silence, *prefixPadding, *createResponse, *interruptResp); err != nil {
		log.Fatalf("session update: %v", err)
	}

	if err := streamClip(ctx, conn, audio, sampleRate, *chunkDurFlag); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("stream audio: %v", err)
	}

	<-ctx.Done()
}

func loadAudioClip(path string) ([]byte, int, error) {
	data, rate, err := readAudioFile(path)
	if err != nil {
		return nil, 0, err
	}
	if rate == 0 {
		rate = 24000
		log.Printf("assuming 24000 Hz for %s (raw PCM)", path)
	}
	if rate != 24000 {
		log.Printf("resampling %s from %d Hz to 24000 Hz", path, rate)
		data, err = resamplePCM16(data, rate, 24000)
		if err != nil {
			return nil, 0, err
		}
		rate = 24000
	}
	return data, rate, nil
}

func sendTurnDetectionUpdate(ctx context.Context, conn *openairt.Conn, sampleRate int, language, vadModel string, threshold float64, silence, prefix time.Duration, createResponse, interruptResponse bool) error {
	if sampleRate <= 0 {
		sampleRate = 24000
	}
	serverVad := map[string]any{
		"threshold":           threshold,
		"silence_duration_ms": silence.Milliseconds(),
		"prefix_padding_ms":   prefix.Milliseconds(),
	}
	if strings.TrimSpace(vadModel) != "" {
		serverVad["provider"] = strings.TrimSpace(vadModel)
	}
	if createResponse {
		serverVad["create_response"] = true
	}
	if interruptResponse {
		serverVad["interrupt_response"] = true
	}

	input := map[string]any{
		"format": map[string]any{
			"type": "audio/pcm",
			"rate": sampleRate,
		},
		"turn_detection": map[string]any{
			"type":       "server_vad",
			"server_vad": serverVad,
		},
	}
	if strings.TrimSpace(language) != "" {
		input["transcription"] = map[string]any{"language": language}
	}

	payload := map[string]any{
		"type": string(openairt.ClientEventTypeSessionUpdate),
		"session": map[string]any{
			"type": "transcription",
			"audio": map[string]any{
				"input": input,
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	provider := ""
	if value, ok := serverVad["provider"].(string); ok {
		provider = value
	}
	log.Printf("sending session.update rate=%dHz vad_model=%s threshold=%.2f silence=%s prefix=%s create_response=%t interrupt_response=%t", sampleRate, provider, threshold, silence, prefix, createResponse, interruptResponse)
	return conn.SendMessageRaw(ctx, data)
}

func streamClip(ctx context.Context, conn *openairt.Conn, audio []byte, sampleRate int, chunkDur time.Duration) error {
	bytesPerChunk := int(math.Round(float64(sampleRate) * chunkDur.Seconds() * 2))
	if bytesPerChunk <= 0 {
		bytesPerChunk = len(audio)
	}

	log.Printf("streaming %d bytes at %d Hz (chunk=%s)", len(audio), sampleRate, chunkDur)

	offset := 0
	ticker := time.NewTicker(chunkDur)
	defer ticker.Stop()

	for offset < len(audio) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		end := offset + bytesPerChunk
		if end > len(audio) {
			end = len(audio)
		}

		chunk := audio[offset:end]
		payload := openairt.InputAudioBufferAppendEvent{
			Audio: base64.StdEncoding.EncodeToString(chunk),
		}
		if err := conn.SendMessage(ctx, payload); err != nil {
			return err
		}
		offset = end
	}

	return nil
}

func logEvents(ctx context.Context, conn *openairt.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, err := conn.ReadMessageRaw(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("read event error: %v", err)
			}
			return
		}

		event, err := openairt.UnmarshalServerEvent(data)
		if err != nil {
			log.Printf("event: %s", string(data))
			continue
		}

		switch evt := event.(type) {
		case openairt.ConversationItemInputAudioTranscriptionCompletedEvent:
			log.Printf("transcription completed: %s", evt.Transcript)
		case openairt.ConversationItemAddedEvent:
			if user := evt.Item.User; user != nil {
				for _, content := range user.Content {
					switch content.Type {
					case openairt.MessageContentTypeInputAudio:
						log.Printf("audio chunk accepted (%d bytes)", len(content.Audio))
					case openairt.MessageContentTypeInputText:
						meta := strings.TrimSpace(content.Text)
						if meta != "" {
							log.Printf("metadata: %s", meta)
						}
					}
				}
			}
		default:
			log.Printf("event: %s", string(data))
		}
	}
}

func readAudioFile(path string) ([]byte, int, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return readWAV(path)
	case ".pcm", ".raw":
		data, err := os.ReadFile(path)
		return data, 0, err
	default:
		return nil, 0, fmt.Errorf("unsupported audio format: %s", path)
	}
}

func readWAV(path string) ([]byte, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("close WAV file: %v", err)
		}
	}()

	header := make([]byte, 44)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, 0, fmt.Errorf("read WAV header: %w", err)
	}

	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("not a WAV file: %s", path)
	}

	audioFormat := binary.LittleEndian.Uint16(header[20:22])
	channels := binary.LittleEndian.Uint16(header[22:24])
	sampleRate := int(binary.LittleEndian.Uint32(header[24:28]))
	bitsPerSample := binary.LittleEndian.Uint16(header[34:36])

	if audioFormat != 1 || channels != 1 || bitsPerSample != 16 {
		return nil, 0, fmt.Errorf("unsupported WAV parameters: format=%d channels=%d bits=%d", audioFormat, channels, bitsPerSample)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if len(data) <= 44 {
		return nil, 0, fmt.Errorf("wav file is too short: %s", path)
	}
	return data[44:], sampleRate, nil
}

func resamplePCM16(data []byte, inputRate, outputRate int) ([]byte, error) {
	if inputRate == outputRate || len(data) == 0 {
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	}

	samples := pcm16ToFloat32(data)
	resampled := resampleLinear(samples, inputRate, outputRate)
	return float32ToPCM16(resampled), nil
}

func pcm16ToFloat32(data []byte) []float32 {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	result := make([]float32, len(data)/2)
	for i := 0; i < len(result); i++ {
		raw := int16(binary.LittleEndian.Uint16(data[i*2:]))
		result[i] = float32(raw) / 32768.0
	}
	return result
}

func float32ToPCM16(samples []float32) []byte {
	data := make([]byte, len(samples)*2)
	for i, v := range samples {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		scaled := int16(math.Round(float64(v) * 32767.0))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(scaled))
	}
	return data
}

func resampleLinear(samples []float32, inputRate, outputRate int) []float32 {
	if inputRate <= 0 {
		inputRate = outputRate
	}
	if outputRate <= 0 {
		outputRate = inputRate
	}
	if inputRate == outputRate || len(samples) == 0 {
		out := make([]float32, len(samples))
		copy(out, samples)
		return out
	}

	ratio := float64(inputRate) / float64(outputRate)
	outLen := int(float64(len(samples)) / ratio)
	if outLen <= 0 {
		outLen = 1
	}

	result := make([]float32, outLen)
	for i := 0; i < outLen; i++ {
		srcIndex := float64(i) * ratio
		idx := int(srcIndex)
		if idx >= len(samples)-1 {
			result[i] = samples[len(samples)-1]
			continue
		}
		frac := srcIndex - float64(idx)
		result[i] = samples[idx]*(1-float32(frac)) + samples[idx+1]*float32(frac)
	}
	return result
}
