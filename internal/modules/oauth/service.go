package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/uptrace/bun"
)

var ErrInvalidRequest = errors.New("invalid OAuth request")

type clientRow struct {
	bun.BaseModel `bun:"table:iam_oauth_clients"`
	ID            string `bun:"id,pk"`
	TenantID      string `bun:"tenant_id"`
	SecretHash    string
	Name          string
	RedirectURIs  string `bun:"redirect_uris"`
	GrantTypes    string `bun:"grant_types"`
	Scopes        string
	PublicClient  bool
	Status        string
	CreatedAt     int64
	UpdatedAt     int64
}

type codeRow struct {
	bun.BaseModel `bun:"table:iam_oauth_codes"`
	CodeHash      string `bun:"code_hash,pk"`
	ClientID      string `bun:"client_id"`
	PrincipalID   string `bun:"principal_id"`
	RedirectURI   string `bun:"redirect_uri"`
	Scope         string
	CodeChallenge string `bun:"code_challenge"`
	Nonce         string
	AuthTime      int64
	ACR           int
	AMR           string
	CreatedAt     int64
	ExpiresAt     int64
	ConsumedAt    int64
}

type refreshRow struct {
	bun.BaseModel `bun:"table:iam_refresh_tokens"`
	IDHash        string `bun:"id_hash,pk"`
	FamilyID      string `bun:"family_id"`
	PrincipalID   string `bun:"principal_id"`
	ClientID      string `bun:"client_id"`
	Scope         string
	AuthTime      int64
	ACR           int
	AMR           string
	CreatedAt     int64
	ExpiresAt     int64
	UsedAt        int64
	RevokedAt     int64
}

type serviceAccountClient struct {
	CredentialID        string `bun:"credential_id"`
	PrincipalID         string `bun:"principal_id"`
	TenantID            string `bun:"tenant_id"`
	LoginName           string `bun:"login_name"`
	SecretHash          string `bun:"secret_hash"`
	Scopes              string `bun:"scopes"`
	CredentialExpiresAt int64  `bun:"credential_expires_at"`
	CredentialRevokedAt int64  `bun:"credential_revoked_at"`
	AccountExpiresAt    int64  `bun:"account_expires_at"`
	TokenVersion        int64  `bun:"token_version"`
	AccountStatus       string `bun:"account_status"`
	PrincipalStatus     string `bun:"principal_status"`
	MemberStatus        string `bun:"member_status"`
	TenantStatus        string `bun:"tenant_status"`
}

type Service struct {
	db    *bun.DB
	authn *authnmod.WebService
	audit *auditmod.Service
	now   func() time.Time
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type Client struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenant_id"`
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	Scopes       []string `json:"scopes"`
	PublicClient bool     `json:"public_client"`
	Status       string   `json:"status"`
}

func (s *Service) CreateClient(ctx context.Context, tenantID, name string, redirects, grants, scopes []string, public bool) (Client, string, error) {
	if err := validateClient(tenantID, name, redirects, grants, scopes, public); err != nil {
		return Client{}, "", err
	}
	id, err := secureToken(18)
	if err != nil {
		return Client{}, "", err
	}
	secret, hash := "", ""
	if !public {
		secret, err = secureToken(32)
		if err != nil {
			return Client{}, "", err
		}
		hash, err = passwordx.Hash(secret)
		if err != nil {
			return Client{}, "", err
		}
	}
	redirectJSON, _ := json.Marshal(redirects)
	now := s.now().UTC().UnixMilli()
	row := clientRow{ID: id, TenantID: tenantID, SecretHash: hash, Name: strings.TrimSpace(name), RedirectURIs: string(redirectJSON), GrantTypes: strings.Join(grants, ","), Scopes: strings.Join(scopes, " "), PublicClient: public, Status: "active", CreatedAt: now, UpdatedAt: now}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return fmt.Errorf("create OAuth client: %w", err)
		}
		_, err := s.audit.AppendTo(ctx, tx, s.clientAudit(ctx, tenantID, id, "oauth_client_created", map[string]any{"public_client": public, "grant_types": grants}))
		return err
	})
	if err != nil {
		return Client{}, "", fmt.Errorf("create OAuth client: %w", err)
	}
	return clientFromRow(row), secret, nil
}

func (s *Service) ListClients(ctx context.Context, tenantID string) ([]Client, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidRequest
	}
	var rows []clientRow
	if err := s.db.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("name ASC", "id ASC").Scan(ctx); err != nil {
		return nil, err
	}
	result := make([]Client, 0, len(rows))
	for _, row := range rows {
		result = append(result, clientFromRow(row))
	}
	return result, nil
}

func (s *Service) UpdateClient(ctx context.Context, tenantID, id, name string, redirects, grants, scopes []string, public bool, status string) (Client, error) {
	if status != "active" && status != "disabled" {
		return Client{}, ErrInvalidRequest
	}
	if err := validateClient(tenantID, name, redirects, grants, scopes, public); err != nil {
		return Client{}, err
	}
	redirectJSON, _ := json.Marshal(redirects)
	now := s.now().UTC().UnixMilli()
	var row clientRow
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*clientRow)(nil)).Set("name = ?", strings.TrimSpace(name)).Set("redirect_uris = ?", string(redirectJSON)).Set("grant_types = ?", strings.Join(grants, ",")).Set("scopes = ?", strings.Join(scopes, " ")).Set("public_client = ?", public).Set("status = ?", status).Set("updated_at = ?", now).Where("tenant_id = ? AND id = ?", tenantID, id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrInvalidRequest
		}
		if err := tx.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, id).Scan(ctx); err != nil {
			return err
		}
		_, err = s.audit.AppendTo(ctx, tx, s.clientAudit(ctx, tenantID, id, "oauth_client_updated", map[string]any{"public_client": public, "grant_types": grants, "status": status}))
		return err
	})
	return clientFromRow(row), err
}

func (s *Service) RotateClientSecret(ctx context.Context, tenantID, id string) (string, error) {
	secret, err := secureToken(32)
	if err != nil {
		return "", err
	}
	hash, err := passwordx.Hash(secret)
	if err != nil {
		return "", err
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*clientRow)(nil)).Set("secret_hash = ?", hash).Set("public_client = ?", false).Set("updated_at = ?", s.now().UTC().UnixMilli()).Where("tenant_id = ? AND id = ?", tenantID, id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrInvalidRequest
		}
		_, err = s.audit.AppendTo(ctx, tx, s.clientAudit(ctx, tenantID, id, "oauth_client_secret_rotated", nil))
		return err
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}

func (s *Service) DeleteClient(ctx context.Context, tenantID, id string) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, _ = tx.NewUpdate().Model((*refreshRow)(nil)).Set("revoked_at = ?", s.now().UTC().UnixMilli()).Where("client_id = ? AND revoked_at = 0", id).Exec(ctx)
		result, err := tx.NewDelete().Model((*clientRow)(nil)).Where("tenant_id = ? AND id = ?", tenantID, id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrInvalidRequest
		}
		_, err = s.audit.AppendTo(ctx, tx, s.clientAudit(ctx, tenantID, id, "oauth_client_deleted", nil))
		return err
	})
}

func NewService(db *bun.DB, authn *authnmod.WebService) *Service {
	if db == nil || authn == nil {
		panic("oauth service requires database and local authentication")
	}
	return &Service{db: db, authn: authn, audit: auditmod.NewService(db), now: time.Now}
}

func (s *Service) clientAudit(ctx context.Context, tenantID, clientID, eventType string, detail map[string]any) auditmod.EventInput {
	principalID := ""
	if claims, ok := authnext.FromContext(ctx); ok {
		principalID = claims.Subject
	}
	return auditmod.EventInput{TenantID: tenantID, PrincipalID: principalID, EventType: eventType, TargetType: "oauth_client", TargetID: clientID, Outcome: "success", Detail: detail}
}

func (s *Service) Authorize(ctx context.Context, cookieHeader, clientID, redirectURI, responseType, scope, state, challenge, challengeMethod, nonce string) (string, error) {
	if responseType != "code" || challengeMethod != "S256" || len(challenge) < 43 || len(challenge) > 128 {
		return "", ErrInvalidRequest
	}
	claims, err := s.authn.Authenticate(ctx, "", cookieHeader)
	if err != nil {
		return "", authnext.ErrInvalidCredentials
	}
	client, err := s.client(ctx, clientID)
	if err != nil || !containsWord(client.GrantTypes, "authorization_code") || !allowedRedirect(client.RedirectURIs, redirectURI) || !allowedScopes(client.Scopes, scope) {
		return "", ErrInvalidRequest
	}
	if active, err := s.activeMember(ctx, client.TenantID, claims.Subject); err != nil || !active {
		return "", ErrInvalidRequest
	}
	code, err := secureToken(32)
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	row := codeRow{
		CodeHash: hashToken(code), ClientID: client.ID, PrincipalID: claims.Subject, RedirectURI: redirectURI,
		Scope: normalizeWords(scope), CodeChallenge: challenge, Nonce: nonce,
		AuthTime: claims.AuthTime.UTC().UnixMilli(), ACR: claims.ACR, AMR: strings.Join(claims.AMR, " "),
		CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(5 * time.Minute).UnixMilli(),
	}
	if _, err := s.db.NewInsert().Model(&row).Exec(ctx); err != nil {
		return "", fmt.Errorf("store authorization code: %w", err)
	}
	target, err := url.Parse(redirectURI)
	if err != nil {
		return "", ErrInvalidRequest
	}
	query := target.Query()
	query.Set("code", code)
	if state != "" {
		query.Set("state", state)
	}
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func (s *Service) Token(ctx context.Context, form url.Values, authorization string) (TokenResponse, error) {
	if form.Get("grant_type") == "client_credentials" {
		id, secret, err := clientCredentials(form.Get("client_id"), form.Get("client_secret"), authorization)
		if err == nil && strings.HasPrefix(id, "sac_") {
			return s.serviceAccountToken(ctx, id, secret, form.Get("scope"))
		}
	}
	client, err := s.authenticateClient(ctx, form.Get("client_id"), form.Get("client_secret"), authorization)
	if err != nil {
		return TokenResponse{}, ErrInvalidRequest
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		return s.authorizationCode(ctx, client, form)
	case "refresh_token":
		return s.refresh(ctx, client, form.Get("refresh_token"))
	case "client_credentials":
		if client.PublicClient || !containsWord(client.GrantTypes, "client_credentials") || !allowedScopes(client.Scopes, form.Get("scope")) {
			return TokenResponse{}, ErrInvalidRequest
		}
		scope := normalizeWords(form.Get("scope"))
		token, expires, err := s.authn.IssueTenantOAuthClientToken(ctx, client.ID, client.TenantID, "", scope, client.Name)
		return TokenResponse{AccessToken: token, TokenType: "Bearer", ExpiresIn: expires, Scope: scope}, err
	default:
		return TokenResponse{}, ErrInvalidRequest
	}
}

func (s *Service) serviceAccountToken(ctx context.Context, id, secret, requestedScope string) (TokenResponse, error) {
	var account serviceAccountClient
	err := s.db.NewSelect().TableExpr("iam_service_account_credentials AS credential").
		ColumnExpr("credential.id AS credential_id, credential.principal_id AS principal_id, credential.secret_hash AS secret_hash, credential.scopes AS scopes").
		ColumnExpr("credential.expires_at AS credential_expires_at, credential.revoked_at AS credential_revoked_at").
		ColumnExpr("account.owner_tenant_id AS tenant_id, account.status AS account_status, account.expires_at AS account_expires_at, account.token_version AS token_version").
		ColumnExpr("principal.login_name AS login_name, principal.status AS principal_status, member.status AS member_status, tenant.status AS tenant_status").
		Join("JOIN iam_service_accounts AS account ON account.principal_id = credential.principal_id").
		Join("JOIN iam_principals AS principal ON principal.id = account.principal_id").
		Join("JOIN iam_tenant_members AS member ON member.tenant_id = account.owner_tenant_id AND member.user_subject = account.principal_id").
		Join("JOIN iam_tenants AS tenant ON tenant.id = account.owner_tenant_id").
		Where("credential.id = ?", id).Scan(ctx, &account)
	if err != nil {
		return TokenResponse{}, ErrInvalidRequest
	}
	now := s.now().UTC().UnixMilli()
	valid, verifyErr := passwordx.Verify(account.SecretHash, secret)
	if verifyErr != nil || !valid || account.CredentialRevokedAt != 0 || account.CredentialExpiresAt > 0 && account.CredentialExpiresAt <= now ||
		account.AccountExpiresAt > 0 && account.AccountExpiresAt <= now || account.AccountStatus != "active" || account.PrincipalStatus != "active" ||
		account.MemberStatus != "active" || account.TenantStatus != "active" {
		return TokenResponse{}, ErrInvalidRequest
	}
	scope := normalizeWords(requestedScope)
	if scope == "" {
		scope = normalizeWords(account.Scopes)
	}
	if !allowedScopes(account.Scopes, scope) {
		return TokenResponse{}, ErrInvalidRequest
	}
	token, expires, err := s.authn.IssueTenantServiceAccountToken(ctx, account.PrincipalID, account.TenantID, "", scope, account.LoginName, account.TokenVersion)
	if err != nil {
		return TokenResponse{}, err
	}
	result, err := s.db.NewUpdate().Table("iam_service_account_credentials").Set("last_used_at = ?", now).Where("id = ? AND revoked_at = 0", id).Exec(ctx)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("update service account credential use: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return TokenResponse{}, ErrInvalidRequest
	}
	return TokenResponse{AccessToken: token, TokenType: "Bearer", ExpiresIn: expires, Scope: scope}, nil
}

func (s *Service) authorizationCode(ctx context.Context, client clientRow, form url.Values) (TokenResponse, error) {
	code := form.Get("code")
	verifier := form.Get("code_verifier")
	var row codeRow
	if code == "" || verifier == "" || s.db.NewSelect().Model(&row).Where("code_hash = ? AND client_id = ?", hashToken(code), client.ID).Scan(ctx) != nil {
		return TokenResponse{}, ErrInvalidRequest
	}
	now := s.now().UTC().UnixMilli()
	if row.ConsumedAt != 0 || row.ExpiresAt <= now || row.RedirectURI != form.Get("redirect_uri") || !verifyPKCE(row.CodeChallenge, verifier) {
		return TokenResponse{}, ErrInvalidRequest
	}
	if active, err := s.activeMember(ctx, client.TenantID, row.PrincipalID); err != nil || !active {
		return TokenResponse{}, ErrInvalidRequest
	}
	assurance := oauthAssurance(row.AuthTime, row.ACR, row.AMR, client.ID)
	access, expires, err := s.authn.IssueTenantAccessTokenWithAssurance(ctx, row.PrincipalID, client.TenantID, "", row.Scope, assurance)
	if err != nil {
		return TokenResponse{}, err
	}
	response := TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: expires, Scope: row.Scope}
	var refresh refreshRow
	if containsWord(client.GrantTypes, "refresh_token") {
		response.RefreshToken, refresh, err = s.makeRefresh(row.PrincipalID, client.ID, row.Scope, "", assurance)
		if err != nil {
			return TokenResponse{}, err
		}
	}
	if containsWord(row.Scope, "openid") {
		response.IDToken, err = s.authn.IssueTenantIDTokenWithAssurance(ctx, row.PrincipalID, client.TenantID, client.ID, row.Nonce, assurance)
		if err != nil {
			return TokenResponse{}, err
		}
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*codeRow)(nil)).Set("consumed_at = ?", now).Where("code_hash = ? AND consumed_at = 0", row.CodeHash).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrInvalidRequest
		}
		if response.RefreshToken != "" {
			if _, err := tx.NewInsert().Model(&refresh).Exec(ctx); err != nil {
				return fmt.Errorf("store refresh token: %w", err)
			}
		}
		return nil
	})
	return response, err
}

func (s *Service) refresh(ctx context.Context, client clientRow, token string) (TokenResponse, error) {
	if token == "" || !containsWord(client.GrantTypes, "refresh_token") {
		return TokenResponse{}, ErrInvalidRequest
	}
	var row refreshRow
	err := s.db.NewSelect().Model(&row).Where("id_hash = ? AND client_id = ?", hashToken(token), client.ID).Scan(ctx)
	if err != nil {
		return TokenResponse{}, ErrInvalidRequest
	}
	now := s.now().UTC().UnixMilli()
	if row.UsedAt != 0 || row.RevokedAt != 0 {
		_, _ = s.db.NewUpdate().Model((*refreshRow)(nil)).Set("revoked_at = ?", now).Where("family_id = ? AND revoked_at = 0", row.FamilyID).Exec(ctx)
		return TokenResponse{}, ErrInvalidRequest
	}
	if row.ExpiresAt <= now {
		return TokenResponse{}, ErrInvalidRequest
	}
	if active, err := s.activeMember(ctx, client.TenantID, row.PrincipalID); err != nil || !active {
		return TokenResponse{}, ErrInvalidRequest
	}
	assurance := oauthAssurance(row.AuthTime, row.ACR, row.AMR, client.ID)
	next, nextRow, err := s.makeRefresh(row.PrincipalID, client.ID, row.Scope, row.FamilyID, assurance)
	if err != nil {
		return TokenResponse{}, err
	}
	access, expires, err := s.authn.IssueTenantAccessTokenWithAssurance(ctx, row.PrincipalID, client.TenantID, "", row.Scope, assurance)
	if err != nil {
		return TokenResponse{}, err
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*refreshRow)(nil)).Set("used_at = ?", now).Where("id_hash = ? AND used_at = 0 AND revoked_at = 0", row.IDHash).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrInvalidRequest
		}
		if _, err := tx.NewInsert().Model(&nextRow).Exec(ctx); err != nil {
			return fmt.Errorf("store refresh token: %w", err)
		}
		return nil
	})
	return TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: expires, RefreshToken: next, Scope: row.Scope}, err
}

func (s *Service) makeRefresh(principalID, clientID, scope, familyID string, assurance authnext.Assurance) (string, refreshRow, error) {
	token, err := secureToken(32)
	if err != nil {
		return "", refreshRow{}, err
	}
	if familyID == "" {
		familyID, err = secureToken(18)
		if err != nil {
			return "", refreshRow{}, err
		}
	}
	now := s.now().UTC()
	row := refreshRow{
		IDHash: hashToken(token), FamilyID: familyID, PrincipalID: principalID, ClientID: clientID, Scope: scope,
		AuthTime: assurance.AuthTime.UTC().UnixMilli(), ACR: assurance.Level, AMR: strings.Join(assurance.Methods, " "),
		CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(30 * 24 * time.Hour).UnixMilli(),
	}
	return token, row, nil
}

func oauthAssurance(authTime int64, acr int, amr, clientID string) authnext.Assurance {
	assurance := authnext.Assurance{Level: acr, Methods: strings.Fields(amr), ClientID: clientID}
	if authTime > 0 {
		assurance.AuthTime = time.UnixMilli(authTime).UTC()
	}
	return assurance
}

func (s *Service) Revoke(ctx context.Context, form url.Values, authorization string) error {
	client, err := s.authenticateClient(ctx, form.Get("client_id"), form.Get("client_secret"), authorization)
	if err != nil {
		return ErrInvalidRequest
	}
	token := form.Get("token")
	if token == "" {
		return nil
	}
	now := s.now().UTC().UnixMilli()
	var row refreshRow
	if s.db.NewSelect().Model(&row).Where("id_hash = ? AND client_id = ?", hashToken(token), client.ID).Scan(ctx) == nil {
		_, _ = s.db.NewUpdate().Model((*refreshRow)(nil)).Set("revoked_at = ?", now).Where("family_id = ? AND revoked_at = 0", row.FamilyID).Exec(ctx)
	}
	return nil
}

func (s *Service) Introspect(ctx context.Context, form url.Values, authorization string) (map[string]any, error) {
	client, err := s.authenticateClient(ctx, form.Get("client_id"), form.Get("client_secret"), authorization)
	if err != nil || client.PublicClient {
		return nil, ErrInvalidRequest
	}
	claims, err := s.authn.Authenticate(ctx, "Bearer "+form.Get("token"), "")
	if err != nil {
		// RFC 7662 §2.2: an invalid or expired token is reported as active:false,
		// not as an error, so introspection responses stay stable for clients.
		return map[string]any{"active": false}, nil //nolint:nilerr // RFC 7662: invalid token => active:false, nil error
	}
	return map[string]any{"active": true, "sub": claims.Subject, "iss": claims.Issuer, "aud": claims.Audience, "exp": claims.ExpiresAt.Unix()}, nil
}

func (s *Service) client(ctx context.Context, id string) (clientRow, error) {
	var row clientRow
	if id == "" {
		return row, ErrInvalidRequest
	}
	err := s.db.NewSelect().Model(&row).Where("id = ? AND status = 'active'", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrInvalidRequest
	}
	return row, err
}

func (s *Service) activeMember(ctx context.Context, tenantID, principalID string) (bool, error) {
	count, err := s.db.NewSelect().Table("iam_tenant_members").Where("tenant_id = ? AND user_subject = ? AND status = 'active'", tenantID, principalID).Count(ctx)
	return count == 1, err
}

func (s *Service) authenticateClient(ctx context.Context, formID, formSecret, authorization string) (clientRow, error) {
	id, secret, err := clientCredentials(formID, formSecret, authorization)
	if err != nil {
		return clientRow{}, ErrInvalidRequest
	}
	client, err := s.client(ctx, id)
	if err != nil {
		return clientRow{}, err
	}
	if client.PublicClient {
		if secret != "" {
			return clientRow{}, ErrInvalidRequest
		}
		return client, nil
	}
	valid, err := passwordx.Verify(client.SecretHash, secret)
	if err != nil || !valid {
		return clientRow{}, ErrInvalidRequest
	}
	return client, nil
}

func clientCredentials(formID, formSecret, authorization string) (string, string, error) {
	id, secret := formID, formSecret
	if strings.HasPrefix(strings.ToLower(authorization), "basic ") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(authorization[6:]))
		if err != nil {
			return "", "", err
		}
		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 {
			return "", "", ErrInvalidRequest
		}
		id, err = url.QueryUnescape(parts[0])
		if err != nil {
			return "", "", ErrInvalidRequest
		}
		secret, err = url.QueryUnescape(parts[1])
		if err != nil {
			return "", "", ErrInvalidRequest
		}
	}
	if id == "" {
		return "", "", ErrInvalidRequest
	}
	return id, secret, nil
}

func allowedRedirect(encoded, redirectURI string) bool {
	var values []string
	return json.Unmarshal([]byte(encoded), &values) == nil && slices.Contains(values, redirectURI)
}

func validateClient(tenantID, name string, redirects, grants, scopes []string, public bool) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(name) == "" || len(name) > 128 || len(grants) == 0 || len(scopes) == 0 {
		return ErrInvalidRequest
	}
	allowedGrants := []string{"authorization_code", "refresh_token", "client_credentials"}
	for _, grant := range grants {
		if !slices.Contains(allowedGrants, grant) || (public && grant == "client_credentials") {
			return ErrInvalidRequest
		}
	}
	if containsWord(strings.Join(grants, " "), "authorization_code") && len(redirects) == 0 {
		return ErrInvalidRequest
	}
	for _, value := range redirects {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Fragment != "" {
			return ErrInvalidRequest
		}
	}
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "" || strings.ContainsAny(scope, " \t\r\n") {
			return ErrInvalidRequest
		}
	}
	return nil
}

func clientFromRow(row clientRow) Client {
	var redirects []string
	_ = json.Unmarshal([]byte(row.RedirectURIs), &redirects)
	return Client{ID: row.ID, TenantID: row.TenantID, Name: row.Name, RedirectURIs: redirects, GrantTypes: strings.Fields(strings.ReplaceAll(row.GrantTypes, ",", " ")), Scopes: strings.Fields(row.Scopes), PublicClient: row.PublicClient, Status: row.Status}
}

func allowedScopes(allowed, requested string) bool {
	for _, scope := range strings.Fields(requested) {
		if !containsWord(allowed, scope) {
			return false
		}
	}
	return true
}

func containsWord(words, target string) bool {
	return slices.Contains(strings.Fields(strings.ReplaceAll(words, ",", " ")), target)
}

func normalizeWords(value string) string {
	words := strings.Fields(value)
	slices.Sort(words)
	words = slices.Compact(words)
	return strings.Join(words, " ")
}

func verifyPKCE(challenge, verifier string) bool {
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

func secureToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
