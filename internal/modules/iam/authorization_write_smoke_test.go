package iam_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	iamapi "github.com/chaos-plus/chaosplus/internal/modules/iam/api"
)

// TestAuthorizationWriteSmoke proves the complete remote path with real
// Zitadel JWT validation and SpiceDB. It is intentionally opt-in because tokens
// are short-lived secrets and the normal unit suite must remain hermetic.
func TestAuthorizationWriteSmoke(t *testing.T) {
	if os.Getenv("IAM_AUTHZ_SMOKE") != "1" {
		t.Skip("set IAM_AUTHZ_SMOKE=1 and the documented remote environment variables")
	}
	ctx := context.Background()
	issuer := requiredEnv(t, "ZITADEL_ISSUER")
	adminToken := requiredEnv(t, "ZITADEL_ADMIN_TOKEN")
	targetToken := requiredEnv(t, "ZITADEL_TARGET_TOKEN")
	audience := splitNonEmpty(os.Getenv("ZITADEL_AUDIENCE"))

	verifier, err := authn.NewVerifier(authn.Config{Enabled: true, Issuer: issuer, Audience: audience})
	require.NoError(t, err)
	adminClaims, err := verifier.VerifyAuthorization(ctx, "Bearer "+adminToken)
	require.NoError(t, err, "admin JWT must be valid")
	targetClaims, err := verifier.VerifyAuthorization(ctx, "Bearer "+targetToken)
	require.NoError(t, err, "target JWT must be valid")
	require.NotEqual(t, adminClaims.Subject, targetClaims.Subject)

	client, err := spicedbx.Open(spicedbx.Config{
		Endpoint: requiredEnv(t, "SPICEDB_ENDPOINT"),
		Token:    requiredEnv(t, "SPICEDB_TOKEN"),
		Insecure: os.Getenv("SPICEDB_INSECURE") != "0",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	registry := authz.DefaultRegistry()
	_, err = client.WriteSchema(ctx, authz.GenerateSchema(registry.All()))
	require.NoError(t, err)

	runHash := shortHash(envOr("IAM_AUTHZ_SMOKE_RUN_ID", "local"))
	tenantID := "iam-smoke-" + runHash
	roleID := "role-" + runHash + "-1"
	adminRel := spicedbx.Relationship{Resource: spicedbx.ObjectRef{Type: "tenant", ID: tenantID}, Relation: "admin", Subject: adminClaims.SubjectRef()}
	grantRel := spicedbx.Relationship{Resource: spicedbx.ObjectRef{Type: "tenant", ID: tenantID}, Relation: "role_view_role", Subject: spicedbx.SubjectRef{Object: spicedbx.ObjectRef{Type: "role", ID: roleID}, Relation: "member"}}
	memberRel := spicedbx.Relationship{Resource: spicedbx.ObjectRef{Type: "role", ID: roleID}, Relation: "member", Subject: targetClaims.SubjectRef()}
	cleanup := func() {
		_, _ = client.WriteRelationshipUpdates(ctx, relationshipUpdates(spicedbx.RelationshipDelete, adminRel, grantRel, memberRel))
	}
	cleanup()
	t.Cleanup(cleanup)
	_, err = client.WriteRelationshipUpdates(ctx, relationshipUpdates(spicedbx.RelationshipTouch, adminRel))
	require.NoError(t, err)

	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(ctx, db))
	var sequence atomic.Int64
	nextID := func() (string, error) {
		n := sequence.Add(1)
		if n == 1 {
			return roleID, nil
		}
		return fmt.Sprintf("%s-%d", runHash, n), nil
	}
	repo := iam.NewRepository(db, nextID)
	_, err = repo.PutMember(ctx, iam.TenantMember{TenantID: tenantID, Subject: adminClaims.Subject, DisplayName: "Smoke admin", Status: iam.MemberActive})
	require.NoError(t, err)
	_, err = repo.PutMember(ctx, iam.TenantMember{TenantID: tenantID, Subject: targetClaims.Subject, DisplayName: "Smoke target", Status: iam.MemberActive})
	require.NoError(t, err)
	worker := iam.NewOutboxWorker(repo, client, iam.OutboxConfig{}, nextID)
	service := iam.NewService(registry, repo, worker, client)
	registrar := authz.NewRegistrar(registry, verifier, client, iam.NewMembershipChecker(db))
	router := chi.NewMux()
	respx.Install()
	api := humachi.New(router, huma.DefaultConfig("iam-smoke", "1.0.0"))
	iamapi.RegisterREST(api, service, registrar)

	targetAuthorization := "Bearer " + targetToken
	adminAuthorization := "Bearer " + adminToken
	require.Equal(t, http.StatusForbidden, smokeRequest(router, http.MethodGet, "/iam/permission-catalog", tenantID, targetAuthorization, nil).Code)

	created := smokeRequest(router, http.MethodPost, "/iam/roles", tenantID, adminAuthorization, map[string]any{"name": "Smoke role"})
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var createdBody struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createdBody))
	require.Equal(t, roleID, createdBody.Data.ID)

	require.Equal(t, http.StatusOK, smokeRequest(router, http.MethodPut, "/iam/roles/"+roleID+"/permissions/role_view", tenantID, adminAuthorization, nil).Code)
	require.Equal(t, http.StatusOK, smokeRequest(router, http.MethodPut, "/iam/roles/"+roleID+"/members/"+targetClaims.Subject, tenantID, adminAuthorization, nil).Code)
	require.NoError(t, worker.RunOnce(ctx))
	require.Equal(t, http.StatusOK, smokeRequest(router, http.MethodGet, "/iam/permission-catalog", tenantID, targetAuthorization, nil).Code)

	require.Equal(t, http.StatusOK, smokeRequest(router, http.MethodDelete, "/iam/roles/"+roleID+"/permissions/role_view", tenantID, adminAuthorization, nil).Code)
	require.NoError(t, worker.RunOnce(ctx))
	require.Equal(t, http.StatusForbidden, smokeRequest(router, http.MethodGet, "/iam/permission-catalog", tenantID, targetAuthorization, nil).Code)
}

func smokeRequest(handler http.Handler, method, path, tenantID, authorization string, body any) *httptest.ResponseRecorder {
	var encoded *bytes.Reader
	if body == nil {
		encoded = bytes.NewReader(nil)
	} else {
		data, _ := json.Marshal(body)
		encoded = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, encoded)
	req.Header.Set(authz.TenantHeader, tenantID)
	req.Header.Set("Authorization", authorization)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func relationshipUpdates(operation spicedbx.RelationshipOperation, relationships ...spicedbx.Relationship) []spicedbx.RelationshipUpdate {
	updates := make([]spicedbx.RelationshipUpdate, 0, len(relationships))
	for _, relationship := range relationships {
		updates = append(updates, spicedbx.RelationshipUpdate{Operation: operation, Relationship: relationship})
	}
	return updates
}

func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required when IAM_AUTHZ_SMOKE=1", name)
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func splitNonEmpty(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:4])
}
