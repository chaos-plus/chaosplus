package bunx

import (
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/extra/bundebug"
)

func ApplyDebugHook(db *bun.DB) { // Add debug hook
	db.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithVerbose(true),
		bundebug.FromEnv("BUNDEBUG"), // Enable with BUNDEBUG=1
	))
}

// Or create custom debug hook
// type DebugHook struct{}

// func (h *DebugHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
// 	return ctx
// }

// func (h *DebugHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
// 	duration := time.Since(event.StartTime)
// 	fmt.Printf("Query: %s\nDuration: %s\n", event.Query, duration.String())
// }
