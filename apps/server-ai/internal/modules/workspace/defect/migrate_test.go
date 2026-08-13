package defect

import (
	"context"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/requirement"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/task"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testcase"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testrun"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/uptrace/bun"
	"testing"
)

func TestSQLiteMigrationCreatesDefectTable(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, migrate := range []func(context.Context, *bun.DB) error{workflow.Migrate, objective.Migrate, requirement.Migrate, task.Migrate, testcase.Migrate, testrun.Migrate, Migrate} {
		if err := migrate(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	count, err := db.NewSelect().Table("sqlite_master").Where("type='table' AND name='workspace_defects'").Count(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("table count=%d err=%v", count, err)
	}
}
