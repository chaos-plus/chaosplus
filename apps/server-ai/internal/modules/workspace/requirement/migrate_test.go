package requirement

import (
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
)

func TestSQLiteMigrationCreatesRequirementRelations(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := objective.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"workspace_requirements", "workspace_requirement_key_results"} {
		count, err := db.NewSelect().Table("sqlite_master").Where("type = 'table' AND name = ?", table).Count(t.Context())
		if err != nil || count != 1 {
			t.Fatalf("table %s: count=%d err=%v", table, count, err)
		}
	}
}
