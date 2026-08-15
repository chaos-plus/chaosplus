package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/machine"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/attachment"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/defect"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/requirement"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/task"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testcase"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testrun"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
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

type workspaceRESTModule interface {
	RegisterREST(huma.API)
}

func workspaceSchemaNamer(value reflect.Type, hint string) string {
	for value != nil && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Slice || value.Kind() == reflect.Array) {
		value = value.Elem()
	}
	if value == nil || value.PkgPath() == "" {
		return huma.DefaultSchemaNamer(value, hint)
	}
	name := value.PkgPath()
	if index := strings.LastIndex(name, "/"); index >= 0 {
		name = name[index+1:]
	}
	return name + "." + huma.DefaultSchemaNamer(value, hint)
}

func workspaceHTTP[T any](t *testing.T, client *http.Client, method, url string, payload any, wantStatus int) T {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(authz.TenantHeader, "11")
	request.Header.Set(authz.EntityHeader, "21")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read response = (%v, %v)", readErr, closeErr)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, url, response.StatusCode, wantStatus, data)
	}
	var value T
	if len(data) != 0 && wantStatus < http.StatusBadRequest {
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatalf("decode %s %s: %v; body = %s", method, url, err, data)
		}
	}
	return value
}

func TestWorkspaceTraceabilityThroughRealHumaListener(t *testing.T) {
	db := legacyTestDB(t)
	generator, err := guid.New(65000)
	if err != nil {
		t.Fatal(err)
	}
	nextID := func() (guid.ID, error) {
		value, err := generator.Next()
		return guid.ID(value), err
	}
	registry := authz.MustRegistry(Actions()...)
	registrar := authz.NewDeclarationOnlyRegistrar(registry)
	objectives := objective.NewModule(db, nextID, registrar)
	requirements := requirement.NewModule(db, nextID, registrar, objectives.References())
	objectives.SetKeyResultUsage(requirements.References())
	tasks := task.NewModule(db, nextID, registrar, nil, nil)
	testCases := testcase.NewModule(db, nextID, registrar, requirements.References())
	testRuns := testrun.NewModule(db, nextID, registrar, testCases.References())
	defects := defect.NewModule(db, nextID, registrar, requirements.References(), tasks.References(), testCases.References(), testRuns.References())

	router := chi.NewMux()
	claims := &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31}
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			next.ServeHTTP(writer, request.WithContext(authn.WithClaims(request.Context(), claims)))
		})
	})
	config := huma.DefaultConfig("workspace-integration", "1.0.0")
	config.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", workspaceSchemaNamer)
	api := humachi.New(router, config)
	for _, module := range []workspaceRESTModule{objectives, requirements, tasks, testCases, testRuns, defects} {
		module.RegisterREST(api)
	}
	if err := authz.ValidateOperations(api, registry); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()

	goal := workspaceHTTP[objective.Objective](t, client, http.MethodPost, server.URL+"/api/objectives", objective.CreateInput{
		Title: "Reliable delivery", Description: "Trace delivery quality", PeriodStart: 1_780_000_000_000, PeriodEnd: 1_780_086_400_000,
		KeyResults: []objective.KeyResultInput{{Title: "Close release blockers", TargetValue: "10", CurrentValue: "0", Unit: "items"}},
	}, http.StatusCreated)
	updatedTitle := "Reliable delivery v2"
	activeObjective := objective.StatusActive
	goal = workspaceHTTP[objective.Objective](t, client, http.MethodPatch, fmt.Sprintf("%s/api/objectives/%s", server.URL, goal.ID), objective.UpdateInput{Title: &updatedTitle, Status: &activeObjective, Version: goal.Version}, http.StatusOK)
	if goal.Title != updatedTitle || goal.Status != activeObjective || len(goal.KeyResults) != 1 {
		t.Fatalf("updated objective = %+v", goal)
	}

	requirementValue := workspaceHTTP[requirement.Requirement](t, client, http.MethodPost, server.URL+"/api/requirements", requirement.CreateInput{
		Title: "Cluster-safe execution", Description: "Route work across control-plane instances", AcceptanceCriteria: "Fenced stale work is rejected", KeyResultIDs: []guid.ID{goal.KeyResults[0].ID},
	}, http.StatusCreated)
	approved := requirement.StatusApproved
	requirementValue = workspaceHTTP[requirement.Requirement](t, client, http.MethodPatch, fmt.Sprintf("%s/api/requirements/%s", server.URL, requirementValue.ID), requirement.UpdateInput{Status: &approved, Version: requirementValue.Version}, http.StatusOK)

	taskValue := workspaceHTTP[task.Task](t, client, http.MethodPost, server.URL+"/api/tasks", task.CreateInput{
		RequirementID: &requirementValue.ID, Title: "Verify fenced route", Description: "Exercise a real runner route", EstimateMS: 60_000,
	}, http.StatusCreated)
	inProgressTask := task.StatusInProgress
	taskValue = workspaceHTTP[task.Task](t, client, http.MethodPatch, fmt.Sprintf("%s/api/tasks/%s", server.URL, taskValue.ID), task.UpdateInput{Status: &inProgressTask, Version: taskValue.Version}, http.StatusOK)

	testCaseValue := workspaceHTTP[testcase.TestCase](t, client, http.MethodPost, server.URL+"/api/test-cases", testcase.CreateInput{
		RequirementID: &requirementValue.ID, Title: "Reject stale fence", Description: "Validate takeover behavior", Preconditions: "Two control-plane instances", Priority: testcase.PriorityHighest,
		Steps: []testcase.StepInput{{Action: "Take over the machine lease", ExpectedResult: "The old route is fenced"}},
	}, http.StatusCreated)
	activeTestCase := testcase.StatusActive
	testCaseValue = workspaceHTTP[testcase.TestCase](t, client, http.MethodPatch, fmt.Sprintf("%s/api/test-cases/%s", server.URL, testCaseValue.ID), testcase.UpdateInput{Status: &activeTestCase, Version: testCaseValue.Version}, http.StatusOK)

	testRunValue := workspaceHTTP[testrun.TestRun](t, client, http.MethodPost, server.URL+"/api/test-runs", testrun.CreateInput{TestCaseID: testCaseValue.ID, Environment: "integration"}, http.StatusCreated)
	running := testrun.StatusRunning
	testRunValue = workspaceHTTP[testrun.TestRun](t, client, http.MethodPatch, fmt.Sprintf("%s/api/test-runs/%s", server.URL, testRunValue.ID), testrun.UpdateInput{Status: running, Version: testRunValue.Version}, http.StatusOK)
	failure := "Old reply was accepted"
	failed := testrun.StatusFailed
	workspaceHTTP[struct{}](t, client, http.MethodPatch, fmt.Sprintf("%s/api/test-runs/%s", server.URL, testRunValue.ID), testrun.UpdateInput{Status: failed, FailureSummary: &failure, Version: testRunValue.Version - 1}, http.StatusConflict)
	testRunValue = workspaceHTTP[testrun.TestRun](t, client, http.MethodPatch, fmt.Sprintf("%s/api/test-runs/%s", server.URL, testRunValue.ID), testrun.UpdateInput{Status: failed, FailureSummary: &failure, Version: testRunValue.Version}, http.StatusOK)

	defectValue := workspaceHTTP[defect.Defect](t, client, http.MethodPost, server.URL+"/api/defects", defect.CreateInput{
		RequirementID: &requirementValue.ID, TaskID: &taskValue.ID, TestCaseID: &testCaseValue.ID, TestRunID: &testRunValue.ID,
		Title: "Stale reply accepted", Description: "A superseded route replied", ReproductionSteps: "Take over while a command is active", ExpectedResult: "Reject the old reply", ActualResult: "Reply was accepted", Severity: defect.SeverityCritical, Priority: defect.PriorityHighest,
	}, http.StatusCreated)
	inProgressDefect := defect.StatusInProgress
	defectValue = workspaceHTTP[defect.Defect](t, client, http.MethodPatch, fmt.Sprintf("%s/api/defects/%s", server.URL, defectValue.ID), defect.UpdateInput{Status: &inProgressDefect, Version: defectValue.Version}, http.StatusOK)
	resolved := defect.StatusResolved
	resolution := defect.ResolutionFixed
	resolutionNote := "Verify the shared route after receiving the reply"
	defectValue = workspaceHTTP[defect.Defect](t, client, http.MethodPatch, fmt.Sprintf("%s/api/defects/%s", server.URL, defectValue.ID), defect.UpdateInput{Status: &resolved, Resolution: &resolution, ResolutionNote: &resolutionNote, Version: defectValue.Version}, http.StatusOK)

	tracedRequirement := workspaceHTTP[requirement.Requirement](t, client, http.MethodGet, fmt.Sprintf("%s/api/requirements/%s", server.URL, requirementValue.ID), nil, http.StatusOK)
	tracedTask := workspaceHTTP[task.Task](t, client, http.MethodGet, fmt.Sprintf("%s/api/tasks/%s", server.URL, taskValue.ID), nil, http.StatusOK)
	tracedTestCase := workspaceHTTP[testcase.TestCase](t, client, http.MethodGet, fmt.Sprintf("%s/api/test-cases/%s", server.URL, testCaseValue.ID), nil, http.StatusOK)
	tracedTestRun := workspaceHTTP[testrun.TestRun](t, client, http.MethodGet, fmt.Sprintf("%s/api/test-runs/%s", server.URL, testRunValue.ID), nil, http.StatusOK)
	tracedDefects := workspaceHTTP[[]defect.Defect](t, client, http.MethodGet, server.URL+"/api/defects?status=resolved", nil, http.StatusOK)
	if len(tracedRequirement.KeyResultIDs) != 1 || tracedRequirement.KeyResultIDs[0] != goal.KeyResults[0].ID || tracedTask.RequirementID == nil || *tracedTask.RequirementID != tracedRequirement.ID || tracedTestCase.RequirementID == nil || *tracedTestCase.RequirementID != tracedRequirement.ID || tracedTestRun.TestCaseID != tracedTestCase.ID || len(tracedDefects) != 1 || tracedDefects[0].ID != defectValue.ID || tracedDefects[0].TestRunID == nil || *tracedDefects[0].TestRunID != tracedTestRun.ID {
		t.Fatalf("traceability mismatch: requirement=%+v task=%+v testcase=%+v testrun=%+v defects=%+v", tracedRequirement, tracedTask, tracedTestCase, tracedTestRun, tracedDefects)
	}
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

func TestWorkspaceAndMachineMigrationDialectLifecycle(t *testing.T) {
	verifyWorkspaceAndMachineMigrationLifecycle(t, newAILifecycleDatabase(t))
}

func TestWorkspaceAndMachineMigrationSQLiteLifecycle(t *testing.T) {
	database, err := (&bunx.Datasource{Type: "sqlite", Dsn: t.TempDir() + "/workspace-lifecycle.db", Writable: true}).Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	verifyWorkspaceAndMachineMigrationLifecycle(t, database)
}

func verifyWorkspaceAndMachineMigrationLifecycle(t *testing.T, database *bun.DB) {
	t.Helper()
	for _, migrate := range []func(context.Context, *bun.DB) error{
		workflow.Migrate, machine.Migrate, objective.Migrate, requirement.Migrate, task.Migrate,
		testcase.Migrate, testrun.Migrate, defect.Migrate, attachment.Migrate,
	} {
		if err := migrate(t.Context(), database); err != nil {
			t.Fatal(err)
		}
	}

	insertLegacyTask(t, database, 101, "task", "open", nil, nil)
	insertLegacyTask(t, database, 102, "test", "done", nil, nil)
	insertLegacyTask(t, database, 103, "bug", "done", nil, 101)
	legacy := newLegacyMigration(database)
	if err := legacy.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate legacy work items: %v", err)
	}
	assertTableCount(t, database, "workspace_tasks", 1)
	assertTableCount(t, database, "workspace_test_cases", 1)
	assertTableCount(t, database, "workspace_defects", 1)
	assertTableCount(t, database, "machine_onboarding_tokens", 0)
	assertTableCount(t, database, "machine_connection_leases", 0)
	if _, err := database.ExecContext(t.Context(), `INSERT INTO workspace_test_cases
		(id,tenant_id,entity_id,owner_id,requirement_id,title,description,preconditions,priority,status,assignee_id,created_at,created_by,updated_at,updated_by,deleted_at,deleted_by,version)
		VALUES (104,11,12,13,NULL,'invalid','','','medium','not-a-status',NULL,1,13,1,13,0,0,1)`); err == nil {
		t.Fatal("test case status constraint accepted an invalid enum")
	}

	if err := legacy.MigrateDownTo(t.Context(), 0); err != nil {
		t.Fatalf("roll back legacy migration: %v", err)
	}
	for _, down := range []func(context.Context, *bun.DB, int64) error{
		attachment.MigrateDownTo, defect.MigrateDownTo, testrun.MigrateDownTo, testcase.MigrateDownTo,
		task.MigrateDownTo, requirement.MigrateDownTo, objective.MigrateDownTo, machine.MigrateDownTo,
	} {
		if err := down(t.Context(), database, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, migrate := range []func(context.Context, *bun.DB) error{
		machine.Migrate, objective.Migrate, requirement.Migrate, task.Migrate, testcase.Migrate,
		testrun.Migrate, defect.Migrate, attachment.Migrate,
	} {
		if err := migrate(t.Context(), database); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Migrate(t.Context()); err != nil {
		t.Fatalf("reapply legacy migration: %v", err)
	}
}

func assertTableCount(t *testing.T, database *bun.DB, table string, expected int) {
	t.Helper()
	var actual int
	if err := database.NewSelect().Table(table).ColumnExpr("COUNT(*)").Scan(t.Context(), &actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("%s count = %d, want %d", table, actual, expected)
	}
}

func newAILifecycleDatabase(t *testing.T) *bun.DB {
	t.Helper()
	dialect := strings.ToLower(strings.TrimSpace(os.Getenv("AI_DB_LIFECYCLE_TYPE")))
	if dialect == "" {
		t.Skip("set AI_DB_LIFECYCLE_TYPE and AI_DB_LIFECYCLE_ADMIN_DSN to test a disposable real database")
	}
	if dialect != "mysql" && dialect != "postgres" {
		t.Fatalf("unsupported lifecycle dialect %q", dialect)
	}
	adminDSN := os.Getenv("AI_DB_LIFECYCLE_ADMIN_DSN")
	if adminDSN == "" {
		t.Fatal("AI_DB_LIFECYCLE_ADMIN_DSN is required")
	}
	admin, err := (&bunx.Datasource{Type: dialect, Dsn: adminDSN}).Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("chaosplus_ai_%d", time.Now().UTC().UnixNano())
	create, drop := aiLifecycleDatabaseStatements(dialect, name)
	if _, err := admin.ExecContext(t.Context(), create); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), drop); err != nil {
			t.Errorf("drop lifecycle database: %v", err)
		}
	})
	dsn := aiLifecycleTargetDSN(t, dialect, adminDSN, name)
	database, err := (&bunx.Datasource{Type: dialect, Dsn: dsn}).Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return database
}

func aiLifecycleDatabaseStatements(dialect, name string) (string, string) {
	if dialect == "mysql" {
		return "CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", "DROP DATABASE IF EXISTS `" + name + "`"
	}
	return `CREATE DATABASE "` + name + `"`, `DROP DATABASE IF EXISTS "` + name + `"`
}

func aiLifecycleTargetDSN(t *testing.T, dialect, adminDSN, name string) string {
	t.Helper()
	if dialect == "mysql" {
		config, err := mysql.ParseDSN(adminDSN)
		if err != nil {
			t.Fatal(err)
		}
		config.DBName = name
		return config.FormatDSN()
	}
	parsed, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}
