package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

const (
	defaultMinRetentionDays = 365
	defaultArchiveAfterDays = 730
	maxRetentionDays        = 36500
)

var ErrRetentionInvalid = errors.New("invalid audit retention policy")

// RetentionPolicy is a tenant's audit retention contract. Events stay
// available for at least MinDays and become archive-eligible after
// ArchiveAfterDays; WORM semantics mean archiving never deletes events.
type RetentionPolicy struct {
	TenantID         string    `json:"tenant_id"`
	MinDays          int       `json:"min_days"`
	ArchiveAfterDays int       `json:"archive_after_days"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

type retentionPolicyRow struct {
	bun.BaseModel    `bun:"table:iam_audit_retention_policies"`
	TenantID         string `bun:"tenant_id,pk"`
	MinDays          int
	ArchiveAfterDays int
	UpdatedAt        int64
}

// Governance is the WORM governance status of one tenant: the retention
// policy, chain integrity, the anchor chain, and archive statistics.
type Governance struct {
	Policy       RetentionPolicy `json:"policy"`
	Integrity    Integrity       `json:"integrity"`
	Anchors      []Anchor        `json:"anchors,omitempty"`
	TotalEvents  int64           `json:"total_events"`
	ArchiveReady int64           `json:"archive_ready_events"`
	Anchored     int64           `json:"anchored_events"`
}

// GetRetentionPolicy returns the tenant's policy, or the platform default
// when the tenant has never configured one.
func (s *Service) GetRetentionPolicy(ctx context.Context, tenantID string) (RetentionPolicy, error) {
	if strings.TrimSpace(tenantID) == "" || len(tenantID) > 128 {
		return RetentionPolicy{}, ErrRetentionInvalid
	}
	var row retentionPolicyRow
	err := s.db.NewSelect().Model(&row).Where("tenant_id = ?", tenantID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return RetentionPolicy{TenantID: tenantID, MinDays: defaultMinRetentionDays, ArchiveAfterDays: defaultArchiveAfterDays}, nil
	}
	if err != nil {
		return RetentionPolicy{}, fmt.Errorf("read audit retention policy: %w", err)
	}
	return policyFromRow(row), nil
}

// SetRetentionPolicy upserts the tenant policy, refusing archive thresholds
// earlier than the minimum retention. The mutation is recorded in the audit
// chain so the compliance record is complete.
func (s *Service) SetRetentionPolicy(ctx context.Context, tenantID string, minDays, archiveAfterDays int) (RetentionPolicy, error) {
	if strings.TrimSpace(tenantID) == "" || len(tenantID) > 128 {
		return RetentionPolicy{}, ErrRetentionInvalid
	}
	if minDays < 1 || minDays > maxRetentionDays || archiveAfterDays < minDays || archiveAfterDays > maxRetentionDays {
		return RetentionPolicy{}, ErrRetentionInvalid
	}
	now := s.now().UTC().UnixMilli()
	row := retentionPolicyRow{TenantID: tenantID, MinDays: minDays, ArchiveAfterDays: archiveAfterDays, UpdatedAt: now}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		query := tx.NewInsert().Model(&row).
			On("CONFLICT (tenant_id) DO UPDATE SET min_days = excluded.min_days, archive_after_days = excluded.archive_after_days, updated_at = excluded.updated_at")
		if s.dialect == "mysql" {
			query = tx.NewInsert().Model(&row).
				On("DUPLICATE KEY UPDATE min_days = VALUES(min_days), archive_after_days = VALUES(archive_after_days), updated_at = VALUES(updated_at)")
		}
		if _, err := query.Exec(ctx); err != nil {
			return fmt.Errorf("save audit retention policy: %w", err)
		}
		if _, err := s.AppendTo(ctx, tx, EventInput{
			TenantID: tenantID, EventType: "audit_retention_policy_updated",
			TargetType: "audit_retention_policy", TargetID: tenantID, Outcome: "success",
			Detail: map[string]any{"min_days": minDays, "archive_after_days": archiveAfterDays},
		}); err != nil {
			return fmt.Errorf("audit retention policy change: %w", err)
		}
		return nil
	})
	if err != nil {
		return RetentionPolicy{}, err
	}
	return policyFromRow(row), nil
}

// Governance assembles the WORM governance report for one tenant.
func (s *Service) Governance(ctx context.Context, tenantID string) (Governance, error) {
	policy, err := s.GetRetentionPolicy(ctx, tenantID)
	if err != nil {
		return Governance{}, err
	}
	integrity, err := s.Verify(ctx, tenantID)
	if err != nil {
		return Governance{}, err
	}
	var anchors []Anchor
	if s.anchor != nil {
		anchors, err = s.anchor.List(ctx, tenantID)
		if err != nil {
			return Governance{}, anchorStoreError(err)
		}
	}
	total, err := s.db.NewSelect().Model((*eventRow)(nil)).Where("tenant_id = ?", tenantID).Count(ctx)
	if err != nil {
		return Governance{}, fmt.Errorf("count audit events: %w", err)
	}
	cutoff := s.now().UTC().AddDate(0, 0, -policy.ArchiveAfterDays).UnixMilli()
	archiveReady, err := s.db.NewSelect().Model((*eventRow)(nil)).Where("tenant_id = ? AND created_at < ?", tenantID, cutoff).Count(ctx)
	if err != nil {
		return Governance{}, fmt.Errorf("count archive-ready audit events: %w", err)
	}
	report := Governance{Policy: policy, Integrity: integrity, Anchors: anchors, TotalEvents: int64(total), ArchiveReady: int64(archiveReady)}
	if len(anchors) > 0 {
		report.Anchored = anchors[len(anchors)-1].HeadSequence
	}
	return report, nil
}

func policyFromRow(row retentionPolicyRow) RetentionPolicy {
	return RetentionPolicy{TenantID: row.TenantID, MinDays: row.MinDays, ArchiveAfterDays: row.ArchiveAfterDays, UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}
}
