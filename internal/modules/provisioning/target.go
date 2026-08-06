package provisioning

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/uptrace/bun"
)

const (
	// provisioningCipherVersion versions the SCIM target bearer-token envelope.
	provisioningCipherVersion = "v1"

	// TargetActive and TargetDisabled are the outbound SCIM target states.
	TargetActive   = DirectoryActive
	TargetDisabled = DirectoryDisabled

	// maxRemoteResponseBytes bounds the SCIM provider response body read.
	maxRemoteResponseBytes = 4 << 20
)

var (
	ErrInvalidTarget      = errors.New("invalid SCIM target")
	ErrTargetMissing      = errors.New("SCIM target not found")
	ErrTargetName         = errors.New("SCIM target name conflict")
	ErrTargetVersion      = errors.New("SCIM target version conflict")
	ErrTargetKeyMissing   = errors.New("SCIM target encryption key not configured")
	ErrTargetDisabled     = errors.New("SCIM target is disabled")
	ErrRemoteUnavailable  = errors.New("SCIM remote request failed")
	ErrRemoteResponse     = errors.New("SCIM remote rejected the request")
	ErrDeprovisionMissing = errors.New("SCIM remote resource has no active mapping")
)

// Target is an outbound SCIM endpoint managed per tenant.
type Target struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	BaseURL   string    `json:"base_url"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TargetSecret wraps a target with its bearer token, shown once on creation.
type TargetSecret struct {
	Target      Target `json:"target"`
	BearerToken string `json:"bearer_token" doc:"Shown once on creation. Store it in a secret manager."`
}

// PushResult reports a completed outbound push and the resulting mapping.
type PushResult struct {
	TargetID     string `json:"target_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	ExternalID   string `json:"external_id"`
	RemoteID     string `json:"remote_id,omitempty"`
}

// remoteResource is a SCIM payload ready to be PUT to a target.
type remoteResource struct {
	externalID string
	resource   any
}

func (s *Service) ListTargets(ctx context.Context, tenantID string) ([]Target, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || len(tenantID) > 128 {
		return nil, ErrInvalidTarget
	}
	rows, err := s.repo.listTargets(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]Target, 0, len(rows))
	for _, row := range rows {
		result = append(result, targetFromRow(row))
	}
	return result, nil
}

func (s *Service) CreateTarget(ctx context.Context, tenantID, name, baseURL, bearerToken string) (TargetSecret, error) {
	tenantID, name, baseURL, bearerToken = strings.TrimSpace(tenantID), strings.TrimSpace(name), strings.TrimSpace(baseURL), strings.TrimSpace(bearerToken)
	if tenantID == "" || len(tenantID) > 128 || name == "" || len(name) > 128 || bearerToken == "" || len(bearerToken) > 4096 || !validTargetURL(baseURL) {
		return TargetSecret{}, ErrInvalidTarget
	}
	if len(s.key) == 0 {
		return TargetSecret{}, ErrTargetKeyMissing
	}
	id, err := s.nextID()
	if err != nil || strings.TrimSpace(id) == "" || len(id) > 128 {
		return TargetSecret{}, fmt.Errorf("generate SCIM target id: %w", firstError(err, ErrInvalidTarget))
	}
	ciphertext, err := s.encryptToken(id, bearerToken)
	if err != nil {
		return TargetSecret{}, err
	}
	now := s.now().UTC().UnixMilli()
	row := targetRow{ID: id, TenantID: tenantID, Name: name, NameKey: strings.ToLower(name), BaseURL: baseURL, BearerTokenCiphertext: ciphertext, Status: TargetActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	event := auditx.NewEvent(ctx, tenantID, "scim_target_created", "scim_target", id)
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		active, err := tx.NewSelect().Table("iam_tenants").Where("id = ? AND status = 'active'", tenantID).Exists(ctx)
		if err != nil {
			return err
		}
		if !active {
			return ErrInvalidTarget
		}
		if err := s.repo.withExecutor(tx).insertTarget(ctx, &row); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return TargetSecret{}, fmt.Errorf("create SCIM target: %w", err)
	}
	return TargetSecret{Target: targetFromRow(row), BearerToken: bearerToken}, nil
}

func (s *Service) ReplaceTarget(ctx context.Context, tenantID, id, name, baseURL, status, bearerToken string, version int64) (Target, error) {
	tenantID, id, name, baseURL, status, bearerToken = strings.TrimSpace(tenantID), strings.TrimSpace(id), strings.TrimSpace(name), strings.TrimSpace(baseURL), strings.TrimSpace(status), strings.TrimSpace(bearerToken)
	if tenantID == "" || id == "" || name == "" || len(name) > 128 || !validTargetURL(baseURL) || len(bearerToken) > 4096 || version < 1 || (status != TargetActive && status != TargetDisabled) {
		return Target{}, ErrInvalidTarget
	}
	now := s.now().UTC().UnixMilli()
	var updated targetRow
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		current, err := repo.getTarget(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return ErrTargetVersion
		}
		updated = current
		updated.Name, updated.NameKey, updated.BaseURL, updated.Status = name, strings.ToLower(name), baseURL, status
		if bearerToken != "" {
			if len(s.key) == 0 {
				return ErrTargetKeyMissing
			}
			updated.BearerTokenCiphertext, err = s.encryptToken(id, bearerToken)
			if err != nil {
				return err
			}
		}
		if updated.Name == current.Name && updated.BaseURL == current.BaseURL && updated.Status == current.Status && bearerToken == "" {
			return nil
		}
		updated.Version, updated.UpdatedAt = current.Version+1, now
		if err := repo.replaceTarget(ctx, &updated, current.Version); err != nil {
			return err
		}
		event := auditx.NewEvent(ctx, tenantID, "scim_target_updated", "scim_target", id)
		event.Detail["name_changed"] = updated.Name != current.Name
		event.Detail["status_changed"] = updated.Status != current.Status
		event.Detail["token_rotated"] = bearerToken != ""
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Target{}, fmt.Errorf("replace SCIM target: %w", err)
	}
	return targetFromRow(updated), nil
}

func (s *Service) DeleteTarget(ctx context.Context, tenantID, id string) error {
	tenantID, id = strings.TrimSpace(tenantID), strings.TrimSpace(id)
	if tenantID == "" || id == "" {
		return ErrInvalidTarget
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		if _, err := repo.getTarget(ctx, tenantID, id); err != nil {
			return err
		}
		if err := repo.deleteTarget(ctx, id); err != nil {
			return err
		}
		return s.audit(ctx, tx, auditx.NewEvent(ctx, tenantID, "scim_target_deleted", "scim_target", id))
	})
	if err != nil {
		return fmt.Errorf("delete SCIM target: %w", err)
	}
	return nil
}

// PushResource PUTs the current local user or group to the target. The mapping
// is upserted with the remote id so later pushes and deprovisioning reuse it.
func (s *Service) PushResource(ctx context.Context, tenantID, targetID, resourceType, resourceID string) (PushResult, error) {
	tenantID, targetID, resourceType, resourceID = strings.TrimSpace(tenantID), strings.TrimSpace(targetID), strings.TrimSpace(resourceType), strings.TrimSpace(resourceID)
	if tenantID == "" || targetID == "" || resourceID == "" || len(targetID) > 128 || len(resourceID) > 128 || (resourceType != ResourceUser && resourceType != ResourceGroup) {
		return PushResult{}, ErrInvalidTarget
	}
	target, err := s.repo.getTarget(ctx, tenantID, targetID)
	if err != nil {
		return PushResult{}, err
	}
	if target.Status != TargetActive {
		return PushResult{}, ErrTargetDisabled
	}
	if len(s.key) == 0 {
		return PushResult{}, ErrTargetKeyMissing
	}
	bearer, err := s.decryptToken(targetID, target.BearerTokenCiphertext)
	if err != nil {
		return PushResult{}, err
	}
	payload, err := s.buildRemoteResource(ctx, tenantID, targetID, resourceType, resourceID)
	if err != nil {
		return PushResult{}, err
	}
	body, err := json.Marshal(payload.resource)
	if err != nil {
		return PushResult{}, fmt.Errorf("encode SCIM push payload: %w", err)
	}
	endpoint := strings.TrimSuffix(target.BaseURL, "/") + "/" + resourceType + "s/" + url.PathEscape(payload.externalID)
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return PushResult{}, fmt.Errorf("build SCIM push request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Content-Type", SCIMContentType)
	request.Header.Set("Accept", SCIMContentType)
	response, err := s.http.Do(request)
	if err != nil {
		return PushResult{}, fmt.Errorf("%w: %v", ErrRemoteUnavailable, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxRemoteResponseBytes))
	if err != nil {
		return PushResult{}, fmt.Errorf("read SCIM push response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PushResult{}, fmt.Errorf("%w: status %d: %s", ErrRemoteResponse, response.StatusCode, trimRemoteDetail(responseBody))
	}
	remoteID := payload.externalID
	var created struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(responseBody, &created) == nil && created.ID != "" {
		remoteID = created.ID
	}
	now := s.now().UTC().UnixMilli()
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row := targetResourceRow{TargetID: targetID, ResourceType: resourceType, ResourceID: resourceID, ExternalID: remoteID, Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := s.repo.withExecutor(tx).upsertTargetResource(ctx, &row); err != nil {
			return err
		}
		event := auditx.NewEvent(ctx, tenantID, "scim_target_pushed", "scim_"+strings.ToLower(resourceType), resourceID)
		event.Detail["target_id"] = targetID
		event.Detail["external_id"] = remoteID
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return PushResult{}, fmt.Errorf("record SCIM push mapping: %w", err)
	}
	return PushResult{TargetID: targetID, ResourceType: resourceType, ResourceID: resourceID, ExternalID: payload.externalID, RemoteID: remoteID}, nil
}

// DeprovisionResource DELETEs the mapped remote resource and soft-deletes the
// mapping so a later push re-creates the resource on the target.
func (s *Service) DeprovisionResource(ctx context.Context, tenantID, targetID, resourceType, resourceID string) error {
	tenantID, targetID, resourceType, resourceID = strings.TrimSpace(tenantID), strings.TrimSpace(targetID), strings.TrimSpace(resourceType), strings.TrimSpace(resourceID)
	if tenantID == "" || targetID == "" || resourceID == "" || len(targetID) > 128 || len(resourceID) > 128 || (resourceType != ResourceUser && resourceType != ResourceGroup) {
		return ErrInvalidTarget
	}
	target, err := s.repo.getTarget(ctx, tenantID, targetID)
	if err != nil {
		return err
	}
	if target.Status != TargetActive {
		return ErrTargetDisabled
	}
	if len(s.key) == 0 {
		return ErrTargetKeyMissing
	}
	mapping, err := s.repo.getTargetResource(ctx, targetID, resourceType, resourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDeprovisionMissing
		}
		return err
	}
	if mapping.DeletedAt != 0 {
		return ErrDeprovisionMissing
	}
	bearer, err := s.decryptToken(targetID, target.BearerTokenCiphertext)
	if err != nil {
		return err
	}
	endpoint := strings.TrimSuffix(target.BaseURL, "/") + "/" + resourceType + "s/" + url.PathEscape(mapping.ExternalID)
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build SCIM deprovision request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Accept", SCIMContentType)
	response, err := s.http.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRemoteUnavailable, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxRemoteResponseBytes))
	if err != nil {
		return err
	}
	if (response.StatusCode < 200 || response.StatusCode >= 300) && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("%w: status %d: %s", ErrRemoteResponse, response.StatusCode, trimRemoteDetail(responseBody))
	}
	now := s.now().UTC().UnixMilli()
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := s.repo.withExecutor(tx).markTargetResourceDeleted(ctx, targetID, resourceType, resourceID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, auditx.NewEvent(ctx, tenantID, "scim_target_deprovisioned", "scim_"+strings.ToLower(resourceType), resourceID))
	})
	if err != nil {
		return fmt.Errorf("record SCIM deprovision: %w", err)
	}
	return nil
}

func (s *Service) buildRemoteResource(ctx context.Context, tenantID, targetID, resourceType, resourceID string) (remoteResource, error) {
	externalID := resourceID
	if mapping, err := s.repo.getTargetResource(ctx, targetID, resourceType, resourceID); err == nil && mapping.ExternalID != "" {
		externalID = mapping.ExternalID
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return remoteResource{}, err
	}
	switch resourceType {
	case ResourceUser:
		principal, err := s.identity.Get(ctx, tenantID, resourceID)
		if err != nil {
			if errors.Is(err, identity.ErrNotFound) {
				return remoteResource{}, ErrResourceMissing
			}
			return remoteResource{}, err
		}
		user := UserResource{
			Schemas:     []string{UserSchema},
			ID:          externalID,
			ExternalID:  resourceID,
			UserName:    principal.LoginName,
			DisplayName: principal.DisplayName,
			Active:      principal.Status == "active",
			Meta:        ResourceMeta{ResourceType: "User"},
		}
		if principal.Email != "" {
			user.Emails = []UserEmail{{Value: principal.Email, Type: "work", Primary: true}}
		}
		return remoteResource{externalID: externalID, resource: user}, nil
	case ResourceGroup:
		group, err := s.groups.Get(ctx, tenantID, resourceID)
		if err != nil {
			if errors.Is(err, organization.ErrGroupNotFound) {
				return remoteResource{}, ErrResourceMissing
			}
			return remoteResource{}, err
		}
		members, err := s.groups.ListMembers(ctx, tenantID, resourceID)
		if err != nil {
			return remoteResource{}, err
		}
		mapped := make([]SCIMGroupMember, 0, len(members))
		for _, member := range members {
			memberMapping, err := s.repo.getTargetResource(ctx, targetID, ResourceUser, member.PrincipalID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return remoteResource{}, err
			}
			if memberMapping.DeletedAt == 0 && memberMapping.ExternalID != "" {
				mapped = append(mapped, SCIMGroupMember{Value: memberMapping.ExternalID})
			}
		}
		resource := GroupResource{
			Schemas:     []string{GroupSchema},
			ID:          externalID,
			ExternalID:  resourceID,
			DisplayName: group.Name,
			Members:     mapped,
			Meta:        ResourceMeta{ResourceType: "Group"},
		}
		return remoteResource{externalID: externalID, resource: resource}, nil
	}
	return remoteResource{}, ErrInvalidTarget
}

func (s *Service) encryptToken(targetID, token string) (string, error) {
	block, err := aes.NewCipher(purposeKey(s.key, "scim-target-bearer"))
	if err != nil {
		return "", fmt.Errorf("create provisioning cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create provisioning AEAD: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate provisioning nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(token), []byte("chaosplus:provisioning:secret\x00"+targetID))
	return provisioningCipherVersion + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Service) decryptToken(targetID, encoded string) (string, error) {
	version, payload, ok := strings.Cut(encoded, ".")
	if !ok || version != provisioningCipherVersion || payload == "" {
		return "", errors.New("unsupported provisioning ciphertext")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", errors.New("invalid provisioning ciphertext")
	}
	block, err := aes.NewCipher(purposeKey(s.key, "scim-target-bearer"))
	if err != nil {
		return "", fmt.Errorf("create provisioning cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create provisioning AEAD: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("invalid provisioning ciphertext")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte("chaosplus:provisioning:secret\x00"+targetID))
	if err != nil {
		return "", errors.New("decrypt provisioning token")
	}
	return string(plain), nil
}

// purposeKey derives a domain-separated key so one provisioning encryption key
// never encrypts two different kinds of payload with the same key material.
func purposeKey(key []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("chaosplus:provisioning:key:" + purpose))
	return mac.Sum(nil)
}

func targetFromRow(row targetRow) Target {
	return Target{ID: row.ID, TenantID: row.TenantID, Name: row.Name, BaseURL: row.BaseURL, Status: row.Status, Version: row.Version, CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}
}

func validTargetURL(raw string) bool {
	if raw == "" || len(raw) > 1024 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	return true
}

func trimRemoteDetail(body []byte) string {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 500 {
		detail = detail[:500] + "..."
	}
	return strings.ReplaceAll(detail, "\n", " ")
}
