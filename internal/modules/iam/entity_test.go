package iam

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntityTreeCRUDValidationAndIsolation(t *testing.T) {
	service := newTestService(t)
	ctx := t.Context()

	root, err := service.CreateEntity(ctx, iamdomain.Entity{
		TenantID: "tenant-a", Type: "company", Name: " Acme ", Metadata: map[string]any{"region": "west"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Acme", root.Name)
	assert.Equal(t, iamdomain.EntityActive, root.Status)
	assert.Equal(t, "west", root.Metadata["region"])

	child, err := service.CreateEntity(ctx, iamdomain.Entity{
		TenantID: "tenant-a", ParentID: root.ID, Type: "store", Name: "Main",
	})
	require.NoError(t, err)
	entities, err := service.ListEntities(ctx, "tenant-a")
	require.NoError(t, err)
	require.Len(t, entities, 2)
	_, err = service.GetEntity(ctx, "tenant-b", root.ID)
	assert.ErrorIs(t, err, iamdomain.ErrEntityNotFound)

	_, err = service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant-a", Type: "company", Name: "Acme"})
	assert.ErrorIs(t, err, iamdomain.ErrEntityConflict)
	_, err = service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant-a", ParentID: root.ID, Type: "store", Name: "Main"})
	assert.ErrorIs(t, err, iamdomain.ErrEntityConflict)
	otherTenant, err := service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant-b", Type: "company", Name: "Other"})
	require.NoError(t, err)
	_, err = service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant-a", ParentID: otherTenant.ID, Type: "store", Name: "Cross tenant"})
	assert.ErrorIs(t, err, iamdomain.ErrEntityNotFound)

	name := "Flagship"
	status := iamdomain.EntityDisabled
	metadata := map[string]any{"tier": float64(1)}
	updated, err := service.UpdateEntity(ctx, "tenant-a", child.ID, iamdomain.EntityPatch{
		Name: &name, Status: &status, Metadata: &metadata,
	})
	require.NoError(t, err)
	assert.Equal(t, name, updated.Name)
	assert.Equal(t, status, updated.Status)
	assert.Equal(t, float64(1), updated.Metadata["tier"])

	cycle := child.ID
	_, err = service.UpdateEntity(ctx, "tenant-a", root.ID, iamdomain.EntityPatch{ParentID: &cycle})
	assert.ErrorIs(t, err, iamdomain.ErrEntityHierarchy)
	assert.ErrorIs(t, service.DeleteEntity(ctx, "tenant-a", root.ID), iamdomain.ErrEntityHasChildren)
	require.NoError(t, service.DeleteEntity(ctx, "tenant-a", child.ID))
	require.NoError(t, service.DeleteEntity(ctx, "tenant-a", root.ID))
	_, err = service.GetEntity(ctx, "tenant-a", root.ID)
	assert.ErrorIs(t, err, iamdomain.ErrEntityNotFound)

	_, err = service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant-a", Type: "Company", Name: "Invalid"})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, err = service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant-a", Type: "company", Name: "Invalid", Metadata: map[string]any{"value": math.NaN()}})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, err = service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant-a", Type: "company", Name: "Invalid", Metadata: map[string]any{"value": strings.Repeat("x", maxEntityMetadataBytes)}})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
}

func TestEntityRejectsInvalidReferencesAndBindingFields(t *testing.T) {
	service := newTestService(t)
	ctx := t.Context()
	root, err := service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", Type: "company", Name: "Root"})
	require.NoError(t, err)
	child, err := service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", ParentID: root.ID, Type: "store", Name: "Store"})
	require.NoError(t, err)

	_, err = service.ListEntities(ctx, "")
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, err = service.GetEntity(ctx, "tenant", "")
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, err = service.UpdateEntity(ctx, "tenant", child.ID, iamdomain.EntityPatch{})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	assert.ErrorIs(t, service.DeleteEntity(ctx, "tenant", ""), iamdomain.ErrInvalidArgument)
	_, err = service.ListEntityRoleBindings(ctx, "tenant", "")
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)

	_, _, err = service.PutEntityRoleBinding(ctx, "tenant", child.ID, "", "principal", iamdomain.BindingAllow, time.Time{})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, _, err = service.PutEntityRoleBinding(ctx, "tenant", child.ID, "role", "", iamdomain.BindingAllow, time.Time{})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, _, err = service.PutEntityRoleBinding(ctx, "tenant", child.ID, "role", "principal", "permit", time.Time{})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, err = service.DeleteEntityRoleBinding(ctx, "tenant", child.ID, "role", "")
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)

	invalidStatus := iamdomain.EntityStatus("pending")
	_, err = service.UpdateEntity(ctx, "tenant", child.ID, iamdomain.EntityPatch{Status: &invalidStatus})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	for _, entityType := range []string{"", "1company", "company.unit", "a" + strings.Repeat("b", 64)} {
		_, err = service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", Type: entityType, Name: "Invalid"})
		assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	}

	parentID, entityType := "", "branch"
	updated, err := service.UpdateEntity(ctx, "tenant", child.ID, iamdomain.EntityPatch{ParentID: &parentID, Type: &entityType})
	require.NoError(t, err)
	assert.Empty(t, updated.ParentID)
	assert.Equal(t, entityType, updated.Type)
}

func TestEntityHierarchyDepthAndCorruptMetadataFailClosed(t *testing.T) {
	service := newTestService(t)
	repo := service.repo
	ctx := t.Context()
	root, err := service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", Type: "company", Name: "Root"})
	require.NoError(t, err)
	parentID := root.ID
	for level := 1; level < maxEntityDepth; level++ {
		entity, createErr := service.CreateEntity(ctx, iamdomain.Entity{
			TenantID: "tenant", ParentID: parentID, Type: "unit", Name: "Level " + string(rune('A'+level)),
		})
		require.NoError(t, createErr)
		parentID = entity.ID
	}
	_, err = service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", ParentID: parentID, Type: "unit", Name: "Too deep"})
	assert.ErrorIs(t, err, iamdomain.ErrEntityHierarchy)

	_, err = repo.db.ExecContext(ctx, "UPDATE iam_entities SET metadata = '{' WHERE tenant_id = ? AND id = ?", "tenant", root.ID)
	require.NoError(t, err)
	_, err = service.GetEntity(ctx, "tenant", root.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode entity metadata")
}

func TestEntityRoleBindingsAuthorizationAndDeletionGuards(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	ctx := context.Background()
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	parent, err := service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", Type: "company", Name: "Company"})
	require.NoError(t, err)
	child, err := service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", ParentID: parent.ID, Type: "store", Name: "Store"})
	require.NoError(t, err)
	allowRole, err := service.CreateRole(ctx, "tenant", "Viewer", "")
	require.NoError(t, err)
	denyRole, err := service.CreateRole(ctx, "tenant", "Restricted", "")
	require.NoError(t, err)
	_, err = service.GrantPermission(ctx, "tenant", allowRole.ID, "store_view")
	require.NoError(t, err)
	_, err = service.GrantPermission(ctx, "tenant", denyRole.ID, "store_view")
	require.NoError(t, err)
	_, err = service.PutTenantMember(ctx, "tenant", "principal", "Principal", "", "", MemberActive)
	require.NoError(t, err)

	revision, err := repo.policyRevision(ctx, "tenant")
	require.NoError(t, err)
	binding, changed, err := service.PutEntityRoleBinding(ctx, "tenant", parent.ID, allowRole.ID, "principal", iamdomain.BindingAllow, time.Time{})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, parent.ID, binding.EntityID)
	revisionAfterAllow, err := repo.policyRevision(ctx, "tenant")
	require.NoError(t, err)
	assert.Equal(t, revision+1, revisionAfterAllow)
	_, changed, err = service.PutEntityRoleBinding(ctx, "tenant", parent.ID, allowRole.ID, "principal", iamdomain.BindingAllow, time.Time{})
	require.NoError(t, err)
	assert.False(t, changed)
	revisionAfterNoop, err := repo.policyRevision(ctx, "tenant")
	require.NoError(t, err)
	assert.Equal(t, revisionAfterAllow, revisionAfterNoop)

	authorizer := NewAuthorizer(repo.db)
	allowed, err := authorizer.CheckEntity(ctx, "tenant", child.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)
	_, changed, err = service.PutEntityRoleBinding(ctx, "tenant", child.ID, denyRole.ID, "principal", iamdomain.BindingDeny, time.Time{})
	require.NoError(t, err)
	assert.True(t, changed)
	allowed, err = authorizer.CheckEntity(ctx, "tenant", child.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.ErrorIs(t, service.DeleteEntity(ctx, "tenant", child.ID), iamdomain.ErrEntityHasBindings)

	expiresAt := time.Now().UTC().Add(time.Hour)
	_, changed, err = service.PutEntityRoleBinding(ctx, "tenant", child.ID, denyRole.ID, "principal", iamdomain.BindingAllow, expiresAt)
	require.NoError(t, err)
	assert.True(t, changed)
	authorizer.now = func() time.Time { return expiresAt.Add(time.Second) }
	allowed, err = authorizer.CheckEntity(ctx, "tenant", child.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed, "the expired child binding must not override the inherited allow")

	bindings, err := service.ListEntityRoleBindings(ctx, "tenant", child.ID)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	assert.Equal(t, expiresAt.UnixMilli(), bindings[0].ExpiresAt.UnixMilli())
	changed, err = service.DeleteEntityRoleBinding(ctx, "tenant", child.ID, denyRole.ID, "principal")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = service.DeleteEntityRoleBinding(ctx, "tenant", child.ID, denyRole.ID, "principal")
	require.NoError(t, err)
	assert.False(t, changed)

	_, _, err = service.PutEntityRoleBinding(ctx, "tenant", child.ID, "missing", "principal", iamdomain.BindingAllow, time.Time{})
	assert.ErrorIs(t, err, ErrRoleNotFound)
	_, err = service.SetTenantMemberStatus(ctx, "tenant", "principal", MemberDisabled)
	require.NoError(t, err)
	_, _, err = service.PutEntityRoleBinding(ctx, "tenant", child.ID, allowRole.ID, "principal", iamdomain.BindingAllow, time.Time{})
	assert.ErrorIs(t, err, ErrMemberInactive)
	_, _, err = service.PutEntityRoleBinding(ctx, "tenant", child.ID, allowRole.ID, "principal", iamdomain.BindingAllow, time.Now().UTC().Add(-time.Second))
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestEntityWritesRollBackWhenAuditFails(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	ctx := t.Context()
	_, err := repo.db.ExecContext(ctx, `CREATE TRIGGER reject_entity_audit BEFORE INSERT ON iam_audit_events
		WHEN NEW.event_type = 'entity_created' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`)
	require.NoError(t, err)
	_, err = service.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", Type: "company", Name: "Rolled back"})
	require.Error(t, err)
	entities, listErr := repo.ListEntities(ctx, "tenant")
	require.NoError(t, listErr)
	assert.Empty(t, entities)
	revision, revisionErr := repo.policyRevision(ctx, "tenant")
	require.NoError(t, revisionErr)
	assert.Zero(t, revision)
}
