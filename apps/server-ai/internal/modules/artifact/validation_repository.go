package artifact

import (
	"context"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

// ValidationResult is one human/validator verdict on a node's artifact
// (PRD §11 / F.2). REVIEW_APPROVED → passed=1; REVIEW_REJECTED → passed=0.
type ValidationResult struct {
	bun.BaseModel `bun:"table:validation_results"`
	ID            guid.ID `bun:"id,pk"`
	TenantID      guid.ID `bun:"tenant_id,notnull"`
	EntityID      guid.ID `bun:"entity_id,notnull"`
	ArtifactID    guid.ID `bun:"artifact_id,notnull"`
	ExecutionKey  string  `bun:"execution_key,notnull,default:''"`
	ValidatorKey  string  `bun:"validator_key,notnull,default:'human'"`
	ValidatorType string  `bun:"validator_type,notnull,default:'human'"`
	Passed        bool    `bun:"passed,notnull"`
	EvidenceJSON  string  `bun:"evidence_json,notnull,default:'{}'"`
	ReviewedBy    guid.ID `bun:"reviewed_by,notnull"`
	TS            int64   `bun:"ts,notnull"`
}

// FeedbackLogEntry is one structured rejection (PRD §13), bound to the artifact
// it rejected — the audit trail + source for next-attempt injection.
type FeedbackLogEntry struct {
	bun.BaseModel `bun:"table:feedback_log"`
	ID            guid.ID `bun:"id,pk"`
	TenantID      guid.ID `bun:"tenant_id,notnull"`
	EntityID      guid.ID `bun:"entity_id,notnull"`
	ArtifactID    guid.ID `bun:"artifact_id,notnull"`
	ExecutionKey  string  `bun:"execution_key,notnull,default:''"`
	ReviewerID    guid.ID `bun:"reviewer_id,notnull"`
	Category      string  `bun:"category,notnull,default:''"`
	Location      string  `bun:"location,notnull,default:''"`
	Expected      string  `bun:"expected,notnull,default:''"`
	Detail        string  `bun:"detail,notnull,default:''"`
	TS            int64   `bun:"ts,notnull"`
}

// RecordValidationResult persists one validator verdict.
func (s *BunRepository) RecordValidationResult(ctx context.Context, v ValidationResult) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if v.ID.Zero() {
		v.ID, err = s.nextID()
		if err != nil {
			return fmt.Errorf("generate validation result id: %w", err)
		}
	}
	v.TenantID, v.EntityID = claims.TenantID, claims.EntityID
	if v.ReviewedBy.Zero() {
		v.ReviewedBy = claims.PrincipalID
	}
	if v.ReviewedBy != claims.PrincipalID {
		return fmt.Errorf("record validation result: reviewer does not match authenticated principal")
	}
	v.TS = time.Now().UTC().UnixMilli()
	if _, err := s.db.NewInsert().Model(&v).Exec(ctx); err != nil {
		return fmt.Errorf("store record validation result: %w", err)
	}
	return nil
}

// RecordFeedbackLog appends one structured rejection entry.
func (s *BunRepository) RecordFeedbackLog(ctx context.Context, f FeedbackLogEntry) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if f.ID.Zero() {
		f.ID, err = s.nextID()
		if err != nil {
			return fmt.Errorf("generate feedback id: %w", err)
		}
	}
	f.TenantID, f.EntityID = claims.TenantID, claims.EntityID
	if f.ReviewerID.Zero() {
		f.ReviewerID = claims.PrincipalID
	}
	if f.ReviewerID != claims.PrincipalID {
		return fmt.Errorf("record feedback: reviewer does not match authenticated principal")
	}
	f.TS = time.Now().UTC().UnixMilli()
	if _, err := s.db.NewInsert().Model(&f).Exec(ctx); err != nil {
		return fmt.Errorf("store record feedback log: %w", err)
	}
	return nil
}

// RecordRejection persists a rejected verdict AND its structured feedback in one
// transaction, so a rejection either writes both audit projections or neither.
func (s *BunRepository) RecordRejection(ctx context.Context, v ValidationResult, f FeedbackLogEntry) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if v.ID.Zero() {
		v.ID, err = s.nextID()
		if err != nil {
			return fmt.Errorf("generate validation result id: %w", err)
		}
	}
	if f.ID.Zero() {
		f.ID, err = s.nextID()
		if err != nil {
			return fmt.Errorf("generate feedback id: %w", err)
		}
	}
	v.TenantID, v.EntityID, v.ReviewedBy = claims.TenantID, claims.EntityID, claims.PrincipalID
	f.TenantID, f.EntityID, f.ReviewerID = claims.TenantID, claims.EntityID, claims.PrincipalID
	now := time.Now().UTC().UnixMilli()
	v.TS, f.TS = now, now
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&v).Exec(ctx); err != nil {
			return fmt.Errorf("insert validation result: %w", err)
		}
		if _, err := tx.NewInsert().Model(&f).Exec(ctx); err != nil {
			return fmt.Errorf("insert feedback log: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("store record rejection: %w", err)
	}
	return nil
}

// ListValidationResults returns the verdicts for a run, newest first.
func (s *BunRepository) ListValidationResults(ctx context.Context, artifactID guid.ID, limit int) ([]ValidationResult, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	out := []ValidationResult{}
	q := s.db.NewSelect().Model(&out).
		Where("tenant_id = ? AND entity_id = ? AND artifact_id = ?", claims.TenantID, claims.EntityID, artifactID)
	if err := q.Order("ts DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list validation results: %w", err)
	}
	return out, nil
}

// ListFeedbackLog returns the structured rejections for a run, newest first.
func (s *BunRepository) ListFeedbackLog(ctx context.Context, artifactID guid.ID, limit int) ([]FeedbackLogEntry, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	out := []FeedbackLogEntry{}
	q := s.db.NewSelect().Model(&out).
		Where("tenant_id = ? AND entity_id = ? AND artifact_id = ?", claims.TenantID, claims.EntityID, artifactID)
	if err := q.Order("ts DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list feedback log: %w", err)
	}
	return out, nil
}
