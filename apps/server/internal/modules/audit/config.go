package audit

// Config configures the audit module. Anchoring is optional; when enabled the
// service writes verified per-tenant heads into an external S3-compatible WORM
// store so tampering after the fact cannot rewrite the committed head.
type Config struct {
	Anchor AnchorConfig `mapstructure:"anchor" group:"anchor"`
}

// AnchorConfig configures external WORM anchoring of the audit hash chain.
type AnchorConfig struct {
	Enabled       bool   `mapstructure:"enabled" description:"anchor verified audit heads into an external S3-compatible object lock store" default:"false"`
	Endpoint      string `mapstructure:"endpoint" description:"S3-compatible endpoint, e.g. http://127.0.0.1:9000" default:""`
	Bucket        string `mapstructure:"bucket" description:"bucket holding audit anchors; created with object locking when it does not exist" default:"audit-anchors"`
	Region        string `mapstructure:"region" description:"signing region; empty is fine for MinIO" default:""`
	AccessKey     string `mapstructure:"access_key" description:"S3 access key" default:""`
	SecretKey     string `mapstructure:"secret_key" description:"S3 secret key" default:""`
	RetentionDays int    `mapstructure:"retention_days" description:"object lock retention in days (COMPLIANCE mode)" default:"365"`
	SigningKey    string `mapstructure:"signing_key" description:"base64-encoded 32-byte Ed25519 seed signing anchored audit roots; anchors embed the public key so signatures verify without further configuration" default:""`
}

func (c AnchorConfig) valid() bool {
	return c.Endpoint != "" && c.Bucket != "" && c.AccessKey != "" && c.SecretKey != "" && c.RetentionDays > 0
}
