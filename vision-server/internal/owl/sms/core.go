package sms

import (
	"context"
	"fmt"
	"strings"

	"vision-server/internal/owl/conf"
	"vision-server/internal/owl/zlm"
)

const ProtocolZLMediaKit = "zlm"

type Core struct {
	NodeManager *NodeManager
}

func NewCore(cfg *conf.Bootstrap) Core {
	media := &MediaServer{
		ID:           "vision-default",
		IP:           cfg.Media.IP,
		Ports:        MediaServerPorts{HTTP: cfg.Media.HTTPPort},
		Secret:       cfg.Media.Secret,
		Type:         cfg.Media.Type,
		SDPIP:        cfg.Media.SDPIP,
		RTPPortRange: cfg.Media.RTPPortRange,
	}
	if media.Type == "" {
		media.Type = ProtocolZLMediaKit
	}
	manager := NewNodeManager(media)
	_ = manager.Refresh(context.Background())
	return Core{NodeManager: manager}
}

type NodeManager struct {
	engine zlm.Engine
	media  *MediaServer
}

func NewNodeManager(media *MediaServer) *NodeManager {
	return &NodeManager{
		engine: zlm.NewEngine(),
		media:  media,
	}
}

func (n *NodeManager) Default() *MediaServer {
	return n.media
}

func (n *NodeManager) Refresh(ctx context.Context) error {
	if n.media == nil {
		return fmt.Errorf("media server config is nil")
	}
	if n.media.IP == "" || n.media.Ports.HTTP == 0 {
		return fmt.Errorf("media server http config is missing")
	}
	engine := n.engine.SetConfig(zlm.Config{
		URL:    fmt.Sprintf("http://%s:%d", n.media.IP, n.media.Ports.HTTP),
		Secret: n.media.Secret,
	})
	resp, err := engine.GetServerConfig()
	if err != nil {
		return err
	}
	if len(resp.Data) == 0 {
		return fmt.Errorf("zlm server config empty")
	}
	cfg := resp.Data[0]
	if cfg.RtspPort > 0 {
		n.media.Ports.RTSP = cfg.RtspPort
	}
	if cfg.RtmpPort > 0 {
		n.media.Ports.RTMP = cfg.RtmpPort
	}
	return nil
}

func (n *NodeManager) OpenRTPServer(ms *MediaServer, req zlm.OpenRTPServerRequest) (*zlm.OpenRTPServerResponse, error) {
	engine := n.engine.SetConfig(zlm.Config{
		URL:    fmt.Sprintf("http://%s:%d", ms.IP, ms.Ports.HTTP),
		Secret: ms.Secret,
	})
	return engine.OpenRTPServer(req)
}

func (n *NodeManager) CloseRTPServer(ms *MediaServer, req zlm.CloseRTPServerRequest) (*zlm.CloseRTPServerResponse, error) {
	engine := n.engine.SetConfig(zlm.Config{
		URL:    fmt.Sprintf("http://%s:%d", ms.IP, ms.Ports.HTTP),
		Secret: ms.Secret,
	})
	return engine.CloseRTPServer(req)
}

func (n *NodeManager) GetSnapshot(ms *MediaServer, req zlm.GetSnapRequest) ([]byte, error) {
	engine := n.engine.SetConfig(zlm.Config{
		URL:    fmt.Sprintf("http://%s:%d", ms.IP, ms.Ports.HTTP),
		Secret: ms.Secret,
	})
	return engine.GetSnap(req)
}

func (n *NodeManager) BuildSnapRequest(url string) zlm.GetSnapRequest {
	return zlm.GetSnapRequest{
		URL:        url,
		TimeoutSec: 10,
		ExpireSec:  15,
	}
}

func (n *NodeManager) GetStreamLiveAddr(ms *MediaServer, app, stream string) StreamLiveAddr {
	httpPrefix := fmt.Sprintf("http://%s:%d", ms.IP, ms.Ports.HTTP)
	wsPrefix := strings.Replace(strings.Replace(httpPrefix, "https", "wss", 1), "http", "ws", 1)
	rtcPrefix := strings.Replace(strings.Replace(httpPrefix, "https", "webrtc", 1), "http", "webrtc", 1)
	return StreamLiveAddr{
		WSFLV:   fmt.Sprintf("%s/proxy/sms/%s.live.flv", wsPrefix, stream),
		HTTPFLV: fmt.Sprintf("%s/proxy/sms/%s.live.flv", httpPrefix, stream),
		HLS:     fmt.Sprintf("%s/proxy/sms/%s/hls.fmp4.m3u8", httpPrefix, stream),
		WebRTC:  fmt.Sprintf("%s/proxy/sms/index/api/webrtc?app=%s&stream=%s&type=play", rtcPrefix, app, stream),
		RTMP:    fmt.Sprintf("rtmp://%s:%d/%s", ms.IP, ms.Ports.RTMP, stream),
		RTSP:    fmt.Sprintf("rtsp://%s:%d/%s", ms.IP, ms.Ports.RTSP, stream),
	}
}
