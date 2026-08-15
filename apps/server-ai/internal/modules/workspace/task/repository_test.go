package task

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/requirement"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

func TestPlanningFieldsAndNestedRollupUseRealSQLite(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, migrate := range []func(context.Context, *bun.DB) error{workflow.Migrate, objective.Migrate, requirement.Migrate, Migrate} {
		if err := migrate(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	next := guid.ID(100)
	repository := NewRepository(db, func() (guid.ID, error) { next++; return next, nil })
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	service := NewService(repository, nil, nil)
	dueAt := int64(1_800_000_000_000)
	root, err := service.Create(ctx, CreateInput{Title: "Release", Priority: PriorityHighest, DueAt: dueAt, EstimateMS: 99})
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.Create(ctx, CreateInput{ParentID: &root.ID, Title: "API", EstimateMS: 3_600_000})
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := service.Create(ctx, CreateInput{ParentID: &child.ID, Title: "Migration", EstimateMS: 7_200_000})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := service.Create(ctx, CreateInput{ParentID: &root.ID, Title: "UI", EstimateMS: 3_600_000})
	if err != nil {
		t.Fatal(err)
	}

	items, err := repository.List(ctx, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyRollups(items, map[guid.ID]ExecutionMetric{
		grandchild.ID: {SpentMS: 5_400_000, LatestStatus: "completed"},
		leaf.ID:       {SpentMS: 1_800_000, LatestStatus: "running"},
	})
	byID := make(map[guid.ID]Task, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	got := byID[root.ID]
	if got.Priority != PriorityHighest || got.DueAt != dueAt || got.EstimateMS != 10_800_000 || got.SpentMS != 7_200_000 || got.Progress != 66 {
		t.Fatalf("root rollup = %+v", got)
	}
	if nested := byID[child.ID]; nested.EstimateMS != 7_200_000 || nested.SpentMS != 5_400_000 || nested.Progress != 100 {
		t.Fatalf("nested rollup = %+v", nested)
	}

	if _, err := db.NewUpdate().Model((*Task)(nil)).Set("parent_id = ?", grandchild.ID).Where("id = ?", root.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	cycled, err := repository.List(ctx, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyRollups(cycled, nil)
}
