package provisioning

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

type provisioningEnvironment struct {
	db      *bun.DB
	service *Service
	auth    AuthContext
	token   string
}

func newProvisioningEnvironment(t *testing.T) provisioningEnvironment {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return newProvisioningEnvironmentWithDB(t, db)
}

func newProvisioningEnvironmentWithDB(t *testing.T, db *bun.DB) provisioningEnvironment {
	t.Helper()
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, "tenant-a"))

	auditService := audit.NewService(db)
	appendAudit := func(ctx context.Context, executor bun.IDB, event auditx.Event) error {
		_, err := auditService.AppendTo(ctx, executor, audit.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID, EventType: event.EventType,
			TargetType: event.TargetType, TargetID: event.TargetID, Outcome: "success", Detail: event.Detail,
		})
		return err
	}
	guard := iam.NewAdministratorGuard()
	identities := identity.NewService(db, appendAudit, guard)
	groups := organization.NewGroupService(db, appendAudit, iam.NewMembershipChecker(db), guard, secureTestID)
	service := NewService(db, appendAudit, secureTestID, identities, groups)
	directory, err := service.CreateDirectory(t.Context(), "tenant-a", "Primary IdP")
	require.NoError(t, err)
	credential, err := service.CreateCredential(t.Context(), "tenant-a", directory.ID, "acceptance", nil)
	require.NoError(t, err)
	auth, err := service.Authenticate(t.Context(), "Bearer "+credential.Token)
	require.NoError(t, err)
	return provisioningEnvironment{db: db, service: service, auth: auth, token: credential.Token}
}

func secureTestID() (string, error) {
	data := make([]byte, 18)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func activeUserInput(externalID, userName, email string) UserInput {
	active := true
	return UserInput{Schemas: []string{UserSchema}, ExternalID: externalID, UserName: userName, DisplayName: "Display " + userName, Active: &active, Emails: []UserEmail{{Value: email, Type: "work", Primary: true}}}
}

func TestProvisionedResourceLifecycle(t *testing.T) {
	env := newProvisioningEnvironment(t)

	user, err := env.service.CreateUser(t.Context(), env.auth, activeUserInput("ext-alice", "Alice", "alice@example.test"))
	require.NoError(t, err)
	assert.Equal(t, "alice", user.UserName)
	assert.True(t, user.Active)
	assert.Equal(t, weakETag(1), user.Meta.Version)

	second, err := env.service.CreateUser(t.Context(), env.auth, activeUserInput("ext-bob", "bob", "bob@example.test"))
	require.NoError(t, err)
	listRequest, err := normalizeListRequest(`userName sw "ali" and active eq true`, 1, 20)
	require.NoError(t, err)
	users, err := env.service.ListUsers(t.Context(), env.auth, listRequest)
	require.NoError(t, err)
	require.Len(t, users.Resources, 1)
	assert.Equal(t, user.ID, users.Resources[0].ID)

	createdAfter := user.Meta.Created.Add(-time.Second).Format(time.RFC3339)
	listRequest, err = normalizeListRequest(`meta.created gt "`+createdAfter+`"`, 1, 20)
	require.NoError(t, err)
	users, err = env.service.ListUsers(t.Context(), env.auth, listRequest)
	require.NoError(t, err)
	assert.Equal(t, 2, users.TotalResults)

	group, err := env.service.CreateGroup(t.Context(), env.auth, GroupInput{Schemas: []string{GroupSchema}, ExternalID: "ext-team", DisplayName: "Engineering", Members: []SCIMGroupMember{{Value: user.ID}, {Value: second.ID}}})
	require.NoError(t, err)
	require.Len(t, group.Members, 2)
	groupRequest, err := normalizeListRequest(`members[value eq "`+user.ID+`"]`, 1, 20)
	require.NoError(t, err)
	groups, err := env.service.ListGroups(t.Context(), env.auth, groupRequest)
	require.NoError(t, err)
	require.Len(t, groups.Resources, 1)
	assert.Equal(t, group.ID, groups.Resources[0].ID)

	group, err = env.service.PatchGroup(t.Context(), env.auth, group.ID, PatchRequest{Schemas: []string{PatchSchema}, Operations: []PatchOperation{{Op: "remove", Path: `members[value eq "` + second.ID + `"]`}}}, 1)
	require.NoError(t, err)
	require.Len(t, group.Members, 1)
	assert.Equal(t, weakETag(2), group.Meta.Version)

	user, err = env.service.PatchUser(t.Context(), env.auth, user.ID, PatchRequest{Schemas: []string{PatchSchema}, Operations: []PatchOperation{{Op: "replace", Path: "displayName", Value: []byte(`"Alice Updated"`)}}}, 1)
	require.NoError(t, err)
	assert.Equal(t, "Alice Updated", user.DisplayName)
	_, err = env.service.ReplaceUser(t.Context(), env.auth, user.ID, activeUserInput("ext-alice", "alice", "alice@example.test"), 1)
	assert.ErrorIs(t, err, ErrResourceVersion)

	require.NoError(t, env.service.DeleteUser(t.Context(), env.auth, user.ID, 2))
	_, err = env.service.GetUser(t.Context(), env.auth, user.ID)
	assert.ErrorIs(t, err, ErrResourceMissing)
	var memberStatus string
	require.NoError(t, env.db.NewSelect().Table("iam_tenant_members").Column("status").Where("tenant_id = ? AND user_subject = ?", env.auth.TenantID, user.ID).Scan(t.Context(), &memberStatus))
	assert.Equal(t, "disabled", memberStatus)

	restored, err := env.service.CreateUser(t.Context(), env.auth, activeUserInput("ext-alice", "alice", "alice@example.test"))
	require.NoError(t, err)
	assert.Equal(t, user.ID, restored.ID)
	assert.True(t, restored.Active)
	assert.Equal(t, weakETag(4), restored.Meta.Version)

	require.NoError(t, env.service.DeleteGroup(t.Context(), env.auth, group.ID, 2))
	_, err = env.service.GetGroup(t.Context(), env.auth, group.ID)
	assert.ErrorIs(t, err, ErrResourceMissing)
	restoredGroup, err := env.service.CreateGroup(t.Context(), env.auth, GroupInput{Schemas: []string{GroupSchema}, ExternalID: "ext-team", DisplayName: "Engineering Restored", Members: []SCIMGroupMember{{Value: restored.ID}}})
	require.NoError(t, err)
	assert.Equal(t, group.ID, restoredGroup.ID)
	assert.Equal(t, weakETag(4), restoredGroup.Meta.Version)
}

func TestDirectoryCredentialLifecycle(t *testing.T) {
	env := newProvisioningEnvironment(t)
	directories, err := env.service.ListDirectories(t.Context(), env.auth.TenantID)
	require.NoError(t, err)
	require.Len(t, directories, 1)
	_, err = env.service.ListDirectories(t.Context(), "")
	assert.ErrorIs(t, err, ErrInvalidDirectory)
	_, err = env.service.CreateDirectory(t.Context(), "missing-tenant", "Missing")
	assert.ErrorIs(t, err, ErrInvalidDirectory)
	credentials, err := env.service.ListCredentials(t.Context(), env.auth.TenantID, env.auth.DirectoryID)
	require.NoError(t, err)
	require.Len(t, credentials, 1)
	_, err = env.service.ListCredentials(t.Context(), "other-tenant", env.auth.DirectoryID)
	assert.ErrorIs(t, err, ErrDirectoryMissing)

	directory := directories[0]
	unchanged, err := env.service.ReplaceDirectory(t.Context(), env.auth.TenantID, directory.ID, directory.Name, directory.Status, directory.Version)
	require.NoError(t, err)
	assert.Equal(t, directory.Version, unchanged.Version)

	expiresAt := time.Now().UTC().Add(time.Hour)
	expiring, err := env.service.CreateCredential(t.Context(), env.auth.TenantID, env.auth.DirectoryID, "expiring", &expiresAt)
	require.NoError(t, err)
	expiredAt := time.Now().UTC().Add(-time.Second)
	_, err = env.service.CreateCredential(t.Context(), env.auth.TenantID, env.auth.DirectoryID, "expired", &expiredAt)
	assert.ErrorIs(t, err, ErrInvalidDirectory)
	_, err = env.service.Authenticate(t.Context(), "Bearer "+env.auth.CredentialID+".wrong-secret")
	assert.ErrorIs(t, err, ErrUnauthorized)
	for index := 0; index < maxActiveCredentials-2; index++ {
		_, err = env.service.CreateCredential(t.Context(), env.auth.TenantID, env.auth.DirectoryID, fmt.Sprintf("credential-%d", index), nil)
		require.NoError(t, err)
	}
	_, err = env.service.CreateCredential(t.Context(), env.auth.TenantID, env.auth.DirectoryID, "over-limit", nil)
	assert.ErrorIs(t, err, ErrCredentialLimit)
	env.service.now = func() time.Time { return expiresAt.Add(time.Second) }
	_, err = env.service.Authenticate(t.Context(), "Bearer "+expiring.Token)
	assert.ErrorIs(t, err, ErrUnauthorized)
	env.service.now = time.Now

	require.NoError(t, env.service.RevokeCredential(t.Context(), env.auth.TenantID, env.auth.DirectoryID, env.auth.CredentialID))
	_, err = env.service.Authenticate(t.Context(), "Bearer "+env.token)
	assert.ErrorIs(t, err, ErrUnauthorized)
	assert.ErrorIs(t, env.service.RevokeCredential(t.Context(), env.auth.TenantID, env.auth.DirectoryID, "missing"), ErrCredentialMissing)

	updated, err := env.service.ReplaceDirectory(t.Context(), env.auth.TenantID, directory.ID, directory.Name, DirectoryDisabled, directory.Version)
	require.NoError(t, err)
	assert.Equal(t, DirectoryDisabled, updated.Status)
	_, err = env.service.CreateCredential(t.Context(), env.auth.TenantID, env.auth.DirectoryID, "disabled", nil)
	assert.ErrorIs(t, err, ErrInvalidDirectory)
	_, err = env.service.ReplaceDirectory(t.Context(), env.auth.TenantID, directory.ID, directory.Name, DirectoryActive, directory.Version)
	assert.ErrorIs(t, err, ErrDirectoryVersion)
}

func TestProvisioningAuditFailureRollsBackResource(t *testing.T) {
	env := newProvisioningEnvironment(t)
	_, err := env.db.ExecContext(t.Context(), `CREATE TRIGGER reject_scim_user_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'scim_user_created' BEGIN SELECT RAISE(ABORT, 'audit rejected'); END`)
	require.NoError(t, err)

	_, err = env.service.CreateUser(t.Context(), env.auth, activeUserInput("rollback", "rollback-user", "rollback@example.test"))
	require.Error(t, err)
	var principalCount, resourceCount int
	principalCount, err = env.db.NewSelect().Table("iam_principals").Where("login_name = ?", "rollback-user").Count(t.Context())
	require.NoError(t, err)
	resourceCount, err = env.db.NewSelect().Table("iam_scim_resources").Where("external_id = ?", "rollback").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, principalCount)
	assert.Zero(t, resourceCount)
}

func TestResourceInputAndAuthorizationFailures(t *testing.T) {
	env := newProvisioningEnvironment(t)
	_, err := env.service.CreateUser(t.Context(), env.auth, UserInput{Schemas: []string{UserSchema}})
	assert.ErrorIs(t, err, ErrInvalidSCIM)
	_, err = env.service.CreateGroup(t.Context(), env.auth, GroupInput{Schemas: []string{GroupSchema}, DisplayName: "Invalid", Members: []SCIMGroupMember{{Value: "missing"}}})
	assert.ErrorIs(t, err, organization.ErrGroupMemberInactive)
	_, err = env.service.Authenticate(t.Context(), "Basic x")
	assert.ErrorIs(t, err, ErrUnauthorized)
	_, err = env.service.Authenticate(t.Context(), "Bearer malformed")
	assert.ErrorIs(t, err, ErrUnauthorized)
	assert.Equal(t, 409, mapSCIMError(t.Context(), identity.ErrLoginConflict).status)
}
