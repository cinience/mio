package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
)

type audioClip struct {
	path string
	data []byte
}

func main() {
	var (
		urlFlag          = flag.String("url", "ws://localhost:8009/v1/realtime", "Realtime server websocket URL")
		audioDir         = flag.String("audio-dir", "", "Directory containing WAV/PCM16 audio samples (e.g. ../xiaozhi-tester/examples)")
		chunkDurFlag     = flag.Duration("chunk", 100*time.Millisecond, "Chunk duration to stream per frame")
		sampleRateCfg    = flag.Int("sample-rate", 0, "Override sample rate for raw PCM files (Hz)")
		targetSampleRate = flag.Int("target-sample-rate", 24000, "Session sample rate (audio is resampled if needed)")
		language         = flag.String("language", "", "Optional ISO-639-1 language hint for transcription")
	)
	flag.Parse()

	if *audioDir == "" {
		log.Fatal("audio-dir is required")
	}
	if *chunkDurFlag <= 0 {
		log.Fatal("chunk duration must be positive")
	}

	files, err := collectAudioFiles(*audioDir)
	if err != nil {
		log.Fatalf("collect audio files: %v", err)
	}

	clips, sampleRate, err := loadAudioClips(files, *sampleRateCfg, *targetSampleRate)
	if err != nil {
		log.Fatalf("prepare audio data: %v", err)
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

	go readLoop(ctx, conn)

	if err := sendSessionUpdate(ctx, conn, sampleRate, *language); err != nil {
		log.Fatalf("send session update: %v", err)
	}

	log.Printf("streaming %d clip(s) at %d Hz (chunk=%s). Press Ctrl+C to stop.", len(clips), sampleRate, chunkDurFlag.String())

	if err := streamClips(ctx, conn, clips, sampleRate, *chunkDurFlag); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("stream audio: %v", err)
	}

	<-ctx.Done()
}

func collectAudioFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".wav" && ext != ".pcm" && ext != ".raw" {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}

	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no supported audio files found in %s", dir)
	}
	return files, nil
}

func loadAudioClips(files []string, sampleRateOverride, targetSampleRate int) ([]audioClip, int, error) {
	var (
		clips     []audioClip
		sessionSR = targetSampleRate
	)

	for _, path := range files {
		data, rate, err := readAudioFile(path)
		if err != nil {
			return nil, 0, fmt.Errorf("load %s: %w", path, err)
		}

		if rate == 0 {
			if sampleRateOverride > 0 {
				rate = sampleRateOverride
			} else if sessionSR > 0 {
				rate = sessionSR
				log.Printf("assuming %d Hz for %s (raw PCM without header)", rate, path)
			} else {
				return nil, 0, fmt.Errorf("unable to detect sample rate from %s; use -sample-rate", path)
			}
		}

		if sessionSR <= 0 {
			sessionSR = rate
		}

		if rate != sessionSR {
			log.Printf("resampling %s from %d Hz to %d Hz", path, rate, sessionSR)
			data, err = resamplePCM16(data, rate, sessionSR)
			if err != nil {
				return nil, 0, fmt.Errorf("resample %s: %w", path, err)
			}
		}

		clips = append(clips, audioClip{path: path, data: data})
	}

	if sessionSR <= 0 {
		return nil, 0, errors.New("sample rate was not determined; supply -sample-rate")
	}

	return clips, sessionSR, nil
}

func readAudioFile(path string) ([]byte, int, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return readWAV(path)
	case ".pcm", ".raw":
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, err
		}
		if len(data)%2 != 0 {
			return nil, 0, fmt.Errorf("raw PCM file length must be even: %s", path)
		}
		return data, 0, nil
	default:
		return nil, 0, fmt.Errorf("unsupported audio extension for %s", path)
	}
}

func readWAV(path string) ([]byte, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if len(data) < 44 {
		return nil, 0, fmt.Errorf("wav header too short")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("invalid wav header")
	}

	var (
		pos           = 12
		sampleRate    int
		numChannels   int
		bitsPerSample int
		audioData     []byte
	)

	for pos+8 <= len(data) {
		chunkID := string(data[pos : pos+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		chunkStart := pos + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(data) {
			chunkEnd = len(data)
		}

		switch chunkID {
		case "fmt ":
			if chunkEnd-chunkStart < 16 {
				return nil, 0, fmt.Errorf("invalid fmt chunk")
			}
			audioFormat := binary.LittleEndian.Uint16(data[chunkStart : chunkStart+2])
			numChannels = int(binary.LittleEndian.Uint16(data[chunkStart+2 : chunkStart+4]))
			sampleRate = int(binary.LittleEndian.Uint32(data[chunkStart+4 : chunkStart+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[chunkStart+14 : chunkStart+16]))
			if audioFormat != 1 {
				return nil, 0, fmt.Errorf("unsupported wav format %d", audioFormat)
			}
		case "data":
			audioData = data[chunkStart:chunkEnd]
		}

		pos = chunkEnd
		if chunkSize%2 == 1 {
			pos++
		}
	}

	if len(audioData) == 0 {
		return nil, 0, fmt.Errorf("wav file missing data chunk")
	}
	if bitsPerSample != 16 {
		return nil, 0, fmt.Errorf("only PCM16 is supported (got %d bits)", bitsPerSample)
	}
	if numChannels != 1 {
		return nil, 0, fmt.Errorf("only mono audio is supported (got %d channels)", numChannels)
	}
	return audioData, sampleRate, nil
}

func resamplePCM16(samples []byte, inputRate, outputRate int) ([]byte, error) {
	if inputRate <= 0 || outputRate <= 0 {
		return nil, fmt.Errorf("invalid sample rates: %d -> %d", inputRate, outputRate)
	}
	if inputRate == outputRate {
		out := make([]byte, len(samples))
		copy(out, samples)
		return out, nil
	}

	inSamples := len(samples) / 2
	floats := make([]float32, inSamples)
	for i := 0; i < inSamples; i++ {
		raw := int16(binary.LittleEndian.Uint16(samples[i*2 : i*2+2]))
		floats[i] = float32(raw) / 32768.0
	}

	resampled := resampleLinear(floats, inputRate, outputRate)
	out := make([]byte, len(resampled)*2)
	for i, sample := range resampled {
		clamped := math.Max(-1, math.Min(1, float64(sample)))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(clamped*32767)))
	}
	return out, nil
}

func resampleLinear(samples []float32, inputRate, outputRate int) []float32 {
	if inputRate == outputRate || len(samples) == 0 {
		out := make([]float32, len(samples))
		copy(out, samples)
		return out
	}

	ratio := float64(len(samples)-1) / float64(inputRate)
	duration := ratio
	outLen := int(math.Round(duration * float64(outputRate)))
	if outLen <= 1 {
		outLen = 1
	}

	result := make([]float32, outLen)
	for i := 0; i < outLen; i++ {
		pos := float64(i) * float64(inputRate) / float64(outputRate)
		idx := int(math.Floor(pos))
		frac := pos - float64(idx)
		if idx >= len(samples)-1 {
			result[i] = samples[len(samples)-1]
			continue
		}
		result[i] = samples[idx]*(1-float32(frac)) + samples[idx+1]*float32(frac)
	}
	return result
}

func readLoop(ctx context.Context, conn *openairt.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, err := conn.ReadMessageRaw(ctx)
		if err != nil {
			return
		}

		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			log.Printf("invalid event: %v", err)
			continue
		}

		switch envelope.Type {
		case string(openairt.ServerEventTypeSessionCreated):
			log.Printf("session created")
		case string(openairt.ServerEventTypeSessionUpdated):
			log.Printf("session updated")
		case string(openairt.ServerEventTypeConversationItemInputAudioTranscriptionCompleted):
			var evt openairt.ConversationItemInputAudioTranscriptionCompletedEvent
			if err := json.Unmarshal(data, &evt); err == nil {
				log.Printf("transcription completed: %s", evt.Transcript)
			}
		case string(openairt.ServerEventTypeConversationItemDone):
			// ignore
		case string(openairt.ServerEventTypeError):
			var evt openairt.ErrorEvent
			if err := json.Unmarshal(data, &evt); err == nil {
				log.Printf("server error: %s", evt.Error.Message)
			}
		default:
			log.Printf("event: %s", envelope.Type)
		}
	}
}

func sendSessionUpdate(ctx context.Context, conn *openairt.Conn, sampleRate int, language string) error {
	update := openairt.SessionUpdateEvent{
		Session: openairt.SessionUnion{
			Transcription: &openairt.TranscriptionSession{
				Audio: &openairt.TranscriptionSessionAudio{
					Input: &openairt.SessionAudioInput{
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: sampleRate},
						},
						Transcription: &openairt.AudioTranscription{Language: language},
					},
				},
			},
		},
	}
	return conn.SendMessage(ctx, update)
}

func streamClips(ctx context.Context, conn *openairt.Conn, clips []audioClip, sampleRate int, chunkDur time.Duration) error {
	frameBytes := int(float64(sampleRate)*chunkDur.Seconds()) * 2
	if frameBytes <= 0 {
		frameBytes = sampleRate * 2 / 10
	}

	ticker := time.NewTicker(chunkDur)
	defer ticker.Stop()

	for {
		for _, clip := range clips {
			log.Printf("streaming %s", clip.path)
			offset := 0
			for offset < len(clip.data) {
				select {
				case <-ctx.Done():
					return context.Canceled
				case <-ticker.C:
				}

				end := offset + frameBytes
				if end > len(clip.data) {
					end = len(clip.data)
				}

				chunk := base64.StdEncoding.EncodeToString(clip.data[offset:end])
				appendEvt := openairt.InputAudioBufferAppendEvent{Audio: chunk}
				if err := conn.SendMessage(ctx, appendEvt); err != nil {
					return err
				}

				offset = end
			}

			commit := openairt.InputAudioBufferCommitEvent{}
			if err := conn.SendMessage(ctx, commit); err != nil {
				return err
			}
		}
	}
}
