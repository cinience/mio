package aliyun

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "backend-server/internal/infrastructure/logger"

	nls "github.com/aliyun/alibabacloud-nls-go-sdk"
)

// StreamingClient 基于阿里云官方NLS SDK的流式ASR客户端
type StreamingClient struct {
	config     *Config
	st         *nls.SpeechTranscription
	nlsConfig  *nls.ConnectionConfig
	logger     *nls.NlsLogger
	state      ConnectionState
	mutex      sync.RWMutex
	resultChan chan string
	errorChan  chan error
	ctx        context.Context
	cancel     context.CancelFunc
	ready      bool
	processing bool
	readyChan  chan bool
	stopChan   chan bool
}

// NewStreamingClient 创建基于官方NLS SDK的新流式客户端
func NewStreamingClient(config *Config) (*StreamingClient, error) {
	client := &StreamingClient{
		config:     config,
		state:      StateDisconnected,
		resultChan: make(chan string, 10),
		errorChan:  make(chan error, 10),
		readyChan:  make(chan bool, 1),
		stopChan:   make(chan bool, 1),
	}

	// 验证配置
	if config.AppKey == "" {
		return nil, fmt.Errorf("AppKey不能为空")
	}

	// 创建NLS连接配置
	var nlsConfig *nls.ConnectionConfig
	var err error

	if config.AccessKeyID != "" && config.AccessKeySecret != "" {
		// 使用AccessKey创建配置
		host := config.Host
		if host == "" {
			host = nls.DEFAULT_URL
		} else {
			// 如果用户提供的host不包含协议前缀，则添加完整的WebSocket URL格式
			if !strings.HasPrefix(host, "wss://") && !strings.HasPrefix(host, "ws://") {
				originalHost := host
				host = "wss://" + host + "/ws/v1"
				log.Debugf("转换ASR服务地址: %s -> %s", originalHost, host)
			}
		}
		nlsConfig, err = nls.NewConnectionConfigWithAKInfoDefault(host, config.AppKey, config.AccessKeyID, config.AccessKeySecret)
		if err != nil {
			return nil, fmt.Errorf("创建连接配置失败: %w", err)
		}
	} else if config.Token != "" {
		// 使用Token创建配置
		host := config.Host
		if host == "" {
			host = nls.DEFAULT_URL
		} else {
			// 如果用户提供的host不包含协议前缀，则添加完整的WebSocket URL格式
			if !strings.HasPrefix(host, "wss://") && !strings.HasPrefix(host, "ws://") {
				originalHost := host
				host = "wss://" + host + "/ws/v1"
				log.Debugf("转换ASR服务地址: %s -> %s", originalHost, host)
			}
		}
		nlsConfig = nls.NewConnectionConfigWithToken(host, config.AppKey, config.Token)
	} else {
		return nil, fmt.Errorf("必须提供AccessKey或Token")
	}

	client.nlsConfig = nlsConfig

	// 创建NLS Logger
	client.logger = nls.NewNlsLogger(nil, "AliyunASR", 0)
	client.logger.SetLogSil(true) // 禁用SDK内部日志，使用我们自己的日志

	log.Info("阿里云ASR客户端创建成功")
	return client, nil
}

// Connect 建立连接并初始化识别器
func (c *StreamingClient) Connect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.state != StateDisconnected {
		return fmt.Errorf("客户端状态错误: %v", c.state)
	}

	c.state = StateConnecting

	// 创建语音识别实例
	st, err := nls.NewSpeechTranscription(c.nlsConfig, c.logger,
		c.onTaskFailed,    // onTaskFailed
		c.onStarted,       // onStarted
		c.onSentenceBegin, // onSentenceBegin
		c.onSentenceEnd,   // onSentenceEnd
		c.onResultChanged, // onResultChanged
		c.onCompleted,     // onCompleted
		c.onClose,         // onClose
		c)                 // param传递自己

	if err != nil {
		c.state = StateDisconnected
		return fmt.Errorf("创建语音识别实例失败: %w", err)
	}

	c.st = st

	// 创建上下文
	c.ctx, c.cancel = context.WithCancel(context.Background())

	c.state = StateConnected
	c.ready = false
	c.processing = false

	log.Info("阿里云ASR连接建立成功")
	return nil
}

// Start 开始识别
func (c *StreamingClient) Start() error {
	c.mutex.RLock()
	st := c.st
	c.mutex.RUnlock()

	if st == nil {
		return fmt.Errorf("语音识别实例未初始化")
	}

	// 设置识别参数
	param := nls.DefaultSpeechTranscriptionParam()
	param.Format = "pcm"
	param.SampleRate = 16000
	param.EnableIntermediateResult = true
	param.EnablePunctuationPrediction = true
	param.EnableInverseTextNormalization = true

	if c.config.MaxSentenceSilence > 0 {
		param.MaxSentenceSilence = c.config.MaxSentenceSilence
	} else {
		param.MaxSentenceSilence = 800
	}

	// 启动识别
	readyChan, err := st.Start(param, nil)
	if err != nil {
		return fmt.Errorf("启动语音识别失败: %w", err)
	}

	// 等待准备就绪
	go func() {
		select {
		case ready := <-readyChan:
			c.mutex.Lock()
			c.ready = ready
			c.processing = ready
			if ready {
				c.state = StateReady
			}
			c.mutex.Unlock()

			if ready {
				log.Info("阿里云ASR服务器已准备，可以发送音频数据")
			} else {
				log.Error("阿里云ASR启动失败")
				select {
				case c.errorChan <- fmt.Errorf("ASR启动失败"):
				default:
				}
			}
		case <-time.After(10 * time.Second):
			log.Error("阿里云ASR启动超时")
			select {
			case c.errorChan <- fmt.Errorf("ASR启动超时"):
			default:
			}
		}
	}()

	return nil
}

// SendAudio 发送音频数据
func (c *StreamingClient) SendAudio(data []byte) error {
	c.mutex.RLock()
	ready := c.ready && c.st != nil
	st := c.st
	c.mutex.RUnlock()

	if !ready {
		return fmt.Errorf("客户端未准备好")
	}

	err := st.SendAudioData(data)
	if err != nil {
		return fmt.Errorf("发送音频数据失败: %w", err)
	}

	return nil
}

// Stop 停止识别
func (c *StreamingClient) Stop() error {
	c.mutex.RLock()
	st := c.st
	c.mutex.RUnlock()

	if st == nil {
		return nil
	}

	stopChan, err := st.Stop()
	if err != nil {
		return fmt.Errorf("停止识别失败: %w", err)
	}

	// 等待停止完成
	go func() {
		select {
		case <-stopChan:
			log.Info("阿里云ASR停止完成")
		case <-time.After(5 * time.Second):
			log.Warn("阿里云ASR停止超时")
		}
	}()

	return nil
}

// GetResult 获取识别结果通道
func (c *StreamingClient) GetResult() <-chan string {
	return c.resultChan
}

// GetError 获取错误通道
func (c *StreamingClient) GetError() <-chan error {
	return c.errorChan
}

// IsReady 检查是否准备好
func (c *StreamingClient) IsReady() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.ready
}

// Disconnect 断开连接
func (c *StreamingClient) Disconnect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.state == StateDisconnected {
		return nil
	}

	c.state = StateClosing

	// 停止识别
	if c.st != nil && c.processing {
		c.st.Stop()
		time.Sleep(100 * time.Millisecond)
	}

	// 取消上下文
	if c.cancel != nil {
		c.cancel()
	}

	// 关闭语音识别实例
	if c.st != nil {
		c.st.Shutdown()
		c.st = nil
	}

	c.ready = false
	c.processing = false
	c.state = StateDisconnected

	log.Info("阿里云ASR连接已断开")
	return nil
}

// Close 关闭客户端
func (c *StreamingClient) Close() error {
	if err := c.Disconnect(); err != nil {
		return err
	}

	// 关闭通道
	close(c.resultChan)
	close(c.errorChan)

	return nil
}

// 回调函数实现

func (c *StreamingClient) onTaskFailed(text string, param interface{}) {
	log.Errorf("阿里云ASR任务失败: %s", text)
	select {
	case c.errorChan <- fmt.Errorf("任务失败: %s", text):
	default:
	}
}

func (c *StreamingClient) onStarted(text string, param interface{}) {
	log.Infof("阿里云ASR已启动: %s", text)
}

func (c *StreamingClient) onSentenceBegin(text string, param interface{}) {
	log.Debugf("阿里云ASR句子开始: %s", text)
}

func (c *StreamingClient) onSentenceEnd(text string, param interface{}) {
	log.Debugf("阿里云ASR句子结束: %s", text)
	if text != "" {
		select {
		case c.resultChan <- text:
		default:
			log.Warn("阿里云ASR结果通道已满，丢弃最终结果")
		}
	}
}

func (c *StreamingClient) onResultChanged(text string, param interface{}) {
	log.Debugf("阿里云ASR中间结果: %s", text)
	if text != "" {
		select {
		case c.resultChan <- text:
		default:
			log.Warn("阿里云ASR结果通道已满，丢弃中间结果")
		}
	}
}

func (c *StreamingClient) onCompleted(text string, param interface{}) {
	log.Infof("阿里云ASR识别完成: %s", text)
	c.mutex.Lock()
	c.processing = false
	c.mutex.Unlock()
}

func (c *StreamingClient) onClose(param interface{}) {
	log.Info("阿里云ASR连接已关闭")
}
