package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

const maxServiceAccountCredentials = 20

var (
	ErrServiceAccountNotFound = errors.New("service account not found")
	ErrServiceAccountInvalid  = errors.New("invalid service account")
	ErrServiceAccountConflict = errors.New("service account version conflict")
	ErrCredentialNotFound     = errors.New("service account credential not found")
	ErrCredentialLimit        = errors.New("service account credential limit reached")
)

type ServiceAccount struct {
	ID          guid.ID    `json:"id"`
	LoginName   string     `json:"login_name"`
	DisplayName string     `json:"display_name"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type ServiceAccountCredential struct {
	ID               guid.ID    `json:"id"`
	ServiceAccountID guid.ID    `json:"service_account_id"`
	Name             string     `json:"name"`
	Scopes           []string   `json:"scopes"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type ServiceAccountCredentialSecret struct {
	Credential ServiceAccountCredential `json:"credential"`
	Secret     string                   `json:"client_secret" doc:"Shown once. Store it in a secret manager."`
}

type serviceAccountRow struct {
	bun.BaseModel `bun:"table:iam_service_accounts,alias:service_account"`
	PrincipalID   guid.ID `bun:"principal_id,pk"`
	OwnerTenantID guid.ID
	Description   string
	Status        string
	ExpiresAt     int64
	TokenVersion  int64
	Version       int64
	CreatedAt     int64
	UpdatedAt     int64
	DeletedAt     int64
}

type serviceAccountViewRow struct {
	serviceAccountRow `bun:",embed"`
	LoginName         string `bun:"login_name"`
	DisplayName       string `bun:"display_name"`
}

type serviceAccountCredentialRow struct {
	bun.BaseModel `bun:"table:iam_service_account_credentials,alias:credential"`
	ID            guid.ID `bun:"id,pk"`
	PrincipalID   guid.ID
	Name          string
	SecretHash    string
	Scopes        string
	ExpiresAt     int64
	LastUsedAt    int64
	RevokedAt     int64
	CreatedAt     int64
}

func (s *Service) CreateServiceAccount(ctx context.Context, tenantID guid.ID, loginName, displayName, description string, expiresAt *time.Time) (ServiceAccount, error) {
	loginName = normalizeLogin(loginName)
	displayName, description = strings.TrimSpace(displayName), strings.TrimSpace(description)
	if displayName == "" {
		displayName = loginName
	}
	if !validServiceAccount(tenantID, loginName, displayName, description, "active", expiresAt, s.now().UTC()) {
		return ServiceAccount{}, ErrServiceAccountInvalid
	}
	id, err := s.nextID()
	if err != nil {
		return ServiceAccount{}, err
	}
	now := s.now().UTC().UnixMilli()
	expires := timeMillis(expiresAt)
	principal := principalRow{ID: id, LoginName: loginName, DisplayName: displayName, Status: "active", CreatedAt: now, UpdatedAt: now}
	account := serviceAccountRow{PrincipalID: id, OwnerTenantID: tenantID, Description: description, Status: "active", ExpiresAt: expires, TokenVersion: 1, Version: 1, CreatedAt: now, UpdatedAt: now}
	event := auditx.NewEvent(ctx, tenantID, "service_account_created", "service_account", id)
	event.Detail["expires_at"] = expires
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		active, err := tx.NewSelect().Table("iam_tenants").Where("id = ? AND status = 'active'", tenantID).Exists(ctx)
		if err != nil || !active {
			if err != nil {
				return err
			}
			return ErrServiceAccountInvalid
		}
		if _, err := tx.NewInsert().Model(&principal).Exec(ctx); err != nil {
			if isUnique(err) {
				return ErrLoginConflict
			}
			return err
		}
		if _, err := tx.NewInsert().Model(&account).Exec(ctx); err != nil {
			return err
		}
		member := tenantMemberRow{TenantID: tenantID, PrincipalID: id, DisplayName: displayName, Status: "active", CreatedAt: now, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(&member).Exec(ctx); err != nil {
			return err
		}
		if err := policyx.Advance(ctx, tx, s.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return ServiceAccount{}, fmt.Errorf("create service account: %w", err)
	}
	return serviceAccountFromRows(account, principal.LoginName, principal.DisplayName), nil
}

func (s *Service) ListServiceAccounts(ctx context.Context, tenantID guid.ID, search string, limit, offset int) ([]ServiceAccount, int64, error) {
	if tenantID.Zero() || limit < 1 || limit > 200 || offset < 0 {
		return nil, 0, ErrServiceAccountInvalid
	}
	query := serviceAccountQuery(s.db).Where("service_account.owner_tenant_id = ? AND service_account.status <> 'deleted'", tenantID)
	if search = strings.ToLower(strings.TrimSpace(search)); search != "" {
		query = query.Where("LOWER(principal.login_name) LIKE ? OR LOWER(principal.display_name) LIKE ? OR LOWER(service_account.description) LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	count, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]serviceAccountViewRow, 0)
	if err := query.Order("principal.login_name ASC", "service_account.principal_id ASC").Limit(limit).Offset(offset).Scan(ctx, &rows); err != nil {
		return nil, 0, err
	}
	items := make([]ServiceAccount, 0, len(rows))
	for _, row := range rows {
		items = append(items, serviceAccountFromRows(row.serviceAccountRow, row.LoginName, row.DisplayName))
	}
	return items, int64(count), nil
}

func (s *Service) GetServiceAccount(ctx context.Context, tenantID, id guid.ID) (ServiceAccount, error) {
	row, err := getServiceAccountRow(ctx, s.db, tenantID, id)
	if err != nil {
		return ServiceAccount{}, err
	}
	return serviceAccountFromRows(row.serviceAccountRow, row.LoginName, row.DisplayName), nil
}

func (s *Service) ReplaceServiceAccount(ctx context.Context, tenantID, id guid.ID, displayName, description, status string, expiresAt *time.Time, version int64) (ServiceAccount, error) {
	displayName, description, status = strings.TrimSpace(displayName), strings.TrimSpace(description), strings.TrimSpace(status)
	if version < 1 || !validServiceAccount(tenantID, "placeholder", displayName, description, status, expiresAt, s.now().UTC()) {
		return ServiceAccount{}, ErrServiceAccountInvalid
	}
	now := s.now().UTC().UnixMilli()
	expires := timeMillis(expiresAt)
	var updated ServiceAccount
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		current, err := getServiceAccountRow(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return ErrServiceAccountConflict
		}
		statusChanged := current.Status != status
		expiryChanged := current.ExpiresAt != expires
		var verify func() error
		if current.Status == "active" && status == "disabled" {
			verify, err = s.administrators.Protect(ctx, tx, s.dialect, tenantID)
			if err != nil {
				return err
			}
		}
		query := tx.NewUpdate().Model((*serviceAccountRow)(nil)).
			Set("description = ?", description).Set("status = ?", status).Set("expires_at = ?", expires).
			Set("version = version + 1").Set("updated_at = ?", now)
		if statusChanged || expiryChanged {
			query = query.Set("token_version = token_version + 1")
		}
		result, err := query.Where("owner_tenant_id = ? AND principal_id = ? AND version = ? AND status <> 'deleted'", tenantID, id, version).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrServiceAccountConflict
		}
		principalStatus := "active"
		disabledAt := int64(0)
		if status == "disabled" {
			principalStatus, disabledAt = "disabled", now
		}
		if _, err := tx.NewUpdate().Model((*principalRow)(nil)).Set("display_name = ?", displayName).Set("status = ?", principalStatus).Set("disabled_at = ?", disabledAt).Set("updated_at = ?", now).Where("id = ?", id).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*tenantMemberRow)(nil)).Set("display_name = ?", displayName).Set("updated_at = ?", now).Where("tenant_id = ? AND principal_id = ?", tenantID, id).Exec(ctx); err != nil {
			return err
		}
		if verify != nil {
			if err := verify(); err != nil {
				return err
			}
		}
		if statusChanged {
			if err := policyx.Advance(ctx, tx, s.dialect, tenantID, now); err != nil {
				return err
			}
		}
		event := auditx.NewEvent(ctx, tenantID, "service_account_updated", "service_account", id)
		event.Detail["status"] = status
		event.Detail["status_changed"] = statusChanged
		event.Detail["expires_at"] = expires
		if err := s.audit(ctx, tx, event); err != nil {
			return err
		}
		current.Description, current.Status, current.ExpiresAt, current.Version, current.UpdatedAt = description, status, expires, version+1, now
		updated = serviceAccountFromRows(current.serviceAccountRow, current.LoginName, displayName)
		return nil
	})
	if err != nil {
		return ServiceAccount{}, fmt.Errorf("replace service account: %w", err)
	}
	return updated, nil
}

func (s *Service) DeleteServiceAccount(ctx context.Context, tenantID, id guid.ID, version int64) error {
	if tenantID.Zero() || id.Zero() || version < 1 {
		return ErrServiceAccountInvalid
	}
	now := s.now().UTC().UnixMilli()
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		current, err := getServiceAccountRow(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return ErrServiceAccountConflict
		}
		verify, err := s.administrators.Protect(ctx, tx, s.dialect, tenantID)
		if err != nil {
			return err
		}
		result, err := tx.NewUpdate().Model((*serviceAccountRow)(nil)).Set("status = 'deleted'").Set("deleted_at = ?", now).
			Set("updated_at = ?", now).Set("version = version + 1").Set("token_version = token_version + 1").
			Where("owner_tenant_id = ? AND principal_id = ? AND version = ? AND status <> 'deleted'", tenantID, id, version).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrServiceAccountConflict
		}
		if _, err := tx.NewUpdate().Model((*principalRow)(nil)).Set("status = 'disabled'").Set("disabled_at = ?", now).Set("updated_at = ?", now).Where("id = ?", id).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*tenantMemberRow)(nil)).Set("status = 'disabled'").Set("disabled_at = ?", now).Set("updated_at = ?", now).Where("tenant_id = ? AND principal_id = ?", tenantID, id).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*serviceAccountCredentialRow)(nil)).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", id).Exec(ctx); err != nil {
			return err
		}
		if err := verify(); err != nil {
			return err
		}
		if err := policyx.Advance(ctx, tx, s.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, auditx.NewEvent(ctx, tenantID, "service_account_deleted", "service_account", id))
	})
	if err != nil {
		return fmt.Errorf("delete service account: %w", err)
	}
	return nil
}

func (s *Service) CreateServiceAccountCredential(ctx context.Context, tenantID, id guid.ID, name string, scopes []string, expiresAt *time.Time) (ServiceAccountCredentialSecret, error) {
	name = strings.TrimSpace(name)
	normalizedScopes, ok := normalizeServiceAccountScopes(scopes)
	if tenantID.Zero() || id.Zero() || name == "" || len(name) > 128 || !ok {
		return ServiceAccountCredentialSecret{}, ErrServiceAccountInvalid
	}
	nowTime := s.now().UTC()
	if expiresAt != nil && !expiresAt.UTC().After(nowTime) {
		return ServiceAccountCredentialSecret{}, ErrServiceAccountInvalid
	}
	credentialID, err := s.nextID()
	if err != nil {
		return ServiceAccountCredentialSecret{}, err
	}
	secret, err := randomServiceAccountSecret()
	if err != nil {
		return ServiceAccountCredentialSecret{}, err
	}
	hash, err := passwordx.Hash(secret)
	if err != nil {
		return ServiceAccountCredentialSecret{}, err
	}
	now := nowTime.UnixMilli()
	row := serviceAccountCredentialRow{ID: credentialID, PrincipalID: id, Name: name, SecretHash: hash, Scopes: strings.Join(normalizedScopes, " "), ExpiresAt: timeMillis(expiresAt), CreatedAt: now}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		account, err := getServiceAccountRow(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if account.Status != "active" || account.ExpiresAt > 0 && account.ExpiresAt <= now || account.ExpiresAt > 0 && row.ExpiresAt > account.ExpiresAt {
			return ErrServiceAccountInvalid
		}
		count, err := tx.NewSelect().Model((*serviceAccountCredentialRow)(nil)).Where("principal_id = ? AND revoked_at = 0 AND (expires_at = 0 OR expires_at > ?)", id, now).Count(ctx)
		if err != nil {
			return err
		}
		if count >= maxServiceAccountCredentials {
			return ErrCredentialLimit
		}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return err
		}
		event := auditx.NewEvent(ctx, tenantID, "service_account_credential_created", "service_account_credential", row.ID)
		event.Detail["service_account_id"] = id
		event.Detail["expires_at"] = row.ExpiresAt
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return ServiceAccountCredentialSecret{}, fmt.Errorf("create service account credential: %w", err)
	}
	return ServiceAccountCredentialSecret{Credential: serviceAccountCredentialFromRow(row), Secret: secret}, nil
}

func (s *Service) ListServiceAccountCredentials(ctx context.Context, tenantID, id guid.ID) ([]ServiceAccountCredential, error) {
	if _, err := getServiceAccountRow(ctx, s.db, tenantID, id); err != nil {
		return nil, err
	}
	rows := make([]serviceAccountCredentialRow, 0)
	if err := s.db.NewSelect().Model(&rows).Where("principal_id = ?", id).Order("created_at DESC", "id ASC").Scan(ctx); err != nil {
		return nil, err
	}
	result := make([]ServiceAccountCredential, 0, len(rows))
	for _, row := range rows {
		result = append(result, serviceAccountCredentialFromRow(row))
	}
	return result, nil
}

func (s *Service) RevokeServiceAccountCredential(ctx context.Context, tenantID, id, credentialID guid.ID) error {
	if tenantID.Zero() || id.Zero() || credentialID.Zero() {
		return ErrServiceAccountInvalid
	}
	now := s.now().UTC().UnixMilli()
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := getServiceAccountRow(ctx, tx, tenantID, id); err != nil {
			return err
		}
		var row serviceAccountCredentialRow
		if err := tx.NewSelect().Model(&row).Where("id = ? AND principal_id = ?", credentialID, id).Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrCredentialNotFound
			}
			return err
		}
		if row.RevokedAt != 0 {
			return nil
		}
		if _, err := tx.NewUpdate().Model((*serviceAccountCredentialRow)(nil)).Set("revoked_at = ?", now).Where("id = ? AND principal_id = ? AND revoked_at = 0", credentialID, id).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*serviceAccountRow)(nil)).Set("token_version = token_version + 1").Set("updated_at = ?", now).Where("principal_id = ? AND owner_tenant_id = ?", id, tenantID).Exec(ctx); err != nil {
			return err
		}
		event := auditx.NewEvent(ctx, tenantID, "service_account_credential_revoked", "service_account_credential", credentialID)
		event.Detail["service_account_id"] = id
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return fmt.Errorf("revoke service account credential: %w", err)
	}
	return nil
}

func serviceAccountQuery(db bun.IDB) *bun.SelectQuery {
	return db.NewSelect().TableExpr("iam_service_accounts AS service_account").
		ColumnExpr("service_account.*").
		ColumnExpr("principal.login_name AS login_name, principal.display_name AS display_name").
		Join("JOIN iam_principals AS principal ON principal.id = service_account.principal_id")
}

func getServiceAccountRow(ctx context.Context, db bun.IDB, tenantID, id guid.ID) (serviceAccountViewRow, error) {
	var row serviceAccountViewRow
	if tenantID.Zero() || id.Zero() {
		return row, ErrServiceAccountInvalid
	}
	if err := serviceAccountQuery(db).Where("service_account.owner_tenant_id = ? AND service_account.principal_id = ? AND service_account.status <> 'deleted'", tenantID, id).Scan(ctx, &row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrServiceAccountNotFound
		}
		return row, err
	}
	return row, nil
}

func validServiceAccount(tenantID guid.ID, loginName, displayName, description, status string, expiresAt *time.Time, now time.Time) bool {
	return !tenantID.Zero() && loginName != "" && len(loginName) <= 200 &&
		displayName != "" && len(displayName) <= 128 && len(description) <= 1000 &&
		(status == "active" || status == "disabled") && (expiresAt == nil || expiresAt.UTC().After(now))
}

func normalizeServiceAccountScopes(values []string) ([]string, bool) {
	if len(values) == 0 || len(values) > 128 {
		return nil, false
	}
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 128 || strings.ContainsAny(value, " \t\r\n") {
			return nil, false
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, true
}

func serviceAccountFromRows(row serviceAccountRow, loginName, displayName string) ServiceAccount {
	return ServiceAccount{ID: row.PrincipalID, LoginName: loginName, DisplayName: displayName, Description: row.Description, Status: row.Status,
		ExpiresAt: optionalTime(row.ExpiresAt), Version: row.Version, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}
}

func serviceAccountCredentialFromRow(row serviceAccountCredentialRow) ServiceAccountCredential {
	return ServiceAccountCredential{ID: row.ID, ServiceAccountID: row.PrincipalID, Name: row.Name, Scopes: strings.Fields(row.Scopes),
		ExpiresAt: optionalTime(row.ExpiresAt), LastUsedAt: optionalTime(row.LastUsedAt), RevokedAt: optionalTime(row.RevokedAt), CreatedAt: time.UnixMilli(row.CreatedAt).UTC()}
}

func optionalTime(value int64) *time.Time {
	if value == 0 {
		return nil
	}
	result := time.UnixMilli(value).UTC()
	return &result
}

func timeMillis(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UTC().UnixMilli()
}

func randomServiceAccountSecret() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

type serviceAccountListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Search   string `query:"search" maxLength:"200"`
	Limit    int    `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Offset   int    `query:"offset" default:"0" minimum:"0"`
}

type serviceAccountListData struct {
	Items []ServiceAccount `json:"items"`
	Total int64            `json:"total"`
}

type serviceAccountIDInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"64"`
}

type serviceAccountCreateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		LoginName   string     `json:"login_name" minLength:"1" maxLength:"200"`
		DisplayName string     `json:"display_name,omitempty" maxLength:"128"`
		Description string     `json:"description,omitempty" maxLength:"1000"`
		ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	}
}

type serviceAccountReplaceInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"64"`
	Body     struct {
		DisplayName string     `json:"display_name" minLength:"1" maxLength:"128"`
		Description string     `json:"description,omitempty" maxLength:"1000"`
		Status      string     `json:"status" enum:"active,disabled"`
		ExpiresAt   *time.Time `json:"expires_at,omitempty"`
		Version     int64      `json:"version" minimum:"1"`
	}
}

type serviceAccountDeleteInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"64"`
	Version  int64  `query:"version" minimum:"1"`
}

type serviceAccountCredentialCreateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"64"`
	Body     struct {
		Name      string     `json:"name" minLength:"1" maxLength:"128"`
		Scopes    []string   `json:"scopes" minItems:"1" maxItems:"128"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
}

type serviceAccountCredentialIDInput struct {
	TenantID     string `header:"X-Tenant-Id" maxLength:"128"`
	ID           string `path:"id" maxLength:"64"`
	CredentialID string `path:"credential_id" maxLength:"64"`
}

func RegisterServiceAccountREST(api huma.API, service *Service, registrar *authz.Registrar) {
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-list-service-accounts", Method: http.MethodGet, Path: "/iam/service-accounts", Summary: "List tenant service accounts", Tags: []string{"identity"}}, authz.Guard{Resource: "service_account", Verb: "view"}, func(ctx context.Context, in *serviceAccountListInput) (*respx.Body[serviceAccountListData], error) {
		tenantID, err := parseIdentityID(in.TenantID)
		if err != nil {
			return nil, err
		}
		items, total, err := service.ListServiceAccounts(ctx, tenantID, in.Search, in.Limit, in.Offset)
		if err != nil {
			return nil, serviceAccountError(err)
		}
		return respx.OK(ctx, serviceAccountListData{Items: items, Total: total}), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-create-service-account", Method: http.MethodPost, Path: "/iam/service-accounts", Summary: "Create a non-interactive tenant service account", Tags: []string{"identity"}, Errors: []int{http.StatusConflict}}, authz.Guard{Resource: "service_account", Verb: "create"}, func(ctx context.Context, in *serviceAccountCreateInput) (*respx.Body[ServiceAccount], error) {
		tenantID, err := parseIdentityID(in.TenantID)
		if err != nil {
			return nil, err
		}
		account, err := service.CreateServiceAccount(ctx, tenantID, in.Body.LoginName, in.Body.DisplayName, in.Body.Description, in.Body.ExpiresAt)
		if err != nil {
			return nil, serviceAccountError(err)
		}
		return respx.OK(ctx, account), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-get-service-account", Method: http.MethodGet, Path: "/iam/service-accounts/{id}", Summary: "Get a tenant service account", Tags: []string{"identity"}, Errors: []int{http.StatusNotFound}}, authz.Guard{Resource: "service_account", Verb: "view"}, func(ctx context.Context, in *serviceAccountIDInput) (*respx.Body[ServiceAccount], error) {
		tenantID, err := parseIdentityID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseIdentityID(in.ID)
		if err != nil {
			return nil, err
		}
		account, err := service.GetServiceAccount(ctx, tenantID, id)
		if err != nil {
			return nil, serviceAccountError(err)
		}
		return respx.OK(ctx, account), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-replace-service-account", Method: http.MethodPut, Path: "/iam/service-accounts/{id}", Summary: "Replace service account profile and status", Tags: []string{"identity"}, Errors: []int{http.StatusNotFound, http.StatusConflict}}, authz.Guard{Resource: "service_account", Verb: "update"}, func(ctx context.Context, in *serviceAccountReplaceInput) (*respx.Body[ServiceAccount], error) {
		tenantID, err := parseIdentityID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseIdentityID(in.ID)
		if err != nil {
			return nil, err
		}
		account, err := service.ReplaceServiceAccount(ctx, tenantID, id, in.Body.DisplayName, in.Body.Description, in.Body.Status, in.Body.ExpiresAt, in.Body.Version)
		if err != nil {
			return nil, serviceAccountError(err)
		}
		return respx.OK(ctx, account), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-delete-service-account", Method: http.MethodDelete, Path: "/iam/service-accounts/{id}", Summary: "Delete a service account and revoke its credentials", Tags: []string{"identity"}, Errors: []int{http.StatusNotFound, http.StatusConflict}}, authz.Guard{Resource: "service_account", Verb: "delete"}, func(ctx context.Context, in *serviceAccountDeleteInput) (*respx.Body[map[string]bool], error) {
		tenantID, err := parseIdentityID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseIdentityID(in.ID)
		if err != nil {
			return nil, err
		}
		if err := service.DeleteServiceAccount(ctx, tenantID, id, in.Version); err != nil {
			return nil, serviceAccountError(err)
		}
		return respx.OK(ctx, map[string]bool{"deleted": true}), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-list-service-account-credentials", Method: http.MethodGet, Path: "/iam/service-accounts/{id}/credentials", Summary: "List service account credentials without secrets", Tags: []string{"identity"}, Errors: []int{http.StatusNotFound}}, authz.Guard{Resource: "service_account", Verb: "manage_credential"}, func(ctx context.Context, in *serviceAccountIDInput) (*respx.Body[[]ServiceAccountCredential], error) {
		tenantID, err := parseIdentityID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseIdentityID(in.ID)
		if err != nil {
			return nil, err
		}
		credentials, err := service.ListServiceAccountCredentials(ctx, tenantID, id)
		if err != nil {
			return nil, serviceAccountError(err)
		}
		return respx.OK(ctx, credentials), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-create-service-account-credential", Method: http.MethodPost, Path: "/iam/service-accounts/{id}/credentials", Summary: "Create a service account client credential", Tags: []string{"identity"}, Errors: []int{http.StatusNotFound, http.StatusConflict}}, authz.Guard{Resource: "service_account", Verb: "manage_credential"}, func(ctx context.Context, in *serviceAccountCredentialCreateInput) (*respx.Body[ServiceAccountCredentialSecret], error) {
		tenantID, err := parseIdentityID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseIdentityID(in.ID)
		if err != nil {
			return nil, err
		}
		credential, err := service.CreateServiceAccountCredential(ctx, tenantID, id, in.Body.Name, in.Body.Scopes, in.Body.ExpiresAt)
		if err != nil {
			return nil, serviceAccountError(err)
		}
		return respx.OK(ctx, credential), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "identity-revoke-service-account-credential", Method: http.MethodDelete, Path: "/iam/service-accounts/{id}/credentials/{credential_id}", Summary: "Revoke a service account credential and its active tokens", Tags: []string{"identity"}, Errors: []int{http.StatusNotFound}}, authz.Guard{Resource: "service_account", Verb: "manage_credential"}, func(ctx context.Context, in *serviceAccountCredentialIDInput) (*respx.Body[map[string]bool], error) {
		tenantID, err := parseIdentityID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseIdentityID(in.ID)
		if err != nil {
			return nil, err
		}
		credentialID, err := parseIdentityID(in.CredentialID)
		if err != nil {
			return nil, err
		}
		if err := service.RevokeServiceAccountCredential(ctx, tenantID, id, credentialID); err != nil {
			return nil, serviceAccountError(err)
		}
		return respx.OK(ctx, map[string]bool{"revoked": true}), nil
	})
}

func serviceAccountError(err error) error {
	switch {
	case errors.Is(err, ErrServiceAccountNotFound):
		return huma.Error404NotFound("service_account_not_found")
	case errors.Is(err, ErrCredentialNotFound):
		return huma.Error404NotFound("service_account_credential_not_found")
	case errors.Is(err, ErrLoginConflict):
		return huma.Error409Conflict("login_name_exists")
	case errors.Is(err, ErrServiceAccountConflict):
		return huma.Error409Conflict("service_account_version_conflict")
	case errors.Is(err, ErrCredentialLimit):
		return huma.Error409Conflict("service_account_credential_limit")
	case errors.Is(err, iamdomain.ErrLastTenantAdministrator):
		return huma.Error409Conflict("last_tenant_administrator")
	case errors.Is(err, ErrServiceAccountInvalid):
		return huma.Error422UnprocessableEntity("invalid_service_account")
	default:
		return huma.Error500InternalServerError("identity_unavailable")
	}
}
