package configdefaults

import _ "embed"

//go:embed config.yaml
var defaultConfig []byte

// DefaultConfig returns a copy of the embedded manager configuration.
func DefaultConfig() []byte {
	if len(defaultConfig) == 0 {
		return nil
	}
	copyBuf := make([]byte, len(defaultConfig))
	copy(copyBuf, defaultConfig)
	return copyBuf
}
