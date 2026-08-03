package organization

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPositionRepositoryDatabaseFailures(t *testing.T) {
	db, service := newPositionService(t)
	repo := service.repo
	require.NoError(t, db.Close())
	row := positionRow{TenantID: "tenant", ID: "position", Code: "position", Name: "Position", Status: StatusActive, Version: 1}

	_, err := repo.list(t.Context(), "tenant")
	assert.Error(t, err)
	_, err = repo.get(t.Context(), "tenant", "position")
	assert.Error(t, err)
	assert.Error(t, repo.insert(t.Context(), &row))
	assert.Error(t, repo.update(t.Context(), &row, 1))
	assert.Error(t, repo.delete(t.Context(), "tenant", "position", 1))
	_, err = repo.listMembers(t.Context(), "tenant", "position")
	assert.Error(t, err)
	_, err = repo.getMember(t.Context(), "tenant", "position", "principal")
	assert.Error(t, err)
	assert.Error(t, repo.putMember(t.Context(), &positionMemberRow{}))
	_, err = repo.deleteMember(t.Context(), "tenant", "position", "principal")
	assert.Error(t, err)
}

func TestPositionRepositoryDialectAndConstraintHelpers(t *testing.T) {
	assert.Contains(t, positionMemberUpsertSQL("sqlite"), "ON CONFLICT")
	assert.Contains(t, positionMemberUpsertSQL("postgres"), "ON CONFLICT")
	assert.Contains(t, positionMemberUpsertSQL("mysql"), "ON DUPLICATE KEY")
	assert.Empty(t, positionMemberUpsertSQL("unknown"))
	assert.True(t, isPositionCodeViolation(errors.New("UNIQUE constraint failed: iam_positions.tenant_id, iam_positions.code")))
	assert.False(t, isPositionCodeViolation(errors.New("connection closed")))
	assert.Panics(t, func() { NewPositionRepository(nil) })
}
