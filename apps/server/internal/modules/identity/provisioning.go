package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

// ProvisionedPrincipalInput is the canonical identity command used by
// transactional directory adapters. PasswordHash is required only on create.
type ProvisionedPrincipalInput struct {
	LoginName    string
	DisplayName  string
	Email        string
	PasswordHash string
	Active       bool
}

// CreateProvisionedTo writes identity state through a caller-owned transaction.
// The caller owns the corresponding provisioning mapping and audit event.
func (s *Service) CreateProvisionedTo(ctx context.Context, db bun.IDB, tenantID guid.ID, input ProvisionedPrincipalInput) (Principal, error) {
	tenantID, input, err := normalizeProvisionedInput(tenantID, input, true)
	if err != nil || db == nil {
		return Principal{}, ErrInvalid
	}
	id, err := s.nextID()
	if err != nil {
		return Principal{}, err
	}
	now := s.now().UTC().UnixMilli()
	principal := principalRow{ID: id, LoginName: input.LoginName, Email: input.Email, ActivationRequired: true, DisplayName: input.DisplayName, Status: "active", CreatedAt: now, UpdatedAt: now}
	if _, err := db.NewInsert().Model(&principal).Exec(ctx); err != nil {
		if isUnique(err) {
			return Principal{}, ErrLoginConflict
		}
		return Principal{}, fmt.Errorf("insert provisioned principal: %w", err)
	}
	credential := credentialRow{PrincipalID: id, PasswordHash: input.PasswordHash, PasswordChangedAt: now, CredentialVersion: 1, UpdatedAt: now}
	if _, err := db.NewInsert().Model(&credential).Exec(ctx); err != nil {
		return Principal{}, fmt.Errorf("insert provisioned credential: %w", err)
	}
	status, disabledAt := "active", int64(0)
	if !input.Active {
		status, disabledAt = "disabled", now
	}
	member := tenantMemberRow{TenantID: tenantID, PrincipalID: id, DisplayName: input.DisplayName, Email: input.Email, Status: status, CreatedAt: now, UpdatedAt: now, DisabledAt: disabledAt}
	if _, err := db.NewInsert().Model(&member).Exec(ctx); err != nil {
		return Principal{}, fmt.Errorf("insert provisioned membership: %w", err)
	}
	if err := policyx.Advance(ctx, db, s.dialect, tenantID, now); err != nil {
		return Principal{}, err
	}
	return fromRow(principal), nil
}

// ReplaceProvisionedTo replaces directory-owned profile and tenant admission
// state without overriding a global security disable.
func (s *Service) ReplaceProvisionedTo(ctx context.Context, db bun.IDB, tenantID, id guid.ID, input ProvisionedPrincipalInput) (Principal, error) {
	tenantID, input, err := normalizeProvisionedInput(tenantID, input, false)
	if err != nil || db == nil || id.Zero() {
		return Principal{}, ErrInvalid
	}
	current, err := getPrincipal(ctx, db, tenantID, id)
	if err != nil {
		return Principal{}, err
	}
	var member tenantMemberRow
	if err := db.NewSelect().Model(&member).Where("tenant_id = ? AND principal_id = ?", tenantID, id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Principal{}, ErrNotFound
		}
		return Principal{}, err
	}
	if input.Active && current.Status != "active" {
		return Principal{}, ErrInvalid
	}
	now := s.now().UTC().UnixMilli()
	wantedMemberStatus := "disabled"
	disabledAt := now
	if input.Active {
		wantedMemberStatus, disabledAt = "active", 0
	}
	var verify func() error
	if member.Status == "active" && wantedMemberStatus == "disabled" {
		verify, err = s.administrators.Protect(ctx, db, s.dialect, tenantID)
		if err != nil {
			return Principal{}, err
		}
	}
	loginChanged := current.LoginName != input.LoginName
	emailChanged := current.Email != input.Email
	profileChanged := loginChanged || emailChanged || current.DisplayName != input.DisplayName
	membershipChanged := member.Status != wantedMemberStatus
	query := db.NewUpdate().Model((*principalRow)(nil)).Set("login_name = ?", input.LoginName).Set("display_name = ?", input.DisplayName).
		Set("email = ?", input.Email).Set("updated_at = ?", now).Where("id = ?", id)
	if emailChanged {
		query = query.Set("email_verified = ?", false)
	}
	if _, err := query.Exec(ctx); err != nil {
		if isUnique(err) {
			return Principal{}, ErrLoginConflict
		}
		return Principal{}, err
	}
	if _, err := db.NewUpdate().Model((*tenantMemberRow)(nil)).Set("display_name = ?", input.DisplayName).Set("email = ?", input.Email).
		Set("status = ?", wantedMemberStatus).Set("disabled_at = ?", disabledAt).Set("updated_at = ?", now).
		Where("tenant_id = ? AND principal_id = ?", tenantID, id).Exec(ctx); err != nil {
		return Principal{}, err
	}
	if loginChanged || emailChanged {
		if _, err := db.NewUpdate().Model((*credentialRow)(nil)).Set("credential_version = credential_version + 1").Set("updated_at = ?", now).Where("principal_id = ?", id).Exec(ctx); err != nil {
			return Principal{}, err
		}
	}
	if loginChanged || emailChanged || membershipChanged {
		if err := revokeProvisionedSecurityState(ctx, db, id, now); err != nil {
			return Principal{}, err
		}
	}
	if verify != nil {
		if err := verify(); err != nil {
			return Principal{}, err
		}
	}
	if emailChanged || membershipChanged {
		if err := policyx.Advance(ctx, db, s.dialect, tenantID, now); err != nil {
			return Principal{}, err
		}
	}
	current.LoginName, current.DisplayName, current.Email = input.LoginName, input.DisplayName, input.Email
	if profileChanged || membershipChanged {
		current.UpdatedAt = time.UnixMilli(now).UTC()
	}
	if emailChanged {
		current.EmailVerified = false
	}
	return current, nil
}

func revokeProvisionedSecurityState(ctx context.Context, db bun.IDB, principalID guid.ID, now int64) error {
	for _, table := range []string{"iam_sessions", "iam_refresh_tokens"} {
		if _, err := db.NewUpdate().Table(table).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", principalID).Exec(ctx); err != nil {
			return fmt.Errorf("revoke provisioned principal state in %s: %w", table, err)
		}
	}
	for _, table := range []string{"iam_password_recovery_tokens", "iam_email_verification_tokens"} {
		if _, err := db.NewUpdate().Table(table).Set("consumed_at = ?", now).Where("principal_id = ? AND consumed_at = 0", principalID).Exec(ctx); err != nil {
			return fmt.Errorf("consume provisioned principal state in %s: %w", table, err)
		}
	}
	return nil
}

func normalizeProvisionedInput(tenantID guid.ID, input ProvisionedPrincipalInput, creating bool) (guid.ID, ProvisionedPrincipalInput, error) {
	input.LoginName = normalizeLogin(input.LoginName)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if input.DisplayName == "" {
		input.DisplayName = input.LoginName
	}
	validEmail := true
	if input.Email != "" {
		parsed, err := mail.ParseAddress(input.Email)
		validEmail = err == nil && strings.EqualFold(parsed.Address, input.Email)
	}
	if tenantID.Zero() || input.LoginName == "" || len(input.LoginName) > 200 ||
		input.DisplayName == "" || len(input.DisplayName) > 128 || len(input.Email) > 320 || !validEmail || creating && input.PasswordHash == "" {
		return 0, ProvisionedPrincipalInput{}, ErrInvalid
	}
	return tenantID, input, nil
}
