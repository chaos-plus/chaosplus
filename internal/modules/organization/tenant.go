package organization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/uptrace/bun"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
)

const (
	TenantActive    = "active"
	TenantSuspended = "suspended"
	TenantDeleted   = "deleted"
)

var (
	ErrTenantNotFound        = errors.New("tenant not found")
	ErrTenantSlugConflict    = errors.New("tenant slug already exists")
	ErrTenantVersionConflict = errors.New("tenant version conflict")
	ErrInvalidTenant         = errors.New("invalid tenant")
	tenantSlugPattern        = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,61}[a-z0-9])?$`)
)

type Tenant struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateTenant struct {
	Slug string
	Name string
}

type UpdateTenant struct {
	Name    *string
	Status  *string
	Version int64
}

type tenantRow struct {
	bun.BaseModel `bun:"table:iam_tenants"`
	ID            string `bun:"id,pk"`
	Slug          string
	Name          string
	Status        string
	Version       int64
	CreatedAt     int64
	UpdatedAt     int64
}

type TenantService struct {
	repo   *Repository
	audit  auditx.Appender
	nextID IDGenerator
	now    func() time.Time
}

func NewTenantService(db *bun.DB, audit auditx.Appender, nextID IDGenerator) *TenantService {
	if db == nil || audit == nil || nextID == nil {
		panic("tenant service requires database, audit appender, and id generator")
	}
	return &TenantService{repo: NewRepository(db), audit: audit, nextID: nextID, now: time.Now}
}

// EnsureTenant idempotently creates the configured bootstrap tenant. Existing
// tenants retain their current name and lifecycle status.
func EnsureTenant(ctx context.Context, db *bun.DB, id string) error {
	if db == nil {
		return fmt.Errorf("ensure tenant: database is required")
	}
	id = strings.TrimSpace(id)
	if !validID(id) {
		return ErrInvalidTenant
	}
	slug := tenantSlugForID(id)
	now := time.Now().UTC().UnixMilli()
	row := tenantRow{ID: id, Slug: slug, Name: id, Status: TenantActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := db.NewInsert().Model(&row).Ignore().Exec(ctx); err != nil {
		return fmt.Errorf("ensure tenant: %w", err)
	}
	return nil
}

func (s *TenantService) List(ctx context.Context, includeDeleted bool) ([]Tenant, error) {
	rows := make([]tenantRow, 0)
	query := s.repo.executor.NewSelect().Model(&rows)
	if !includeDeleted {
		query = query.Where("status <> ?", TenantDeleted)
	}
	if err := query.Order("name ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	tenants := make([]Tenant, 0, len(rows))
	for _, row := range rows {
		tenants = append(tenants, tenantFromRow(row))
	}
	return tenants, nil
}

func (s *TenantService) Get(ctx context.Context, id string) (Tenant, error) {
	row, err := getTenantRow(ctx, s.repo.executor, strings.TrimSpace(id))
	if err != nil {
		return Tenant{}, err
	}
	return tenantFromRow(row), nil
}

func (s *TenantService) Create(ctx context.Context, input CreateTenant) (Tenant, error) {
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))
	input.Name = strings.TrimSpace(input.Name)
	if !validTenantSlug(input.Slug) || !validTenantName(input.Name) {
		return Tenant{}, ErrInvalidTenant
	}
	id, err := s.nextID()
	if err != nil || !validID(id) {
		return Tenant{}, fmt.Errorf("generate tenant id: %w", errors.Join(err, ErrInvalidTenant))
	}
	now := s.now().UTC().UnixMilli()
	row := tenantRow{ID: id, Slug: input.Slug, Name: input.Name, Status: TenantActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	event := auditx.NewEvent(ctx, "_system", "tenant_created", "tenant", id)
	event.Detail["slug"] = input.Slug
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			if bunx.IsUniqueViolation(err) {
				return ErrTenantSlugConflict
			}
			return fmt.Errorf("insert tenant: %w", err)
		}
		if err := policyx.Advance(ctx, tx, s.repo.dialect, id, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Tenant{}, fmt.Errorf("create tenant: %w", err)
	}
	return tenantFromRow(row), nil
}

func (s *TenantService) Update(ctx context.Context, id string, input UpdateTenant) (Tenant, error) {
	id = strings.TrimSpace(id)
	if !validID(id) || input.Version < 1 || (input.Name == nil && input.Status == nil) {
		return Tenant{}, ErrInvalidTenant
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if !validTenantName(name) {
			return Tenant{}, ErrInvalidTenant
		}
		input.Name = &name
	}
	if input.Status != nil && *input.Status != TenantActive && *input.Status != TenantSuspended {
		return Tenant{}, ErrInvalidTenant
	}
	now := s.now().UTC().UnixMilli()
	var updated tenantRow
	event := auditx.NewEvent(ctx, "_system", "tenant_updated", "tenant", id)
	err := s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		current, err := getTenantRow(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Status == TenantDeleted || current.Version != input.Version {
			return ErrTenantVersionConflict
		}
		updated = current
		if input.Name != nil {
			updated.Name = *input.Name
		}
		if input.Status != nil {
			updated.Status = *input.Status
		}
		if updated.Name == current.Name && updated.Status == current.Status {
			return nil
		}
		updated.Version++
		updated.UpdatedAt = now
		result, err := tx.NewUpdate().Model(&updated).Column("name", "status", "version", "updated_at").
			Where("id = ? AND version = ?", id, current.Version).Exec(ctx)
		if err != nil {
			return fmt.Errorf("update tenant: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrTenantVersionConflict
		}
		if err := policyx.Advance(ctx, tx, s.repo.dialect, id, now); err != nil {
			return err
		}
		event.Detail["status"] = updated.Status
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Tenant{}, fmt.Errorf("update tenant: %w", err)
	}
	return tenantFromRow(updated), nil
}

func (s *TenantService) Delete(ctx context.Context, id string, version int64) error {
	id = strings.TrimSpace(id)
	if !validID(id) || version < 1 {
		return ErrInvalidTenant
	}
	now := s.now().UTC().UnixMilli()
	event := auditx.NewEvent(ctx, "_system", "tenant_deleted", "tenant", id)
	err := s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		current, err := getTenantRow(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Status == TenantDeleted || current.Version != version {
			return ErrTenantVersionConflict
		}
		result, err := tx.NewUpdate().Model((*tenantRow)(nil)).Set("status = ?", TenantDeleted).
			Set("version = version + 1").Set("updated_at = ?", now).Where("id = ? AND version = ?", id, version).Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete tenant: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrTenantVersionConflict
		}
		if err := policyx.Advance(ctx, tx, s.repo.dialect, id, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	return nil
}

func getTenantRow(ctx context.Context, db bun.IDB, id string) (tenantRow, error) {
	if !validID(id) {
		return tenantRow{}, ErrInvalidTenant
	}
	var row tenantRow
	if err := db.NewSelect().Model(&row).Where("id = ?", id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tenantRow{}, ErrTenantNotFound
		}
		return tenantRow{}, fmt.Errorf("get tenant: %w", err)
	}
	return row, nil
}

func tenantFromRow(row tenantRow) Tenant {
	return Tenant{ID: row.ID, Slug: row.Slug, Name: row.Name, Status: row.Status, Version: row.Version, CreatedAt: unixTime(row.CreatedAt), UpdatedAt: unixTime(row.UpdatedAt)}
}

func validTenantSlug(value string) bool { return tenantSlugPattern.MatchString(value) }
func validTenantName(value string) bool { return validName(value) }

func tenantSlugForID(id string) string {
	lower := strings.ToLower(id)
	if validTenantSlug(lower) {
		return lower
	}
	if len(lower) < 3 && strings.IndexFunc(lower, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) < 0 {
		return "tenant-" + lower
	}
	var slug strings.Builder
	for _, r := range lower {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			slug.WriteRune(r)
		} else if slug.Len() > 0 && !strings.HasSuffix(slug.String(), "-") {
			slug.WriteByte('-')
		}
	}
	base := strings.Trim(slug.String(), "-")
	if base == "" {
		base = "tenant"
	}
	suffix := fmt.Sprintf("-%x", sha256.Sum256([]byte(id)))[:9]
	if len(base) > 63-len(suffix) {
		base = strings.TrimRight(base[:63-len(suffix)], "-")
	}
	return base + suffix
}
