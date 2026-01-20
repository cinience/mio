package state

import (
	msg "backend-server/internal/server/transport/types"
)

const (
	DeviceMockPubTopicPrefix = msg.MDeviceMockPubTopicPrefix
	DeviceMockSubTopicPrefix = msg.MDeviceMockSubTopicPrefix
	DeviceSubTopicPrefix     = msg.MDeviceSubTopicPrefix
	DevicePubTopicPrefix     = msg.MDevicePubTopicPrefix
	ServerSubTopicPrefix     = msg.MServerSubTopicPrefix
	ServerPubTopicPrefix     = msg.MServerPubTopicPrefix
)

const (
	ClientActiveTs = 120
)
