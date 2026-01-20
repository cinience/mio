package onvifadapter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"vision-server/internal/store"

	"github.com/gowvp/onvif"
	devicemodel "github.com/gowvp/onvif/device"
	m "github.com/gowvp/onvif/media"
	sdkdevice "github.com/gowvp/onvif/sdk/device"
	sdkmedia "github.com/gowvp/onvif/sdk/media"
	xsdonvif "github.com/gowvp/onvif/xsd/onvif"
)

type Adapter struct {
	store  *store.Store
	client *http.Client
}

func NewAdapter(store *store.Store) *Adapter {
	client := *http.DefaultClient
	client.Timeout = 4 * time.Second
	return &Adapter{store: store, client: &client}
}

func (a *Adapter) AddDevice(ctx context.Context, payload *store.Device) (*store.Device, []*store.Channel, error) {
	onvifDev, err := onvif.NewDevice(onvif.DeviceParams{
		Xaddr:      payload.Address,
		Username:   payload.Username,
		Password:   payload.Password,
		HttpClient: a.client,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("onvif init failed: %w", err)
	}

	info, err := sdkdevice.Call_GetDeviceInformation(ctx, onvifDev, devicemodel.GetDeviceInformation{})
	if err != nil {
		return nil, nil, fmt.Errorf("onvif auth failed: %w", err)
	}

	payload.Protocol = "onvif"
	payload.Online = true
	if payload.Meta == nil {
		payload.Meta = map[string]string{}
	}
	payload.Meta["manufacturer"] = info.Manufacturer
	payload.Meta["model"] = info.Model
	payload.Meta["firmware"] = info.FirmwareVersion
	payload = a.store.SaveDevice(payload)

	profiles, err := sdkmedia.Call_GetProfiles(ctx, onvifDev, m.GetProfiles{})
	if err != nil {
		return payload, nil, fmt.Errorf("onvif get profiles failed: %w", err)
	}

	channels := make([]*store.Channel, 0, len(profiles.Profiles))
	for _, profile := range profiles.Profiles {
		streamURI, err := getStreamURI(ctx, onvifDev, string(profile.Token), payload.Username, payload.Password)
		if err != nil {
			continue
		}
		channel := &store.Channel{
			DeviceID:   payload.ID,
			Name:       string(profile.Name),
			Protocol:   "onvif",
			StreamURL:  streamURI,
			StreamRTSP: streamURI,
			ExternalID: string(profile.Token),
			Online:     true,
		}
		channel = a.store.SaveChannel(channel)
		channels = append(channels, channel)
	}
	return payload, channels, nil
}

func getStreamURI(ctx context.Context, dev *onvif.Device, profileToken, username, password string) (string, error) {
	var param m.GetStreamUri
	param.StreamSetup.Transport.Protocol = "RTSP"
	param.StreamSetup.Stream = "RTP-Unicast"
	param.ProfileToken = xsdonvif.ReferenceToken(profileToken)
	resp, err := sdkmedia.Call_GetStreamUri(ctx, dev, param)
	if err != nil {
		return "", err
	}
	return buildPlayURL(string(resp.MediaUri.Uri), username, password), nil
}

func buildPlayURL(rawurl, username, password string) string {
	if username != "" && password != "" {
		return strings.Replace(rawurl, "rtsp://", fmt.Sprintf("rtsp://%s:%s@", username, password), 1)
	}
	return rawurl
}
