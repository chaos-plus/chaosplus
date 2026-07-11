package providers

import (
	"context"
	"log/slog"
	"time"
)

// dbRefreshInterval is how often a database-backed provider re-downloads its
// database to pick up upstream updates.
const dbRefreshInterval = time.Hour

// maintainDB keeps a provider's local database current under caller control. It
// returns immediately and runs all work in a background goroutine: an initial
// download when the database is missing, then a periodic refresh on
// dbRefreshInterval. The goroutine stops when ctx is cancelled, so a provider's
// background work is tied to the application lifecycle instead of leaking for
// the life of the process (as an init-time cron would).
//
// name is used only for log context. dbPath reports the current database path
// (a non-nil error or empty string means "missing"); download fetches/refreshes
// it. Both are provided by the provider so this helper stays storage-agnostic.
func maintainDB(ctx context.Context, name string, dbPath func() (string, error), download func() error) {
	go func() {
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
	}()
}
