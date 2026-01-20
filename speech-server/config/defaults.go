package config

import _ "embed"

// Default contains the embedded server configuration.
//
//go:embed config.yaml
var Default []byte
