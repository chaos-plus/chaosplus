package workflow

import (
	"context"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/infra/runnergateway"
)

// RunnerLink is the transport-neutral runner surface the engine needs: spawn +
// wait for completion, read artifacts, run validator commands, and list runners.
// The runner wire protocol and the control-plane's cluster bus stay outside this
// business port.
type RunnerLink interface {
	// SpawnAndWait spawns and blocks until done/error. idle > 0 resets on live
	// activity; max > 0 is an absolute bound. Either may be 0 (no limit).
	SpawnAndWait(ctx context.Context, runnerID string, sp gateway.Spawn, idle, max, heartbeat time.Duration) (gateway.SpawnResult, error)
	Kill(ctx context.Context, runnerID, spawnID string) error
	ReadArtifact(ctx context.Context, runnerID, spawnID, path string) ([]byte, error)
	RunCmd(ctx context.Context, runnerID, spawnID, cmdTemplate string, timeoutMs int) (gateway.CmdResult, error)
	RegisteredRunners(context.Context) []string
}

// GatewayRunnerLink adapts the control-plane runner gateway to RunnerLink.
// The gateway may use a cluster bus internally; that implementation detail is
// never part of the runner's WebSocket protocol or configuration.
type GatewayRunnerLink struct{ G *gateway.Gateway }

func (l *GatewayRunnerLink) SpawnAndWait(ctx context.Context, runnerID string, sp gateway.Spawn, idle, max, heartbeat time.Duration) (gateway.SpawnResult, error) {
	var opts []gateway.SpawnWaitOption
	if idle > 0 {
		opts = append(opts, gateway.WithIdleTimeout(idle))
	}
	if max > 0 {
		opts = append(opts, gateway.WithMaxTimeout(max))
	}
	if heartbeat > 0 {
		opts = append(opts, gateway.WithHeartbeatTimeout(heartbeat))
	}
	return l.G.SpawnAndWaitOpts(ctx, runnerID, sp, opts...)
}

func (l *GatewayRunnerLink) Kill(ctx context.Context, runnerID, spawnID string) error {
	return l.G.Kill(ctx, runnerID, spawnID)
}

func (l *GatewayRunnerLink) ReadArtifact(ctx context.Context, runnerID, spawnID, path string) ([]byte, error) {
	return l.G.ReadArtifact(ctx, runnerID, spawnID, path)
}

func (l *GatewayRunnerLink) RunCmd(ctx context.Context, runnerID, spawnID, cmdTemplate string, timeoutMs int) (gateway.CmdResult, error) {
	return l.G.RunCmd(ctx, runnerID, spawnID, cmdTemplate, timeoutMs)
}

func (l *GatewayRunnerLink) RegisteredRunners(ctx context.Context) []string {
	return l.G.RegisteredRunnersContext(ctx)
}

var _ RunnerLink = (*GatewayRunnerLink)(nil)
