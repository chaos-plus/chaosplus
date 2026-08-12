package provisioning

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

type scimProviderRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    string
}

type fakeSCIMProvider struct {
	mu       sync.Mutex
	requests []scimProviderRequest
	status   int
	echo     bool
	body     string
}

// TestSyncTargetPushesEveryMappedResource drives the reconciler that the
// background SCIM worker calls on every tick. It had no test at all, so neither
// SyncTarget nor listTargetResources was ever executed.
func TestSyncTargetPushesEveryMappedResource(t *testing.T) {
	env := newProvisioningEnvironment(t)
	provider, providerServer := newFakeSCIMProvider(t, http.StatusOK, "")
	provider.echo = true
	ctx := t.Context()

	created, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", providerServer.URL, "bearer-secret")
	require.NoError(t, err)

	// An empty target reconciles to zero without contacting the provider.
	synced, err := env.service.SyncTarget(ctx, testID("tenant-a"), parseGUID(created.Target.ID))
	require.NoError(t, err)
	assert.Zero(t, synced)
	assert.Zero(t, provider.count())

	// Once resources are mapped, reconciliation re-pushes each of them.
	alice, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-alice", "alice", "alice@example.test"))
	require.NoError(t, err)
	bob, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-bob", "bob", "bob@example.test"))
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(alice.ID))
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(bob.ID))
	require.NoError(t, err)
	before := provider.count()

	synced, err = env.service.SyncTarget(ctx, testID("tenant-a"), parseGUID(created.Target.ID))
	require.NoError(t, err)
	assert.Equal(t, 2, synced)
	assert.Greater(t, provider.count(), before, "reconciliation must reach the provider")

	// Invalid and unknown targets fail before any network call.
	_, err = env.service.SyncTarget(ctx, 0, parseGUID(created.Target.ID))
	assert.ErrorIs(t, err, ErrInvalidTarget)
	_, err = env.service.SyncTarget(ctx, testID("tenant-a"), 0)
	assert.ErrorIs(t, err, ErrInvalidTarget)
	_, err = env.service.SyncTarget(ctx, testID("tenant-a"), testID("missing"))
	assert.Error(t, err)
}

func newFakeSCIMProvider(t *testing.T, status int, body string) (*fakeSCIMProvider, *httptest.Server) {
	t.Helper()
	provider := &fakeSCIMProvider{status: status, body: body}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}
		provider.mu.Lock()
		provider.requests = append(provider.requests, scimProviderRequest{Method: r.Method, Path: r.URL.Path, Headers: r.Header.Clone(), Body: string(data)})
		provider.mu.Unlock()
		w.Header().Set("Content-Type", SCIMContentType)
		w.WriteHeader(provider.status)
		body := provider.body
		if provider.echo {
			resourceType := "User"
			if strings.Contains(r.URL.Path, "/Groups/") {
				resourceType = "Group"
			}
			body = fmt.Sprintf(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:%s"],"id":"remote-%s","userName":"%s"}`, resourceType, path.Base(r.URL.Path), path.Base(r.URL.Path))
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return provider, server
}

func (p *fakeSCIMProvider) last(t *testing.T) scimProviderRequest {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.True(t, len(p.requests) > 0, "no SCIM provider request was recorded")
	return p.requests[len(p.requests)-1]
}

func (p *fakeSCIMProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func TestTargetCRUDLifecycle(t *testing.T) {
	env := newProvisioningEnvironment(t)
	ctx := t.Context()

	created, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", "https://scim.example.test/v2", "bearer-secret")
	require.NoError(t, err)
	assert.Equal(t, TargetActive, created.Target.Status)
	assert.Equal(t, int64(1), created.Target.Version)
	assert.Equal(t, "bearer-secret", created.BearerToken)
	var stored targetRow
	require.NoError(t, env.db.NewSelect().Model(&stored).Where("id = ?", created.Target.ID).Scan(ctx))
	assert.NotContains(t, stored.BearerTokenCiphertext, "bearer-secret")
	plain, err := env.service.decryptToken(stored.ID, stored.BearerTokenCiphertext)
	require.NoError(t, err)
	assert.Equal(t, "bearer-secret", plain)

	_, err = env.service.CreateTarget(ctx, testID("tenant-a"), "okta", "https://other.example.test/v2", "x")
	assert.ErrorIs(t, err, ErrTargetName)

	targets, err := env.service.ListTargets(ctx, testID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, created.Target.ID, targets[0].ID)

	updated, err := env.service.ReplaceTarget(ctx, testID("tenant-a"), parseGUID(created.Target.ID), "Okta Prod", "https://scim.example.test/v2", TargetActive, "rotated-secret", 1)
	require.NoError(t, err)
	assert.Equal(t, "Okta Prod", updated.Name)
	assert.Equal(t, int64(2), updated.Version)
	var rotated targetRow
	require.NoError(t, env.db.NewSelect().Model(&rotated).Where("id = ?", created.Target.ID).Scan(ctx))
	assert.NotEqual(t, stored.BearerTokenCiphertext, rotated.BearerTokenCiphertext)

	_, err = env.service.ReplaceTarget(ctx, testID("tenant-a"), parseGUID(created.Target.ID), "Okta Prod", "https://scim.example.test/v2", TargetActive, "", 1)
	assert.ErrorIs(t, err, ErrTargetVersion)

	noop, err := env.service.ReplaceTarget(ctx, testID("tenant-a"), parseGUID(created.Target.ID), "Okta Prod", "https://scim.example.test/v2", TargetActive, "", 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), noop.Version)

	require.NoError(t, env.service.DeleteTarget(ctx, testID("tenant-a"), parseGUID(created.Target.ID)))
	assert.ErrorIs(t, env.service.DeleteTarget(ctx, testID("tenant-a"), parseGUID(created.Target.ID)), ErrTargetMissing)
}

func TestTargetValidation(t *testing.T) {
	env := newProvisioningEnvironment(t)
	ctx := t.Context()

	_, err := env.service.CreateTarget(ctx, testID("tenant-a"), "bad", "not-a-url", "token")
	assert.ErrorIs(t, err, ErrInvalidTarget)
	_, err = env.service.CreateTarget(ctx, testID("tenant-a"), "bad", "ftp://scim.example.test/v2", "token")
	assert.ErrorIs(t, err, ErrInvalidTarget)
	_, err = env.service.CreateTarget(ctx, testID("tenant-a"), "bad", "https://scim.example.test/v2", "")
	assert.ErrorIs(t, err, ErrInvalidTarget)
	_, err = env.service.CreateTarget(ctx, testID("tenant-a"), "bad", "https://scim.example.test/v2", strings.Repeat("x", 4097))
	assert.ErrorIs(t, err, ErrInvalidTarget)
	_, err = env.service.CreateTarget(ctx, testID("tenant-a"), "", "https://scim.example.test/v2", "token")
	assert.ErrorIs(t, err, ErrInvalidTarget)
	_, err = env.service.ListTargets(ctx, 0)
	assert.ErrorIs(t, err, ErrInvalidTarget)
	_, err = env.service.ReplaceTarget(ctx, testID("tenant-a"), testID("missing"), "x", "https://scim.example.test/v2", TargetActive, "", 1)
	assert.ErrorIs(t, err, ErrTargetMissing)
	_, err = env.service.ReplaceTarget(ctx, testID("tenant-a"), testID("missing"), "x", "https://scim.example.test/v2", "weird", "", 1)
	assert.ErrorIs(t, err, ErrInvalidTarget)
}

func TestTargetRequiresEncryptionKey(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	env := newProvisioningEnvironmentWithDB(t, db, nil)
	ctx := t.Context()

	_, err = env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", "https://scim.example.test/v2", "token")
	assert.ErrorIs(t, err, ErrTargetKeyMissing)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), testID("missing"), ResourceUser, testID("user-1"))
	assert.ErrorIs(t, err, ErrTargetMissing)

	// An existing target with a nil key fails on push with the key error.
	now := env.service.now().UTC().UnixMilli()
	row := targetRow{ID: testID("target-1"), TenantID: testID("tenant-a"), Name: "Okta", NameKey: "okta", BaseURL: "https://scim.example.test/v2", Status: TargetActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	_, err = env.db.NewInsert().Model(&row).Exec(ctx)
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), testID("target-1"), ResourceUser, testID("user-1"))
	assert.ErrorIs(t, err, ErrTargetKeyMissing)
	_, err = env.service.ReplaceTarget(ctx, testID("tenant-a"), testID("target-1"), "Okta", "https://scim.example.test/v2", TargetActive, "rotated", 1)
	assert.ErrorIs(t, err, ErrTargetKeyMissing)
	assert.ErrorIs(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), testID("target-1"), ResourceUser, testID("user-1")), ErrTargetKeyMissing)
}

func TestPushUserLifecycle(t *testing.T) {
	env := newProvisioningEnvironment(t)
	provider, providerServer := newFakeSCIMProvider(t, http.StatusOK, "")
	provider.echo = true
	ctx := t.Context()

	created, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", providerServer.URL, "bearer-secret")
	require.NoError(t, err)
	user, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-alice", "alice", "alice@example.test"))
	require.NoError(t, err)

	result, err := env.service.PushResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(user.ID))
	require.NoError(t, err)
	assert.Equal(t, user.ID, result.ExternalID)
	assert.Equal(t, "remote-"+user.ID, result.RemoteID)
	request := provider.last(t)
	assert.Equal(t, http.MethodPut, request.Method)
	assert.Equal(t, "/Users/"+user.ID, request.Path)
	assert.Equal(t, "Bearer bearer-secret", request.Headers.Get("Authorization"))
	assert.Equal(t, SCIMContentType, request.Headers.Get("Content-Type"))
	assert.Contains(t, request.Body, `"userName":"alice"`)
	assert.Contains(t, request.Body, `"active":true`)
	assert.Contains(t, request.Body, `"value":"alice@example.test"`)

	// A later push reuses the remote mapping.
	result2, err := env.service.PushResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(user.ID))
	require.NoError(t, err)
	assert.Equal(t, "remote-"+user.ID, result2.ExternalID)
	assert.Equal(t, "/Users/remote-"+user.ID, provider.last(t).Path)
	assert.Equal(t, 2, provider.count())
}

func TestPushGroupMapsOnlyPushedMembers(t *testing.T) {
	env := newProvisioningEnvironment(t)
	provider, providerServer := newFakeSCIMProvider(t, http.StatusOK, "")
	provider.echo = true
	ctx := t.Context()

	created, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", providerServer.URL, "bearer-secret")
	require.NoError(t, err)
	alice, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-alice", "alice", "alice@example.test"))
	require.NoError(t, err)
	bob, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-bob", "bob", "bob@example.test"))
	require.NoError(t, err)
	carol, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-carol", "carol", "carol@example.test"))
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(alice.ID))
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(bob.ID))
	require.NoError(t, err)

	group, err := env.service.CreateGroup(ctx, env.auth, GroupInput{
		Schemas:     []string{GroupSchema},
		ExternalID:  "ext-team",
		DisplayName: "Engineering",
		Members:     []SCIMGroupMember{{Value: alice.ID}, {Value: bob.ID}, {Value: carol.ID}},
	})
	require.NoError(t, err)

	result, err := env.service.PushResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceGroup, parseGUID(group.ID))
	require.NoError(t, err)
	assert.Equal(t, "remote-"+group.ID, result.RemoteID)
	request := provider.last(t)
	assert.Equal(t, "/Groups/"+group.ID, request.Path)
	assert.Contains(t, request.Body, `"value":"remote-`+alice.ID+`"`)
	assert.Contains(t, request.Body, `"value":"remote-`+bob.ID+`"`)
	assert.NotContains(t, request.Body, carol.ID)
}

func TestDeprovisionAndRepush(t *testing.T) {
	env := newProvisioningEnvironment(t)
	provider, providerServer := newFakeSCIMProvider(t, http.StatusOK, "")
	provider.echo = true
	ctx := t.Context()

	created, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", providerServer.URL, "bearer-secret")
	require.NoError(t, err)
	user, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-alice", "alice", "alice@example.test"))
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(user.ID))
	require.NoError(t, err)

	require.NoError(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(user.ID)))
	request := provider.last(t)
	assert.Equal(t, http.MethodDelete, request.Method)
	assert.Equal(t, "/Users/remote-"+user.ID, request.Path)
	assert.Equal(t, "Bearer bearer-secret", request.Headers.Get("Authorization"))

	assert.ErrorIs(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(user.ID)), ErrDeprovisionMissing)

	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(created.Target.ID), ResourceUser, parseGUID(user.ID))
	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, provider.last(t).Method)
	assert.Equal(t, "/Users/remote-"+user.ID, provider.last(t).Path)
}

func TestPushRemoteFailures(t *testing.T) {
	env := newProvisioningEnvironment(t)
	ctx := t.Context()
	user, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-alice", "alice", "alice@example.test"))
	require.NoError(t, err)

	_, server500 := newFakeSCIMProvider(t, http.StatusInternalServerError, "remote exploded")
	target500, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Failing", server500.URL, "token")
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(target500.Target.ID), ResourceUser, parseGUID(user.ID))
	assert.ErrorIs(t, err, ErrRemoteResponse)

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	targetClosed, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Down", closedURL, "token")
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(targetClosed.Target.ID), ResourceUser, parseGUID(user.ID))
	assert.ErrorIs(t, err, ErrRemoteUnavailable)

	providerOK, serverOK := newFakeSCIMProvider(t, http.StatusOK, "")
	providerOK.echo = true
	target, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", serverOK.URL, "token")
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, testID("ghost-user"))
	assert.ErrorIs(t, err, ErrResourceMissing)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), "Widget", parseGUID(user.ID))
	assert.ErrorIs(t, err, ErrInvalidTarget)
	assert.ErrorIs(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, testID("ghost-user")), ErrDeprovisionMissing)

	disabled, err := env.service.ReplaceTarget(ctx, testID("tenant-a"), parseGUID(target.Target.ID), "Okta", serverOK.URL, TargetDisabled, "", target.Target.Version)
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(disabled.ID), ResourceUser, parseGUID(user.ID))
	assert.ErrorIs(t, err, ErrTargetDisabled)
	assert.ErrorIs(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), parseGUID(disabled.ID), ResourceUser, parseGUID(user.ID)), ErrTargetDisabled)

	// Deprovision a mapped resource when the remote answers 404 still succeeds.
	_, err = env.service.ReplaceTarget(ctx, testID("tenant-a"), parseGUID(disabled.ID), "Okta", serverOK.URL, TargetActive, "", disabled.Version)
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, parseGUID(user.ID))
	require.NoError(t, err)
	providerOK.status = http.StatusNotFound
	providerOK.body = `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"detail":"gone"}`
	providerOK.echo = false
	require.NoError(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, parseGUID(user.ID)))
	assert.Equal(t, http.MethodDelete, providerOK.last(t).Method)
}

func TestDecryptTokenErrors(t *testing.T) {
	env := newProvisioningEnvironment(t)

	_, err := env.service.decryptToken(testID("target"), "v9.garbage")
	assert.Error(t, err)
	_, err = env.service.decryptToken(testID("target"), "v1.!!not-base64!!")
	assert.Error(t, err)
	_, err = env.service.decryptToken(testID("target"), "v1.AA")
	assert.Error(t, err)

	created, err := env.service.CreateTarget(t.Context(), testID("tenant-a"), "Okta", "https://scim.example.test/v2", "secret")
	require.NoError(t, err)
	var stored targetRow
	require.NoError(t, env.db.NewSelect().Model(&stored).Where("id = ?", created.Target.ID).Scan(t.Context()))
	tamper := byte('A')
	if stored.BearerTokenCiphertext[8] == tamper {
		tamper = 'B'
	}
	tampered := stored.BearerTokenCiphertext[:8] + string(tamper) + stored.BearerTokenCiphertext[9:]
	_, err = env.service.decryptToken(parseGUID(created.Target.ID), tampered)
	assert.Error(t, err)
}

func TestParseAndResolveEncryptionKey(t *testing.T) {
	key, err := ParseEncryptionKey("")
	require.NoError(t, err)
	assert.Nil(t, key)

	_, err = ParseEncryptionKey("too-short")
	assert.Error(t, err)

	encoded := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	key, err = ParseEncryptionKey(encoded)
	require.NoError(t, err)
	assert.Len(t, key, 32)

	resolved, err := ResolveEncryptionKey(Config{EncryptionKey: encoded})
	require.NoError(t, err)
	assert.Equal(t, key, resolved)

	dir := t.TempDir()
	keyFile := filepath.Join(dir, "provisioning.key")
	require.NoError(t, os.WriteFile(keyFile, []byte(encoded+"\n"), 0o600))
	resolved, err = ResolveEncryptionKey(Config{EncryptionKeyFile: keyFile})
	require.NoError(t, err)
	assert.Equal(t, key, resolved)

	_, err = ResolveEncryptionKey(Config{EncryptionKey: encoded, EncryptionKeyFile: keyFile})
	assert.Error(t, err)
}

func TestTargetErrorPaths(t *testing.T) {
	env := newProvisioningEnvironment(t)
	ctx := t.Context()

	assert.ErrorIs(t, env.service.DeleteTarget(ctx, 0, 0), ErrInvalidTarget)
	assert.ErrorIs(t, env.service.DeleteTarget(ctx, testID("tenant-a"), 0), ErrInvalidTarget)

	assert.ErrorIs(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), testID("missing-target"), ResourceUser, testID("user-1")), ErrTargetMissing)
	assert.ErrorIs(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), testID("missing-target"), "Widget", testID("user-1")), ErrInvalidTarget)

	_, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Too Long", "https://scim.example.test/"+strings.Repeat("a", 1025), "token")
	assert.ErrorIs(t, err, ErrInvalidTarget)

	_, err = env.service.buildRemoteResource(ctx, testID("tenant-a"), testID("target-missing"), "Widget", testID("x"))
	assert.ErrorIs(t, err, ErrInvalidTarget)

	target, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", "https://scim.example.test/v2", "token")
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceGroup, testID("ghost-group"))
	assert.ErrorIs(t, err, ErrResourceMissing)
}

func TestCreateTargetRejectsInactiveTenant(t *testing.T) {
	env := newProvisioningEnvironment(t)
	ctx := t.Context()
	_, err := env.db.NewUpdate().Table("iam_tenants").Set("status = ?", "suspended").Where("id = ?", testID("tenant-a")).Exec(ctx)
	require.NoError(t, err)
	_, err = env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", "https://scim.example.test/v2", "token")
	assert.ErrorIs(t, err, ErrInvalidTarget)
}

func TestReplaceTargetNameConflict(t *testing.T) {
	env := newProvisioningEnvironment(t)
	ctx := t.Context()
	_, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", "https://one.example.test/v2", "token")
	require.NoError(t, err)
	second, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Azure", "https://two.example.test/v2", "token")
	require.NoError(t, err)
	_, err = env.service.ReplaceTarget(ctx, testID("tenant-a"), parseGUID(second.Target.ID), "okta", "https://two.example.test/v2", TargetActive, "", second.Target.Version)
	assert.ErrorIs(t, err, ErrTargetName)
}

func TestPushRemoteDetailTruncation(t *testing.T) {
	env := newProvisioningEnvironment(t)
	ctx := t.Context()
	_, server := newFakeSCIMProvider(t, http.StatusBadGateway, strings.Repeat("x", 600))
	target, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Verbose", server.URL, "token")
	require.NoError(t, err)
	user, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-long", "long", "long@example.test"))
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, parseGUID(user.ID))
	require.ErrorIs(t, err, ErrRemoteResponse)
	assert.Contains(t, err.Error(), "...")
	assert.NotContains(t, err.Error(), strings.Repeat("x", 501))
}

func TestDeprovisionRemoteFailures(t *testing.T) {
	env := newProvisioningEnvironment(t)
	provider, server := newFakeSCIMProvider(t, http.StatusOK, "")
	provider.echo = true
	ctx := t.Context()
	target, err := env.service.CreateTarget(ctx, testID("tenant-a"), "Okta", server.URL, "token")
	require.NoError(t, err)
	user, err := env.service.CreateUser(ctx, env.auth, activeUserInput("ext-dp", "dp", "dp@example.test"))
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, parseGUID(user.ID))
	require.NoError(t, err)

	// A corrupted stored token fails both local decryption paths.
	var stored targetRow
	require.NoError(t, env.db.NewSelect().Model(&stored).Where("id = ?", target.Target.ID).Scan(ctx))
	tamper := byte('A')
	if stored.BearerTokenCiphertext[8] == tamper {
		tamper = 'B'
	}
	corrupt := stored.BearerTokenCiphertext[:8] + string(tamper) + stored.BearerTokenCiphertext[9:]
	_, err = env.db.NewUpdate().Table("iam_scim_targets").Set("bearer_token_ciphertext = ?", corrupt).Where("id = ?", target.Target.ID).Exec(ctx)
	require.NoError(t, err)
	_, err = env.service.PushResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, parseGUID(user.ID))
	assert.Error(t, err)
	assert.Error(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, parseGUID(user.ID)))
	_, err = env.db.NewUpdate().Table("iam_scim_targets").Set("bearer_token_ciphertext = ?", stored.BearerTokenCiphertext).Where("id = ?", target.Target.ID).Exec(ctx)
	require.NoError(t, err)

	// Connection failure on deprovision.
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	_, err = env.db.NewUpdate().Table("iam_scim_targets").Set("base_url = ?", closedURL).Where("id = ?", target.Target.ID).Exec(ctx)
	require.NoError(t, err)
	assert.ErrorIs(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, parseGUID(user.ID)), ErrRemoteUnavailable)

	// Remote rejection on deprovision.
	_, server500 := newFakeSCIMProvider(t, http.StatusInternalServerError, "rejected")
	_, err = env.db.NewUpdate().Table("iam_scim_targets").Set("base_url = ?", server500.URL).Where("id = ?", target.Target.ID).Exec(ctx)
	require.NoError(t, err)
	assert.ErrorIs(t, env.service.DeprovisionResource(ctx, testID("tenant-a"), parseGUID(target.Target.ID), ResourceUser, parseGUID(user.ID)), ErrRemoteResponse)
}

func TestServiceConstructorGuards(t *testing.T) {
	assert.Panics(t, func() { NewService(nil, nil, nil, nil, nil, Config{}, nil) })
	assert.Panics(t, func() { NewRepository(nil) })
}

func TestDirectoryAndCredentialErrorPaths(t *testing.T) {
	env := newProvisioningEnvironment(t)
	ctx := t.Context()

	_, err := env.service.CreateDirectory(ctx, 0, "x")
	assert.ErrorIs(t, err, ErrInvalidDirectory)

	_, err = env.service.ReplaceDirectory(ctx, 0, 0, "x", DirectoryActive, 1)
	assert.ErrorIs(t, err, ErrInvalidDirectory)

	_, err = env.service.ReplaceDirectory(ctx, testID("tenant-a"), testID("missing"), "x", DirectoryActive, 1)
	assert.ErrorIs(t, err, ErrDirectoryMissing)

	secondary, err := env.service.CreateDirectory(ctx, testID("tenant-a"), "Secondary IdP")
	require.NoError(t, err)
	_, err = env.service.ReplaceDirectory(ctx, testID("tenant-a"), secondary.ID, "primary idp", DirectoryActive, 1)
	assert.ErrorIs(t, err, ErrDirectoryName)

	_, err = env.service.CreateCredential(ctx, testID("tenant-a"), testID("missing-directory"), "acceptance", nil)
	assert.ErrorIs(t, err, ErrDirectoryMissing)

	assert.ErrorIs(t, env.service.RevokeCredential(ctx, 0, 0, 0), ErrInvalidDirectory)
	assert.ErrorIs(t, env.service.RevokeCredential(ctx, testID("tenant-a"), testID("missing-directory"), testID("scim_x")), ErrDirectoryMissing)

	_, err = env.service.Authenticate(ctx, "Bearer scim_missing.secret")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestServiceIDGeneratorFailures(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx := t.Context()
	require.NoError(t, iam.Migrate(ctx, db))
	require.NoError(t, organization.Migrate(ctx, db))
	require.NoError(t, Migrate(ctx, db))
	require.NoError(t, organization.EnsureTenant(ctx, db, testID("tenant-a")))
	auditService := audit.NewService(db, newTestIDGenerator())
	appendAudit := func(ctx context.Context, executor bun.IDB, event auditx.Event) error {
		_, err := auditService.AppendTo(ctx, executor, audit.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID, EventType: event.EventType,
			TargetType: event.TargetType, TargetID: event.TargetID, Outcome: "success", Detail: event.Detail,
		})
		return err
	}
	guard := iam.NewAdministratorGuard()
	identities := identity.NewService(db, appendAudit, guard, newTestIDGenerator())
	groups := organization.NewGroupService(db, appendAudit, iam.NewMembershipChecker(db), guard, secureTestID)
	build := func(nextID IDGenerator) *Service {
		return NewService(db, appendAudit, nextID, identities, groups, Config{}, testProvisioningKey())
	}

	failing := build(func() (guid.ID, error) { return 0, errors.New("id generator unavailable") })
	_, err = failing.CreateTarget(ctx, testID("tenant-a"), "Okta", "https://scim.example.test/v2", "token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generate SCIM target id")
	_, err = failing.CreateDirectory(ctx, testID("tenant-a"), "Directory")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generate SCIM directory id")
	_, err = failing.CreateCredential(ctx, testID("tenant-a"), testID("directory"), "credential", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id generator unavailable")

	empty := build(func() (guid.ID, error) { return 0, nil })
	_, err = empty.CreateTarget(ctx, testID("tenant-a"), "Okta", "https://scim.example.test/v2", "token")
	require.ErrorIs(t, err, ErrInvalidTarget)
	assert.Contains(t, err.Error(), "generate SCIM target id")
}
