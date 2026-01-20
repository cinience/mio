package xiaozhi

type AudioParams struct {
	Format        string
	SampleRate    int
	Channels      int
	FrameDuration int
}

type Config struct {
	BackendURL      string
	ProtocolVersion int
	AudioParams     AudioParams
	DeviceID        string
	ClientID        string
	AccessToken     string
}
