package gb28181

import (
	"context"
	"fmt"

	"vision-server/internal/owl/conc"
	"vision-server/internal/owl/gbs"
	"vision-server/internal/owl/gbs/sip"
	"vision-server/internal/owl/ipc"
	"vision-server/internal/store"
)

type MemoryStore struct {
	store   *store.Store
	adapter ipc.Adapter
	devices conc.Map[string, *gbs.Device]
}

func NewMemoryStore(store *store.Store, adapter ipc.Adapter) *MemoryStore {
	return &MemoryStore{store: store, adapter: adapter}
}

func (m *MemoryStore) LoadOrStore(deviceID string, value *gbs.Device) {
	m.devices.LoadOrStore(deviceID, value)
}

func (m *MemoryStore) LoadDeviceToMemory(conn sip.Connection) {
	// In-memory store does not preload GB28181 devices.
}

func (m *MemoryStore) RangeDevices(fn func(key string, value *gbs.Device) bool) {
	m.devices.Range(fn)
}

func (m *MemoryStore) Change(deviceID string, changeFn func(*ipc.Device) error, changeFn2 func(*gbs.Device)) error {
	device, err := m.adapter.GetDeviceByDeviceID(deviceID)
	if err != nil {
		return err
	}
	if err := changeFn(device); err != nil {
		return err
	}
	if err := m.applyDeviceUpdate(deviceID, device); err != nil {
		return err
	}
	if dev, ok := m.devices.Load(deviceID); ok {
		changeFn2(dev)
	}
	return nil
}

func (m *MemoryStore) Load(deviceID string) (*gbs.Device, bool) {
	return m.devices.Load(deviceID)
}

func (m *MemoryStore) Store(deviceID string, value *gbs.Device) {
	m.devices.Store(deviceID, value)
}

func (m *MemoryStore) GetChannel(deviceID, channelID string) (*gbs.Channel, bool) {
	dev, ok := m.devices.Load(deviceID)
	if !ok {
		return nil, false
	}
	return dev.GetChannel(channelID)
}

func (m *MemoryStore) applyDeviceUpdate(deviceID string, source *ipc.Device) error {
	stored, ok := m.store.FindDeviceByExternalID("gb28181", deviceID)
	if !ok {
		return fmt.Errorf("device not found: %s", deviceID)
	}
	stored.Online = source.IsOnline
	stored.Username = source.Username
	stored.Password = source.Password
	stored.Address = source.Address
	if stored.Meta == nil {
		stored.Meta = map[string]string{}
	}
	stored.Meta["transport"] = source.Transport
	if source.Ext.GBVersion != "" {
		stored.Meta["gb_version"] = source.Ext.GBVersion
	}
	if source.Ext.Manufacturer != "" {
		stored.Meta["manufacturer"] = source.Ext.Manufacturer
	}
	if source.Ext.Model != "" {
		stored.Meta["model"] = source.Ext.Model
	}
	if source.Ext.Firmware != "" {
		stored.Meta["firmware"] = source.Ext.Firmware
	}
	if source.Ext.Name != "" {
		stored.Meta["device_name"] = source.Ext.Name
	}
	m.store.SaveDevice(stored)
	return nil
}

func (m *MemoryStore) UpdatePlaying(ctx context.Context, deviceID, channelID string, playing bool) error {
	stored, ok := m.store.FindDeviceByExternalID("gb28181", deviceID)
	if !ok {
		return fmt.Errorf("device not found: %s", deviceID)
	}
	channel, ok := m.store.FindChannelByExternalID("gb28181", stored.ID, channelID)
	if !ok {
		return fmt.Errorf("channel not found: %s", channelID)
	}
	channel.Playing = playing
	m.store.SaveChannel(channel)
	return nil
}
