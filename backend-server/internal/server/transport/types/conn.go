package types

import "context"

// IConn 是协议无关的连接接口，由 websocket/mqtt_udp 等协议适配器实现
// 你可以根据实际需要扩展方法

const (
	TransportTypeWebsocket = "websocket"
	TransportTypeMqttUdp   = "udp"
	TransportTypeServer    = "server"
)

type IConn interface {
	// SendCmd 发送命令/信令数据
	SendCmd(msg []byte) error
	// RecvCmd 接收命令/信令数据
	RecvCmd(ctx context.Context, timeout int) ([]byte, error)
	// SendAudio 发送语音数据
	SendAudio(audio []byte) error
	// RecvAudio 接收语音数据
	RecvAudio(ctx context.Context, timeout int) ([]byte, error)

	GetDeviceID() string

	GetRemoteAddr() string

	// GetExternalAddr 返回服务端当前对外可见的地址（通常为本地监听地址或代理提供的地址）
	GetExternalAddr() string

	Close() error

	OnClose(func(deviceId string))

	CloseAudioChannel() error

	GetTransportType() string

	// GetData 获取私有数据
	GetData(key string) (interface{}, error)
}

type OnNewConnection func(conn IConn)

// ProtocolVersionAware describes transports that can negotiate binary protocol versions.
type ProtocolVersionAware interface {
	SetProtocolVersion(version int)
	GetProtocolVersion() int
}
