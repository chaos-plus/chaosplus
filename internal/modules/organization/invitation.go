package organization

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/uptrace/bun"
)

const (
	InvitationPending    = "pending"
	InvitationAccepted   = "accepted"
	InvitationRevoked    = "revoked"
	InvitationExpired    = "expired"
	defaultInvitationTTL = 72 * time.Hour
	maxInvitationTTL     = 30 * 24 * time.Hour
)

var (
	ErrInvitationNotFound        = errors.New("invitation not found")
	ErrInvitationInvalid         = errors.New("invalid invitation")
	ErrInvitationCredential      = errors.New("invalid invitation credential")
	ErrInvitationExpired         = errors.New("invitation expired")
	ErrInvitationState           = errors.New("invalid invitation state")
	ErrInvitationLoginConflict   = errors.New("invitation login name conflict")
	ErrInvitationBindingMissing  = errors.New("invitation binding not found")
	ErrInvitationBindingInactive = errors.New("invitation binding inactive")
)

type Invitation struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Email        string    `json:"email"`
	Status       string    `json:"status"`
	DepartmentID string    `json:"department_id,omitempty"`
	RoleIDs      []string  `json:"role_ids"`
	ExpiresAt    time.Time `json:"expires_at"`
	AcceptedBy   string    `json:"accepted_by,omitempty"`
	AcceptedAt   time.Time `json:"accepted_at,omitempty"`
	RevokedAt    time.Time `json:"revoked_at,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type InvitationAcceptance struct {
	InvitationID    string `json:"invitation_id"`
	TenantID        string `json:"tenant_id"`
	PrincipalID     string `json:"principal_id"`
	Email           string `json:"email"`
	AlreadyAccepted bool   `json:"already_accepted"`
}

type CreateInvitation struct {
	Email        string
	DepartmentID string
	RoleIDs      []string
	TTL          time.Duration
}

type AcceptInvitation struct {
	Token       string
	LoginName   string
	Password    string
	DisplayName string
}

type InvitationCredentials interface {
	IssueInvitationToken(string) (string, string, error)
	InvitationTokenDigest(string) (string, error)
}

type InvitationPrincipalCreator func(context.Context, bun.IDB, string, string, string, string, string) (string, error)

type invitationRow struct {
	bun.BaseModel `bun:"table:iam_invitations"`
	TenantID      string `bun:"tenant_id,pk"`
	ID            string `bun:"id,pk"`
	Email         string
	EmailKey      string
	TokenHMAC     string
	Status        string
	DepartmentID  string
	ExpiresAt     int64
	AcceptedBy    string
	AcceptedAt    int64
	RevokedAt     int64
	CreatedAt     int64
	UpdatedAt     int64
}

type invitationRoleRow struct {
	bun.BaseModel `bun:"table:iam_invitation_roles"`
	TenantID      string `bun:"tenant_id,pk"`
	InvitationID  string `bun:"invitation_id,pk"`
	RoleID        string `bun:"role_id,pk"`
}

type InvitationService struct {
	db              *bun.DB
	dialect         string
	audit           auditx.Appender
	nextID          IDGenerator
	credentials     InvitationCredentials
	createPrincipal InvitationPrincipalCreator
	now             func() time.Time
}

func NewInvitationService(db *bun.DB, audit auditx.Appender, nextID IDGenerator, credentials InvitationCredentials, createPrincipal InvitationPrincipalCreator) *InvitationService {
	if db == nil || audit == nil || nextID == nil || credentials == nil || createPrincipal == nil {
		panic("invitation service requires database, audit appender, id generator, credentials, and principal creator")
	}
	dialect := db.Dialect().Name().String()
	if dialect == "pg" {
		dialect = "postgres"
	}
	return &InvitationService{db: db, dialect: dialect, audit: audit, nextID: nextID, credentials: credentials, createPrincipal: createPrincipal, now: time.Now}
}

func (s *InvitationService) List(ctx context.Context, tenantID string) ([]Invitation, error) {
	tenantID = strings.TrimSpace(tenantID)
	if !validTenant(tenantID) {
		return nil, ErrInvitationInvalid
	}
	rows := make([]invitationRow, 0)
	if err := s.db.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("created_at DESC", "id DESC").Limit(200).Scan(ctx); err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	return s.invitations(ctx, rows)
}

func (s *InvitationService) Create(ctx context.Context, tenantID string, input CreateInvitation) (Invitation, string, error) {
	tenantID, input, err := normalizeInvitation(tenantID, input)
	if err != nil {
		return Invitation{}, "", err
	}
	id, err := s.nextID()
	if err != nil || !validInvitationID(id) {
		return Invitation{}, "", fmt.Errorf("generate invitation id: %w", errors.Join(err, ErrInvitationInvalid))
	}
	token, digest, err := s.credentials.IssueInvitationToken(id)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("issue invitation credential: %w", err)
	}
	now := s.now().UTC()
	row := invitationRow{TenantID: tenantID, ID: id, Email: input.Email, EmailKey: input.Email, TokenHMAC: digest, Status: InvitationPending, DepartmentID: input.DepartmentID, ExpiresAt: now.Add(input.TTL).UnixMilli(), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}
	event := auditx.NewEvent(ctx, tenantID, "invitation_created", "invitation", id)
	event.Detail["department_assigned"] = input.DepartmentID != ""
	event.Detail["role_count"] = len(input.RoleIDs)
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := policyx.Lock(ctx, tx, s.dialect, tenantID); err != nil {
			return err
		}
		if err := s.validateTenantAndBindings(ctx, tx, tenantID, input.DepartmentID, input.RoleIDs); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*invitationRow)(nil)).Set("status = ?", InvitationRevoked).Set("revoked_at = ?", now.UnixMilli()).Set("updated_at = ?", now.UnixMilli()).Where("tenant_id = ? AND email_key = ? AND status = ?", tenantID, input.Email, InvitationPending).Exec(ctx); err != nil {
			return fmt.Errorf("revoke previous invitation: %w", err)
		}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return fmt.Errorf("insert invitation: %w", err)
		}
		if err := insertInvitationRoles(ctx, tx, tenantID, id, input.RoleIDs); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Invitation{}, "", fmt.Errorf("create invitation: %w", err)
	}
	return invitationFromRow(row, input.RoleIDs, now), token, nil
}

func (s *InvitationService) Revoke(ctx context.Context, tenantID, id string) error {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if !validTenant(tenantID) || !validInvitationID(id) {
		return ErrInvitationInvalid
	}
	now := s.now().UTC()
	event := auditx.NewEvent(ctx, tenantID, "invitation_revoked", "invitation", id)
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := policyx.Lock(ctx, tx, s.dialect, tenantID); err != nil {
			return err
		}
		row, err := getInvitation(ctx, tx, s.dialect, tenantID, id)
		if err != nil {
			return err
		}
		if row.Status != InvitationPending {
			return ErrInvitationState
		}
		if row.ExpiresAt <= now.UnixMilli() {
			return ErrInvitationExpired
		}
		result, err := tx.NewUpdate().Model((*invitationRow)(nil)).Set("status = ?", InvitationRevoked).Set("revoked_at = ?", now.UnixMilli()).Set("updated_at = ?", now.UnixMilli()).Where("tenant_id = ? AND id = ? AND status = ?", tenantID, id, InvitationPending).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrInvitationState
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}
	return nil
}

func (s *InvitationService) Resend(ctx context.Context, tenantID, id string, ttl time.Duration) (Invitation, string, error) {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if ttl == 0 {
		ttl = defaultInvitationTTL
	}
	if !validTenant(tenantID) || !validInvitationID(id) || ttl < time.Hour || ttl > maxInvitationTTL {
		return Invitation{}, "", ErrInvitationInvalid
	}
	token, digest, err := s.credentials.IssueInvitationToken(id)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("issue invitation credential: %w", err)
	}
	now := s.now().UTC()
	var updated invitationRow
	event := auditx.NewEvent(ctx, tenantID, "invitation_resent", "invitation", id)
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := policyx.Lock(ctx, tx, s.dialect, tenantID); err != nil {
			return err
		}
		current, err := getInvitation(ctx, tx, s.dialect, tenantID, id)
		if err != nil {
			return err
		}
		if current.Status != InvitationPending {
			return ErrInvitationState
		}
		result, err := tx.NewUpdate().Model((*invitationRow)(nil)).Set("token_hmac = ?", digest).Set("expires_at = ?", now.Add(ttl).UnixMilli()).Set("updated_at = ?", now.UnixMilli()).Where("tenant_id = ? AND id = ? AND status = ?", tenantID, id, InvitationPending).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrInvitationState
		}
		updated = current
		updated.TokenHMAC = digest
		updated.ExpiresAt = now.Add(ttl).UnixMilli()
		updated.UpdatedAt = now.UnixMilli()
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Invitation{}, "", fmt.Errorf("resend invitation: %w", err)
	}
	roles, err := invitationRoleIDs(ctx, s.db, tenantID, id)
	if err != nil {
		return Invitation{}, "", err
	}
	return invitationFromRow(updated, roles, now), token, nil
}

func (s *InvitationService) Accept(ctx context.Context, input AcceptInvitation) (InvitationAcceptance, error) {
	input.Token, input.LoginName, input.DisplayName = strings.TrimSpace(input.Token), strings.TrimSpace(input.LoginName), strings.TrimSpace(input.DisplayName)
	id := invitationIDFromToken(input.Token)
	if !validInvitationID(id) || input.LoginName == "" || len(input.LoginName) > 200 || len(input.Password) < 12 || len(input.Password) > 1024 || len(input.DisplayName) > 128 {
		return InvitationAcceptance{}, ErrInvitationCredential
	}
	digest, err := s.credentials.InvitationTokenDigest(input.Token)
	if err != nil {
		return InvitationAcceptance{}, ErrInvitationCredential
	}
	now := s.now().UTC()
	var accepted InvitationAcceptance
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row, err := getInvitationByID(ctx, tx, s.dialect, id)
		if err != nil {
			return ErrInvitationCredential
		}
		if subtle.ConstantTimeCompare([]byte(row.TokenHMAC), []byte(digest)) != 1 {
			return ErrInvitationCredential
		}
		if err := policyx.Lock(ctx, tx, s.dialect, row.TenantID); err != nil {
			return err
		}
		if row.Status == InvitationAccepted {
			accepted = InvitationAcceptance{InvitationID: row.ID, TenantID: row.TenantID, PrincipalID: row.AcceptedBy, Email: row.Email, AlreadyAccepted: true}
			return nil
		}
		if row.Status != InvitationPending {
			return ErrInvitationCredential
		}
		if row.ExpiresAt <= now.UnixMilli() {
			return ErrInvitationExpired
		}
		roles, err := invitationRoleIDs(ctx, tx, row.TenantID, row.ID)
		if err != nil {
			return err
		}
		if err := s.validateTenantAndBindings(ctx, tx, row.TenantID, row.DepartmentID, roles); err != nil {
			return err
		}
		principalID, err := s.createPrincipal(ctx, tx, row.TenantID, input.LoginName, input.Password, input.DisplayName, row.Email)
		if err != nil {
			return err
		}
		if row.DepartmentID != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO iam_member_departments (tenant_id, principal_id, department_id, updated_at) VALUES (?, ?, ?, ?)`, row.TenantID, principalID, row.DepartmentID, now.UnixMilli()); err != nil {
				return fmt.Errorf("assign invitation department: %w", err)
			}
		}
		for _, roleID := range roles {
			if _, err := tx.ExecContext(ctx, `INSERT INTO iam_role_members (tenant_id, role_id, user_subject, created_at) VALUES (?, ?, ?, ?)`, row.TenantID, roleID, principalID, now.UnixMilli()); err != nil {
				return fmt.Errorf("assign invitation role: %w", err)
			}
		}
		result, err := tx.NewUpdate().Model((*invitationRow)(nil)).Set("status = ?", InvitationAccepted).Set("accepted_by = ?", principalID).Set("accepted_at = ?", now.UnixMilli()).Set("updated_at = ?", now.UnixMilli()).Where("tenant_id = ? AND id = ? AND status = ? AND token_hmac = ? AND expires_at > ?", row.TenantID, row.ID, InvitationPending, digest, now.UnixMilli()).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrInvitationCredential
		}
		if err := policyx.Advance(ctx, tx, s.dialect, row.TenantID, now.UnixMilli()); err != nil {
			return err
		}
		event := auditx.NewEvent(ctx, row.TenantID, "invitation_accepted", "invitation", row.ID)
		event.PrincipalID = principalID
		event.Detail["department_assigned"] = row.DepartmentID != ""
		event.Detail["role_count"] = len(roles)
		if err := s.audit(ctx, tx, event); err != nil {
			return err
		}
		accepted = InvitationAcceptance{InvitationID: row.ID, TenantID: row.TenantID, PrincipalID: principalID, Email: row.Email}
		return nil
	})
	if err != nil {
		return InvitationAcceptance{}, fmt.Errorf("accept invitation: %w", err)
	}
	return accepted, nil
}

func normalizeInvitation(tenantID string, input CreateInvitation) (string, CreateInvitation, error) {
	tenantID = strings.TrimSpace(tenantID)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.DepartmentID = strings.TrimSpace(input.DepartmentID)
	if input.TTL == 0 {
		input.TTL = defaultInvitationTTL
	}
	parsed, err := mail.ParseAddress(input.Email)
	if !validTenant(tenantID) || err != nil || !strings.EqualFold(parsed.Address, input.Email) || len(input.Email) > 320 ||
		!validOptionalID(input.DepartmentID) || input.TTL < time.Hour || input.TTL > maxInvitationTTL {
		return "", CreateInvitation{}, ErrInvitationInvalid
	}
	seen := make(map[string]struct{}, len(input.RoleIDs))
	roles := make([]string, 0, len(input.RoleIDs))
	for _, roleID := range input.RoleIDs {
		roleID = strings.TrimSpace(roleID)
		if !validInvitationID(roleID) {
			return "", CreateInvitation{}, ErrInvitationInvalid
		}
		if _, ok := seen[roleID]; ok {
			continue
		}
		seen[roleID] = struct{}{}
		roles = append(roles, roleID)
	}
	sort.Strings(roles)
	input.RoleIDs = roles
	return tenantID, input, nil
}

func (s *InvitationService) validateTenantAndBindings(ctx context.Context, db bun.IDB, tenantID, departmentID string, roleIDs []string) error {
	tenant, err := getTenantRow(ctx, db, tenantID)
	if err != nil {
		return err
	}
	if tenant.Status != TenantActive {
		return ErrInvitationBindingInactive
	}
	if departmentID != "" {
		var status string
		if err := db.NewSelect().Table("iam_departments").Column("status").Where("tenant_id = ? AND id = ?", tenantID, departmentID).Scan(ctx, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvitationBindingMissing
			}
			return fmt.Errorf("get invitation department: %w", err)
		}
		if status != StatusActive {
			return ErrInvitationBindingInactive
		}
	}
	if len(roleIDs) > 0 {
		count, err := db.NewSelect().Table("iam_roles").Where("tenant_id = ? AND id IN (?)", tenantID, bun.List(roleIDs)).Count(ctx)
		if err != nil {
			return fmt.Errorf("count invitation roles: %w", err)
		}
		if count != len(roleIDs) {
			return ErrInvitationBindingMissing
		}
	}
	return nil
}

func getInvitation(ctx context.Context, db bun.IDB, dialect, tenantID, id string) (invitationRow, error) {
	query := db.NewSelect().Model((*invitationRow)(nil)).Where("tenant_id = ? AND id = ?", tenantID, id)
	return scanInvitation(ctx, query, dialect)
}

func getInvitationByID(ctx context.Context, db bun.IDB, dialect, id string) (invitationRow, error) {
	query := db.NewSelect().Model((*invitationRow)(nil)).Where("id = ?", id)
	return scanInvitation(ctx, query, dialect)
}

func scanInvitation(ctx context.Context, query *bun.SelectQuery, dialect string) (invitationRow, error) {
	var row invitationRow
	if dialect != "sqlite" {
		query = query.For("UPDATE")
	}
	if err := query.Scan(ctx, &row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return invitationRow{}, ErrInvitationNotFound
		}
		return invitationRow{}, fmt.Errorf("get invitation: %w", err)
	}
	return row, nil
}

func insertInvitationRoles(ctx context.Context, db bun.IDB, tenantID, invitationID string, roleIDs []string) error {
	for _, roleID := range roleIDs {
		row := invitationRoleRow{TenantID: tenantID, InvitationID: invitationID, RoleID: roleID}
		if _, err := db.NewInsert().Model(&row).Exec(ctx); err != nil {
			return fmt.Errorf("insert invitation role: %w", err)
		}
	}
	return nil
}

func invitationRoleIDs(ctx context.Context, db bun.IDB, tenantID, invitationID string) ([]string, error) {
	roleIDs := make([]string, 0)
	if err := db.NewSelect().Model((*invitationRoleRow)(nil)).Column("role_id").Where("tenant_id = ? AND invitation_id = ?", tenantID, invitationID).Order("role_id ASC").Scan(ctx, &roleIDs); err != nil {
		return nil, fmt.Errorf("list invitation roles: %w", err)
	}
	return roleIDs, nil
}

func (s *InvitationService) invitations(ctx context.Context, rows []invitationRow) ([]Invitation, error) {
	items := make([]Invitation, 0, len(rows))
	now := s.now().UTC()
	for _, row := range rows {
		roles, err := invitationRoleIDs(ctx, s.db, row.TenantID, row.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, invitationFromRow(row, roles, now))
	}
	return items, nil
}

func invitationFromRow(row invitationRow, roleIDs []string, now time.Time) Invitation {
	status := row.Status
	if status == InvitationPending && row.ExpiresAt <= now.UnixMilli() {
		status = InvitationExpired
	}
	item := Invitation{ID: row.ID, TenantID: row.TenantID, Email: row.Email, Status: status, DepartmentID: row.DepartmentID, RoleIDs: append([]string{}, roleIDs...), ExpiresAt: unixTime(row.ExpiresAt), AcceptedBy: row.AcceptedBy, CreatedAt: unixTime(row.CreatedAt), UpdatedAt: unixTime(row.UpdatedAt)}
	if row.AcceptedAt > 0 {
		item.AcceptedAt = unixTime(row.AcceptedAt)
	}
	if row.RevokedAt > 0 {
		item.RevokedAt = unixTime(row.RevokedAt)
	}
	return item
}

func invitationIDFromToken(token string) string {
	const prefix = "cpi1_"
	if !strings.HasPrefix(token, prefix) {
		return ""
	}
	id, _, ok := strings.Cut(strings.TrimPrefix(token, prefix), ".")
	if !ok {
		return ""
	}
	return id
}

func validInvitationID(value string) bool { return value != "" && len(value) <= 32 }
