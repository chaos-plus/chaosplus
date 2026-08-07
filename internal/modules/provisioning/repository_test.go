package provisioning

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvisioningRepositoryConstraintsAndFailures(t *testing.T) {
	env := newProvisioningEnvironment(t)
	repo := NewRepository(env.db)
	directories, err := repo.listDirectories(t.Context(), env.auth.TenantID)
	require.NoError(t, err)
	require.Len(t, directories, 1)
	directory, err := repo.getDirectory(t.Context(), env.auth.TenantID, env.auth.DirectoryID)
	require.NoError(t, err)
	_, err = repo.getDirectory(t.Context(), env.auth.TenantID, "missing")
	assert.ErrorIs(t, err, ErrDirectoryMissing)
	duplicate := directory
	duplicate.ID = "duplicate-directory"
	assert.ErrorIs(t, repo.insertDirectory(t.Context(), &duplicate), ErrDirectoryName)
	directory.Version++
	assert.ErrorIs(t, repo.updateDirectory(t.Context(), &directory, 99), ErrDirectoryVersion)

	credentials, err := repo.listCredentials(t.Context(), env.auth.DirectoryID)
	require.NoError(t, err)
	require.Len(t, credentials, 1)
	authRow, err := repo.credentialForAuth(t.Context(), credentials[0].ID)
	require.NoError(t, err)
	assert.Equal(t, env.auth.TenantID, authRow.TenantID)
	_, err = repo.credentialForAuth(t.Context(), "missing")
	assert.ErrorIs(t, err, ErrUnauthorized)

	user, err := env.service.CreateUser(t.Context(), env.auth, activeUserInput("repo-user", "repo-user", "repo@example.test"))
	require.NoError(t, err)
	resource, err := repo.getResource(t.Context(), env.auth.DirectoryID, ResourceUser, user.ID, false)
	require.NoError(t, err)
	byExternal, err := repo.getResourceByExternalKey(t.Context(), env.auth.DirectoryID, ResourceUser, externalKey("repo-user", ""))
	require.NoError(t, err)
	assert.Equal(t, resource.ResourceID, byExternal.ResourceID)
	assert.ErrorIs(t, repo.insertResource(t.Context(), &resource), ErrResourceConflict)
	resource.Version++
	assert.ErrorIs(t, repo.updateResource(t.Context(), &resource, 99), ErrResourceVersion)
	_, err = repo.getResource(t.Context(), env.auth.DirectoryID, ResourceUser, "missing", false)
	assert.ErrorIs(t, err, ErrResourceMissing)

	group, err := env.service.CreateGroup(t.Context(), env.auth, GroupInput{Schemas: []string{GroupSchema}, DisplayName: "Repository Group", Members: []SCIMGroupMember{{Value: user.ID}}})
	require.NoError(t, err)
	groupRecord, err := repo.group(t.Context(), env.auth.DirectoryID, group.ID)
	require.NoError(t, err)
	assert.Equal(t, group.DisplayName, groupRecord.Name)
	members, err := repo.groupMembers(t.Context(), env.auth.DirectoryID, group.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{user.ID}, members)
	_, err = repo.group(t.Context(), env.auth.DirectoryID, "missing")
	assert.ErrorIs(t, err, ErrResourceMissing)

	assert.Panics(t, func() { NewRepository(nil) })
	closed, err := bunxtest.Memory()
	require.NoError(t, err)
	closedRepo := NewRepository(closed)
	require.NoError(t, closed.Close())
	_, err = closedRepo.listDirectories(t.Context(), "tenant-a")
	assert.Error(t, err)
	_, err = closedRepo.credentialForAuth(t.Context(), "credential")
	assert.Error(t, err)
	_, _, err = closedRepo.userPage(t.Context(), "directory", ListRequest{StartIndex: 1, Count: 1}, "sqlite")
	assert.Error(t, err)
	_, _, err = closedRepo.groupPage(t.Context(), "directory", ListRequest{StartIndex: 1, Count: 1}, "sqlite")
	assert.Error(t, err)
}
