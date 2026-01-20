package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
)

func main() {
	var (
		urlFlag        = flag.String("url", "ws://localhost:8009/v1/realtime", "Realtime websocket URL")
		sampleRate     = flag.Int("rate", 24000, "Input sample rate for stdin stream (Hz)")
		chunkDurFlag   = flag.Duration("chunk", 120*time.Millisecond, "Chunk duration to batch microphone samples")
		threshold      = flag.Float64("threshold", 0.55, "Server VAD activation threshold (0-1)")
		silence        = flag.Duration("silence", 700*time.Millisecond, "Silence duration to finalise a turn")
		prefixPadding  = flag.Duration("prefix-padding", 300*time.Millisecond, "Audio padding forwarded to the detector before speech start")
		language       = flag.String("language", "", "Optional ISO-639-1 language hint")
		vadModel       = flag.String("vad-model", "", "Target VAD model name (matches config.vad.models key); default uses server configuration")
		createResponse = flag.Bool("create-response", false, "Set server_vad.create_response so the server auto-generates replies on speech end")
		interruptResp  = flag.Bool("interrupt-response", false, "Set server_vad.interrupt_response so new speech interrupts ongoing replies")
		switchModel    = flag.String("switch-model", "", "Optional VAD model to switch to during the same session")
		switchAfter    = flag.Duration("switch-after", 0, "Delay before switching to --switch-model (requires --switch-model)")
	)
	flag.Parse()

	if *sampleRate <= 0 {
		log.Fatal("sample rate must be positive")
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

	bytesPerChunk := int(math.Round(float64(*sampleRate) * chunkDurFlag.Seconds() * 2))
	if bytesPerChunk <= 0 {
		bytesPerChunk = int(float64(*sampleRate) * 2)
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

	if err := sendTurnDetectionUpdate(ctx, conn, *sampleRate, *language, *vadModel, *threshold, *silence, *prefixPadding, *createResponse, *interruptResp); err != nil {
		log.Fatalf("session update: %v", err)
	}

	if strings.TrimSpace(*switchModel) != "" {
		delay := *switchAfter
		if delay <= 0 {
			delay = 500 * time.Millisecond
		}
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			log.Printf("switching VAD model to %s after %s", *switchModel, delay)
			if err := sendTurnDetectionUpdate(ctx, conn, *sampleRate, *language, *switchModel, *threshold, *silence, *prefixPadding, *createResponse, *interruptResp); err != nil {
				log.Printf("switch vad model: %v", err)
			}
		}()
	}

	if err := streamFromStdin(ctx, conn, bytesPerChunk); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("stream stdin: %v", err)
	}

	<-ctx.Done()
}

func streamFromStdin(ctx context.Context, conn *openairt.Conn, chunkSize int) error {
	reader := bufio.NewReader(os.Stdin)
	buf := make([]byte, chunkSize)

	log.Printf("streaming microphone data from stdin (chunk=%d bytes)", chunkSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := io.ReadFull(reader, buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				if n > 0 {
					if err := sendChunk(ctx, conn, buf[:n]); err != nil {
						return err
					}
				}
				return nil
			}
			return err
		}

		if err := sendChunk(ctx, conn, buf[:n]); err != nil {
			return err
		}
	}
}

func sendChunk(ctx context.Context, conn *openairt.Conn, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	payload := openairt.InputAudioBufferAppendEvent{
		Audio: base64.StdEncoding.EncodeToString(data),
	}
	return conn.SendMessage(ctx, payload)
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
