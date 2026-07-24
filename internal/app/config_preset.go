package app

import (
	_ "embed"
	"fmt"
)

const ConfigPresetEnv = "CHAOSPLUS_CONFIG_PRESET"

//go:embed config.compose.yaml
var composeConfig []byte

func ConfigPreset(name string) ([]byte, error) {
	switch name {
	case "compose":
		return append([]byte(nil), composeConfig...), nil
	default:
		return nil, fmt.Errorf("unknown config preset %q", name)
	}
}
