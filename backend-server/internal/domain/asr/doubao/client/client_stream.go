package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"backend-server/internal/domain/asr/doubao/request"
	"backend-server/internal/domain/asr/doubao/response"
	"backend-server/pkg/audio"

	log "backend-server/internal/infrastructure/logger"
)

type AsrWsClient struct {
	seq       int
	url       string
	connect   *websocket.Conn
	appId     string
	accessKey string
}

func NewAsrWsClient(url string, appKey, accessKey string) *AsrWsClient {
	return &AsrWsClient{
		seq:       1,
		url:       url,
		appId:     appKey,
		accessKey: accessKey,
	}
}

func (c *AsrWsClient) CreateConnection(ctx context.Context) error {
	header := request.NewAuthHeader(c.appId, c.accessKey)
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, c.url, header)
	if err != nil {
		return fmt.Errorf("dial websocket err: %w", err)
	}
	_ = resp
	//log.Debugf("logid: %s", resp.Header.Get("X-Tt-Logid"))
	c.connect = conn
	return nil
}

func (c *AsrWsClient) SendFullClientRequest() error {
	fullClientRequest := request.NewFullClientRequest()
	c.seq++
	err := c.connect.WriteMessage(websocket.BinaryMessage, fullClientRequest)
	if err != nil {
		return fmt.Errorf("full client message write websocket err: %w", err)
	}
	_, resp, err := c.connect.ReadMessage()
	if err != nil {
		return fmt.Errorf("full client message read err: %w", err)
	}
	_ = resp
	//respStruct := response.ParseResponse(resp)
	//log.Println(respStruct)
	return nil
}

func (c *AsrWsClient) SendMessages(ctx context.Context, audioStream <-chan []float32, stopChan <-chan struct{}) error {
	messageChan := make(chan []byte)
	go func() {
		for message := range messageChan {
			err := c.connect.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				//log.Printf("write message err: %s", err)
				return
			}
		}
	}()

	silenceTicker := time.NewTicker(2 * time.Second)
	defer silenceTicker.Stop()
	defer close(messageChan)

	lastSent := time.Now()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("send messages context done")
		case <-stopChan:
			return fmt.Errorf("send messages stop chan")
		case audioData, ok := <-audioStream:
			if !ok {
				log.Debugf("sendMessages audioStream closed")

				endMessage := request.NewAudioOnlyRequest(-c.seq, []byte{})
				messageChan <- endMessage
				return nil
			}
			byteData := make([]byte, len(audioData)*2)
			audio.Float32ToPCMBytes(audioData, byteData)
			message := request.NewAudioOnlyRequest(c.seq, byteData)
			messageChan <- message
			c.seq++
			lastSent = time.Now()
		case <-silenceTicker.C:
			if time.Since(lastSent) < 1500*time.Millisecond {
				continue
			}
			silence := make([]byte, 320*2)
			messageChan <- request.NewAudioOnlyRequest(c.seq, silence)
			c.seq++
			lastSent = time.Now()
		}
	}
}

func (c *AsrWsClient) recvMessages(ctx context.Context, resChan chan<- *response.AsrResponse, stopChan chan<- struct{}, stopOnce *sync.Once) {
	defer close(resChan)
	for {
		_, message, err := c.connect.ReadMessage()
		if err != nil {
			log.Warnf("doubao recvMessages read error: %v", err)
			return
		}
		resp := response.ParseResponse(message)
		if resp.Code != 0 {
			log.Warnf("doubao recvMessages code: %d, payload err: %s", resp.Code, payloadError(resp))
		}
		resChan <- resp
		if resp.IsLastPackage {
			return
		}
		if resp.Code != 0 {
			// 使用 sync.Once 确保 stopChan 只被关闭一次
			stopOnce.Do(func() {
				close(stopChan)
			})
			return
		}
	}
}

func payloadError(resp *response.AsrResponse) string {
	if resp == nil || resp.PayloadMsg == nil {
		return ""
	}
	return resp.PayloadMsg.Error
}

func (c *AsrWsClient) StartAudioStream(ctx context.Context, audioStream <-chan []float32, resChan chan<- *response.AsrResponse) error {
	stopChan := make(chan struct{})
	var stopOnce sync.Once // 确保 stopChan 只被关闭一次

	go func() {
		err := c.SendMessages(ctx, audioStream, stopChan)
		if err != nil {
			//log.Fatalf("failed to send audio stream: %s", err)
			log.Errorf("failed to send audio stream: %s", err)
			// 使用 sync.Once 确保 stopChan 只被关闭一次
			stopOnce.Do(func() {
				close(stopChan)
			})
			return
		}
	}()
	c.recvMessages(ctx, resChan, stopChan, &stopOnce)
	return nil
}

func (c *AsrWsClient) Excute(ctx context.Context, audioStream chan []float32, resChan chan<- *response.AsrResponse) error {
	c.seq = 1
	if c.url == "" {
		return errors.New("url is empty")
	}
	err := c.CreateConnection(ctx)
	if err != nil {
		return fmt.Errorf("create connection err: %w", err)
	}
	err = c.SendFullClientRequest()
	if err != nil {
		return fmt.Errorf("send full request err: %w", err)
	}

	err = c.StartAudioStream(ctx, audioStream, resChan)
	if err != nil {
		return fmt.Errorf("start audio stream err: %w", err)
	}
	return nil
}
