package audio

const (
	SampleRate    = 16000
	Channels      = 1
	FrameDuration = 60
	Format        = "opus"
	FormatPCM16LE = "pcm_s16le"
	FormatWAV     = "wav"
)

type AudioFormat struct {
	Format        string `json:"format,omitempty"`
	OutputFormat  string `json:"output_format,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	FrameDuration int    `json:"frame_duration,omitempty"`
}
