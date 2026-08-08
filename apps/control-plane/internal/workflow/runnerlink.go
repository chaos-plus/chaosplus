package workflow

import (
	"context"
	"time"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
)

// RunnerLink is the runner-facing surface the engine needs: spawn + wait for
// completion, read artifacts, run validator commands, list runners. Both the
// NATS gateway (cloud/desktop) and the WS machine hub (PRD §5.3.1) implement it.
type RunnerLink interface {
	// SpawnAndWait spawns and blocks until done/error. idle > 0 resets on live
	// activity; max > 0 is an absolute bound. Either may be 0 (no limit).
	SpawnAndWait(ctx context.Context, runnerID string, sp gateway.Spawn, idle, max time.Duration) (gateway.SpawnResult, error)
	Kill(ctx context.Context, runnerID, spawnID string) error
	ReadArtifact(ctx context.Context, runnerID, spawnID, path string) ([]byte, error)
	RunCmd(ctx context.Context, runnerID, spawnID, cmdTemplate string, timeoutMs int) (gateway.CmdResult, error)
	RegisteredRunners() []string
}

// NatsRunnerLink adapts the NATS Gateway to RunnerLink (idle/max are re-wrapped
// into SpawnWaitOptions). Kept so the NATS daemon path stays available.
type NatsRunnerLink struct{ G *gateway.Gateway }

func (l *NatsRunnerLink) SpawnAndWait(ctx context.Context, runnerID string, sp gateway.Spawn, idle, max time.Duration) (gateway.SpawnResult, error) {
	var opts []gateway.SpawnWaitOption
	if idle > 0 {
		opts = append(opts, gateway.WithIdleTimeout(idle))
	}
	if max > 0 {
		opts = append(opts, gateway.WithMaxTimeout(max))
	}
	return l.G.SpawnAndWaitOpts(ctx, runnerID, sp, opts...)
}

func (l *NatsRunnerLink) Kill(ctx context.Context, runnerID, spawnID string) error {
	return l.G.Kill(ctx, runnerID, spawnID)
}

func (l *NatsRunnerLink) ReadArtifact(ctx context.Context, runnerID, spawnID, path string) ([]byte, error) {
	return l.G.ReadArtifact(ctx, runnerID, spawnID, path)
}

func (l *NatsRunnerLink) RunCmd(ctx context.Context, runnerID, spawnID, cmdTemplate string, timeoutMs int) (gateway.CmdResult, error) {
	return l.G.RunCmd(ctx, runnerID, spawnID, cmdTemplate, timeoutMs)
}

func (l *NatsRunnerLink) RegisteredRunners() []string {
	return l.G.RegisteredRunners()
}

var _ RunnerLink = (*NatsRunnerLink)(nil)
