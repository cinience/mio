package ipc

import (
	"testing"

	"vision-server/internal/store"
)

func TestSaveChannelsUsesExternalIDAsChannelID(t *testing.T) {
	st := store.NewStore()
	device := &store.Device{
		Protocol:   "gb28181",
		ExternalID: "34020000002000000001",
		Name:       "gb-device",
	}
	st.SaveDevice(device)

	adapter := NewAdapter(st)
	ch := &Channel{
		DeviceID:  "34020000002000000001",
		ChannelID: "34020000001320000001",
		Name:      "channel-1",
		Type:      TypeGB28181,
	}

	if err := adapter.SaveChannels([]*Channel{ch}); err != nil {
		t.Fatalf("SaveChannels failed: %v", err)
	}

	saved, ok := st.FindChannelByExternalID("gb28181", device.ID, "34020000001320000001")
	if !ok || saved == nil {
		t.Fatalf("channel not saved")
	}
	if saved.ID != "34020000001320000001" {
		t.Fatalf("expected channel id to match external id, got %q", saved.ID)
	}
	if saved.DeviceID != device.ID {
		t.Fatalf("expected device id %q, got %q", device.ID, saved.DeviceID)
	}
}
