package providers

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// dbRefreshInterval is how often a database-backed provider re-downloads its
// database to pick up upstream updates.
const dbRefreshInterval = time.Hour

type maintenanceWorker struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func (w *maintenanceWorker) start(parent context.Context, run func(context.Context)) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	w.mu.Lock()
	if w.done != nil {
		w.mu.Unlock()
		cancel()
		return
	}
	w.cancel, w.done = cancel, done
	w.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			w.mu.Lock()
			if w.done == done {
				w.cancel, w.done = nil, nil
			}
			close(done)
			w.mu.Unlock()
		}()
		run(ctx)
	}()
}

func (w *maintenanceWorker) stop(ctx context.Context) error {
	w.mu.Lock()
	cancel, done := w.cancel, w.done
	w.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// maintainDB keeps a provider's local database current under caller control. A
// maintenanceWorker runs it in the background and joins it during shutdown.
//
// name is used only for log context. dbPath reports the current database path
// (a non-nil error or empty string means "missing"); download fetches/refreshes
// it. Both are provided by the provider so this helper stays storage-agnostic.
func maintainDB(ctx context.Context, name string, dbPath func() (string, error), download func() error) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	if p, err := dbPath(); err != nil || p == "" {
		if err := download(); err != nil {
			slog.Error("geoip initial db download failed", "provider", name, "err", err)
		}
	}

	t := time.NewTicker(dbRefreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := download(); err != nil {
				slog.Error("geoip db refresh failed", "provider", name, "err", err)
			}
		}
	}
}
