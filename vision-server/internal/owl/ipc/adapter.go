package ipc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vision-server/internal/store"
)

type Adapter struct {
	store *store.Store
}

func NewAdapter(store *store.Store) Adapter {
	return Adapter{store: store}
}

func (a Adapter) SaveChannels(channels []*Channel) error {
	if len(channels) == 0 {
		return nil
	}
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		device, ok := a.store.FindDeviceByExternalID("gb28181", channel.DeviceID)
		if !ok {
			continue
		}
		stored, ok := a.store.FindChannelByExternalID("gb28181", device.ID, channel.ChannelID)
		if !ok {
			stored = &store.Channel{}
		}
		if stored.ID == "" && channel.ChannelID != "" {
			stored.ID = channel.ChannelID
		}
		stored.DeviceID = device.ID
		stored.Protocol = "gb28181"
		stored.ExternalID = channel.ChannelID
		stored.Name = channel.Name
		stored.Online = channel.IsOnline
		if channel.Ext.Manufacturer != "" {
			if stored.Meta == nil {
				stored.Meta = map[string]string{}
			}
			stored.Meta["manufacturer"] = channel.Ext.Manufacturer
			stored.Meta["model"] = channel.Ext.Model
			stored.Meta["firmware"] = channel.Ext.Firmware
		}
		a.store.SaveChannel(stored)
	}
	return nil
}

func (a Adapter) GetDeviceByDeviceID(deviceID string) (*Device, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, errors.New("device id is empty")
	}
	if device, ok := a.store.FindDeviceByExternalID("gb28181", deviceID); ok {
		return mapStoreDevice(device), nil
	}
	dev := &store.Device{
		Protocol:   "gb28181",
		ExternalID: deviceID,
		Name:       deviceID,
		Online:     false,
	}
	dev = a.store.SaveDevice(dev)
	return mapStoreDevice(dev), nil
}

func (a Adapter) Edit(deviceID string, fn func(*Device)) error {
	device, ok := a.store.FindDeviceByExternalID("gb28181", deviceID)
	if !ok {
		return fmt.Errorf("device not found: %s", deviceID)
	}
	mapped := mapStoreDevice(device)
	fn(mapped)
	applyDeviceUpdate(device, mapped)
	a.store.SaveDevice(device)
	return nil
}

func (a Adapter) EditPlaying(ctx context.Context, deviceID, channelID string, playing bool) error {
	device, ok := a.store.FindDeviceByExternalID("gb28181", deviceID)
	if !ok {
		return fmt.Errorf("device not found: %s", deviceID)
	}
	channel, ok := a.store.FindChannelByExternalID("gb28181", device.ID, channelID)
	if !ok {
		return fmt.Errorf("channel not found: %s", channelID)
	}
	channel.Playing = playing
	a.store.SaveChannel(channel)
	return nil
}

func mapStoreDevice(dev *store.Device) *Device {
	meta := dev.Meta
	if meta == nil {
		meta = map[string]string{}
	}
	out := &Device{
		ID:         dev.ID,
		Type:       "GB28181",
		DeviceID:   dev.ExternalID,
		Name:       dev.Name,
		Transport:  meta["transport"],
		IP:         meta["ip"],
		Port:       0,
		IsOnline:   dev.Online,
		Password:   dev.Password,
		Address:    dev.Address,
		Username:   dev.Username,
		StreamMode: 1,
		Ext: DeviceExt{
			Manufacturer: meta["manufacturer"],
			Model:        meta["model"],
			Firmware:     meta["firmware"],
			Name:         meta["device_name"],
			GBVersion:    meta["gb_version"],
		},
	}
	return out
}

func applyDeviceUpdate(target *store.Device, source *Device) {
	target.Name = source.Name
	target.Online = source.IsOnline
	target.Username = source.Username
	target.Password = source.Password
	target.Address = source.Address
	if target.Meta == nil {
		target.Meta = map[string]string{}
	}
	if source.Transport != "" {
		target.Meta["transport"] = source.Transport
	}
	if source.Ext.GBVersion != "" {
		target.Meta["gb_version"] = source.Ext.GBVersion
	}
	if source.Ext.Manufacturer != "" {
		target.Meta["manufacturer"] = source.Ext.Manufacturer
	}
	if source.Ext.Model != "" {
		target.Meta["model"] = source.Ext.Model
	}
	if source.Ext.Firmware != "" {
		target.Meta["firmware"] = source.Ext.Firmware
	}
	if source.Ext.Name != "" {
		target.Meta["device_name"] = source.Ext.Name
	}
}
