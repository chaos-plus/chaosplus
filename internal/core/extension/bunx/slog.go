package bunx

import (
	"log/slog"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/extra/bunslog"
)

func ApplySlogHook(db *bun.DB) {

	hook := bunslog.NewQueryHook(
		bunslog.WithQueryLogLevel(slog.LevelDebug),
		bunslog.WithSlowQueryLogLevel(slog.LevelWarn),
		bunslog.WithErrorQueryLogLevel(slog.LevelError),
		bunslog.WithSlowQueryThreshold(3*time.Second),
	)

	db.AddQueryHook(hook)
}
