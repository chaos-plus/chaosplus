package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// ValidationResult is one human/validator verdict on a node's artifact
// (PRD §11 / F.2). REVIEW_APPROVED → passed=1; REVIEW_REJECTED → passed=0.
type ValidationResult struct {
	bun.BaseModel `bun:"table:validation_results"`
	ID            string `bun:"id,pk"`
	ArtifactID    string `bun:"artifact_id,notnull,default:''"`
	ExecutionID   string `bun:"execution_id,notnull,default:''"`
	ValidatorID   string `bun:"validator_id,notnull,default:'human'"`
	ValidatorType string `bun:"validator_type,notnull,default:'human'"`
	Passed        int    `bun:"passed,notnull"`
	EvidenceJSON  string `bun:"evidence_json,notnull,default:'{}'"`
	ReviewedBy    string `bun:"reviewed_by,notnull,default:''"`
	TS            int64  `bun:"ts,notnull"`
}

// FeedbackLogEntry is one structured rejection (PRD §13), bound to the artifact
// it rejected — the audit trail + source for next-attempt injection.
type FeedbackLogEntry struct {
	bun.BaseModel `bun:"table:feedback_log"`
	ID            string `bun:"id,pk"`
	ArtifactID    string `bun:"artifact_id,notnull,default:''"`
	ExecutionID   string `bun:"execution_id,notnull,default:''"`
	Reviewer      string `bun:"reviewer,notnull,default:''"`
	Category      string `bun:"category,notnull,default:''"`
	Location      string `bun:"location,notnull,default:''"`
	Expected      string `bun:"expected,notnull,default:''"`
	Detail        string `bun:"detail,notnull,default:''"`
	TS            int64  `bun:"ts,notnull"`
}

// RecordValidationResult persists one validator verdict.
func (s *Store) RecordValidationResult(ctx context.Context, v ValidationResult) error {
	v.TS = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(&v).Exec(ctx); err != nil {
		return fmt.Errorf("store record validation result: %w", err)
	}
	return nil
}

// RecordFeedbackLog appends one structured rejection entry.
func (s *Store) RecordFeedbackLog(ctx context.Context, f FeedbackLogEntry) error {
	f.TS = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(&f).Exec(ctx); err != nil {
		return fmt.Errorf("store record feedback log: %w", err)
	}
	return nil
}

// RecordRejection persists a rejected verdict AND its structured feedback in one
// transaction, so a rejection either writes both audit projections or neither.
func (s *Store) RecordRejection(ctx context.Context, v ValidationResult, f FeedbackLogEntry) error {
	v.TS, f.TS = time.Now().UnixMilli(), time.Now().UnixMilli()
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
func (s *Store) ListValidationResults(ctx context.Context, artifactID string, limit int) ([]ValidationResult, error) {
	out := []ValidationResult{}
	q := s.db.NewSelect().Model(&out).Where("artifact_id = ?", artifactID)
	if err := q.Order("ts DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list validation results: %w", err)
	}
	return out, nil
}

// ListFeedbackLog returns the structured rejections for a run, newest first.
func (s *Store) ListFeedbackLog(ctx context.Context, artifactID string, limit int) ([]FeedbackLogEntry, error) {
	out := []FeedbackLogEntry{}
	q := s.db.NewSelect().Model(&out).Where("artifact_id = ?", artifactID)
	if err := q.Order("ts DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list feedback log: %w", err)
	}
	return out, nil
}
