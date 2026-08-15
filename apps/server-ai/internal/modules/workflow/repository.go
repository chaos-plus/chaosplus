package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type EventProjector interface {
	Project(context.Context, bun.IDB, EventRecord) error
}

type BunRepository struct {
	db         *bun.DB
	projectors []EventProjector
}

func NewBunRepository(db *bun.DB, projectors ...EventProjector) *BunRepository {
	if db == nil {
		panic("workflow repository requires database")
	}
	return &BunRepository{db: db, projectors: append([]EventProjector(nil), projectors...)}
}

func (r *BunRepository) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

type EventRecord struct {
	bun.BaseModel  `bun:"table:workflow_events"`
	Seq            int64   `bun:"seq,pk,autoincrement"`
	ID             guid.ID `bun:"id,notnull,unique"`
	TenantID       guid.ID `bun:"tenant_id,notnull"`
	EntityID       guid.ID `bun:"entity_id,notnull"`
	ProjectID      guid.ID `bun:"project_id,notnull"`
	ActorID        guid.ID `bun:"actor_id,notnull"`
	RunID          guid.ID `bun:"run_id,notnull"`
	TS             int64   `bun:"ts,notnull"`
	Type           string  `bun:"type,notnull"`
	IdempotencyKey string  `bun:"idempotency_key,notnull,unique"`
	SchemaVersion  int64   `bun:"schema_version,notnull,default:1"`
	PayloadJSON    string  `bun:"payload_json,notnull,default:'{}'"`
}

func (r *BunRepository) Append(ctx context.Context, event EventRecord) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if event.TenantID.Zero() {
		event.TenantID = claims.TenantID
	}
	if event.EntityID.Zero() {
		event.EntityID = claims.EntityID
	}
	if event.ActorID.Zero() {
		event.ActorID = claims.PrincipalID
	}
	if event.TenantID != claims.TenantID || event.EntityID != claims.EntityID || event.ActorID != claims.PrincipalID {
		return fmt.Errorf("append workflow event: scope does not match authenticated claims")
	}
	if event.TS == 0 {
		event.TS = time.Now().UTC().UnixMilli()
	}
	_, err = r.db.NewInsert().Model(&event).On("CONFLICT (idempotency_key) DO NOTHING").Exec(ctx)
	return err
}

func (r *BunRepository) ListEvents(ctx context.Context, runID guid.ID, sinceSeq int64, limit int) ([]EventRecord, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	return r.listEvents(ctx, runID, sinceSeq, limit, claims.TenantID, claims.EntityID)
}

// ListAllEvents returns a run's events without tenant/entity claims. Used by
// crash recovery, which rehydrates the control plane's own runs at startup
// before any request context exists.
func (r *BunRepository) ListAllEvents(ctx context.Context, runID guid.ID, sinceSeq int64, limit int) ([]EventRecord, error) {
	return r.listEvents(ctx, runID, sinceSeq, limit)
}

func (r *BunRepository) listEvents(ctx context.Context, runID guid.ID, sinceSeq int64, limit int, scope ...guid.ID) ([]EventRecord, error) {
	items := make([]EventRecord, 0)
	query := r.db.NewSelect().Model(&items)
	if len(scope) == 2 {
		query = query.Where("tenant_id = ? AND entity_id = ? AND run_id = ?", scope[0], scope[1], runID)
	} else {
		query = query.Where("run_id = ?", runID)
	}
	if sinceSeq > 0 {
		query = query.Where("seq > ?", sinceSeq)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Order("seq ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list workflow events: %w", err)
	}
	return items, nil
}

type RunDef struct {
	bun.BaseModel `bun:"table:workflow_runs"`
	ID            guid.ID   `bun:"id,pk" json:"id"`
	TenantID      guid.ID   `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID      guid.ID   `bun:"entity_id,notnull" json:"entityId"`
	ProjectID     guid.ID   `bun:"project_id,notnull" json:"projectId"`
	OwnerID       guid.ID   `bun:"owner_id,notnull" json:"ownerId"`
	DefJSON       string    `bun:"def_json,notnull" json:"-"`
	Status        RunStatus `bun:"status,notnull" json:"status"`
	ContextJSON   string    `bun:"context_json,notnull" json:"-"`
	Workspace     string    `bun:"workspace,notnull" json:"workspace"`
	RunnerHandle  string    `bun:"runner_handle,notnull" json:"runnerHandle"`
	CreatedAt     int64     `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy     guid.ID   `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt     int64     `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy     guid.ID   `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt     int64     `bun:"deleted_at,notnull" json:"-"`
	DeletedBy     guid.ID   `bun:"deleted_by,notnull" json:"-"`
	Version       int64     `bun:"version,notnull" json:"version"`
}

type RunMetric struct {
	ID          guid.ID
	Status      RunStatus
	ContextJSON string
	CreatedAt   int64
	UpdatedAt   int64
}

func (r *BunRepository) ListRunMetrics(ctx context.Context) ([]RunMetric, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	items := []RunMetric{}
	err = r.db.NewSelect().Table("workflow_runs").Column("id", "status", "context_json", "created_at", "updated_at").
		Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", claims.TenantID, claims.EntityID).
		Order("created_at ASC", "id ASC").Scan(ctx, &items)
	if err != nil {
		return nil, fmt.Errorf("list workflow run metrics: %w", err)
	}
	return items, nil
}

func (r *BunRepository) SaveRunDefinition(ctx context.Context, run RunDef) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if run.TenantID.Zero() {
		run.TenantID = claims.TenantID
	}
	if run.EntityID.Zero() {
		run.EntityID = claims.EntityID
	}
	if run.OwnerID.Zero() {
		run.OwnerID = claims.PrincipalID
	}
	if run.TenantID != claims.TenantID || run.EntityID != claims.EntityID || run.OwnerID != claims.PrincipalID {
		return fmt.Errorf("save workflow run: scope does not match authenticated claims")
	}
	now := time.Now().UTC().UnixMilli()
	if run.CreatedAt == 0 {
		run.CreatedAt = now
	}
	if run.CreatedBy.Zero() {
		run.CreatedBy = claims.PrincipalID
	}
	run.UpdatedAt, run.UpdatedBy = now, claims.PrincipalID
	if run.Version < 1 {
		run.Version = 1
	}
	if run.Status == "" {
		run.Status = RunRunning
	}
	result, err := r.db.NewUpdate().Model((*RunDef)(nil)).
		Set("def_json = ?", run.DefJSON).Set("status = ?", run.Status).Set("context_json = ?", run.ContextJSON).
		Set("workspace = ?", run.Workspace).Set("runner_handle = ?", run.RunnerHandle).
		Set("updated_at = ?", run.UpdatedAt).Set("updated_by = ?", run.UpdatedBy).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", run.ID, claims.TenantID, claims.EntityID).Exec(ctx)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		_, err = r.db.NewInsert().Model(&run).Exec(ctx)
	}
	return err
}

func (r *BunRepository) LoadActiveRunDefinitions(ctx context.Context) ([]RunDef, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	return r.loadActiveRunDefinitions(ctx, "tenant_id = ? AND entity_id = ? AND deleted_at = 0", claims.TenantID, claims.EntityID)
}

// LoadAllActiveRunDefinitions returns every non-terminal run across all
// tenants/entities. It exists for control-plane crash recovery, which must
// rehydrate the control plane's own runs without a user principal in context.
func (r *BunRepository) LoadAllActiveRunDefinitions(ctx context.Context) ([]RunDef, error) {
	return r.loadActiveRunDefinitions(ctx, "deleted_at = 0")
}

func (r *BunRepository) loadActiveRunDefinitions(ctx context.Context, where string, args ...any) ([]RunDef, error) {
	items := make([]RunDef, 0)
	err := r.db.NewSelect().Model(&items).
		Where(where, args...).
		Where("status NOT IN ('completed','failed','cancelled')").Order("created_at DESC").Scan(ctx)
	return items, err
}

func (r *BunRepository) ListRunDefinitions(ctx context.Context, projectID guid.ID, limit int) ([]RunDef, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]RunDef, 0)
	query := r.db.NewSelect().Model(&items).Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", claims.TenantID, claims.EntityID)
	if !projectID.Zero() {
		query = query.Where("project_id = ?", projectID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	err = query.Order("created_at DESC").Scan(ctx)
	return items, err
}

func (r *BunRepository) UpdateRunStatus(ctx context.Context, runID guid.ID, status RunStatus) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	_, err = r.db.NewUpdate().Model((*RunDef)(nil)).Set("status = ?", status).
		Set("updated_at = ?", time.Now().UTC().UnixMilli()).Set("updated_by = ?", claims.PrincipalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", runID, claims.TenantID, claims.EntityID).Exec(ctx)
	return err
}

type WorkflowDefModel struct {
	bun.BaseModel `bun:"table:workflows"`
	ID            guid.ID `bun:"id,pk" json:"id"`
	TenantID      guid.ID `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID      guid.ID `bun:"entity_id,notnull" json:"entityId"`
	OwnerID       guid.ID `bun:"owner_id,notnull" json:"ownerId"`
	WorkflowKey   string  `bun:"workflow_key,notnull" json:"key"`
	Revision      int64   `bun:"revision,notnull" json:"revision"`
	Name          string  `bun:"name,notnull" json:"name"`
	DefJSON       string  `bun:"def_json,notnull" json:"-"`
	DefRaw        any     `bun:"-" json:"def,omitempty"`
	CreatedAt     int64   `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy     guid.ID `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt     int64   `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy     guid.ID `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt     int64   `bun:"deleted_at,notnull" json:"-"`
	DeletedBy     guid.ID `bun:"deleted_by,notnull" json:"-"`
	Version       int64   `bun:"version,notnull" json:"version"`
}

func (r *BunRepository) SaveWorkflow(ctx context.Context, model WorkflowDefModel) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if model.TenantID.Zero() {
		model.TenantID = claims.TenantID
	}
	if model.EntityID.Zero() {
		model.EntityID = claims.EntityID
	}
	if model.OwnerID.Zero() {
		model.OwnerID = claims.PrincipalID
	}
	if model.TenantID != claims.TenantID || model.EntityID != claims.EntityID || model.OwnerID != claims.PrincipalID {
		return fmt.Errorf("save workflow: scope mismatch")
	}
	now := time.Now().UTC().UnixMilli()
	if model.CreatedAt == 0 {
		model.CreatedAt = now
	}
	if model.CreatedBy.Zero() {
		model.CreatedBy = claims.PrincipalID
	}
	model.UpdatedAt, model.UpdatedBy = now, claims.PrincipalID
	if model.Revision < 1 {
		model.Revision = 1
	}
	if model.Version < 1 {
		model.Version = 1
	}
	_, err = r.db.NewInsert().Model(&model).Exec(ctx)
	return err
}

func (r *BunRepository) ListWorkflows(ctx context.Context) ([]WorkflowDefModel, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]WorkflowDefModel, 0)
	err = r.db.NewSelect().Model(&items).Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", claims.TenantID, claims.EntityID).Order("updated_at DESC").Scan(ctx)
	return items, err
}

func (r *BunRepository) GetWorkflow(ctx context.Context, id guid.ID) (*WorkflowDefModel, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	model := new(WorkflowDefModel)
	err = r.db.NewSelect().Model(model).Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, claims.TenantID, claims.EntityID).Scan(ctx)
	return model, err
}

func (r *BunRepository) DeleteWorkflow(ctx context.Context, id guid.ID) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	_, err = r.db.NewUpdate().Model((*WorkflowDefModel)(nil)).Set("deleted_at = ?", now).Set("deleted_by = ?", claims.PrincipalID).
		Set("updated_at = ?", now).Set("updated_by = ?", claims.PrincipalID).Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, claims.TenantID, claims.EntityID).Exec(ctx)
	return err
}

func requireClaims(ctx context.Context) (*authn.Claims, error) {
	claims, ok := authn.FromContext(ctx)
	if !ok || claims.TenantID.Zero() || claims.EntityID.Zero() || claims.PrincipalID.Zero() {
		return nil, fmt.Errorf("workflow repository requires authenticated tenant, entity, and principal claims")
	}
	return claims, nil
}
