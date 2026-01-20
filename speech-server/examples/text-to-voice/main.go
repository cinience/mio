package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
)

func main() {
	var (
		urlFlag    = flag.String("url", "ws://localhost:8009/v1/realtime", "Realtime server websocket URL")
		modelFlag  = flag.String("model", "local-matcha-tts", "Model identifier to request (query parameter)")
		textFlag   = flag.String("text", "", "Text to synthesize (required unless --instructions is set)")
		instrFlag  = flag.String("instructions", "", "Optional system instructions passed with the request")
		voiceFlag  = flag.String("voice", "", "Voice to synthesize (e.g. alloy, verse)")
		speedFlag  = flag.Float64("speed", 1.0, "Speech speed multiplier (1.0 = normal)")
		rateFlag   = flag.Int("sample-rate", 24000, "PCM sample rate for synthesized audio")
		outputFlag = flag.String("output", "tts-output.wav", "Path to write the synthesized WAV file")
	)
	flag.Parse()

	if strings.TrimSpace(*textFlag) == "" && strings.TrimSpace(*instrFlag) == "" {
		log.Fatal("either --text or --instructions must be provided")
	}
	if *speedFlag <= 0 {
		log.Fatal("speed must be > 0")
	}
	if *rateFlag <= 0 {
		log.Fatal("sample-rate must be > 0")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg := openairt.DefaultConfig("")
	cfg.BaseURL = *urlFlag
	client := openairt.NewClientWithConfig(cfg)

	conn, err := client.Connect(ctx, openairt.WithModel(*modelFlag))
	if err != nil {
		log.Fatalf("connect realtime server: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("close websocket: %v", err)
		}
	}()

	collector := newTTSCollector(*rateFlag)
	done := make(chan error, 1)
	go func() {
		done <- collector.consume(ctx, conn)
	}()

	if err := sendRealtimePreferences(ctx, conn, *voiceFlag, float32(*speedFlag), *rateFlag); err != nil {
		log.Fatalf("send session update: %v", err)
	}
	if err := sendResponseCreate(ctx, conn, *textFlag, *instrFlag, *voiceFlag, *rateFlag); err != nil {
		log.Fatalf("send response.create: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			log.Fatalf("stream error: %v", err)
		}
	case <-ctx.Done():
		log.Fatalf("operation cancelled")
	}

	if err := collector.writeWAV(*outputFlag); err != nil {
		log.Fatalf("write wav: %v", err)
	}

	log.Printf("wrote %s (%d bytes)", *outputFlag, collector.audio.Len())
	if transcript := collector.transcript(); transcript != "" {
		log.Printf("transcript: %s", transcript)
	}
}

type ttsCollector struct {
	audio           bytes.Buffer
	sampleRate      int
	transcriptBuf   strings.Builder
	finalTranscript string
}

func newTTSCollector(sampleRate int) *ttsCollector {
	return &ttsCollector{sampleRate: sampleRate}
}

func (c *ttsCollector) consume(ctx context.Context, conn *openairt.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := conn.ReadMessageRaw(ctx)
		if err != nil {
			return err
		}

		var envelope struct {
			Type openairt.ServerEventType `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			log.Printf("invalid event: %v", err)
			continue
		}

		switch envelope.Type {
		case openairt.ServerEventTypeSessionCreated:
			log.Printf("session created")
		case openairt.ServerEventTypeSessionUpdated:
			log.Printf("session updated")
		case openairt.ServerEventTypeResponseOutputAudioDelta:
			var evt openairt.ResponseOutputAudioDeltaEvent
			if err := json.Unmarshal(data, &evt); err != nil {
				return fmt.Errorf("decode audio delta: %w", err)
			}
			chunk, err := base64.StdEncoding.DecodeString(evt.Delta)
			if err != nil {
				return fmt.Errorf("decode audio base64: %w", err)
			}
			c.audio.Write(chunk)
		case openairt.ServerEventTypeResponseOutputAudioDone:
			log.Printf("audio stream completed")
		case openairt.ServerEventTypeResponseOutputAudioTranscriptDelta:
			var evt openairt.ResponseOutputAudioTranscriptDeltaEvent
			if err := json.Unmarshal(data, &evt); err == nil {
				c.transcriptBuf.WriteString(evt.Delta)
			}
		case openairt.ServerEventTypeResponseOutputTextDelta:
			var evt openairt.ResponseOutputTextDeltaEvent
			if err := json.Unmarshal(data, &evt); err == nil {
				c.transcriptBuf.WriteString(evt.Delta)
			}
		case openairt.ServerEventTypeResponseOutputTextDone:
			var evt openairt.ResponseOutputTextDoneEvent
			if err := json.Unmarshal(data, &evt); err == nil {
				c.finalTranscript = evt.Text
			}
		case openairt.ServerEventTypeResponseDone:
			return nil
		case openairt.ServerEventTypeError:
			var evt openairt.ErrorEvent
			if err := json.Unmarshal(data, &evt); err != nil {
				return fmt.Errorf("server error: %s", string(data))
			}
			return errors.New(evt.Error.Message)
		default:
			log.Printf("event: %s", envelope.Type)
		}
	}
}

func (c *ttsCollector) transcript() string {
	if strings.TrimSpace(c.finalTranscript) != "" {
		return strings.TrimSpace(c.finalTranscript)
	}
	return strings.TrimSpace(c.transcriptBuf.String())
}

func (c *ttsCollector) writeWAV(path string) error {
	if c.audio.Len() == 0 {
		return errors.New("no audio data received")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("close WAV file: %v", err)
		}
	}()

	data := c.audio.Bytes()
	const (
		bitsPerSample = 16
		numChannels   = 1
	)
	blockAlign := numChannels * bitsPerSample / 8
	byteRate := c.sampleRate * blockAlign
	head := make([]byte, 44)
	copy(head[0:], "RIFF")
	binary.LittleEndian.PutUint32(head[4:], uint32(len(data)+36))
	copy(head[8:], "WAVE")
	copy(head[12:], "fmt ")
	binary.LittleEndian.PutUint32(head[16:], 16)
	binary.LittleEndian.PutUint16(head[20:], 1)
	binary.LittleEndian.PutUint16(head[22:], numChannels)
	binary.LittleEndian.PutUint32(head[24:], uint32(c.sampleRate))
	binary.LittleEndian.PutUint32(head[28:], uint32(byteRate))
	binary.LittleEndian.PutUint16(head[32:], uint16(blockAlign))
	binary.LittleEndian.PutUint16(head[34:], bitsPerSample)
	copy(head[36:], "data")
	binary.LittleEndian.PutUint32(head[40:], uint32(len(data)))

	if _, err := f.Write(head); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func sendRealtimePreferences(ctx context.Context, conn *openairt.Conn, voice string, speed float32, sampleRate int) error {
	voice = strings.TrimSpace(voice)
	if voice == "" && (speed == 1.0) && sampleRate <= 0 {
		return nil
	}

	output := &openairt.SessionAudioOutput{}
	if sampleRate > 0 {
		output.Format = &openairt.AudioFormatUnion{
			PCM: &openairt.AudioFormatPCM{Rate: sampleRate},
		}
	}
	if voice != "" {
		output.Voice = openairt.Voice(strings.ToLower(voice))
	}
	if speed > 0 && speed != 1 {
		output.Speed = speed
	}

	update := openairt.SessionUpdateEvent{
		Session: openairt.SessionUnion{
			Realtime: &openairt.RealtimeSession{
				Audio: &openairt.RealtimeSessionAudio{
					Output: output,
				},
			},
		},
	}
	return conn.SendMessage(ctx, update)
}

func sendResponseCreate(ctx context.Context, conn *openairt.Conn, text, instructions, voice string, sampleRate int) error {
	params := openairt.ResponseCreateParams{
		OutputModalities: []openairt.Modality{openairt.ModalityAudio},
	}
	if strings.TrimSpace(instructions) != "" {
		params.Instructions = instructions
	}
	if strings.TrimSpace(text) != "" {
		params.Input = []openairt.MessageItemUnion{
			{
				User: &openairt.MessageItemUser{
					Content: []openairt.MessageContentInput{
						{
							Type: openairt.MessageContentTypeInputText,
							Text: text,
						},
					},
				},
			},
		}
	}

	voice = strings.TrimSpace(voice)
	if voice != "" || sampleRate > 0 {
		output := &openairt.ResponseAudioOutput{}
		if voice != "" {
			output.Voice = openairt.Voice(strings.ToLower(voice))
		}
		if sampleRate > 0 {
			output.Format = &openairt.AudioFormatUnion{
				PCM: &openairt.AudioFormatPCM{Rate: sampleRate},
			}
		}
		params.Audio = &openairt.ResponseAudio{Output: output}
	}

	evt := openairt.ResponseCreateEvent{Response: params}
	return conn.SendMessage(ctx, evt)
}
