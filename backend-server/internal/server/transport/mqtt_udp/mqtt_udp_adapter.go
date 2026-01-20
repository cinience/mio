package mqtt_udp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	. "backend-server/internal/infrastructure/logger"
	log "backend-server/internal/infrastructure/logger"
	state "backend-server/internal/server/chat/session/state"
	"backend-server/internal/server/transport/types"
)

type MqttConfig struct {
	Broker   string
	Type     string
	Port     int
	ClientID string
	Username string
	Password string
}

// MqttUdpAdapter MQTT-UDP适配器结构
type MqttUdpAdapter struct {
	client          mqtt.Client
	udpServer       *UdpServer
	mqttConfig      *MqttConfig
	deviceId2Conn   *sync.Map
	msgChan         chan mqtt.Message
	onNewConnection types.OnNewConnection

	// 生命周期控制
	ctx    context.Context
	cancel context.CancelFunc
	sync.RWMutex
}

// MqttUdpAdapterOption 用于可选参数
type MqttUdpAdapterOption func(*MqttUdpAdapter)

// WithUdpServer 设置 udpServer
func WithUdpServer(udpServer *UdpServer) MqttUdpAdapterOption {
	return func(s *MqttUdpAdapter) {
		s.udpServer = udpServer
	}
}

func WithOnNewConnection(onNewConnection types.OnNewConnection) MqttUdpAdapterOption {
	return func(s *MqttUdpAdapter) {
		s.onNewConnection = onNewConnection
	}
}

// NewMqttUdpAdapter 创建新的MQTT-UDP适配器，config为必传，其它参数用Option
func NewMqttUdpAdapter(config *MqttConfig, opts ...MqttUdpAdapterOption) *MqttUdpAdapter {
	ctx, cancel := context.WithCancel(context.Background())
	s := &MqttUdpAdapter{
		mqttConfig:    config,
		deviceId2Conn: &sync.Map{},
		msgChan:       make(chan mqtt.Message, 10000),
		ctx:           ctx,
		cancel:        cancel,
	}
	for _, opt := range opts {
		opt(s)
	}

	go s.processMessage()
	return s
}

// Start 启动MQTT服务器
func (s *MqttUdpAdapter) Start() error {
	const retryInterval = 5 * time.Second

	Info("MqttUdpAdapter开始启动，尝试连接MQTT服务器...")
	Infof("MQTT配置: Broker=%s:%d, ClientID=%s, Username=%s", s.mqttConfig.Broker, s.mqttConfig.Port, s.mqttConfig.ClientID, s.mqttConfig.Username)

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("%s://%s:%d", s.mqttConfig.Type, s.mqttConfig.Broker, s.mqttConfig.Port))
	opts.SetClientID(s.mqttConfig.ClientID)
	opts.SetUsername(s.mqttConfig.Username)
	opts.SetPassword(s.mqttConfig.Password)

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		Errorf("MQTT连接丢失: %v", err)
	})

	opts.SetOnConnectHandler(func(client mqtt.Client) {
		Info("MQTT已连接")
		// 订阅客户端消息主题
		topic := state.ServerSubTopicPrefix // 默认主题前缀
		if token := client.Subscribe(topic, 0, s.handleMessage); token.Wait() && token.Error() != nil {
			Errorf("订阅主题失败: %v", token.Error())
		}
	})

	var lastErr error
	var retryCount int
	for {
		s.client = mqtt.NewClient(opts)
		if token := s.client.Connect(); token.Wait() && token.Error() != nil {
			lastErr = token.Error()
			retryCount++
			Errorf("连接MQTT服务器失败(第%d次): %v，%d秒后重试", retryCount, lastErr, int(retryInterval.Seconds()))
			time.Sleep(retryInterval)
			continue
		} else if token != nil && token.Error() == nil {
			lastErr = nil
			break
		}
	}

	if lastErr != nil {
		return fmt.Errorf("连接MQTT服务器失败: %v", lastErr)
	}

	err := s.checkClientActive()
	if err != nil {
		Errorf("检查客户端活跃失败: %v", err)
		return err
	}

	return nil
}

func (s *MqttUdpAdapter) checkClientActive() error {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.deviceId2Conn.Range(func(key, value interface{}) bool {
					conn := value.(*MqttUdpConn)
					if !conn.IsActive() {
						conn.Destroy()
					}
					return true
				})
			}
		}
	}()
	return nil
}

func (s *MqttUdpAdapter) SetDeviceSession(deviceId string, conn *MqttUdpConn) {
	Debugf("SetDeviceSession, deviceId: %s", deviceId)
	s.deviceId2Conn.Store(deviceId, conn)
}

func (s *MqttUdpAdapter) getDeviceSession(deviceId string) *MqttUdpConn {
	Debugf("getDeviceSession, deviceId: %s", deviceId)
	if conn, ok := s.deviceId2Conn.Load(deviceId); ok {
		return conn.(*MqttUdpConn)
	}
	return nil
}

// handleMessage 将消息丢进队列
func (s *MqttUdpAdapter) handleMessage(client mqtt.Client, msg mqtt.Message) {
	select {
	case s.msgChan <- msg:
		return
	default:
		Debugf("handleMessage msg chan is full, topic: %s, payload: %s", msg.Topic(), string(msg.Payload()))
	}
}

// 断开连接，超时或goodbye主动断开
func (s *MqttUdpAdapter) handleDisconnect(deviceId string) {
	Debugf("handleDisconnect, deviceId: %s", deviceId)

	conn := s.getDeviceSession(deviceId)
	if conn == nil {
		Debugf("handleDisconnect, deviceId: %s not found", deviceId)
		return
	}
	s.udpServer.CloseSession(conn.UdpSession.ConnId)
	s.deviceId2Conn.Delete(deviceId)
}

// 处理消息
func (s *MqttUdpAdapter) processMessage() {
	for {
		select {
		case msg := <-s.msgChan:
			Debugf("mqtt handleMessage, topic: %s, payload: %s", msg.Topic(), string(msg.Payload()))
			var clientMsg state.ClientMessage
			if err := json.Unmarshal(msg.Payload(), &clientMsg); err != nil {
				Errorf("解析JSON失败: %v", err)
				continue
			}
			topicMacAddr, deviceId := s.getDeviceIdByTopic(msg.Topic())
			if deviceId == "" {
				Errorf("mac_addr解析失败: %v", msg.Topic())
				continue
			}

			deviceSession := s.getDeviceSession(deviceId)
			if deviceSession == nil {
				// 从UDP服务端获取会话信息
				udpSession := s.udpServer.CreateSession(deviceId, "")
				if udpSession == nil {
					Errorf("创建 udpSession 失败, deviceId: %s", deviceId)
					continue
				}

				publicTopic := fmt.Sprintf("%s%s", state.ServerPubTopicPrefix, topicMacAddr)

				deviceSession = NewMqttUdpConn(deviceId, publicTopic, s.client, s.udpServer, udpSession)

				strAesKey, strFullNonce := udpSession.GetAesKeyAndNonce()
				deviceSession.SetData("aes_key", strAesKey)
				deviceSession.SetData("full_nonce", strFullNonce)

				//保存至deviceId2UdpSession
				s.SetDeviceSession(deviceId, deviceSession)

				deviceSession.OnClose(s.handleDisconnect)

				s.onNewConnection(deviceSession)
			}

			err := deviceSession.PushMsgToRecvCmd(msg.Payload())
			if err != nil {
				Errorf("InternalRecvCmd失败: %v", err)
				continue
			}
		case <-s.ctx.Done():
			log.Info("消息处理goroutine已停止")
			return
		}
	}
}

// Shutdown 优雅关闭MQTT UDP适配器
func (s *MqttUdpAdapter) Shutdown(ctx context.Context) error {
	s.Lock()
	defer s.Unlock()

	log.Info("正在关闭 MQTT UDP 适配器...")

	// 取消所有内部操作
	s.cancel()

	// 关闭所有设备连接
	s.deviceId2Conn.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*MqttUdpConn); ok {
			log.Debugf("关闭设备连接: %v", key)
			conn.Close()
		}
		return true
	})

	// 断开MQTT客户端
	if s.client != nil && s.client.IsConnected() {
		s.client.Disconnect(250) // 250ms超时
	}

	// 关闭UDP服务器
	if s.udpServer != nil {
		if err := s.udpServer.Shutdown(ctx); err != nil {
			log.Errorf("关闭UDP服务器失败: %v", err)
			return err
		}
	}

	log.Info("MQTT UDP 适配器已关闭")
	return nil
}

func (s *MqttUdpAdapter) getDeviceIdByTopic(topic string) (string, string) {
	var topicMacAddr, deviceId string
	//根据topic(/p2p/device_public/mac_addr)解析出来mac_addr
	strList := strings.Split(topic, "/")
	if len(strList) == 4 {
		topicMacAddr = strList[3]

		// 检查是否为新格式: "GID_test@@@ba_8f_17_de_94_94@@@e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6d"
		if strings.Contains(topicMacAddr, "@@@") {
			parts := strings.Split(topicMacAddr, "@@@")
			if len(parts) >= 2 {
				// 提取中间部分作为MAC地址
				macAddr := parts[1]
				deviceId = strings.ReplaceAll(macAddr, "_", ":")
			}
		} else {
			deviceId = strings.ReplaceAll(topicMacAddr, "_", ":")
		}
	}

	log.Log().Debugf("topicMacAddr: %s, deviceId: %s", topicMacAddr, deviceId)
	return topicMacAddr, deviceId
}
