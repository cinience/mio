package sms

type MediaServerPorts struct {
	HTTP int
	RTSP int
	RTMP int
}

type MediaServer struct {
	ID           string
	IP           string
	Ports        MediaServerPorts
	Secret       string
	Type         string
	SDPIP        string
	RTPPortRange string
}

func (m *MediaServer) GetSDPIP() string {
	if m.SDPIP != "" {
		return m.SDPIP
	}
	return m.IP
}

type StreamLiveAddr struct {
	RTSP    string
	RTMP    string
	HLS     string
	HTTPFLV string
	WSFLV   string
	WebRTC  string
}
