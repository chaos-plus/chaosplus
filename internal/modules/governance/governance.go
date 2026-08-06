package governance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/uptrace/bun"
)

const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusCancelled = "cancelled"
	StatusRevoked   = "revoked"
	StatusExpired   = "expired"

	requestTTL        = 7 * 24 * time.Hour
	maxAccessDuration = 365 * 24 * time.Hour
)

var (
	ErrInvalid           = errors.New("invalid access request")
	ErrNotFound          = errors.New("access request not found")
	ErrRoleNotFound      = errors.New("requested role not found")
	ErrRequesterInactive = errors.New("access requester is not an active tenant member")
	ErrAlreadyGranted    = errors.New("requested access is already granted")
	ErrStateConflict     = errors.New("access request state conflict")
	ErrExpired           = errors.New("access request expired")
	ErrSelfApproval      = errors.New("access requester cannot approve their own request")
	ErrRequesterOnly     = errors.New("only the requester can withdraw this access request")
)

type AccessRequest struct {
	ID               string     `json:"id"`
	TenantID         string     `json:"tenant_id"`
	RequesterID      string     `json:"requester_id"`
	RoleID           string     `json:"role_id"`
	RoleName         string     `json:"role_name"`
	Reason           string     `json:"reason"`
	Status           string     `json:"status"`
	AccessExpiresAt  time.Time  `json:"access_expires_at"`
	RequestExpiresAt time.Time  `json:"request_expires_at"`
	DecidedBy        string     `json:"decided_by,omitempty"`
	DecisionNote     string     `json:"decision_note,omitempty"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
	RevokedBy        string     `json:"revoked_by,omitempty"`
	RevokeReason     string     `json:"revoke_reason,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type RequestableRole struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CreateAccessRequest struct {
	RoleID          string
	Reason          string
	AccessExpiresAt time.Time
}

type RoleGrantStore struct {
	Grant           func(context.Context, bun.IDB, string, string, string, string, string, time.Time, time.Time) error
	Revoke          func(context.Context, bun.IDB, string, string) (bool, error)
	RemovePermanent func(context.Context, bun.IDB, string, string, string, int64) (bool, error)
}

type IDGenerator func() (string, error)

type Service struct {
	db      *bun.DB
	audit   auditx.Appender
	grants  RoleGrantStore
	nextID  IDGenerator
	dialect string
	now     func() time.Time
}

type accessRequestRow struct {
	bun.BaseModel    `bun:"table:iam_access_requests"`
	TenantID         string `bun:"tenant_id,pk"`
	ID               string `bun:"id,pk"`
	RequesterID      string `bun:"requester_id"`
	RoleID           string `bun:"role_id"`
	RoleName         string `bun:"role_name"`
	Reason           string `bun:"reason"`
	SnapshotJSON     string `bun:"snapshot_json"`
	Status           string `bun:"status"`
	AccessExpiresAt  int64  `bun:"access_expires_at"`
	RequestExpiresAt int64  `bun:"request_expires_at"`
	DecidedBy        string `bun:"decided_by"`
	DecisionNote     string `bun:"decision_note"`
	DecidedAt        int64  `bun:"decided_at"`
	RevokedBy        string `bun:"revoked_by"`
	RevokeReason     string `bun:"revoke_reason"`
	RevokedAt        int64  `bun:"revoked_at"`
	CreatedAt        int64  `bun:"created_at"`
	UpdatedAt        int64  `bun:"updated_at"`
}

type requestSnapshot struct {
	Version         int    `json:"version"`
	RequesterID     string `json:"requester_id"`
	RoleID          string `json:"role_id"`
	RoleName        string `json:"role_name"`
	AccessExpiresAt string `json:"access_expires_at"`
}

func NewService(db *bun.DB, audit auditx.Appender, grants RoleGrantStore, nextID IDGenerator) *Service {
	if db == nil || audit == nil || grants.Grant == nil || grants.Revoke == nil || grants.RemovePermanent == nil || nextID == nil {
		panic("governance service requires database, audit appender, role grant store, and id generator")
	}
	dialect := db.Dialect().Name().String()
	if dialect == "pg" {
		dialect = "postgres"
	}
	return &Service{db: db, audit: audit, grants: grants, nextID: nextID, dialect: dialect, now: time.Now}
}

func (s *Service) RequestableRoles(ctx context.Context, tenantID string) ([]RequestableRole, error) {
	if !validID(tenantID, 128) {
		return nil, ErrInvalid
	}
	roles := make([]RequestableRole, 0)
	if err := s.db.NewSelect().Table("iam_roles").Column("id", "name", "description").Where("tenant_id = ?", tenantID).Order("name ASC", "id ASC").Scan(ctx, &roles); err != nil {
		return nil, fmt.Errorf("list requestable roles: %w", err)
	}
	return roles, nil
}

func (s *Service) Create(ctx context.Context, tenantID, requesterID string, input CreateAccessRequest) (AccessRequest, error) {
	tenantID = strings.TrimSpace(tenantID)
	requesterID = strings.TrimSpace(requesterID)
	input.RoleID = strings.TrimSpace(input.RoleID)
	input.Reason = strings.TrimSpace(input.Reason)
	now := s.now().UTC()
	if !validID(tenantID, 128) || !validID(requesterID, 255) || !validID(input.RoleID, 32) || len(input.Reason) < 3 || len(input.Reason) > 500 ||
		!input.AccessExpiresAt.After(now) || input.AccessExpiresAt.After(now.Add(maxAccessDuration)) {
		return AccessRequest{}, ErrInvalid
	}
	id, err := s.nextID()
	if err != nil || !validID(id, 64) {
		return AccessRequest{}, fmt.Errorf("generate access request id: %w", errors.Join(err, ErrInvalid))
	}
	var row accessRequestRow
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		active, err := activeTenantMember(ctx, tx, tenantID, requesterID)
		if err != nil {
			return err
		}
		if !active {
			return ErrRequesterInactive
		}
		role, err := getRequestableRole(ctx, tx, tenantID, input.RoleID)
		if err != nil {
			return err
		}
		granted, err := roleAlreadyGranted(ctx, tx, tenantID, input.RoleID, requesterID, now.UnixMilli())
		if err != nil {
			return err
		}
		if granted {
			return ErrAlreadyGranted
		}
		snapshot, err := json.Marshal(requestSnapshot{Version: 1, RequesterID: requesterID, RoleID: role.ID, RoleName: role.Name, AccessExpiresAt: input.AccessExpiresAt.UTC().Format(time.RFC3339Nano)})
		if err != nil {
			return fmt.Errorf("encode access request snapshot: %w", err)
		}
		row = accessRequestRow{
			TenantID: tenantID, ID: id, RequesterID: requesterID, RoleID: role.ID, RoleName: role.Name, Reason: input.Reason,
			SnapshotJSON: string(snapshot), Status: StatusPending, AccessExpiresAt: input.AccessExpiresAt.UTC().UnixMilli(),
			RequestExpiresAt: now.Add(requestTTL).UnixMilli(), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
		}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return fmt.Errorf("insert access request: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO iam_approval_steps
			(tenant_id, request_id, step, decision, decided_by, decided_at, note)
			VALUES (?, ?, 1, 'pending', '', 0, '')`, tenantID, id); err != nil {
			return fmt.Errorf("insert access approval step: %w", err)
		}
		event := auditx.NewEvent(ctx, tenantID, "access_request_created", "access_request", id)
		event.Detail["role_id"] = role.ID
		event.Detail["requester_id"] = requesterID
		event.Detail["access_expires_at"] = input.AccessExpiresAt.UTC().Format(time.RFC3339Nano)
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return AccessRequest{}, fmt.Errorf("create access request: %w", err)
	}
	return requestFromRow(row, now), nil
}

func (s *Service) List(ctx context.Context, tenantID, requesterID string) ([]AccessRequest, error) {
	if !validID(tenantID, 128) || requesterID != "" && !validID(requesterID, 255) {
		return nil, ErrInvalid
	}
	rows := make([]accessRequestRow, 0)
	query := s.db.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID)
	if requesterID != "" {
		query = query.Where("requester_id = ?", requesterID)
	}
	if err := query.Order("created_at DESC", "id DESC").Limit(200).Scan(ctx); err != nil {
		return nil, fmt.Errorf("list access requests: %w", err)
	}
	now := s.now().UTC()
	items := make([]AccessRequest, 0, len(rows))
	for _, row := range rows {
		items = append(items, requestFromRow(row, now))
	}
	return items, nil
}

func (s *Service) Approve(ctx context.Context, tenantID, requestID, actorID, note string) (AccessRequest, error) {
	return s.decide(ctx, tenantID, requestID, actorID, note, StatusApproved)
}

func (s *Service) Reject(ctx context.Context, tenantID, requestID, actorID, note string) (AccessRequest, error) {
	return s.decide(ctx, tenantID, requestID, actorID, note, StatusRejected)
}

func (s *Service) decide(ctx context.Context, tenantID, requestID, actorID, note, decision string) (AccessRequest, error) {
	tenantID, requestID, actorID, note = strings.TrimSpace(tenantID), strings.TrimSpace(requestID), strings.TrimSpace(actorID), strings.TrimSpace(note)
	if !validID(tenantID, 128) || !validID(requestID, 64) || !validID(actorID, 255) || len(note) > 500 || decision != StatusApproved && decision != StatusRejected {
		return AccessRequest{}, ErrInvalid
	}
	now := s.now().UTC()
	var updated accessRequestRow
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row, err := getAccessRequestRow(ctx, tx, tenantID, requestID)
		if err != nil {
			return err
		}
		if row.RequesterID == actorID {
			return ErrSelfApproval
		}
		if row.Status != StatusPending {
			return ErrStateConflict
		}
		if row.RequestExpiresAt <= now.UnixMilli() || row.AccessExpiresAt <= now.UnixMilli() {
			return ErrExpired
		}
		if decision == StatusApproved {
			if err := policyx.Lock(ctx, tx, s.dialect, tenantID); err != nil {
				return err
			}
		}
		result, err := tx.NewUpdate().Model((*accessRequestRow)(nil)).Set("status = ?", decision).Set("decided_by = ?", actorID).
			Set("decision_note = ?", note).Set("decided_at = ?", now.UnixMilli()).Set("updated_at = ?", now.UnixMilli()).
			Where("tenant_id = ? AND id = ? AND status = ? AND request_expires_at > ?", tenantID, requestID, StatusPending, now.UnixMilli()).Exec(ctx)
		if err != nil {
			return fmt.Errorf("decide access request: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrStateConflict
		}
		result, err = tx.NewUpdate().Table("iam_approval_steps").Set("decision = ?", decision).Set("decided_by = ?", actorID).
			Set("decided_at = ?", now.UnixMilli()).Set("note = ?", note).
			Where("tenant_id = ? AND request_id = ? AND step = 1 AND decision = ?", tenantID, requestID, StatusPending).Exec(ctx)
		if err != nil {
			return fmt.Errorf("update access approval step: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrStateConflict
		}
		if decision == StatusApproved {
			if err := s.grants.Grant(ctx, tx, tenantID, requestID, row.RoleID, row.RequesterID, actorID, now, time.UnixMilli(row.AccessExpiresAt)); err != nil {
				return fmt.Errorf("grant approved role: %w", err)
			}
			if err := policyx.Advance(ctx, tx, s.dialect, tenantID, now.UnixMilli()); err != nil {
				return err
			}
		}
		row.Status, row.DecidedBy, row.DecisionNote, row.DecidedAt, row.UpdatedAt = decision, actorID, note, now.UnixMilli(), now.UnixMilli()
		updated = row
		event := auditx.NewEvent(ctx, tenantID, "access_request_"+decision, "access_request", requestID)
		event.Detail["role_id"] = row.RoleID
		event.Detail["requester_id"] = row.RequesterID
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return AccessRequest{}, fmt.Errorf("%s access request: %w", decision, err)
	}
	return requestFromRow(updated, now), nil
}

func (s *Service) Withdraw(ctx context.Context, tenantID, requestID, requesterID, reason string) (AccessRequest, error) {
	return s.revoke(ctx, tenantID, requestID, requesterID, reason, true)
}

func (s *Service) Revoke(ctx context.Context, tenantID, requestID, actorID, reason string) (AccessRequest, error) {
	return s.revoke(ctx, tenantID, requestID, actorID, reason, false)
}

func (s *Service) revoke(ctx context.Context, tenantID, requestID, actorID, reason string, requesterOnly bool) (AccessRequest, error) {
	tenantID, requestID, actorID, reason = strings.TrimSpace(tenantID), strings.TrimSpace(requestID), strings.TrimSpace(actorID), strings.TrimSpace(reason)
	if !validID(tenantID, 128) || !validID(requestID, 64) || !validID(actorID, 255) || len(reason) > 500 {
		return AccessRequest{}, ErrInvalid
	}
	now := s.now().UTC()
	var updated accessRequestRow
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row, err := getAccessRequestRow(ctx, tx, tenantID, requestID)
		if err != nil {
			return err
		}
		if requesterOnly && row.RequesterID != actorID {
			return ErrRequesterOnly
		}
		nextStatus, eventType := StatusCancelled, "access_request_cancelled"
		policyChanged := false
		switch row.Status {
		case StatusPending:
			if !requesterOnly {
				return ErrStateConflict
			}
		case StatusApproved:
			if row.AccessExpiresAt <= now.UnixMilli() {
				return ErrExpired
			}
			if err := policyx.Lock(ctx, tx, s.dialect, tenantID); err != nil {
				return err
			}
			changed, err := s.grants.Revoke(ctx, tx, tenantID, requestID)
			if err != nil {
				return err
			}
			if !changed {
				return ErrStateConflict
			}
			nextStatus, eventType, policyChanged = StatusRevoked, "access_grant_revoked", true
		default:
			return ErrStateConflict
		}
		result, err := tx.NewUpdate().Model((*accessRequestRow)(nil)).Set("status = ?", nextStatus).Set("revoked_by = ?", actorID).
			Set("revoke_reason = ?", reason).Set("revoked_at = ?", now.UnixMilli()).Set("updated_at = ?", now.UnixMilli()).
			Where("tenant_id = ? AND id = ? AND status = ?", tenantID, requestID, row.Status).Exec(ctx)
		if err != nil {
			return fmt.Errorf("revoke access request: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrStateConflict
		}
		if policyChanged {
			if err := policyx.Advance(ctx, tx, s.dialect, tenantID, now.UnixMilli()); err != nil {
				return err
			}
		}
		row.Status, row.RevokedBy, row.RevokeReason, row.RevokedAt, row.UpdatedAt = nextStatus, actorID, reason, now.UnixMilli(), now.UnixMilli()
		updated = row
		event := auditx.NewEvent(ctx, tenantID, eventType, "access_request", requestID)
		event.Detail["role_id"] = row.RoleID
		event.Detail["requester_id"] = row.RequesterID
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return AccessRequest{}, fmt.Errorf("revoke access request: %w", err)
	}
	return requestFromRow(updated, now), nil
}

func getAccessRequestRow(ctx context.Context, db bun.IDB, tenantID, id string) (accessRequestRow, error) {
	var row accessRequestRow
	if err := db.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrNotFound
		}
		return row, fmt.Errorf("get access request: %w", err)
	}
	return row, nil
}

func getRequestableRole(ctx context.Context, db bun.IDB, tenantID, roleID string) (RequestableRole, error) {
	var role RequestableRole
	if err := db.NewSelect().Table("iam_roles").Column("id", "name", "description").Where("tenant_id = ? AND id = ?", tenantID, roleID).Scan(ctx, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return role, ErrRoleNotFound
		}
		return role, fmt.Errorf("get requestable role: %w", err)
	}
	return role, nil
}

func activeTenantMember(ctx context.Context, db bun.IDB, tenantID, principalID string) (bool, error) {
	var count int
	if err := db.NewSelect().TableExpr("iam_tenant_members AS members").ColumnExpr("COUNT(*)").
		Join("JOIN iam_tenants AS tenants ON tenants.id = members.tenant_id AND tenants.status = 'active'").
		Where("members.tenant_id = ? AND members.user_subject = ? AND members.status = 'active'", tenantID, principalID).Scan(ctx, &count); err != nil {
		return false, fmt.Errorf("check access requester membership: %w", err)
	}
	return count == 1, nil
}

func roleAlreadyGranted(ctx context.Context, db bun.IDB, tenantID, roleID, principalID string, now int64) (bool, error) {
	var count int
	if err := db.NewRaw(`SELECT COUNT(*) FROM (
		SELECT role_id FROM iam_role_members WHERE tenant_id = ? AND role_id = ? AND user_subject = ?
		UNION ALL
		SELECT role_id FROM iam_temporary_role_grants WHERE tenant_id = ? AND role_id = ? AND principal_id = ? AND starts_at <= ? AND ends_at > ?
	) AS grants`, tenantID, roleID, principalID, tenantID, roleID, principalID, now, now).Scan(ctx, &count); err != nil {
		return false, fmt.Errorf("check existing role grant: %w", err)
	}
	return count > 0, nil
}

func requestFromRow(row accessRequestRow, now time.Time) AccessRequest {
	status := row.Status
	if status == StatusPending && row.RequestExpiresAt <= now.UnixMilli() || status == StatusApproved && row.AccessExpiresAt <= now.UnixMilli() {
		status = StatusExpired
	}
	item := AccessRequest{
		ID: row.ID, TenantID: row.TenantID, RequesterID: row.RequesterID, RoleID: row.RoleID, RoleName: row.RoleName,
		Reason: row.Reason, Status: status, AccessExpiresAt: time.UnixMilli(row.AccessExpiresAt).UTC(),
		RequestExpiresAt: time.UnixMilli(row.RequestExpiresAt).UTC(), DecidedBy: row.DecidedBy, DecisionNote: row.DecisionNote,
		RevokedBy: row.RevokedBy, RevokeReason: row.RevokeReason, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC(),
	}
	if row.DecidedAt > 0 {
		value := time.UnixMilli(row.DecidedAt).UTC()
		item.DecidedAt = &value
	}
	if row.RevokedAt > 0 {
		value := time.UnixMilli(row.RevokedAt).UTC()
		item.RevokedAt = &value
	}
	return item
}

func validID(value string, limit int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= limit
}
