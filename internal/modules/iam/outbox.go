package iam

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

type RelationshipWriter interface {
	WriteRelationshipUpdates(context.Context, []spicedbx.RelationshipUpdate) (spicedbx.ZedToken, error)
}

type OutboxConfig struct {
	PollInterval    time.Duration `mapstructure:"poll_interval" description:"IAM authz outbox polling interval" default:"250ms"`
	BatchSize       int           `mapstructure:"batch_size" description:"maximum authz outbox rows claimed per poll" default:"32"`
	LockTimeout     time.Duration `mapstructure:"lock_timeout" description:"time before an abandoned outbox claim is recovered" default:"30s"`
	DeliveryTimeout time.Duration `mapstructure:"delivery_timeout" description:"deadline for one SpiceDB outbox write; must be shorter than lock timeout" default:"10s"`
	MaxAttempts     int           `mapstructure:"max_attempts" description:"outbox attempts before a row becomes dead" default:"8"`
	BaseBackoff     time.Duration `mapstructure:"base_backoff" description:"initial authz outbox retry delay" default:"250ms"`
	MaxBackoff      time.Duration `mapstructure:"max_backoff" description:"maximum authz outbox retry delay" default:"30s"`
}

func (c OutboxConfig) withDefaults() OutboxConfig {
	if c.PollInterval <= 0 {
		c.PollInterval = 250 * time.Millisecond
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 32
	}
	if c.LockTimeout <= 0 {
		c.LockTimeout = 30 * time.Second
	}
	if c.DeliveryTimeout <= 0 || c.DeliveryTimeout >= c.LockTimeout {
		c.DeliveryTimeout = c.LockTimeout / 3
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 8
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 250 * time.Millisecond
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
	return c
}

func (r *Repository) ClaimOutbox(ctx context.Context, workerID string, cfg OutboxConfig) ([]OutboxMessage, error) {
	cfg = cfg.withDefaults()
	now := r.now().UTC().UnixMilli()
	stale := now - cfg.LockTimeout.Milliseconds()
	var claimed []OutboxMessage
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model((*outboxRow)(nil)).
			Set("status = ?", OutboxPending).Set("locked_by = ''").Set("locked_at = 0").Set("available_at = ?", now).
			Where("status = ? AND locked_at > 0 AND locked_at <= ?", OutboxProcessing, stale).Exec(ctx); err != nil {
			return fmt.Errorf("recover stale authz outbox claims: %w", err)
		}
		var ids []string
		if err := tx.NewSelect().Model((*outboxRow)(nil)).Column("id").
			Where("status = ? AND available_at <= ?", OutboxPending, now).
			Order("available_at ASC", "id ASC").Limit(cfg.BatchSize).Scan(ctx, &ids); err != nil {
			return fmt.Errorf("select authz outbox candidates: %w", err)
		}
		for _, id := range ids {
			result, err := tx.NewUpdate().Model((*outboxRow)(nil)).
				Set("status = ?", OutboxProcessing).Set("locked_by = ?", workerID).Set("locked_at = ?", now).
				Set("attempts = attempts + 1").Set("updated_at = ?", now).
				Where("id = ? AND status = ? AND available_at <= ?", id, OutboxPending, now).Exec(ctx)
			if err != nil {
				return fmt.Errorf("claim authz outbox row: %w", err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("read authz outbox claim result: %w", err)
			}
			if affected == 0 {
				continue
			}
			var row outboxRow
			if err := tx.NewSelect().Model(&row).Where("id = ? AND status = ? AND locked_by = ?", id, OutboxProcessing, workerID).Scan(ctx); err != nil {
				return fmt.Errorf("read claimed authz outbox row: %w", err)
			}
			claimed = append(claimed, outboxMessage(row))
		}
		return nil
	})
	return claimed, err
}

func (r *Repository) CompleteOutbox(ctx context.Context, message OutboxMessage, workerID string, token spicedbx.ZedToken) error {
	now := r.now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*outboxRow)(nil)).
		Set("status = ?", OutboxDelivered).Set("locked_by = ''").Set("locked_at = 0").Set("last_error = ''").
		Set("zed_token = ?", string(token)).Set("delivered_at = ?", now).Set("updated_at = ?", now).
		Where("id = ? AND version = ? AND status = ? AND locked_by = ?", message.ID, message.Version, OutboxProcessing, workerID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("complete authz outbox row: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read authz outbox completion result: %w", err)
	}
	if affected > 0 {
		return nil
	}
	if requeued, err := r.requeueLateOpposite(ctx, message, now); err != nil {
		return err
	} else if requeued {
		return nil
	}
	_, err = r.db.NewUpdate().Model((*outboxRow)(nil)).
		Set("status = ?", OutboxPending).Set("attempts = 0").Set("available_at = ?", now).
		Set("locked_by = ''").Set("locked_at = 0").Set("last_error = ''").Set("updated_at = ?", now).
		Where("id = ? AND status = ? AND locked_by = ?", message.ID, OutboxProcessing, workerID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("requeue changed authz outbox row: %w", err)
	}
	return nil
}

func (r *Repository) FailOutbox(ctx context.Context, message OutboxMessage, workerID string, cause error, cfg OutboxConfig) error {
	cfg = cfg.withDefaults()
	now := r.now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*outboxRow)(nil)).
		Set("status = ?", OutboxPending).Set("attempts = 0").Set("available_at = ?", now).
		Set("locked_by = ''").Set("locked_at = 0").Set("last_error = ''").Set("updated_at = ?", now).
		Where("id = ? AND version <> ? AND status = ? AND locked_by = ?", message.ID, message.Version, OutboxProcessing, workerID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("requeue changed failed authz outbox row: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		return nil
	}
	if requeued, err := r.requeueLateOpposite(ctx, message, now); err != nil {
		return err
	} else if requeued {
		return nil
	}

	status := OutboxPending
	availableAt := now + backoff(cfg, message.Attempts).Milliseconds()
	if message.Attempts >= cfg.MaxAttempts {
		status = OutboxDead
		availableAt = now
	}
	lastError := cause.Error()
	if len(lastError) > 2048 {
		lastError = lastError[:2048]
	}
	_, err = r.db.NewUpdate().Model((*outboxRow)(nil)).
		Set("status = ?", status).Set("available_at = ?", availableAt).Set("locked_by = ''").Set("locked_at = 0").
		Set("last_error = ?", lastError).Set("updated_at = ?", now).
		Where("id = ? AND version = ? AND status = ? AND locked_by = ?", message.ID, message.Version, OutboxProcessing, workerID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("fail authz outbox row: %w", err)
	}
	return nil
}

func (r *Repository) requeueLateOpposite(ctx context.Context, message OutboxMessage, now int64) (bool, error) {
	result, err := r.db.NewUpdate().Model((*outboxRow)(nil)).
		Set("status = ?", OutboxPending).Set("attempts = 0").Set("available_at = ?", now).
		Set("locked_by = ''").Set("locked_at = 0").Set("last_error = ''").Set("updated_at = ?", now).
		Where("id = ? AND version > ? AND operation <> ? AND status IN (?, ?)",
			message.ID, message.Version, message.Operation, OutboxDelivered, OutboxDead).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("requeue authz outbox after late opposite write: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read late authz outbox requeue result: %w", err)
	}
	return affected > 0, nil
}

func (r *Repository) OutboxStatus(ctx context.Context, tenantID string, rel spicedbx.Relationship) (OutboxStatus, spicedbx.ZedToken, error) {
	var row outboxRow
	err := r.db.NewSelect().Model(&row).Where("tenant_id = ? AND relationship_key = ?", tenantID, relationshipKey(tenantID, rel)).Scan(ctx)
	if err != nil {
		return "", "", err
	}
	return OutboxStatus(row.Status), spicedbx.ZedToken(row.ZedToken), nil
}

func outboxMessage(row outboxRow) OutboxMessage {
	return OutboxMessage{ID: row.ID, TenantID: row.TenantID, Operation: spicedbx.RelationshipOperation(row.Operation), Version: row.Version, Attempts: row.Attempts,
		Relationship: spicedbx.Relationship{Resource: spicedbx.ObjectRef{Type: row.ResourceType, ID: row.ResourceID}, Relation: row.ResourceRelation,
			Subject: spicedbx.SubjectRef{Object: spicedbx.ObjectRef{Type: row.SubjectType, ID: row.SubjectID}, Relation: row.SubjectRelation}}}
}

func backoff(cfg OutboxConfig, attempt int) time.Duration {
	delay := cfg.BaseBackoff
	for i := 1; i < attempt && delay < cfg.MaxBackoff; i++ {
		delay *= 2
		if delay > cfg.MaxBackoff {
			return cfg.MaxBackoff
		}
	}
	return delay
}

type OutboxWorker struct {
	repo     *Repository
	writer   RelationshipWriter
	cfg      OutboxConfig
	nextID   IDGenerator
	wake     chan struct{}
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	workerID string
}

func NewOutboxWorker(repo *Repository, writer RelationshipWriter, cfg OutboxConfig, nextID IDGenerator) *OutboxWorker {
	if repo == nil || writer == nil || nextID == nil {
		panic("iam outbox worker requires repository, writer, and id generator")
	}
	return &OutboxWorker{repo: repo, writer: writer, cfg: cfg.withDefaults(), nextID: nextID, wake: make(chan struct{}, 1)}
}

func (w *OutboxWorker) Start(ctx context.Context) error {
	if w.cancel != nil {
		return nil
	}
	id, err := w.nextID()
	if err != nil {
		return fmt.Errorf("generate authz outbox worker id: %w", err)
	}
	w.workerID = truncateWorkerID("iam-" + id)
	workerCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.wg.Add(1)
	go w.loop(workerCtx)
	return nil
}

func (w *OutboxWorker) Stop() {
	if w.cancel == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
	w.cancel = nil
}

func (w *OutboxWorker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *OutboxWorker) loop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("iam authz outbox delivery failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-w.wake:
		}
	}
}

func (w *OutboxWorker) RunOnce(ctx context.Context) error {
	if w.workerID == "" {
		id, err := w.nextID()
		if err != nil {
			return err
		}
		w.workerID = truncateWorkerID("iam-" + id)
	}
	messages, err := w.repo.ClaimOutbox(ctx, w.workerID, w.cfg)
	if err != nil {
		return err
	}
	var deliveryErrors []error
	for _, message := range messages {
		deliveryCtx, cancel := context.WithTimeout(ctx, w.cfg.DeliveryTimeout)
		token, writeErr := w.writer.WriteRelationshipUpdates(deliveryCtx, []spicedbx.RelationshipUpdate{{Operation: message.Operation, Relationship: message.Relationship}})
		cancel()
		if writeErr == nil {
			if err := w.repo.CompleteOutbox(ctx, message, w.workerID, token); err != nil {
				deliveryErrors = append(deliveryErrors, err)
			}
			continue
		}
		if err := w.repo.FailOutbox(ctx, message, w.workerID, writeErr, w.cfg); err != nil {
			deliveryErrors = append(deliveryErrors, err)
		}
		deliveryErrors = append(deliveryErrors, fmt.Errorf("deliver %s: %w", message.Relationship, writeErr))
	}
	return errors.Join(deliveryErrors...)
}

func truncateWorkerID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 60 {
		return id[:60]
	}
	return id
}
