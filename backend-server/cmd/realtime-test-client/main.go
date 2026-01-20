// Command realtime-test-client connects to the backend websocket endpoint,
// sends hello + listen start (realtime) control messages, streams an optional
// PCM/WAV file as Opus frames, and prints downstream server messages. This
// is intended for manual validation of realtime listen mode (partial downlink,
// interrupts, ASR restarts).
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"backend-server/pkg/audio"

	"github.com/go-audio/wav"
	"github.com/gorilla/websocket"
)

type audioParams struct {
	Format        string `json:"format"`
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	FrameDuration int    `json:"frame_duration"`
}

type clientMessage struct {
	Type        string       `json:"type"`
	State       string       `json:"state,omitempty"`
	Mode        string       `json:"mode,omitempty"`
	Transport   string       `json:"transport,omitempty"`
	AudioParams *audioParams `json:"audio_params,omitempty"`
	Text        string       `json:"text,omitempty"`
}

func main() {
	urlFlag := flag.String("url", "ws://localhost:8989/xiaozhi/v1/", "WebSocket URL of backend-server")
	deviceIDFlag := flag.String("device-id", "", "Device-Id header; random if empty")
	modeFlag := flag.String("mode", "realtime", "listen mode: auto/manual/realtime")
	wavPathFlag := flag.String("wav", "", "Path to 16k mono WAV/PCM file for streaming (optional)")
	frameMsFlag := flag.Int("frame-ms", 20, "Frame duration in ms for opus encoding")
	sampleRateFlag := flag.Int("sample-rate", 16000, "Sample rate when encoding PCM")
	channelsFlag := flag.Int("channels", 1, "Channel count for encoding")
	tokenFlag := flag.String("token", "", "Optional Bearer token header")
	flag.Parse()

	deviceID := *deviceIDFlag
	if deviceID == "" {
		deviceID = "8A:0D:33:D7:19:7B"
		rand.Seed(time.Now().UnixNano())
	}

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
	}
	headers := http.Header{}
	headers.Set("Device-Id", deviceID)
	headers.Set("Protocol-Version", "1")
	headers.Set("Client-Id", "realtime-test-client")
	if strings.TrimSpace(*tokenFlag) != "" {
		headers.Set("Authorization", "Bearer "+strings.TrimSpace(*tokenFlag))
	}

	conn, resp, err := dialer.Dial(*urlFlag, headers)
	if err != nil {
		log.Fatalf("dial websocket: %v (resp=%v)", err, resp)
	}
	defer conn.Close()

	log.Printf("connected to %s as %s", *urlFlag, deviceID)

	var wg sync.WaitGroup
	stopRead := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopRead:
				return
			default:
			}
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				log.Printf("read error: %v", err)
				return
			}
			if msgType == websocket.TextMessage {
				log.Printf("[server text] %s", data)
			} else {
				log.Printf("[server binary] %d bytes", len(data))
			}
		}
	}()

	hello := clientMessage{
		Type:      "hello",
		Transport: "websocket",
		AudioParams: &audioParams{
			Format:        "opus",
			SampleRate:    *sampleRateFlag,
			Channels:      *channelsFlag,
			FrameDuration: *frameMsFlag,
		},
	}
	if err := sendJSON(conn, hello); err != nil {
		log.Fatalf("send hello: %v", err)
	}
	log.Printf("sent hello %+v", hello.AudioParams)

	listenStart := clientMessage{
		Type:  "listen",
		State: "start",
		Mode:  *modeFlag,
	}
	if err := sendJSON(conn, listenStart); err != nil {
		log.Fatalf("send listen start: %v", err)
	}
	log.Printf("sent listen start (mode=%s)", *modeFlag)

	if *wavPathFlag != "" {
		if err := streamWavAsOpus(conn, *wavPathFlag, *sampleRateFlag, *channelsFlag, *frameMsFlag); err != nil {
			log.Fatalf("stream wav: %v", err)
		}
		log.Printf("audio streaming done")
	}

	listenStop := clientMessage{
		Type:  "listen",
		State: "stop",
		Mode:  *modeFlag,
	}
	if err := sendJSON(conn, listenStop); err != nil {
		log.Printf("send listen stop: %v", err)
	} else {
		log.Printf("sent listen stop")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("waiting for server messages, press Ctrl+C to exit")
	<-sigCh
	close(stopRead)
	wg.Wait()
}

func sendJSON(conn *websocket.Conn, v interface{}) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

func streamWavAsOpus(conn *websocket.Conn, path string, sampleRate, channels, frameMs int) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open wav: %w", err)
	}
	defer f.Close()

	intBuf, err := readPCM16(f, sampleRate, channels)
	if err != nil {
		log.Printf("decode wav failed (%v), fallback to generated sine", err)
		intBuf = generateSinePCM(sampleRate, channels, 2*time.Second, 440)
	}

	encoder, err := audio.NewOpusEncoder(sampleRate, channels, frameMs)
	if err != nil {
		return fmt.Errorf("create opus encoder: %w", err)
	}
	defer encoder.Close()

	frameBytes := encoder.GetFrameBytes()
	raw := int16ToBytes(intBuf)
	log.Printf("streaming %d frames (frameBytes=%d)", (len(raw)+frameBytes-1)/frameBytes, frameBytes)

	reader := bytes.NewReader(raw)
	buf := make([]byte, frameBytes)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			frame := buf[:n]
			opusFrame, encErr := encoder.Encode(frame)
			if encErr != nil {
				return fmt.Errorf("encode opus: %w", encErr)
			}
			if len(opusFrame) > 0 {
				if err := conn.WriteMessage(websocket.BinaryMessage, opusFrame); err != nil {
					return fmt.Errorf("write opus frame: %w", err)
				}
			}
			time.Sleep(time.Duration(frameMs) * time.Millisecond)
		}
		if err != nil {
			break
		}
	}
	return nil
}

func int16ToBytes(samples []int) []byte {
	buf := new(bytes.Buffer)
	for _, s := range samples {
		binary.Write(buf, binary.LittleEndian, int16(s))
	}
	return buf.Bytes()
}

// readPCM16 attempts to load a 16-bit PCM WAV file. Returns decoded samples or error.
func readPCM16(f *os.File, sampleRate, channels int) ([]int, error) {
	dec := wav.NewDecoder(f)
	if !dec.IsValidFile() {
		return nil, fmt.Errorf("invalid wav file")
	}
	if dec.BitDepth != 16 {
		return nil, fmt.Errorf("only 16-bit PCM supported, got %d-bit", dec.BitDepth)
	}
	if int(dec.SampleRate) != sampleRate {
		log.Printf("warning: wav sample rate %d != requested %d, continuing", dec.SampleRate, sampleRate)
	}
	if int(dec.NumChans) != channels {
		return nil, fmt.Errorf("wav channels %d != requested %d", dec.NumChans, channels)
	}

	samples, err := dec.FullPCMBuffer()
	if err != nil {
		return nil, fmt.Errorf("read wav samples: %w", err)
	}
	intBuf := samples.AsIntBuffer().Data
	if len(intBuf) == 0 {
		return nil, fmt.Errorf("no audio samples")
	}
	return intBuf, nil
}

// generateSinePCM produces a mono/stereo sine wave PCM int16 buffer.
func generateSinePCM(sampleRate, channels int, dur time.Duration, freq int) []int {
	totalSamples := int(float64(sampleRate) * dur.Seconds())
	buf := make([]int, totalSamples*channels)
	for i := 0; i < totalSamples; i++ {
		value := int(30000 * math.Sin(2*math.Pi*float64(freq)*float64(i)/float64(sampleRate)))
		for ch := 0; ch < channels; ch++ {
			buf[i*channels+ch] = value
		}
	}
	return buf
}
