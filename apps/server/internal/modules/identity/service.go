package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/uptrace/bun"
)

var (
	ErrInvalid           = errors.New("invalid principal")
	ErrNotFound          = errors.New("principal not found")
	ErrLoginConflict     = errors.New("login name already exists")
	ErrPrincipalInactive = errors.New("principal or tenant membership is not active")
)

// AdministratorGuard is a consumer-side interface satisfied by
// iam.AdministratorGuard. Defined here rather than imported to keep the
// dependency direction identity → iam unidirectional (interface at call site).
type AdministratorGuard interface {
	Protect(context.Context, bun.IDB, string, string) (func() error, error)
}

type Principal struct {
	ID                 string    `json:"id"`
	LoginName          string    `json:"login_name"`
	Email              string    `json:"email,omitempty"`
	EmailVerified      bool      `json:"email_verified"`
	ActivationRequired bool      `json:"activation_required"`
	DisplayName        string    `json:"display_name"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type principalRow struct {
	bun.BaseModel      `bun:"table:iam_principals"`
	ID                 string `bun:"id,pk"`
	LoginName          string
	Email              string
	EmailVerified      bool
	ActivationRequired bool
	DisplayName        string
	Status             string
	CreatedAt          int64
	UpdatedAt          int64
	DisabledAt         int64
}

type credentialRow struct {
	bun.BaseModel     `bun:"table:iam_credentials"`
	PrincipalID       string `bun:"principal_id,pk"`
	PasswordHash      string
	TOTPSecret        string
	MFARequired       bool
	FailedAttempts    int
	LockedUntil       int64
	PasswordChangedAt int64
	CredentialVersion int64
	UpdatedAt         int64
}

type sessionRow struct {
	bun.BaseModel `bun:"table:iam_sessions"`
	IDHash        string `bun:"id_hash,pk"`
	PrincipalID   string
	RevokedAt     int64
}

type refreshTokenRow struct {
	bun.BaseModel `bun:"table:iam_refresh_tokens"`
	IDHash        string `bun:"id_hash,pk"`
	PrincipalID   string
	RevokedAt     int64
}

type tenantMemberRow struct {
	bun.BaseModel `bun:"table:iam_tenant_members"`
	TenantID      string `bun:"tenant_id,pk"`
	UserSubject   string `bun:"user_subject,pk"`
	DisplayName   string
	Email         string
	Status        string
	CreatedAt     int64
	UpdatedAt     int64
	DisabledAt    int64
}

type Service struct {
	db             *bun.DB
	dialect        string
	audit          auditx.Appender
	administrators AdministratorGuard
	now            func() time.Time
}

func NewService(db *bun.DB, audit auditx.Appender, administrators AdministratorGuard) *Service {
	if db == nil || audit == nil || administrators == nil {
		panic("identity service requires database, audit appender, and administrator guard")
	}
	dialect := db.Dialect().Name().String()
	if dialect == "pg" {
		dialect = "postgres"
	}
	return &Service{db: db, dialect: dialect, audit: audit, administrators: administrators, now: time.Now}
}

func (s *Service) Create(ctx context.Context, tenantID, loginName, password, displayName, email string) (Principal, error) {
	tenantID = strings.TrimSpace(tenantID)
	loginName, email, displayName = normalizeLogin(loginName), strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(displayName)
	if tenantID == "" || len(tenantID) > 128 || loginName == "" || len(loginName) > 200 || len(password) < 12 || len(password) > 1024 || len(displayName) > 128 || len(email) > 320 {
		return Principal{}, ErrInvalid
	}
	if displayName == "" {
		displayName = loginName
	}
	hash, err := passwordx.Hash(password)
	if err != nil {
		return Principal{}, err
	}
	id, err := randomID()
	if err != nil {
		return Principal{}, err
	}
	now := s.now().UTC().UnixMilli()
	row := principalRow{ID: id, LoginName: loginName, Email: email, DisplayName: displayName, Status: "active", CreatedAt: now, UpdatedAt: now}
	event := auditx.NewEvent(ctx, tenantID, "principal_created", "principal", id)
	event.Detail["membership_created"] = true
	event.Detail["status"] = "active"
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			if isUnique(err) {
				return ErrLoginConflict
			}
			return err
		}
		credential := credentialRow{PrincipalID: id, PasswordHash: hash, PasswordChangedAt: now, CredentialVersion: 1, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(&credential).Exec(ctx); err != nil {
			return err
		}
		member := tenantMemberRow{TenantID: tenantID, UserSubject: id, DisplayName: displayName, Email: email, Status: "active", CreatedAt: now, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(&member).Exec(ctx); err != nil {
			return err
		}
		if err := policyx.Advance(ctx, tx, s.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Principal{}, fmt.Errorf("create principal: %w", err)
	}
	return fromRow(row), nil
}

// CreateInvitedPrincipal writes a verified local principal and tenant
// membership through the caller's transaction. The invitation owner appends
// audit and advances policy revision after all default bindings are applied.
func (s *Service) CreateInvitedPrincipal(ctx context.Context, db bun.IDB, tenantID, loginName, password, displayName, email string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	loginName, email, displayName = normalizeLogin(loginName), strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(displayName)
	parsedEmail, emailErr := mail.ParseAddress(email)
	if db == nil || tenantID == "" || len(tenantID) > 128 || loginName == "" || len(loginName) > 200 ||
		len(password) < 12 || len(password) > 1024 || len(displayName) > 128 || len(email) > 320 ||
		emailErr != nil || !strings.EqualFold(parsedEmail.Address, email) {
		return "", ErrInvalid
	}
	if displayName == "" {
		displayName = loginName
	}
	hash, err := passwordx.Hash(password)
	if err != nil {
		return "", err
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	now := s.now().UTC().UnixMilli()
	principal := principalRow{ID: id, LoginName: loginName, Email: email, EmailVerified: true, DisplayName: displayName, Status: "active", CreatedAt: now, UpdatedAt: now}
	if _, err := db.NewInsert().Model(&principal).Exec(ctx); err != nil {
		if isUnique(err) {
			return "", ErrLoginConflict
		}
		return "", fmt.Errorf("insert invited principal: %w", err)
	}
	credential := credentialRow{PrincipalID: id, PasswordHash: hash, PasswordChangedAt: now, CredentialVersion: 1, UpdatedAt: now}
	if _, err := db.NewInsert().Model(&credential).Exec(ctx); err != nil {
		return "", fmt.Errorf("insert invited credential: %w", err)
	}
	member := tenantMemberRow{TenantID: tenantID, UserSubject: id, DisplayName: displayName, Email: email, Status: "active", CreatedAt: now, UpdatedAt: now}
	if _, err := db.NewInsert().Model(&member).Exec(ctx); err != nil {
		return "", fmt.Errorf("insert invited membership: %w", err)
	}
	return id, nil
}

// CreatePendingPrincipal writes the identity and pre-hashed password inside a
// caller-owned registration transaction. It never grants tenant membership.
func CreatePendingPrincipal(ctx context.Context, db bun.IDB, email, passwordHash, displayName string, now time.Time) (string, error) {
	email, displayName = strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(displayName)
	parsed, err := mail.ParseAddress(email)
	if db == nil || len(email) > 320 || passwordHash == "" || len(displayName) > 128 || err != nil || !strings.EqualFold(parsed.Address, email) {
		return "", ErrInvalid
	}
	if displayName == "" {
		displayName = email
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	timestamp := now.UTC().UnixMilli()
	principal := principalRow{ID: id, LoginName: email, Email: email, ActivationRequired: true, DisplayName: displayName, Status: "active", CreatedAt: timestamp, UpdatedAt: timestamp}
	if _, err := db.NewInsert().Model(&principal).Exec(ctx); err != nil {
		if isUnique(err) {
			return "", ErrLoginConflict
		}
		return "", fmt.Errorf("insert pending principal: %w", err)
	}
	credential := credentialRow{PrincipalID: id, PasswordHash: passwordHash, PasswordChangedAt: timestamp, CredentialVersion: 1, UpdatedAt: timestamp}
	if _, err := db.NewInsert().Model(&credential).Exec(ctx); err != nil {
		return "", fmt.Errorf("insert pending credential: %w", err)
	}
	return id, nil
}

// EnsureExternalPrincipal returns the global principal for an externally
// verified identity, creating it (plus tenant membership) when missing. The
// write runs inside a caller-owned transaction; no audit event is emitted so
// the caller can describe the triggering flow. External principals never get
// local credentials and always start with a verified email.
func (s *Service) EnsureExternalPrincipal(ctx context.Context, db bun.IDB, tenantID, email, displayName string, now time.Time) (string, bool, error) {
	tenantID = strings.TrimSpace(tenantID)
	email = strings.ToLower(strings.TrimSpace(email))
	displayName = strings.TrimSpace(displayName)
	parsed, err := mail.ParseAddress(email)
	if db == nil || tenantID == "" || len(tenantID) > 128 || err != nil || !strings.EqualFold(parsed.Address, email) || len(email) > 320 || len(displayName) > 128 {
		return "", false, ErrInvalid
	}
	if displayName == "" {
		displayName = email
	}
	timestamp := now.UTC().UnixMilli()
	row, err := principalByLoginOrEmail(ctx, db, email)
	if err != nil {
		return "", false, err
	}
	if row != nil {
		if row.Status != "active" {
			return "", false, ErrPrincipalInactive
		}
		if err := ensureTenantMember(ctx, db, s.dialect, tenantID, *row, timestamp); err != nil {
			return "", false, err
		}
		return row.ID, false, nil
	}
	id, err := randomID()
	if err != nil {
		return "", false, err
	}
	principal := principalRow{ID: id, LoginName: email, Email: email, EmailVerified: true, DisplayName: displayName, Status: "active", CreatedAt: timestamp, UpdatedAt: timestamp}
	if _, err := db.NewInsert().Model(&principal).Exec(ctx); err != nil {
		if isUnique(err) {
			// A concurrent login created the principal first; reuse it.
			row, err := principalByLoginOrEmail(ctx, db, email)
			if err != nil {
				return "", false, err
			}
			if row == nil || row.Status != "active" {
				return "", false, ErrPrincipalInactive
			}
			if err := ensureTenantMember(ctx, db, s.dialect, tenantID, *row, timestamp); err != nil {
				return "", false, err
			}
			return row.ID, false, nil
		}
		return "", false, fmt.Errorf("insert external principal: %w", err)
	}
	member := tenantMemberRow{TenantID: tenantID, UserSubject: id, DisplayName: displayName, Email: email, Status: "active", CreatedAt: timestamp, UpdatedAt: timestamp}
	if _, err := db.NewInsert().Model(&member).Exec(ctx); err != nil {
		return "", false, fmt.Errorf("insert external membership: %w", err)
	}
	if err := policyx.Advance(ctx, db, s.dialect, tenantID, timestamp); err != nil {
		return "", false, err
	}
	return id, true, nil
}

func principalByLoginOrEmail(ctx context.Context, db bun.IDB, email string) (*principalRow, error) {
	var row principalRow
	err := db.NewSelect().Model(&row).Where("login_name = ?", email).Scan(ctx)
	if err == nil {
		return &row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find external principal: %w", err)
	}
	err = db.NewSelect().Model(&row).Where("email = ?", email).Order("created_at ASC", "id ASC").Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find external principal: %w", err)
	}
	return &row, nil
}

func ensureTenantMember(ctx context.Context, db bun.IDB, dialect, tenantID string, principal principalRow, timestamp int64) error {
	exists, err := db.NewSelect().Table("iam_tenant_members").Where("tenant_id = ? AND user_subject = ?", tenantID, principal.ID).Exists(ctx)
	if err != nil {
		return fmt.Errorf("check external membership: %w", err)
	}
	if exists {
		var status string
		if err := db.NewSelect().Table("iam_tenant_members").Column("status").Where("tenant_id = ? AND user_subject = ?", tenantID, principal.ID).Scan(ctx, &status); err != nil {
			return fmt.Errorf("read external membership: %w", err)
		}
		if status != "active" {
			return ErrPrincipalInactive
		}
		return nil
	}
	member := tenantMemberRow{TenantID: tenantID, UserSubject: principal.ID, DisplayName: principal.DisplayName, Email: principal.Email, Status: "active", CreatedAt: timestamp, UpdatedAt: timestamp}
	if _, err := db.NewInsert().Model(&member).Exec(ctx); err != nil {
		return fmt.Errorf("insert external membership: %w", err)
	}
	if err := policyx.Advance(ctx, db, dialect, tenantID, timestamp); err != nil {
		return err
	}
	return nil
}

func (s *Service) List(ctx context.Context, tenantID, search string, limit, offset int) ([]Principal, int64, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || len(tenantID) > 128 || limit < 1 || limit > 200 || offset < 0 {
		return nil, 0, ErrInvalid
	}
	query := s.db.NewSelect().Model((*principalRow)(nil)).
		Join("JOIN iam_tenant_members AS tm ON tm.user_subject = principal_row.id").
		Where("tm.tenant_id = ?", tenantID).
		Where("NOT EXISTS (SELECT 1 FROM iam_service_accounts AS service_account WHERE service_account.principal_id = principal_row.id)")
	search = strings.ToLower(strings.TrimSpace(search))
	if search != "" {
		query = query.Where("LOWER(principal_row.login_name) LIKE ? OR LOWER(principal_row.display_name) LIKE ? OR LOWER(principal_row.email) LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	count, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	var rows []principalRow
	if err := query.Order("principal_row.login_name ASC", "principal_row.id ASC").Limit(limit).Offset(offset).Scan(ctx, &rows); err != nil {
		return nil, 0, err
	}
	result := make([]Principal, 0, len(rows))
	for _, row := range rows {
		result = append(result, fromRow(row))
	}
	return result, int64(count), nil
}

func (s *Service) Get(ctx context.Context, tenantID, id string) (Principal, error) {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if tenantID == "" || len(tenantID) > 128 || id == "" {
		return Principal{}, ErrInvalid
	}
	return getPrincipal(ctx, s.db, tenantID, id)
}

func getPrincipal(ctx context.Context, db bun.IDB, tenantID, id string) (Principal, error) {
	var row principalRow
	if err := db.NewSelect().Model(&row).
		Join("JOIN iam_tenant_members AS tm ON tm.user_subject = principal_row.id").
		Where("tm.tenant_id = ? AND principal_row.id = ?", tenantID, id).
		Where("NOT EXISTS (SELECT 1 FROM iam_service_accounts AS service_account WHERE service_account.principal_id = principal_row.id)").
		Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Principal{}, ErrNotFound
		}
		return Principal{}, err
	}
	return fromRow(row), nil
}

func (s *Service) Update(ctx context.Context, tenantID, id string, displayName, email *string) (Principal, error) {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if tenantID == "" || len(tenantID) > 128 || id == "" {
		return Principal{}, ErrInvalid
	}
	if displayName == nil && email == nil {
		return Principal{}, ErrInvalid
	}
	var requestedDisplay, requestedEmail *string
	if displayName != nil {
		value := strings.TrimSpace(*displayName)
		if value == "" || len(value) > 128 {
			return Principal{}, ErrInvalid
		}
		requestedDisplay = &value
	}
	if email != nil {
		value := strings.ToLower(strings.TrimSpace(*email))
		if len(value) > 320 {
			return Principal{}, ErrInvalid
		}
		requestedEmail = &value
	}
	now := s.now().UTC().UnixMilli()
	event := auditx.NewEvent(ctx, tenantID, "principal_updated", "principal", id)
	var updated Principal
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		current, err := getPrincipal(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		queryDisplay, queryEmail := current.DisplayName, current.Email
		if requestedDisplay != nil {
			queryDisplay = *requestedDisplay
		}
		if requestedEmail != nil {
			queryEmail = *requestedEmail
		}
		displayChanged := queryDisplay != current.DisplayName
		emailChanged := queryEmail != current.Email
		principalUpdate := tx.NewUpdate().Model((*principalRow)(nil)).Set("display_name = ?", queryDisplay).Set("email = ?", queryEmail).
			Set("updated_at = ?", now).Where("id = ?", id)
		if emailChanged {
			principalUpdate = principalUpdate.Set("email_verified = ?", false)
		}
		result, err := principalUpdate.Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrNotFound
		}
		if emailChanged {
			result, err := tx.NewUpdate().Model((*credentialRow)(nil)).Set("credential_version = credential_version + 1").Set("updated_at = ?", now).Where("principal_id = ?", id).Exec(ctx)
			if err != nil {
				return err
			}
			if affected, _ := result.RowsAffected(); affected != 1 {
				return errors.New("principal credential not found")
			}
			if _, err := tx.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", id).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewUpdate().Model((*refreshTokenRow)(nil)).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", id).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewUpdate().Table("iam_password_recovery_tokens").Set("consumed_at = ?", now).Where("principal_id = ? AND consumed_at = 0", id).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewUpdate().Table("iam_email_verification_tokens").Set("consumed_at = ?", now).Where("principal_id = ? AND consumed_at = 0", id).Exec(ctx); err != nil {
				return err
			}
		}
		result, err = tx.NewUpdate().Model((*tenantMemberRow)(nil)).Set("display_name = ?", queryDisplay).Set("email = ?", queryEmail).
			Set("updated_at = ?", now).Where("tenant_id = ? AND user_subject = ?", tenantID, id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrNotFound
		}
		event.Detail["display_name_changed"] = displayChanged
		event.Detail["email_changed"] = emailChanged
		if err := s.audit(ctx, tx, event); err != nil {
			return err
		}
		current.DisplayName = queryDisplay
		current.Email = queryEmail
		current.UpdatedAt = time.UnixMilli(now).UTC()
		if emailChanged {
			current.EmailVerified = false
		}
		updated = current
		return nil
	})
	if err != nil {
		return Principal{}, fmt.Errorf("update principal: %w", err)
	}
	return updated, nil
}

func (s *Service) SetStatus(ctx context.Context, tenantID, id, status string) (Principal, error) {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if tenantID == "" || len(tenantID) > 128 || id == "" {
		return Principal{}, ErrInvalid
	}
	if status != "active" && status != "disabled" {
		return Principal{}, ErrInvalid
	}
	now := s.now().UTC().UnixMilli()
	disabledAt := int64(0)
	if status == "disabled" {
		disabledAt = now
	}
	eventType := "principal_restored"
	if status == "disabled" {
		eventType = "principal_disabled"
	}
	event := auditx.NewEvent(ctx, tenantID, eventType, "principal", id)
	var updated Principal
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		current, err := getPrincipal(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		verifiers := make([]func() error, 0)
		if status == "disabled" && current.Status != "disabled" {
			tenantIDs, err := activeTenantIDs(ctx, tx, id)
			if err != nil {
				return err
			}
			verifiers = make([]func() error, 0, len(tenantIDs))
			for _, affectedTenantID := range tenantIDs {
				verify, err := s.administrators.Protect(ctx, tx, s.dialect, affectedTenantID)
				if err != nil {
					return err
				}
				verifiers = append(verifiers, verify)
			}
		}
		result, err := tx.NewUpdate().Model((*principalRow)(nil)).Set("status = ?", status).Set("disabled_at = ?", disabledAt).
			Set("updated_at = ?", now).Where("id = ?", id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrNotFound
		}
		if status == "disabled" {
			if _, err := tx.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", id).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewUpdate().Model((*refreshTokenRow)(nil)).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", id).Exec(ctx); err != nil {
				return err
			}
		}
		for _, verify := range verifiers {
			if err := verify(); err != nil {
				return err
			}
		}
		event.Detail["changed"] = current.Status != status
		event.Detail["status"] = status
		if err := s.audit(ctx, tx, event); err != nil {
			return err
		}
		current.Status = status
		current.UpdatedAt = time.UnixMilli(now).UTC()
		updated = current
		return nil
	})
	if err != nil {
		return Principal{}, fmt.Errorf("set principal status: %w", err)
	}
	return updated, nil
}

func activeTenantIDs(ctx context.Context, db bun.IDB, principalID string) ([]string, error) {
	tenantIDs := make([]string, 0)
	if err := db.NewSelect().Model((*tenantMemberRow)(nil)).Column("tenant_id").
		Where("user_subject = ? AND status = 'active'", principalID).Order("tenant_id ASC").Scan(ctx, &tenantIDs); err != nil {
		return nil, fmt.Errorf("list active principal tenants: %w", err)
	}
	return tenantIDs, nil
}

func fromRow(row principalRow) Principal {
	return Principal{ID: row.ID, LoginName: row.LoginName, Email: row.Email, EmailVerified: row.EmailVerified, ActivationRequired: row.ActivationRequired, DisplayName: row.DisplayName, Status: row.Status, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}
}
func normalizeLogin(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func isUnique(err error) bool {
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "unique constraint") || strings.Contains(value, "duplicate entry") || strings.Contains(value, "duplicate key")
}
func randomID() (string, error) {
	data := make([]byte, 18)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
