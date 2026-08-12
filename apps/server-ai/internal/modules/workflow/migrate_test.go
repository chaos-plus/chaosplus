package workflow

import (
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"testing"
)

func TestSQLiteMigrationUp(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"workflows", "workflow_runs", "workflow_events", "node_executions", "run_leases"} {
		var count int
		if err := db.NewRaw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(t.Context(), &count); err != nil || count != 1 {
			t.Fatalf("table %s: count=%d err=%v", table, count, err)
		}
	}
}
