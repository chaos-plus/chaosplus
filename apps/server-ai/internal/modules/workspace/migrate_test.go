package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/attachment"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/defect"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/requirement"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/task"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testcase"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testrun"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

func legacyTestDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, migrate := range []func(context.Context, *bun.DB) error{
		workflow.Migrate, objective.Migrate, requirement.Migrate, task.Migrate,
		testcase.Migrate, testrun.Migrate, defect.Migrate,
		attachment.Migrate,
	} {
		if err := migrate(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestObjectivePreservesReferencedKeyResultIdentity(t *testing.T) {
	db := legacyTestDB(t)
	next := guid.ID(100)
	nextID := func() (guid.ID, error) { next++; return next, nil }
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	objectives := objective.NewRepository(db, nextID)
	requirements := requirement.NewRepository(db, nextID)
	service := objective.NewService(objectives)
	service.SetKeyResultUsage(requirements)
	untrustedID := guid.ID(999)
	if _, err := service.Create(ctx, objective.CreateInput{Title: "Untrusted", PeriodStart: 1, PeriodEnd: 2, KeyResults: []objective.KeyResultInput{{ID: &untrustedID, Title: "Injected", TargetValue: "1", CurrentValue: "0"}}}); !errors.Is(err, objective.ErrInvalid) {
		t.Fatalf("create with client key result id = %v", err)
	}
	goal, err := service.Create(ctx, objective.CreateInput{Title: "Delivery", PeriodStart: 1, PeriodEnd: 2, KeyResults: []objective.KeyResultInput{{Title: "Ship", TargetValue: "10", CurrentValue: "1", Unit: "items"}}})
	if err != nil {
		t.Fatal(err)
	}
	wantID := goal.KeyResults[0].ID
	item := &requirement.Requirement{Title: "Release", Status: requirement.StatusDraft, KeyResultIDs: []guid.ID{wantID}}
	if err := requirements.Create(ctx, item); err != nil {
		t.Fatal(err)
	}
	title := "Delivery updated"
	resultTitle := "Ship safely"
	goal, err = service.Update(ctx, goal.ID, objective.UpdateInput{Title: &title, KeyResults: &[]objective.KeyResultInput{{ID: &wantID, Title: resultTitle, TargetValue: "10", CurrentValue: "2", Unit: "items"}}, Version: goal.Version})
	if err != nil {
		t.Fatal(err)
	}
	if len(goal.KeyResults) != 1 || goal.KeyResults[0].ID != wantID || goal.KeyResults[0].Title != resultTitle {
		t.Fatalf("updated key result = %+v", goal.KeyResults)
	}
	listed, err := requirements.List(ctx, "", nil)
	if err != nil || len(listed) != 1 || len(listed[0].KeyResultIDs) != 1 || listed[0].KeyResultIDs[0] != wantID {
		t.Fatalf("requirement relationship = (%+v, %v)", listed, err)
	}
	empty := []objective.KeyResultInput{}
	if _, err := service.Update(ctx, goal.ID, objective.UpdateInput{KeyResults: &empty, Version: goal.Version}); !errors.Is(err, objective.ErrStateConflict) {
		t.Fatalf("remove referenced key result = %v", err)
	}
	if err := service.Delete(ctx, goal.ID, goal.Version-1); !errors.Is(err, objective.ErrVersionConflict) {
		t.Fatalf("delete stale objective = %v", err)
	}
	if err := service.Delete(ctx, goal.ID, goal.Version); !errors.Is(err, objective.ErrStateConflict) {
		t.Fatalf("delete referenced objective = %v", err)
	}
}

func TestAttachmentReferencesRequireCurrentScopeResource(t *testing.T) {
	db := legacyTestDB(t)
	next := guid.ID(500)
	nextID := func() (guid.ID, error) { next++; return next, nil }
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	tasks := task.NewRepository(db, nextID)
	value := &task.Task{Title: "Evidence target", Status: task.StatusOpen}
	if err := tasks.Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	references := resourceReferences{tasks: tasks}
	if err := references.Exists(ctx, attachment.ResourceTask, value.ID); err != nil {
		t.Fatalf("same-scope task reference = %v", err)
	}
	other := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 12, EntityID: 22, PrincipalID: 32})
	if err := references.Exists(other, attachment.ResourceTask, value.ID); !errors.Is(err, attachment.ErrNotFound) {
		t.Fatalf("cross-scope task reference = %v", err)
	}
	if err := references.Exists(ctx, attachment.ResourceTask, 999999); !errors.Is(err, attachment.ErrNotFound) {
		t.Fatalf("missing task reference = %v", err)
	}
}

func insertLegacyTask(t *testing.T, db *bun.DB, id int64, kind, status string, requirementID, parentID any) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `INSERT INTO workspace_tasks
		(id,tenant_id,entity_id,owner_id,requirement_id,parent_id,kind,title,description,status,estimate_ms,spent_ms,progress,workflow_run_id,workflow_id,project_id,workspace,assignee_id,channel_id,created_at,created_by,updated_at,updated_by,deleted_at,deleted_by,version)
		VALUES (?,?,?,?,?,?,?,?,'legacy description',?,0,0,0,NULL,NULL,NULL,'',NULL,NULL,1000,13,2000,14,0,0,3)`,
		id, 11, 12, 13, requirementID, parentID, kind, kind+" title", status)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLegacyMigrationConvertsTestAndBugAndContractsTasks(t *testing.T) {
	db := legacyTestDB(t)
	insertLegacyTask(t, db, 101, "task", "open", nil, nil)
	insertLegacyTask(t, db, 102, "test", "done", nil, nil)
	insertLegacyTask(t, db, 103, "bug", "done", nil, 101)
	for _, value := range []struct {
		id, resourceID int64
		objectKey      string
	}{{301, 102, "workspace/11/12/301"}, {302, 103, "workspace/11/12/302"}} {
		if _, err := db.ExecContext(t.Context(), `INSERT INTO workspace_attachments
			(id,tenant_id,entity_id,owner_id,resource_type,resource_id,filename,content_type,size_bytes,checksum,object_key,status,created_at,created_by,updated_at,updated_by,deleted_at,deleted_by,version)
			VALUES (11+?,11,12,13,'task',?,'evidence.txt','text/plain',1,?,?,'available',1000,13,2000,14,0,0,3)`, value.id, value.resourceID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", value.objectKey); err != nil {
			t.Fatal(err)
		}
	}

	if err := newLegacyMigration(db).Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}

	var testCase struct {
		ID, TenantID, EntityID, OwnerID, CreatedBy, UpdatedBy, Version int64
		Status                                                         string
	}
	if err := db.NewSelect().Table("workspace_test_cases").Column("id", "tenant_id", "entity_id", "owner_id", "created_by", "updated_by", "version", "status").Where("id = 102").Scan(t.Context(), &testCase); err != nil {
		t.Fatal(err)
	}
	if testCase.ID != 102 || testCase.TenantID != 11 || testCase.EntityID != 12 || testCase.OwnerID != 13 || testCase.CreatedBy != 13 || testCase.UpdatedBy != 14 || testCase.Version != 3 || testCase.Status != "retired" {
		t.Fatalf("migrated test case = %+v", testCase)
	}

	var migratedDefect struct {
		ID, TaskID, Version      int64
		Status, Resolution, Note string
	}
	if err := db.NewSelect().Table("workspace_defects").ColumnExpr("id, task_id, version, status, resolution, resolution_note AS note").Where("id = 103").Scan(t.Context(), &migratedDefect); err != nil {
		t.Fatal(err)
	}
	if migratedDefect.ID != 103 || migratedDefect.TaskID != 101 || migratedDefect.Version != 3 || migratedDefect.Status != "resolved" || migratedDefect.Resolution != "fixed" || migratedDefect.Note == "" {
		t.Fatalf("migrated defect = %+v", migratedDefect)
	}
	var resourceTypes []string
	if err := db.NewSelect().Table("workspace_attachments").Column("resource_type").Order("resource_id").Scan(t.Context(), &resourceTypes); err != nil {
		t.Fatal(err)
	}
	if len(resourceTypes) != 2 || resourceTypes[0] != "testcase" || resourceTypes[1] != "defect" {
		t.Fatalf("migrated attachment resource types = %v", resourceTypes)
	}

	columns, err := db.QueryContext(t.Context(), "PRAGMA table_info(workspace_tasks)")
	if err != nil {
		t.Fatal(err)
	}
	defer columns.Close()
	hasKind := false
	for columns.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := columns.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		hasKind = hasKind || name == "kind"
	}
	if hasKind {
		t.Fatal("workspace_tasks still has kind column")
	}
	var taskCount int
	if err := db.NewSelect().Table("workspace_tasks").ColumnExpr("COUNT(*)").Scan(t.Context(), &taskCount); err != nil || taskCount != 1 {
		t.Fatalf("task count = %d, err = %v", taskCount, err)
	}
}

func TestLegacyMigrationRejectsSourceLessBugWithoutPartialConversion(t *testing.T) {
	db := legacyTestDB(t)
	insertLegacyTask(t, db, 201, "test", "open", nil, nil)
	insertLegacyTask(t, db, 202, "bug", "open", nil, nil)

	if err := newLegacyMigration(db).Migrate(t.Context()); err == nil {
		t.Fatal("source-less bug migration succeeded")
	}
	var taskCount, testCaseCount, defectCount int
	if err := db.NewSelect().Table("workspace_tasks").ColumnExpr("COUNT(*)").Scan(t.Context(), &taskCount); err != nil {
		t.Fatal(err)
	}
	if err := db.NewSelect().Table("workspace_test_cases").ColumnExpr("COUNT(*)").Scan(t.Context(), &testCaseCount); err != nil {
		t.Fatal(err)
	}
	if err := db.NewSelect().Table("workspace_defects").ColumnExpr("COUNT(*)").Scan(t.Context(), &defectCount); err != nil {
		t.Fatal(err)
	}
	if taskCount != 2 || testCaseCount != 0 || defectCount != 0 {
		t.Fatalf("counts after rejected migration = tasks:%d tests:%d defects:%d", taskCount, testCaseCount, defectCount)
	}
}
