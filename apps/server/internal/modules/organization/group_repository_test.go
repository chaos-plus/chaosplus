package organization

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupRepositoryDatabaseFailures(t *testing.T) {
	db, service := newGroupService(t)
	repo := service.repo
	require.NoError(t, db.Close())
	row := groupRow{TenantID: testID("tenant"), ID: testID("group"), Name: "Group", NameKey: "group", GroupType: GroupTypeStatic, Status: StatusActive, Version: 1}

	_, err := repo.list(t.Context(), testID("tenant"))
	assert.Error(t, err)
	_, err = repo.get(t.Context(), testID("tenant"), testID("group"))
	assert.Error(t, err)
	assert.Error(t, repo.insert(t.Context(), &row))
	assert.Error(t, repo.update(t.Context(), &row, 1))
	assert.Error(t, repo.delete(t.Context(), testID("tenant"), testID("group"), 1))
	_, err = repo.listMembers(t.Context(), testID("tenant"), testID("group"))
	assert.Error(t, err)
	_, err = repo.getMember(t.Context(), testID("tenant"), testID("group"), testID("principal"))
	assert.Error(t, err)
	assert.Error(t, repo.putMember(t.Context(), &groupMemberRow{}))
	_, err = repo.deleteMember(t.Context(), testID("tenant"), testID("group"), testID("principal"))
	assert.Error(t, err)
}

func TestGroupRepositoryDialectAndConstraintHelpers(t *testing.T) {
	assert.Contains(t, groupMemberUpsertSQL("sqlite"), "ON CONFLICT")
	assert.Contains(t, groupMemberUpsertSQL("postgres"), "ON CONFLICT")
	assert.Contains(t, groupMemberUpsertSQL("mysql"), "ON DUPLICATE KEY")
	assert.Empty(t, groupMemberUpsertSQL("unknown"))
	assert.True(t, isGroupNameViolation(errors.New("UNIQUE constraint failed: iam_groups.tenant_id, iam_groups.name_key")))
	assert.False(t, isGroupNameViolation(errors.New("connection closed")))
	assert.Panics(t, func() { NewGroupRepository(nil) })
}
