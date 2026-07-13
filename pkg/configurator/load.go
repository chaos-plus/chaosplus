package configurator

import (
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// LoadStrict reads a YAML/JSON/TOML config file into o and fails on any key that
// does not map to a field (ErrorUnused), so typos and stale keys are reported
// rather than silently ignored. Durations ("30m") and string slices decode via
// viper's default hooks. It powers `config validate`.
func LoadStrict(file string, o interface{}) error {
	v := viper.New()
	v.SetConfigFile(file)
	if err := v.ReadInConfig(); err != nil {
		return err
	}
	return v.Unmarshal(o, func(dc *mapstructure.DecoderConfig) { dc.ErrorUnused = true })
}
