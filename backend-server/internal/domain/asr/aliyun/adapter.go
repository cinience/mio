package aliyun

import (
	"context"
	"fmt"
	"time"

	"backend-server/constants"
	"backend-server/internal/domain/asr/types"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/asr"
)

// init 函数注册阿里云ASR提供者
func init() {
	asr.Register(
		[]string{constants.AsrTypeAliyun},
		"阿里云ASR 语音识别服务",
		func(config map[string]interface{}) (asr.BaseASRProvider, error) {
			adapter, err := NewAliyunAdapter(config)
			if err != nil {
				log.Errorf("阿里云ASR适配器创建失败: %v", err)
				return nil, err
			}
			log.Info("阿里云ASR适配器创建成功")
			return adapter, nil
		},
	)
}

// AliyunAdapter 基于官方NLS SDK的阿里云ASR适配器
type AliyunAdapter struct {
	config *Config
}

// NewAliyunAdapter 创建基于官方NLS SDK的阿里云ASR适配器
func NewAliyunAdapter(configMap map[string]interface{}) (*AliyunAdapter, error) {
	config := &Config{}

	// 解析配置
	if accessKeyID, ok := configMap["access_key_id"].(string); ok {
		config.AccessKeyID = accessKeyID
	}
	if accessKeySecret, ok := configMap["access_key_secret"].(string); ok {
		config.AccessKeySecret = accessKeySecret
	}
	if appKey, ok := configMap["appkey"].(string); ok {
		config.AppKey = appKey
	}
	if token, ok := configMap["token"].(string); ok {
		config.Token = token
	}
	if host, ok := configMap["host"].(string); ok {
		config.Host = host
	}
	if maxSilence, ok := configMap["max_sentence_silence"].(int); ok {
		config.MaxSentenceSilence = maxSilence
	}

	// 验证配置
	if config.AppKey == "" {
		return nil, fmt.Errorf("aliyun asr appkey 不能为空")
	}

	if config.AccessKeyID == "" && config.Token == "" {
		return nil, fmt.Errorf("aliyun asr 必须提供 access_key_id+access_key_secret 或者 token")
	}

	if config.AccessKeyID != "" && config.AccessKeySecret == "" {
		return nil, fmt.Errorf("aliyun asr access_key_secret 不能为空")
	}

	adapter := &AliyunAdapter{
		config: config,
	}

	log.Infof("阿里云ASR适配器创建成功，AppKey: %s", config.AppKey)
	return adapter, nil
}

// Process 一次性处理整段音频
func (a *AliyunAdapter) Process(pcmData []float32) (string, error) {
	// 将float32 PCM转换为int16 PCM
	pcmBytes := make([]byte, len(pcmData)*2)
	for i, sample := range pcmData {
		// 将float32转换为int16
		intSample := int16(sample * 32767)
		pcmBytes[i*2] = byte(intSample)
		pcmBytes[i*2+1] = byte(intSample >> 8)
	}

	// 创建临时客户端进行一次性识别
	client, err := NewStreamingClient(a.config)
	if err != nil {
		return "", fmt.Errorf("创建ASR客户端失败: %w", err)
	}
	defer client.Close()

	if err := client.Connect(); err != nil {
		return "", fmt.Errorf("连接阿里云ASR服务失败: %w", err)
	}

	// 启动识别
	if err := client.Start(); err != nil {
		return "", fmt.Errorf("启动阿里云ASR识别失败: %w", err)
	}

	// 等待服务器准备好
	timeout := time.After(10 * time.Second)
	for !client.IsReady() {
		select {
		case <-timeout:
			return "", fmt.Errorf("等待阿里云ASR服务器准备超时")
		case err := <-client.GetError():
			return "", fmt.Errorf("阿里云ASR连接错误: %w", err)
		case <-time.After(50 * time.Millisecond):
			// 继续等待
		}
	}

	// 发送音频数据
	if err := client.SendAudio(pcmBytes); err != nil {
		return "", fmt.Errorf("发送音频数据失败: %w", err)
	}

	// 停止识别以触发识别完成
	client.Stop()

	// 等待结果
	resultTimeout := time.After(10 * time.Second)
	var lastResult string

	for {
		select {
		case result := <-client.GetResult():
			lastResult = result
		case err := <-client.GetError():
			if lastResult != "" {
				return lastResult, nil
			}
			return "", fmt.Errorf("阿里云ASR识别错误: %w", err)
		case <-resultTimeout:
			if lastResult != "" {
				return lastResult, nil
			}
			return "", fmt.Errorf("等待阿里云ASR识别结果超时")
		case <-time.After(100 * time.Millisecond):
			// 检查是否有结果
			if lastResult != "" {
				// 等待一小段时间看是否有更多结果
				select {
				case result := <-client.GetResult():
					lastResult = result
				case <-time.After(500 * time.Millisecond):
					return lastResult, nil
				}
			}
		}
	}
}

// StreamingRecognize 流式识别
func (a *AliyunAdapter) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	resultChan := make(chan types.StreamingResult, 10)

	go func() {
		defer close(resultChan)

		// 创建客户端
		client, err := NewStreamingClient(a.config)
		if err != nil {
			select {
			case resultChan <- types.StreamingResult{Text: "", IsFinal: true}:
			case <-ctx.Done():
			}
			log.Errorf("创建ASR客户端失败: %v", err)
			return
		}
		defer client.Close()

		// 建立连接
		if err := client.Connect(); err != nil {
			select {
			case resultChan <- types.StreamingResult{Text: "", IsFinal: true}:
			case <-ctx.Done():
			}
			log.Errorf("连接阿里云ASR服务失败: %v", err)
			return
		}

		// 启动识别
		if err := client.Start(); err != nil {
			select {
			case resultChan <- types.StreamingResult{Text: "", IsFinal: true}:
			case <-ctx.Done():
			}
			log.Errorf("启动阿里云ASR识别失败: %v", err)
			return
		}

		// 启动结果处理协程
		go func() {
			for {
				select {
				case result, ok := <-client.GetResult():
					if !ok {
						return
					}
					// 发送中间结果
					select {
					case resultChan <- types.StreamingResult{Text: result, IsFinal: false}:
					case <-ctx.Done():
						return
					}
				case err, ok := <-client.GetError():
					if !ok {
						return
					}
					log.Errorf("阿里云ASR识别错误: %v", err)
				case <-ctx.Done():
					return
				}
			}
		}()

		// 等待服务器准备好
		timeout := time.After(10 * time.Second)
		for !client.IsReady() {
			select {
			case <-timeout:
				log.Error("等待阿里云ASR服务器准备超时")
				return
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
				// 继续等待
			}
		}

		// 处理音频流
		for {
			select {
			case pcmData, ok := <-audioStream:
				if !ok {
					// 音频流结束，停止识别触发最终结果
					client.Stop()

					// 等待一小段时间收集最终结果
					time.Sleep(500 * time.Millisecond)

					// 发送结束标志
					select {
					case resultChan <- types.StreamingResult{Text: "", IsFinal: true}:
					case <-ctx.Done():
					}
					return
				}

				// 将float32 PCM转换为int16 PCM字节
				pcmBytes := make([]byte, len(pcmData)*2)
				for i, sample := range pcmData {
					intSample := int16(sample * 32767)
					pcmBytes[i*2] = byte(intSample)
					pcmBytes[i*2+1] = byte(intSample >> 8)
				}

				// 发送音频数据
				if err := client.SendAudio(pcmBytes); err != nil {
					log.Errorf("发送音频数据到阿里云ASR失败: %v", err)
					return
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	return resultChan, nil
}
