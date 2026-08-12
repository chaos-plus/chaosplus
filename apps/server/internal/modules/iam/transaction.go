package iam

import (
	"context"
	"fmt"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type transactionRecord struct {
	auditx.Event
	PolicyChanged bool
	SkipAudit     bool
}

type transactionCoordinator struct {
	repo  *Repository
	audit auditx.Appender
}

func newTransactionCoordinator(repo *Repository, audit auditx.Appender) *transactionCoordinator {
	return &transactionCoordinator{repo: repo, audit: audit}
}

func (c *transactionCoordinator) Run(ctx context.Context, record *transactionRecord, mutate func(*Repository) error) error {
	if record == nil || mutate == nil {
		return fmt.Errorf("invalid IAM write transaction")
	}
	return c.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := c.repo.withExecutor(tx)
		if err := mutate(repo); err != nil {
			return err
		}
		if record.PolicyChanged && !record.TenantID.Zero() {
			if err := repo.bumpPolicyRevision(ctx, record.TenantID); err != nil {
				return err
			}
		}
		if record.SkipAudit {
			return nil
		}
		return c.audit(ctx, tx, record.Event)
	})
}

func newAuditRecord(ctx context.Context, tenantID guid.ID, eventType, targetType string, targetID guid.ID) *transactionRecord {
	return &transactionRecord{Event: auditx.NewEvent(ctx, tenantID, eventType, targetType, targetID)}
}

func (r *Repository) bumpPolicyRevision(ctx context.Context, tenantID guid.ID) error {
	return policyx.Advance(ctx, r.executor, r.dialect, tenantID, r.now().UTC().UnixMilli())
}

func (r *Repository) policyRevision(ctx context.Context, tenantID guid.ID) (int64, error) {
	revision, err := policyx.Current(ctx, r.executor, tenantID)
	if err != nil {
		return 0, fmt.Errorf("get IAM policy revision: %w", err)
	}
	return revision, nil
}
