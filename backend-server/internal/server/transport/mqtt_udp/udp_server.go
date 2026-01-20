package mqtt_udp

import (
	"context"
	"crypto/aes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"

	. "backend-server/internal/infrastructure/logger"
	log "backend-server/internal/infrastructure/logger"
)

// UDPServer UDP服务器结构
/*
type UDPServer struct {
	conn       *net.UDPConn
	sessions   map[string]*Session
	mqttServer *MqttServer
	udpPort    int
	sync.RWMutex
}*/

type UdpServer struct {
	conn          *net.UDPConn
	udpPort       int      //udp server listen port
	externalHost  string   //udp server external host
	externalPort  int      //udp server external port
	nonce2Session sync.Map //nonce => UdpSession
	addr2Session  sync.Map //addr => UdpSession
	mqttAdapter   *MqttUdpAdapter

	// 生命周期控制
	ctx    context.Context
	cancel context.CancelFunc
	sync.RWMutex
}

// NewUDPServer 创建新的UDP服务器
func NewUDPServer(udpPort int, externalHost string, externalPort int) *UdpServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &UdpServer{
		udpPort:       udpPort,
		externalHost:  externalHost,
		externalPort:  externalPort,
		nonce2Session: sync.Map{},
		addr2Session:  sync.Map{},
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start 启动UDP服务器
func (s *UdpServer) Start() error {
	addr := &net.UDPAddr{
		IP:   net.ParseIP("0.0.0.0"),
		Port: s.udpPort,
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("监听UDP失败: %v", err)
	}

	s.conn = conn
	Infof("UDP服务器启动在 %s:%d", "0.0.0.0", s.udpPort)

	// 启动会话清理
	//go s.cleanupSessions()

	// 启动数据包处理
	go s.handlePackets()

	return nil
}

// handlePackets 处理接收到的数据包
func (s *UdpServer) handlePackets() {
	buffer := make([]byte, 4096) // 使用默认的缓冲区大小
	for {
		select {
		case <-s.ctx.Done():
			log.Info("UDP数据包处理goroutine已停止")
			return
		default:
			// 设置读取超时，避免阻塞
			s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, addr, err := s.conn.ReadFromUDP(buffer)
			if err != nil {
				// 检查是否是超时错误
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // 超时是正常的，继续下一次循环检查 ctx
				}
				// 检查是否是关闭错误
				if err.Error() == "use of closed network connection" {
					log.Info("UDP连接已关闭")
					return
				}
				Errorf("读取UDP数据失败: %v", err)
				continue
			}

			// 复制数据，避免并发修改
			data := make([]byte, n)
			copy(data, buffer[:n])

			// 处理数据包
			s.processPacket(addr, data)
		}
	}
}

func (s *UdpServer) getSessionByNonce(connID string) *UdpSession {
	val, ok := s.nonce2Session.Load(connID)
	if ok {
		return val.(*UdpSession)
	}
	return nil
}

// processPacket 处理单个数据包
func (s *UdpServer) processPacket(addr *net.UDPAddr, data []byte) {
	// 检查数据包大小
	if len(data) < 16 {
		Warn("数据包太小")
		return
	}

	var udpSession *UdpSession
	//从addr
	udpSession = s.getUdpSession(addr)
	if udpSession == nil {
		// 获取会话ID
		fullNonce := data[:16]
		connID := fullNonce[4:8] // 取5-8字节作为连接id
		strConnID := hex.EncodeToString(connID)
		//Debugf("收到数据包, fullNonce: %s, connID: %s", hex.EncodeToString(fullNonce), strConnID)
		udpSession = s.getSessionByNonce(strConnID)
		if udpSession == nil {
			Warnf("session不存在 addr: %s", addr)
			return
		}
		udpSession.RemoteAddr = addr
		s.addUdpSession(addr, udpSession)
	}

	if udpSession == nil {
		Warnf("udpSession不存在 addr: %s", addr)
		return
	}

	// 更新最后活动时间
	udpSession.LastActive = time.Now()

	decrypted, err := udpSession.Decrypt(data)
	if err != nil {
		Errorf("addr: %s 解密失败: %v", addr, err)
		return
	}
	Debugf("收到音频数据, addr: %s, 大小: %d 字节", addr, len(decrypted))
	select {
	case udpSession.RecvChannel <- decrypted:
		return
	default:
		Warnf("udpSession.RecvChannel is full, addr: %s", addr)
	}
}

// cleanupSessions 清理过期会话
func (s *UdpServer) cleanupSessions() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		now := time.Now()
		s.nonce2Session.Range(func(key, value interface{}) bool {
			session := value.(*UdpSession)
			if now.Sub(session.LastActive) > 5*time.Minute {
				s.nonce2Session.Delete(key)
				Infof("清理过期会话: %s", key)
			}
			return true
		})
	}
}

// CreateSession 创建新会话
func (s *UdpServer) CreateSession(deviceId, clientId string) *UdpSession {
	// 生成会话ID
	sessionID := generateSessionID()

	// 生成AES密钥
	key := make([]byte, 16)
	rand.Read(key)

	// 生成4字节连接id
	connID := make([]byte, 4)
	rand.Read(connID)
	strConnID := hex.EncodeToString(connID)

	// 4字节时间戳
	timestamp := make([]byte, 4)
	binary.BigEndian.PutUint32(timestamp, uint32(time.Now().Unix()))

	// 拼接nonce: 4字节连接id + 4字节时间戳
	nonce := append(connID, timestamp...)

	// 创建AES块
	block, err := aes.NewCipher(key)
	if err != nil {
		Errorf("创建AES块失败: %v", err)
		return nil
	}

	// 将key转换为[16]byte
	aesKey := [16]byte{}
	copy(aesKey[:], key)

	// 将nonce转换为[8]byte
	nonceBytes := [8]byte{}
	copy(nonceBytes[:], nonce)

	// 创建会话
	session := &UdpSession{
		ID:          sessionID,
		ConnId:      strConnID,
		ClientId:    clientId,
		DeviceId:    deviceId,
		AesKey:      aesKey,
		Nonce:       nonceBytes, // 保存原始nonce模板
		CreatedAt:   time.Now(),
		LastActive:  time.Now(),
		Block:       block,
		RecvChannel: make(chan []byte, 100),
		SendChannel: make(chan []byte, 100),
	}
	//通过channel发送音频数据, 当channel关闭的时候停止
	go func() {
		for data := range session.SendChannel {
			if session.RemoteAddr == nil {
				continue
			}
			encrypted, err := session.Encrypt(data)
			if err != nil {
				Errorf("加密失败: %v", err)
				continue
			}
			//Debugf("发送音频数据, nonce: %s, 大小: %d 字节", hex.EncodeToString(encrypted[:16]), len(encrypted))
			_, err = s.conn.WriteToUDP(encrypted, session.RemoteAddr)
			if err != nil {
				Errorf("发送音频数据失败: %v", err)
				continue
			}
			//Debugf("发送音频数据成功, nonce: %s, 大小: %d 字节, 发送字节数: %d", hex.EncodeToString(encrypted[:16]), len(encrypted), n)
		}
	}()

	// 只用连接id（前4字节）作为key
	s.SetNonce2Session(strConnID, session)

	return session
}

// CloseSession 关闭会话
func (s *UdpServer) CloseSession(connID string) {
	session := s.getSessionByNonce(connID)
	if session != nil {
		s.addr2Session.Delete(session.RemoteAddr.String())
		session.Destroy()
	}
	s.nonce2Session.Delete(connID)
}

func (s *UdpServer) SetNonce2Session(connID string, session *UdpSession) {
	Debugf("SetNonce2Session, connID: %s, session: %+v", connID, session)
	s.nonce2Session.Store(connID, session)
}

// GetSession 获取会话信息
func (s *UdpServer) GetNonce(connID string) *UdpSession {
	val, ok := s.nonce2Session.Load(connID)
	if ok {
		return val.(*UdpSession)
	}
	return nil
}

// generateSessionID 生成会话ID
func generateSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *UdpServer) getUdpSession(addr *net.UDPAddr) *UdpSession {
	val, ok := s.addr2Session.Load(addr.String())
	if ok {
		return val.(*UdpSession)
	}
	return nil
}

func (s *UdpServer) addUdpSession(addr *net.UDPAddr, session *UdpSession) {
	s.addr2Session.Store(addr.String(), session)
}

func (s *UdpServer) removeUdpSession(addr *net.UDPAddr) {
	s.addr2Session.Delete(addr.String())
}

// Shutdown 优雅关闭UDP服务器
func (s *UdpServer) Shutdown(ctx context.Context) error {
	s.Lock()
	defer s.Unlock()

	log.Info("正在关闭 UDP 服务器...")

	// 取消所有内部操作
	s.cancel()

	// 关闭所有会话
	s.addr2Session.Range(func(key, value interface{}) bool {
		if session, ok := value.(*UdpSession); ok {
			log.Debugf("关闭UDP会话: %v", key)
			s.CloseSession(session.ConnId)
		}
		return true
	})

	// 关闭UDP连接
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			log.Errorf("关闭UDP连接失败: %v", err)
			return err
		}
	}

	log.Info("UDP 服务器已关闭")
	return nil
}
