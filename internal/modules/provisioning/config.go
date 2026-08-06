package provisioning

import (
	"encoding/base64"
	"errors"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
)

// Config controls outbound SCIM provisioning. The encryption key protects the
// bearer tokens stored for SCIM targets; resolve it from a secret manager via
// encryption_key_file in production. An unset key disables outbound
// provisioning (target creation and pushes fail with a clear error).
type Config struct {
	EncryptionKey     string        `mapstructure:"encryption_key" description:"base64 32-byte key encrypting outbound SCIM target bearer tokens; prefer encryption_key_file" default:""`
	EncryptionKeyFile string        `mapstructure:"encryption_key_file" description:"file containing the base64 32-byte provisioning encryption key" default:""`
	HTTPTimeout       time.Duration `mapstructure:"http_timeout" description:"outbound SCIM request timeout" default:"10s"`
}

// ParseEncryptionKey decodes a base64-encoded 32-byte key. An empty value
// returns nil so deployments that do not use outbound SCIM stay unconfigured.
func ParseEncryptionKey(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, nil
	}
	for _, decoder := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.URLEncoding} {
		key, err := decoder.DecodeString(encoded)
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errors.New("provisioning encryption key must be base64-encoded 32 bytes")
}

// ResolveEncryptionKey loads and validates the configured provisioning key so
// startup fails fast instead of surfacing a runtime secret error.
func ResolveEncryptionKey(cfg Config) ([]byte, error) {
	encoded, err := secretx.Resolve("provisioning.encryption_key", cfg.EncryptionKey, cfg.EncryptionKeyFile, 4096)
	if err != nil {
		return nil, err
	}
	return ParseEncryptionKey(encoded)
}
