package provisioning

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/uptrace/bun"
)

const maxActiveCredentials = 10

type IDGenerator func() (guid.ID, error)

type IdentityProvisioner interface {
	CreateProvisionedTo(context.Context, bun.IDB, guid.ID, identity.ProvisionedPrincipalInput) (identity.Principal, error)
	ReplaceProvisionedTo(context.Context, bun.IDB, guid.ID, guid.ID, identity.ProvisionedPrincipalInput) (identity.Principal, error)
	Get(context.Context, guid.ID, guid.ID) (identity.Principal, error)
}

type GroupProvisioner interface {
	CreateProvisionedTo(context.Context, bun.IDB, guid.ID, organization.ProvisionedGroupInput) (organization.Group, error)
	ReplaceProvisionedTo(context.Context, bun.IDB, guid.ID, guid.ID, organization.ProvisionedGroupInput) (organization.Group, error)
	DisableProvisionedTo(context.Context, bun.IDB, guid.ID, guid.ID) (organization.Group, error)
	Get(context.Context, guid.ID, guid.ID) (organization.Group, error)
	ListMembers(context.Context, guid.ID, guid.ID) ([]organization.GroupMember, error)
}

type Service struct {
	db       *bun.DB
	repo     *Repository
	audit    auditx.Appender
	nextID   IDGenerator
	identity IdentityProvisioner
	groups   GroupProvisioner
	key      []byte
	http     *http.Client
	now      func() time.Time

	// ponytail: per-resource mutex serializes push/deprovision to prevent
	// remote state drift; a distributed lock (DB advisory) replaces this
	// when multi-process deployment is needed.
	pushMu    sync.Mutex
	pushLocks map[string]*sync.Mutex
}

func NewService(db *bun.DB, audit auditx.Appender, nextID IDGenerator, identities IdentityProvisioner, groups GroupProvisioner, cfg Config, key []byte) *Service {
	if db == nil || audit == nil || nextID == nil || identities == nil || groups == nil {
		panic("provisioning service requires database, audit appender, id generator, identity provisioner, and group provisioner")
	}
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Service{db: db, repo: NewRepository(db), audit: audit, nextID: nextID, identity: identities, groups: groups, key: key, http: &http.Client{Timeout: timeout}, now: time.Now, pushLocks: make(map[string]*sync.Mutex)}
}

// lockPushResource serializes push and deprovision operations for a single
// (target, resource_type, resource_id) key so concurrent operations on the
// same resource cannot race to stale remote state.
func (s *Service) lockPushResource(targetID guid.ID, resourceType string, resourceID guid.ID) func() {
	key := targetID.String() + "\x00" + resourceType + "\x00" + resourceID.String()
	s.pushMu.Lock()
	mu, ok := s.pushLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		s.pushLocks[key] = mu
	}
	s.pushMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func (s *Service) ListDirectories(ctx context.Context, tenantID guid.ID) ([]Directory, error) {
	if tenantID.Zero() {
		return nil, ErrInvalidDirectory
	}
	rows, err := s.repo.listDirectories(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]Directory, 0, len(rows))
	for _, row := range rows {
		result = append(result, directoryFromRow(row))
	}
	return result, nil
}

func (s *Service) CreateDirectory(ctx context.Context, tenantID guid.ID, name string) (Directory, error) {
	name = strings.TrimSpace(name)
	if tenantID.Zero() || name == "" || len(name) > 128 {
		return Directory{}, ErrInvalidDirectory
	}
	id, err := s.nextID()
	if err != nil || id.Zero() {
		return Directory{}, fmt.Errorf("generate SCIM directory id: %w", firstError(err, ErrInvalidDirectory))
	}
	now := s.now().UTC().UnixMilli()
	row := directoryRow{ID: id, TenantID: tenantID, Name: name, NameKey: strings.ToLower(name), Status: DirectoryActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	event := auditx.NewEvent(ctx, tenantID, "scim_directory_created", "scim_directory", id)
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		active, err := tx.NewSelect().Table("iam_tenants").Where("id = ? AND status = 'active'", tenantID).Exists(ctx)
		if err != nil {
			return err
		}
		if !active {
			return ErrInvalidDirectory
		}
		if err := s.repo.withExecutor(tx).insertDirectory(ctx, &row); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Directory{}, fmt.Errorf("create SCIM directory: %w", err)
	}
	return directoryFromRow(row), nil
}

func (s *Service) ReplaceDirectory(ctx context.Context, tenantID, id guid.ID, name, status string, version int64) (Directory, error) {
	name, status = strings.TrimSpace(name), strings.TrimSpace(status)
	if tenantID.Zero() || id.Zero() || name == "" || len(name) > 128 || version < 1 || status != DirectoryActive && status != DirectoryDisabled {
		return Directory{}, ErrInvalidDirectory
	}
	now := s.now().UTC().UnixMilli()
	var updated directoryRow
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		current, err := repo.getDirectory(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return ErrDirectoryVersion
		}
		updated = current
		updated.Name, updated.NameKey, updated.Status = name, strings.ToLower(name), status
		if updated.Name == current.Name && updated.Status == current.Status {
			return nil
		}
		updated.Version, updated.UpdatedAt = current.Version+1, now
		if err := repo.updateDirectory(ctx, &updated, current.Version); err != nil {
			return err
		}
		event := auditx.NewEvent(ctx, tenantID, "scim_directory_updated", "scim_directory", id)
		event.Detail["name_changed"] = updated.Name != current.Name
		event.Detail["status_changed"] = updated.Status != current.Status
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Directory{}, fmt.Errorf("replace SCIM directory: %w", err)
	}
	return directoryFromRow(updated), nil
}

func (s *Service) ListCredentials(ctx context.Context, tenantID, directoryID guid.ID) ([]Credential, error) {
	if _, err := s.repo.getDirectory(ctx, tenantID, directoryID); err != nil {
		return nil, err
	}
	rows, err := s.repo.listCredentials(ctx, directoryID)
	if err != nil {
		return nil, err
	}
	result := make([]Credential, 0, len(rows))
	for _, row := range rows {
		result = append(result, credentialFromRow(row))
	}
	return result, nil
}

func (s *Service) CreateCredential(ctx context.Context, tenantID, directoryID guid.ID, name string, expiresAt *time.Time) (CredentialSecret, error) {
	name = strings.TrimSpace(name)
	nowTime := s.now().UTC()
	if tenantID.Zero() || directoryID.Zero() || name == "" || len(name) > 128 || expiresAt != nil && !expiresAt.UTC().After(nowTime) {
		return CredentialSecret{}, ErrInvalidDirectory
	}
	id, err := s.nextID()
	if err != nil || id.Zero() {
		return CredentialSecret{}, firstError(err, ErrInvalidDirectory)
	}
	secret, err := randomTokenSecret()
	if err != nil {
		return CredentialSecret{}, err
	}
	hash, err := passwordx.Hash(secret)
	if err != nil {
		return CredentialSecret{}, err
	}
	now := nowTime.UnixMilli()
	row := credentialRow{ID: id, DirectoryID: directoryID, Name: name, TokenHash: hash, ExpiresAt: unixMillis(expiresAt), CreatedAt: now}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		directory, err := repo.getDirectory(ctx, tenantID, directoryID)
		if err != nil {
			return err
		}
		if directory.Status != DirectoryActive {
			return ErrInvalidDirectory
		}
		count, err := repo.activeCredentialCount(ctx, directoryID, now)
		if err != nil {
			return err
		}
		if count >= maxActiveCredentials {
			return ErrCredentialLimit
		}
		if err := repo.insertCredential(ctx, &row); err != nil {
			return err
		}
		event := auditx.NewEvent(ctx, tenantID, "scim_credential_created", "scim_credential", id)
		event.Detail["directory_id"] = directoryID
		event.Detail["expires_at"] = row.ExpiresAt
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return CredentialSecret{}, fmt.Errorf("create SCIM credential: %w", err)
	}
	return CredentialSecret{Credential: credentialFromRow(row), Token: id.String() + "." + secret}, nil
}

func (s *Service) RevokeCredential(ctx context.Context, tenantID, directoryID, id guid.ID) error {
	if tenantID.Zero() || directoryID.Zero() || id.Zero() {
		return ErrInvalidDirectory
	}
	now := s.now().UTC().UnixMilli()
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		if _, err := repo.getDirectory(ctx, tenantID, directoryID); err != nil {
			return err
		}
		changed, err := repo.revokeCredential(ctx, directoryID, id, now)
		if err != nil || !changed {
			return err
		}
		event := auditx.NewEvent(ctx, tenantID, "scim_credential_revoked", "scim_credential", id)
		event.Detail["directory_id"] = directoryID
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return fmt.Errorf("revoke SCIM credential: %w", err)
	}
	return nil
}

func (s *Service) Authenticate(ctx context.Context, authorization string) (AuthContext, error) {
	fields := strings.Fields(authorization)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return AuthContext{}, ErrUnauthorized
	}
	id, secret, ok := strings.Cut(fields[1], ".")
	if !ok || secret == "" || strings.Contains(secret, ".") {
		return AuthContext{}, ErrUnauthorized
	}
	credentialID, err := guid.Parse(id)
	if err != nil {
		return AuthContext{}, ErrUnauthorized
	}
	row, err := s.repo.credentialForAuth(ctx, credentialID)
	if err != nil {
		return AuthContext{}, err
	}
	now := s.now().UTC().UnixMilli()
	if row.DirectoryStatus != DirectoryActive || row.RevokedAt != 0 || row.ExpiresAt > 0 && row.ExpiresAt <= now {
		return AuthContext{}, ErrUnauthorized
	}
	valid, err := passwordx.Verify(row.TokenHash, secret)
	if err != nil || !valid {
		return AuthContext{}, ErrUnauthorized
	}
	if err := s.repo.touchCredential(ctx, credentialID, now); err != nil {
		return AuthContext{}, fmt.Errorf("record SCIM credential use: %w", err)
	}
	return AuthContext{TenantID: row.TenantID, DirectoryID: row.DirectoryID, CredentialID: row.ID}, nil
}

func directoryFromRow(row directoryRow) Directory {
	return Directory{ID: row.ID, TenantID: row.TenantID, Name: row.Name, Status: row.Status, Version: row.Version, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}
}

func credentialFromRow(row credentialRow) Credential {
	return Credential{ID: row.ID, DirectoryID: row.DirectoryID, Name: row.Name, ExpiresAt: optionalTime(row.ExpiresAt), LastUsedAt: optionalTime(row.LastUsedAt), RevokedAt: optionalTime(row.RevokedAt), CreatedAt: time.UnixMilli(row.CreatedAt).UTC()}
}

func optionalTime(value int64) *time.Time {
	if value == 0 {
		return nil
	}
	result := time.UnixMilli(value).UTC()
	return &result
}

func unixMillis(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UTC().UnixMilli()
}

func randomTokenSecret() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func firstError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func serviceErrorKind(err error) error {
	for _, known := range []error{ErrInvalidDirectory, ErrDirectoryMissing, ErrDirectoryName, ErrDirectoryVersion, ErrCredentialMissing, ErrCredentialLimit, ErrUnauthorized, ErrResourceMissing, ErrResourceConflict, ErrResourceVersion, ErrInvalidSCIM, ErrInvalidFilter, ErrInvalidPath, ErrTooMany, ErrInvalidTarget, ErrTargetMissing, ErrTargetName, ErrTargetVersion, ErrTargetKeyMissing, ErrTargetDisabled, ErrDeprovisionMissing, ErrRemoteUnavailable, ErrRemoteResponse} {
		if errors.Is(err, known) {
			return known
		}
	}
	return err
}
