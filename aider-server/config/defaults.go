package config

import _ "embed"

// Default holds the embedded default configuration.
//
//go:embed config.yaml
var Default []byte
