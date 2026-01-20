package doubao

import (
	"context"
	"fmt"
	"time"

	"backend-server/constants"
	"backend-server/internal/domain/asr/doubao/client"
	"backend-server/internal/domain/asr/doubao/response"
	"backend-server/internal/domain/asr/types"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/asr"
	"backend-server/pkg/confighelper"
)

// init 函数注册豆包ASR提供者
func init() {
	asr.Register(
		[]string{constants.AsrTypeDoubao},
		"豆包ASR 语音识别服务",
		NewDoubaoStreamProvider,
	)
}

// DoubaoV2ASR 豆包ASR实现
type DoubaoV2ASR struct {
	config    DoubaoV2Config
	connectID string
}

func NewDoubaoStreamProvider(config map[string]interface{}) (asr.BaseASRProvider, error) {
	// 创建豆包ASR配置
	helper := confighelper.New(config)
	validator := confighelper.NewValidator()
	validator.RequireString(helper, "appid", "应用ID")
	validator.RequireString(helper, "access_token", "访问令牌")
	if validator.HasErrors() {
		return nil, validator.GetError()
	}

	var doubaoConfig DoubaoV2Config
	// 从 map 中获取配置项
	doubaoConfig.AppID = helper.GetString("appid")
	doubaoConfig.AccessToken = helper.GetString("access_token")
	doubaoConfig.Host = helper.GetString("host", DefaultConfig.Host)
	doubaoConfig.WsURL = helper.GetString("ws_url", DefaultConfig.WsURL)
	doubaoConfig.ModelName = helper.GetString("model_name", DefaultConfig.ModelName)

	doubaoConfig.EndWindowSize = DefaultConfig.EndWindowSize
	doubaoConfig.ChunkDuration = DefaultConfig.ChunkDuration
	doubaoConfig.Timeout = DefaultConfig.Timeout
	doubaoConfig.EnablePunc = helper.GetBool("enable_punc", DefaultConfig.EnablePunc)
	doubaoConfig.EnableITN = helper.GetBool("enable_itn", DefaultConfig.EnableITN)
	doubaoConfig.EnableDDC = helper.GetBool("enable_ddc", DefaultConfig.EnableDDC)
	doubaoConfig.ChunkDuration = helper.GetInt("chunk_duration", DefaultConfig.ChunkDuration)
	doubaoConfig.Timeout = helper.GetInt("timeout", DefaultConfig.Timeout)

	connectID := fmt.Sprintf("%d", time.Now().UnixNano())
	return &DoubaoV2ASR{
		config:    doubaoConfig,
		connectID: connectID,
	}, nil
}

// StreamingRecognize 实现流式识别接口
func (d *DoubaoV2ASR) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	// 建立连接
	c := client.NewAsrWsClient(d.config.WsURL, d.config.AppID, d.config.AccessToken)

	// 豆包返回的识别结果
	doubaoResultChan := make(chan *response.AsrResponse, 10)
	//程序内部的结果通道
	resultChan := make(chan types.StreamingResult, 10)

	err := c.CreateConnection(ctx)
	if err != nil {
		log.Errorf("doubao asr failed to create connection: %v", err)
		return nil, fmt.Errorf("create connection err: %w", err)
	}
	err = c.SendFullClientRequest()
	if err != nil {
		log.Errorf("doubao asr failed to send full request: %v", err)
		return nil, fmt.Errorf("send full request err: %w", err)
	}

	go func() {
		err = c.StartAudioStream(ctx, audioStream, doubaoResultChan)
	}()

	// 启动音频发送goroutine
	//go d.forwardStreamAudio(ctx, audioStream, resultChan)

	// 启动结果接收goroutine
	go d.receiveStreamResults(ctx, resultChan, doubaoResultChan)

	return resultChan, nil
}

// receiveStreamResults 接收流式识别结果
func (d *DoubaoV2ASR) receiveStreamResults(ctx context.Context, resultChan chan types.StreamingResult, asrResponseChan chan *response.AsrResponse) {
	defer func() {
		close(resultChan)
	}()
	for {
		select {
		case <-ctx.Done():
			log.Debugf("receiveStreamResults 上下文已取消")
			return
		case result, ok := <-asrResponseChan:
			if !ok {
				log.Debugf("receiveStreamResults asrResponseChan 已关闭")
				return
			}
			if result.IsLastPackage {
				resultChan <- types.StreamingResult{
					Text:    result.PayloadMsg.Result.Text,
					IsFinal: true,
				}
				return
			}
		}
	}
}

// Reset 重置ASR状态
func (d *DoubaoV2ASR) Reset() error {
	log.Info("ASR状态已重置")
	return nil
}

// Process 一次性处理整段音频，返回完整识别结果
func (d *DoubaoV2ASR) Process(pcmData []float32) (string, error) {
	// TODO: 实现一次性识别逻辑
	return "", fmt.Errorf("豆包ASR暂不支持一次性识别，请使用流式识别")
}
