package app

import (
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/infra/natsclient"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/attachment"
	sharedapp "github.com/chaos-plus/chaosplus/internal/app"
)

// Config composes the shared application host configuration with AI-specific
// infrastructure. IAM, HTTP, database, security, and GUID settings remain
// owned by the shared host under server.
type Config struct {
	Server    sharedapp.Config         `mapstructure:"server" group:"server"`
	NATS      natsclient.Config        `mapstructure:"nats" group:"nats"`
	Storage   attachment.StorageConfig `mapstructure:"storage" group:"storage"`
	Runner    RunnerConfig             `mapstructure:"runner" group:"runner"`
	Reconcile ReconcileConfig          `mapstructure:"reconcile" group:"reconcile"`
}

type RunnerConfig struct {
	DefaultHandle string `mapstructure:"default_handle" description:"default machine runner handle when a workflow does not select one" default:""`
}

type ReconcileConfig struct {
	Interval time.Duration `mapstructure:"interval" description:"artifact reconciliation interval; zero disables background reconciliation" default:"0s"`
}
