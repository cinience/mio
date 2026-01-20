package ipc

import "vision-server/internal/owl/orm"

const (
	TypeGB28181 = "GB28181"
	TypeOnvif   = "ONVIF"
)

type DeviceExt struct {
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	Firmware     string `json:"firmware,omitempty"`
	Name         string `json:"name,omitempty"`
	GBVersion    string `json:"gbVersion,omitempty"`
}

type Device struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	DeviceID     string    `json:"device_id"`
	Name         string    `json:"name"`
	Transport    string    `json:"transport"`
	StreamMode   int8      `json:"stream_mode"`
	IP           string    `json:"ip"`
	Port         int       `json:"port"`
	IsOnline     bool      `json:"is_online"`
	RegisteredAt orm.Time  `json:"registered_at"`
	KeepaliveAt  orm.Time  `json:"keepalive_at"`
	Expires      int       `json:"expires"`
	Channels     int       `json:"channels"`
	Password     string    `json:"password"`
	Address      string    `json:"address"`
	Ext          DeviceExt `json:"ext"`
	Username     string    `json:"username"`
}

func (d *Device) GetGB28181DeviceID() string {
	return d.DeviceID
}

func (d *Device) GetUsername() string {
	if d.Username != "" {
		return d.Username
	}
	return d.DeviceID
}

func (d *Device) GetPassword() string {
	return d.Password
}

func (d *Device) IsOnvif() bool {
	return d.Type == TypeOnvif
}

func (d *Device) IsGB28181() bool {
	return d.Type == TypeGB28181 || d.Type == ""
}

type Channel struct {
	ID        string    `json:"id"`
	DID       string    `json:"did"`
	DeviceID  string    `json:"device_id"`
	ChannelID string    `json:"channel_id"`
	Name      string    `json:"name"`
	IsOnline  bool      `json:"is_online"`
	IsPlaying bool      `json:"is_playing"`
	Ext       DeviceExt `json:"ext"`
	Type      string    `json:"type"`
}
