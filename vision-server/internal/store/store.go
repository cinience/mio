package store

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Device struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Protocol   string            `json:"protocol"`
	Address    string            `json:"address"`
	Username   string            `json:"username,omitempty"`
	Password   string            `json:"password,omitempty"`
	ExternalID string            `json:"externalId,omitempty"`
	Online     bool              `json:"online"`
	Meta       map[string]string `json:"meta,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

type Channel struct {
	ID            string            `json:"id"`
	DeviceID      string            `json:"deviceId"`
	Name          string            `json:"name"`
	Protocol      string            `json:"protocol"`
	StreamURL     string            `json:"streamUrl"`
	StreamRTSP    string            `json:"streamRtsp,omitempty"`
	StreamHLS     string            `json:"streamHls,omitempty"`
	StreamHTTPFLV string            `json:"streamHttpFlv,omitempty"`
	StreamWebRTC  string            `json:"streamWebRtc,omitempty"`
	Username      string            `json:"username,omitempty"`
	Password      string            `json:"password,omitempty"`
	ExternalID    string            `json:"externalId,omitempty"`
	Online        bool              `json:"online"`
	Playing       bool              `json:"playing"`
	Meta          map[string]string `json:"meta,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

type Store struct {
	mu       sync.RWMutex
	devices  map[string]*Device
	channels map[string]*Channel
	counter  atomic.Uint64
}

func NewStore() *Store {
	return &Store{
		devices:  make(map[string]*Device),
		channels: make(map[string]*Channel),
	}
}

func (s *Store) ListDevices() []*Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*Device, 0, len(s.devices))
	for _, device := range s.devices {
		items = append(items, device)
	}
	return items
}

func (s *Store) GetDevice(id string) (*Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	device, ok := s.devices[id]
	return device, ok
}

func (s *Store) SaveDevice(device *Device) *Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	if device.ID == "" {
		device.ID = s.nextID("dev")
	}
	now := time.Now()
	if device.CreatedAt.IsZero() {
		device.CreatedAt = now
	}
	device.UpdatedAt = now
	s.devices[device.ID] = device
	return device
}

func (s *Store) DeleteDevice(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.devices, id)
	for channelID, channel := range s.channels {
		if channel.DeviceID == id {
			delete(s.channels, channelID)
		}
	}
}

func (s *Store) ListChannels(deviceID string) []*Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*Channel, 0, len(s.channels))
	for _, channel := range s.channels {
		if deviceID != "" && channel.DeviceID != deviceID {
			continue
		}
		items = append(items, channel)
	}
	return items
}

func (s *Store) GetChannel(id string) (*Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	channel, ok := s.channels[id]
	return channel, ok
}

func (s *Store) SaveChannel(channel *Channel) *Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	if channel.ID == "" {
		channel.ID = s.nextID("ch")
	}
	now := time.Now()
	if channel.CreatedAt.IsZero() {
		channel.CreatedAt = now
	}
	channel.UpdatedAt = now
	s.channels[channel.ID] = channel
	return channel
}

func (s *Store) FindDeviceByExternalID(protocol, externalID string) (*Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, device := range s.devices {
		if protocol != "" && device.Protocol != protocol {
			continue
		}
		if device.ExternalID == externalID {
			return device, true
		}
	}
	return nil, false
}

func (s *Store) FindChannelByExternalID(protocol, deviceID, externalID string) (*Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, channel := range s.channels {
		if protocol != "" && channel.Protocol != protocol {
			continue
		}
		if deviceID != "" && channel.DeviceID != deviceID {
			continue
		}
		if channel.ExternalID == externalID {
			return channel, true
		}
	}
	return nil, false
}

func (s *Store) DeleteChannel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.channels, id)
}

func (s *Store) nextID(prefix string) string {
	seq := s.counter.Add(1)
	return fmt.Sprintf("%s-%d", prefix, seq)
}
