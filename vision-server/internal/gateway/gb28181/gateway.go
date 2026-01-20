package gb28181

import (
	"context"
	"fmt"
	"strings"

	"vision-server/internal/owl/conf"
	"vision-server/internal/owl/gbs"
	"vision-server/internal/owl/ipc"
	"vision-server/internal/owl/sms"
	"vision-server/internal/store"
)

type Gateway struct {
	store     *store.Store
	adapter   ipc.Adapter
	memory    *MemoryStore
	smsCore   sms.Core
	server    *gbs.Server
	cleanup   func()
	media     *sms.MediaServer
	streamApp string
}

func NewGateway(store *store.Store, cfg *conf.Bootstrap) (*Gateway, error) {
	adapter := ipc.NewAdapter(store)
	memory := NewMemoryStore(store, adapter)
	smsCore := sms.NewCore(cfg)
	media := smsCore.NodeManager.Default()
	if media == nil {
		return nil, fmt.Errorf("media server config missing")
	}
	server, cleanup := gbs.NewServer(cfg, adapter, memory, smsCore)
	return &Gateway{
		store:     store,
		adapter:   adapter,
		memory:    memory,
		smsCore:   smsCore,
		server:    server,
		cleanup:   cleanup,
		media:     media,
		streamApp: "rtp",
	}, nil
}

func (g *Gateway) Close() {
	if g.cleanup != nil {
		g.cleanup()
	}
}

func (g *Gateway) EnsurePlay(ctx context.Context, channel *store.Channel) (sms.StreamLiveAddr, error) {
	if channel == nil {
		return sms.StreamLiveAddr{}, fmt.Errorf("channel is nil")
	}
	device, ok := g.store.GetDevice(channel.DeviceID)
	if !ok {
		return sms.StreamLiveAddr{}, fmt.Errorf("device not found")
	}
	if strings.TrimSpace(device.ExternalID) == "" || strings.TrimSpace(channel.ExternalID) == "" {
		return sms.StreamLiveAddr{}, fmt.Errorf("gb28181 device/channel external id missing")
	}

	ipcChannel := &ipc.Channel{
		ID:        channel.ID,
		DeviceID:  device.ExternalID,
		ChannelID: channel.ExternalID,
		Name:      channel.Name,
		Type:      ipc.TypeGB28181,
	}
	streamMode := deviceMetaStreamMode(device)
	if err := g.server.API().Play(&gbs.PlayInput{
		Channel:    ipcChannel,
		SMS:        g.media,
		StreamMode: streamMode,
	}); err != nil {
		return sms.StreamLiveAddr{}, err
	}

	streamID := channel.ID
	streamPath := fmt.Sprintf("%s/%s", g.streamApp, streamID)
	addr := g.smsCore.NodeManager.GetStreamLiveAddr(g.media, g.streamApp, streamPath)

	channel.StreamRTSP = addr.RTSP
	channel.StreamHLS = addr.HLS
	channel.StreamHTTPFLV = addr.HTTPFLV
	channel.StreamWebRTC = addr.WebRTC
	if channel.StreamURL == "" {
		channel.StreamURL = addr.RTSP
	}
	channel.Playing = true
	channel.Online = true
	g.store.SaveChannel(channel)
	return addr, nil
}

func (g *Gateway) Snapshot(ctx context.Context, channel *store.Channel) ([]byte, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}
	if channel.StreamRTSP == "" {
		if _, err := g.EnsurePlay(ctx, channel); err != nil {
			return nil, err
		}
	}
	return g.smsCore.NodeManager.GetSnapshot(g.media, g.smsCore.NodeManager.BuildSnapRequest(channel.StreamRTSP))
}

func deviceMetaStreamMode(device *store.Device) int8 {
	if device == nil || device.Meta == nil {
		return 1
	}
	switch strings.TrimSpace(device.Meta["stream_mode"]) {
	case "0":
		return 0
	case "2":
		return 2
	default:
		return 1
	}
}
