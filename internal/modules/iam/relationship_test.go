package iam

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelationshipAuthorizationLifecycle(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), "tenant", "principal", "Principal", "", "", iamdomain.MemberActive)
	require.NoError(t, err)
	company, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "company", Name: "Company"})
	require.NoError(t, err)
	store, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "store", Name: "Store"})
	require.NoError(t, err)

	direct := iamdomain.Relationship{TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "owner", ResourceType: "company", ResourceID: company.ID}
	stored, changed, err := service.PutRelationship(t.Context(), direct)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.False(t, stored.CreatedAt.IsZero())
	revision, err := repo.policyRevision(t.Context(), "tenant")
	require.NoError(t, err)
	_, changed, err = service.PutRelationship(t.Context(), direct)
	require.NoError(t, err)
	assert.False(t, changed)
	revisionAfterNoop, err := repo.policyRevision(t.Context(), "tenant")
	require.NoError(t, err)
	assert.Equal(t, revision, revisionAfterNoop)

	inherited := iamdomain.Relationship{
		TenantID: "tenant", SubjectType: "entity", SubjectID: company.ID, SubjectRelation: "owner",
		Relation: "viewer", ResourceType: "store", ResourceID: store.ID,
	}
	_, changed, err = service.PutRelationship(t.Context(), inherited)
	require.NoError(t, err)
	assert.True(t, changed)
	filtered, err := service.ListRelationships(t.Context(), "tenant", iamdomain.RelationshipFilter{ResourceType: "store", ResourceID: store.ID})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, inherited.SubjectID, filtered[0].SubjectID)

	authorizer := NewAuthorizer(repo.db)
	explanation, err := authorizer.ExplainEntity(t.Context(), "tenant", store.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, explanation.Allowed)
	assert.Equal(t, "relationship_grant", explanation.Reason)
	require.Len(t, explanation.Matches, 1)
	assert.Equal(t, "viewer", explanation.Matches[0].Relation)
	assert.Len(t, explanation.Matches[0].Path, 2)
	allowed, err := authorizer.CheckEntity(t.Context(), "tenant", store.ID, "store_update", "principal")
	require.NoError(t, err)
	assert.False(t, allowed, "viewer must not satisfy update")
	constraint, err := authorizer.Constraint(t.Context(), "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.Equal(t, []string{store.ID}, constraint.ResourceIDs)

	denyRole, err := service.CreateRole(t.Context(), "tenant", "Denied viewer", "")
	require.NoError(t, err)
	_, err = service.GrantPermission(t.Context(), "tenant", denyRole.ID, "store_view")
	require.NoError(t, err)
	_, _, err = service.PutEntityRoleBinding(t.Context(), "tenant", store.ID, denyRole.ID, "principal", iamdomain.BindingDeny, time.Time{})
	require.NoError(t, err)
	explanation, err = authorizer.ExplainEntity(t.Context(), "tenant", store.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, explanation.Allowed)
	assert.Equal(t, "explicit_deny", explanation.Reason)
	_, err = service.DeleteEntityRoleBinding(t.Context(), "tenant", store.ID, denyRole.ID, "principal")
	require.NoError(t, err)

	changed, err = service.DeleteRelationship(t.Context(), inherited)
	require.NoError(t, err)
	assert.True(t, changed)
	allowed, err = authorizer.CheckEntity(t.Context(), "tenant", store.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed, "committed revocation must be immediate")
	assert.ErrorIs(t, service.DeleteEntity(t.Context(), "tenant", company.ID), iamdomain.ErrEntityHasRelationships)
	changed, err = service.DeleteRelationship(t.Context(), direct)
	require.NoError(t, err)
	assert.True(t, changed)
	require.NoError(t, service.DeleteEntity(t.Context(), "tenant", company.ID))
}

func TestBusinessResourceRelationshipAuthorizationLifecycle(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), "tenant", "principal", "Principal", "", "", iamdomain.MemberActive)
	require.NoError(t, err)
	entity, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "merchant", Name: "Merchant"})
	require.NoError(t, err)

	entityGrant := iamdomain.Relationship{
		TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "owner",
		ResourceType: "merchant", ResourceID: entity.ID,
	}
	_, changed, err := service.PutRelationship(t.Context(), entityGrant)
	require.NoError(t, err)
	assert.True(t, changed)
	resourceGrant := iamdomain.Relationship{
		TenantID: "tenant", EntityID: entity.ID, SubjectType: "entity", SubjectID: entity.ID, SubjectRelation: "owner",
		Relation: "viewer", ResourceType: "store", ResourceID: "store-opaque-id",
	}
	stored, changed, err := service.PutRelationship(t.Context(), resourceGrant)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.False(t, stored.CreatedAt.IsZero())

	items, err := service.ListRelationships(t.Context(), "tenant", iamdomain.RelationshipFilter{EntityID: entity.ID, ResourceType: "store", ResourceID: "store-opaque-id"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, entity.ID, items[0].EntityID)

	authorizer := NewAuthorizer(repo.db)
	explanation, err := service.ExplainResourceAuthorization(t.Context(), "tenant", entity.ID, "store", "store-opaque-id", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, explanation.Allowed)
	assert.Equal(t, "relationship_grant", explanation.Reason)
	require.Len(t, explanation.Matches, 1)
	assert.Len(t, explanation.Matches[0].Path, 2)
	assert.Equal(t, entity.ID, explanation.Matches[0].Path[1].EntityID)
	allowed, err := authorizer.CheckResource(t.Context(), "tenant", entity.ID, "store", "store-opaque-id", "store_update", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)

	denyRole, err := service.CreateRole(t.Context(), "tenant", "Denied business resource", "")
	require.NoError(t, err)
	_, err = service.GrantPermission(t.Context(), "tenant", denyRole.ID, "store_view")
	require.NoError(t, err)
	_, _, err = service.PutEntityRoleBinding(t.Context(), "tenant", entity.ID, denyRole.ID, "principal", iamdomain.BindingDeny, time.Time{})
	require.NoError(t, err)
	explanation, err = authorizer.ExplainResource(t.Context(), "tenant", entity.ID, "store", "store-opaque-id", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, explanation.Allowed)
	assert.Equal(t, "explicit_deny", explanation.Reason)
	_, err = service.DeleteEntityRoleBinding(t.Context(), "tenant", entity.ID, denyRole.ID, "principal")
	require.NoError(t, err)

	assert.ErrorIs(t, service.DeleteEntity(t.Context(), "tenant", entity.ID), iamdomain.ErrEntityHasRelationships)
	changed, err = service.DeleteRelationship(t.Context(), resourceGrant)
	require.NoError(t, err)
	assert.True(t, changed)
	allowed, err = authorizer.CheckResource(t.Context(), "tenant", entity.ID, "store", "store-opaque-id", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed, "committed business-resource revocation must be immediate")
	directResourceGrant := iamdomain.Relationship{
		TenantID: "tenant", EntityID: entity.ID, SubjectType: "principal", SubjectID: "principal",
		Relation: "viewer", ResourceType: "store", ResourceID: "store-opaque-id",
	}
	_, _, err = service.PutRelationship(t.Context(), directResourceGrant)
	require.NoError(t, err)
	explanation, err = service.ExplainResourceAuthorization(t.Context(), "tenant", entity.ID, "store", "store-opaque-id", "store_view", "principal")
	require.NoError(t, err)
	require.Len(t, explanation.Matches, 1)
	assert.Len(t, explanation.Matches[0].Path, 1)
	_, err = service.DeleteRelationship(t.Context(), directResourceGrant)
	require.NoError(t, err)

	disabled := iamdomain.EntityDisabled
	_, err = service.UpdateEntity(t.Context(), "tenant", entity.ID, iamdomain.EntityPatch{Status: &disabled})
	require.NoError(t, err)
	explanation, err = authorizer.ExplainResource(t.Context(), "tenant", entity.ID, "store", "store-opaque-id", "store_view", "principal")
	require.NoError(t, err)
	assert.Equal(t, "inactive_resource", explanation.Reason)
	_, _, err = service.PutRelationship(t.Context(), resourceGrant)
	assert.ErrorIs(t, err, iamdomain.ErrRelationshipResourceInactive)
	resourceGrant.EntityID = "missing"
	_, _, err = service.PutRelationship(t.Context(), resourceGrant)
	assert.ErrorIs(t, err, iamdomain.ErrEntityNotFound)
	resourceGrant.EntityID, resourceGrant.ResourceType = entity.ID, "undeclared"
	_, _, err = service.PutRelationship(t.Context(), resourceGrant)
	assert.ErrorIs(t, err, iamdomain.ErrInvalidRelationship)
}

func TestRelationshipValidityWindows(t *testing.T) {
	fixed := time.Date(2026, time.August, 3, 12, 0, 0, 123456789, time.UTC)
	repo := newIAMRepository(t)
	repo.now = func() time.Time { return fixed }
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	authorizer := NewAuthorizer(repo.db)
	authorizer.now = func() time.Time { return fixed }
	service := NewService(authz.DefaultRegistry(), repo, authorizer, newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), "tenant", "principal", "Principal", "", "", iamdomain.MemberActive)
	require.NoError(t, err)
	entity, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "store", Name: "Store"})
	require.NoError(t, err)

	startsAt, endsAt := fixed.Add(time.Hour), fixed.Add(2*time.Hour)
	grant := iamdomain.Relationship{
		TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "viewer",
		ResourceType: "store", ResourceID: entity.ID, StartsAt: &startsAt, EndsAt: &endsAt,
	}
	stored, changed, err := service.PutRelationship(t.Context(), grant)
	require.NoError(t, err)
	assert.True(t, changed)
	require.NotNil(t, stored.StartsAt)
	require.NotNil(t, stored.EndsAt)
	assert.Equal(t, startsAt.Truncate(time.Millisecond), *stored.StartsAt)
	assert.Equal(t, endsAt.Truncate(time.Millisecond), *stored.EndsAt)
	createdAt := stored.CreatedAt

	allowed, err := authorizer.CheckEntity(t.Context(), "tenant", entity.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed, "a future grant must not be active early")
	authorizer.now = func() time.Time { return fixed.Add(90 * time.Minute) }
	allowed, err = authorizer.CheckEntity(t.Context(), "tenant", entity.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)
	authorizer.now = func() time.Time { return endsAt }
	allowed, err = authorizer.CheckEntity(t.Context(), "tenant", entity.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed, "a grant must expire at its end instant")

	revision, err := repo.policyRevision(t.Context(), "tenant")
	require.NoError(t, err)
	updatedEnd := fixed.Add(3 * time.Hour)
	grant.StartsAt, grant.EndsAt = nil, &updatedEnd
	stored, changed, err = service.PutRelationship(t.Context(), grant)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, createdAt, stored.CreatedAt)
	assert.Nil(t, stored.StartsAt)
	require.NotNil(t, stored.EndsAt)
	updatedRevision, err := repo.policyRevision(t.Context(), "tenant")
	require.NoError(t, err)
	assert.Equal(t, revision+1, updatedRevision)
	_, changed, err = service.PutRelationship(t.Context(), grant)
	require.NoError(t, err)
	assert.False(t, changed)
	idempotentRevision, err := repo.policyRevision(t.Context(), "tenant")
	require.NoError(t, err)
	assert.Equal(t, updatedRevision, idempotentRevision)

	resourceEnd := fixed.Add(time.Hour)
	resourceGrant := iamdomain.Relationship{
		TenantID: "tenant", EntityID: entity.ID, SubjectType: "principal", SubjectID: "principal", Relation: "viewer",
		ResourceType: "store", ResourceID: "business-store", EndsAt: &resourceEnd,
	}
	_, changed, err = service.PutRelationship(t.Context(), resourceGrant)
	require.NoError(t, err)
	assert.True(t, changed)
	authorizer.now = func() time.Time { return fixed }
	allowed, err = authorizer.CheckResource(t.Context(), "tenant", entity.ID, "store", "business-store", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)
	authorizer.now = func() time.Time { return resourceEnd }
	allowed, err = authorizer.CheckResource(t.Context(), "tenant", entity.ID, "store", "business-store", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)

	past := fixed.Add(-time.Millisecond)
	for _, invalid := range []iamdomain.Relationship{
		{TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "viewer", ResourceType: "store", ResourceID: entity.ID, EndsAt: &past},
		{TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "viewer", ResourceType: "store", ResourceID: entity.ID, StartsAt: &endsAt, EndsAt: &startsAt},
		{TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "viewer", ResourceType: "store", ResourceID: entity.ID, StartsAt: &startsAt, EndsAt: &startsAt},
	} {
		_, _, err = service.PutRelationship(t.Context(), invalid)
		assert.ErrorIs(t, err, iamdomain.ErrInvalidRelationshipWindow)
	}
}

func TestRelationshipConditionsUseTrustedContextAndFailClosed(t *testing.T) {
	fixed := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repo := newIAMRepository(t)
	repo.now = func() time.Time { return fixed }
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	authorizer := NewAuthorizer(repo.db)
	authorizer.now = func() time.Time { return fixed }
	service := NewService(authz.DefaultRegistry(), repo, authorizer, newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), "tenant", "principal", "Principal", "", "", iamdomain.MemberActive)
	require.NoError(t, err)
	entity, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "store", Name: "Store"})
	require.NoError(t, err)

	condition := json.RawMessage(`{"version":1,"all":[{"gte":[{"context":"auth.acr"},{"value":2}]},{"eq":[{"context":"client.id"},{"value":"console"}]},{"between_time":["11:00","13:00","UTC"]}]}`)
	grant := iamdomain.Relationship{
		TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "viewer",
		ResourceType: "store", ResourceID: entity.ID, Condition: condition,
	}
	stored, changed, err := service.PutRelationship(t.Context(), grant)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.JSONEq(t, string(condition), string(stored.Condition))
	createdAt := stored.CreatedAt

	allowed, err := authorizer.CheckEntity(t.Context(), "tenant", entity.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed, "missing trusted context must not activate a conditioned grant")
	trusted := policyx.WithTrustedContext(t.Context(), policyx.TrustedContext{ACR: 2, AMR: []string{"pwd", "mfa"}, ClientID: "console"})
	explanation, err := authorizer.ExplainEntity(trusted, "tenant", entity.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, explanation.Allowed)
	require.Len(t, explanation.Matches, 1)
	require.Len(t, explanation.Matches[0].Path, 1)
	assert.JSONEq(t, string(condition), string(explanation.Matches[0].Path[0].Condition))

	wrongClient := policyx.WithTrustedContext(t.Context(), policyx.TrustedContext{ACR: 2, ClientID: "other"})
	allowed, err = authorizer.CheckEntity(wrongClient, "tenant", entity.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
	authorizer.now = func() time.Time { return fixed.Add(2 * time.Hour) }
	allowed, err = authorizer.CheckEntity(trusted, "tenant", entity.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)

	authorizer.now = func() time.Time { return fixed }
	revision, err := repo.policyRevision(t.Context(), "tenant")
	require.NoError(t, err)
	grant.Condition = json.RawMessage(`{"gte":[{"context":"auth.acr"},{"value":1}],"version":1}`)
	stored, changed, err = service.PutRelationship(t.Context(), grant)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, createdAt, stored.CreatedAt)
	updatedRevision, err := repo.policyRevision(t.Context(), "tenant")
	require.NoError(t, err)
	assert.Equal(t, revision+1, updatedRevision)
	_, changed, err = service.PutRelationship(t.Context(), grant)
	require.NoError(t, err)
	assert.False(t, changed)

	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"version":2,"eq":[{"context":"auth.acr"},{"value":1}]}`),
		json.RawMessage(`{"version":1,"eq":[{"context":"request.header.zone"},{"value":"trusted"}]}`),
	} {
		grant.Condition = invalid
		_, _, err = service.PutRelationship(t.Context(), grant)
		assert.ErrorIs(t, err, iamdomain.ErrInvalidRelationshipCondition)
	}

	_, err = repo.db.NewUpdate().Model((*relationshipRow)(nil)).Set("condition_json = ?", `{"version":9}`).Where("tenant_id = ? AND resource_id = ?", "tenant", entity.ID).Exec(t.Context())
	require.NoError(t, err)
	allowed, err = authorizer.CheckEntity(trusted, "tenant", entity.ID, "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed, "a corrupted stored condition must fail closed")
}

func TestRelationshipValidationRejectsInvalidFactsCyclesAndDepth(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), "tenant", "principal", "Principal", "", "", iamdomain.MemberActive)
	require.NoError(t, err)
	_, err = service.PutTenantMember(t.Context(), "tenant", "disabled", "Disabled", "", "", iamdomain.MemberDisabled)
	require.NoError(t, err)
	a, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "store", Name: "A"})
	require.NoError(t, err)
	b, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "store", Name: "B"})
	require.NoError(t, err)

	invalid := iamdomain.Relationship{TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", SubjectRelation: "member", Relation: "viewer", ResourceType: "store", ResourceID: a.ID}
	_, _, err = service.PutRelationship(t.Context(), invalid)
	assert.ErrorIs(t, err, iamdomain.ErrInvalidRelationship)
	_, _, err = service.PutRelationship(t.Context(), iamdomain.Relationship{})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidRelationship)
	_, err = service.DeleteRelationship(t.Context(), iamdomain.Relationship{})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidRelationship)
	missingPrincipal := iamdomain.Relationship{TenantID: "tenant", SubjectType: "principal", SubjectID: "missing", Relation: "viewer", ResourceType: "store", ResourceID: a.ID}
	_, _, err = service.PutRelationship(t.Context(), missingPrincipal)
	assert.ErrorIs(t, err, iamdomain.ErrRelationshipSubjectMissing)
	missing := iamdomain.Relationship{TenantID: "tenant", SubjectType: "group", SubjectID: "missing", SubjectRelation: "member", Relation: "viewer", ResourceType: "store", ResourceID: a.ID}
	_, _, err = service.PutRelationship(t.Context(), missing)
	assert.ErrorIs(t, err, iamdomain.ErrRelationshipSubjectMissing)
	missingPosition := iamdomain.Relationship{TenantID: "tenant", SubjectType: "position", SubjectID: "missing", SubjectRelation: "member", Relation: "viewer", ResourceType: "store", ResourceID: a.ID}
	_, _, err = service.PutRelationship(t.Context(), missingPosition)
	assert.ErrorIs(t, err, iamdomain.ErrRelationshipSubjectMissing)
	inactive := iamdomain.Relationship{TenantID: "tenant", SubjectType: "principal", SubjectID: "disabled", Relation: "viewer", ResourceType: "store", ResourceID: a.ID}
	_, _, err = service.PutRelationship(t.Context(), inactive)
	assert.ErrorIs(t, err, iamdomain.ErrRelationshipSubjectInactive)
	for _, invalidShape := range []iamdomain.Relationship{
		{TenantID: "tenant", SubjectType: "group", SubjectID: "group", Relation: "viewer", ResourceType: "store", ResourceID: a.ID},
		{TenantID: "tenant", SubjectType: "entity", SubjectID: a.ID, SubjectRelation: "member", Relation: "viewer", ResourceType: "store", ResourceID: b.ID},
		{TenantID: "tenant", SubjectType: "unknown", SubjectID: "subject", Relation: "viewer", ResourceType: "store", ResourceID: a.ID},
	} {
		_, _, err = service.PutRelationship(t.Context(), invalidShape)
		assert.ErrorIs(t, err, iamdomain.ErrInvalidRelationship)
	}
	_, err = service.ListRelationships(t.Context(), "tenant", iamdomain.RelationshipFilter{ResourceType: "bad type"})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, err = service.ListRelationships(t.Context(), "tenant", iamdomain.RelationshipFilter{ResourceID: string(make([]byte, 65))})
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)

	wrongType := iamdomain.Relationship{TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "viewer", ResourceType: "company", ResourceID: a.ID}
	_, _, err = service.PutRelationship(t.Context(), wrongType)
	assert.ErrorIs(t, err, iamdomain.ErrInvalidRelationship)
	disabledEntity, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "store", Name: "Disabled"})
	require.NoError(t, err)
	disabledStatus := iamdomain.EntityDisabled
	disabledEntity, err = service.UpdateEntity(t.Context(), "tenant", disabledEntity.ID, iamdomain.EntityPatch{Status: &disabledStatus})
	require.NoError(t, err)
	_, _, err = service.PutRelationship(t.Context(), iamdomain.Relationship{
		TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "viewer", ResourceType: "store", ResourceID: disabledEntity.ID,
	})
	assert.ErrorIs(t, err, iamdomain.ErrRelationshipResourceInactive)
	_, _, err = service.PutRelationship(t.Context(), iamdomain.Relationship{
		TenantID: "tenant", SubjectType: "entity", SubjectID: "missing", SubjectRelation: "owner", Relation: "viewer", ResourceType: "store", ResourceID: a.ID,
	})
	assert.ErrorIs(t, err, iamdomain.ErrRelationshipSubjectMissing)
	_, _, err = service.PutRelationship(t.Context(), iamdomain.Relationship{
		TenantID: "tenant", SubjectType: "entity", SubjectID: disabledEntity.ID, SubjectRelation: "owner", Relation: "viewer", ResourceType: "store", ResourceID: a.ID,
	})
	assert.ErrorIs(t, err, iamdomain.ErrRelationshipSubjectInactive)

	now := repo.now().UnixMilli()
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','active-group','Active Group','active group','static','','active',0,1,?,?),
		       ('tenant','disabled-group','Disabled Group','disabled group','static','','disabled',0,1,?,?)`, now, now, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_positions
		(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','active-position','active','Active Position','active',0,1,?,?),
		       ('tenant','disabled-position','disabled','Disabled Position','disabled',0,1,?,?)`, now, now, now, now)
	require.NoError(t, err)
	for _, subject := range []struct{ subjectType, subjectID string }{{"group", "active-group"}, {"position", "active-position"}} {
		_, changed, putErr := service.PutRelationship(t.Context(), iamdomain.Relationship{
			TenantID: "tenant", SubjectType: subject.subjectType, SubjectID: subject.subjectID, SubjectRelation: "member",
			Relation: "viewer", ResourceType: "store", ResourceID: a.ID,
		})
		require.NoError(t, putErr)
		assert.True(t, changed)
	}
	for _, subject := range []struct{ subjectType, subjectID string }{{"group", "disabled-group"}, {"position", "disabled-position"}} {
		_, _, err = service.PutRelationship(t.Context(), iamdomain.Relationship{
			TenantID: "tenant", SubjectType: subject.subjectType, SubjectID: subject.subjectID, SubjectRelation: "member",
			Relation: "viewer", ResourceType: "store", ResourceID: b.ID,
		})
		assert.ErrorIs(t, err, iamdomain.ErrRelationshipSubjectInactive)
	}

	first := iamdomain.Relationship{TenantID: "tenant", SubjectType: "entity", SubjectID: a.ID, SubjectRelation: "owner", Relation: "editor", ResourceType: "store", ResourceID: b.ID}
	second := iamdomain.Relationship{TenantID: "tenant", SubjectType: "entity", SubjectID: b.ID, SubjectRelation: "editor", Relation: "owner", ResourceType: "store", ResourceID: a.ID}
	future := time.Now().UTC().Add(time.Hour)
	second.StartsAt = &future
	_, _, err = service.PutRelationship(t.Context(), first)
	require.NoError(t, err)
	beforeCycle, err := service.ListRelationships(t.Context(), "tenant", iamdomain.RelationshipFilter{})
	require.NoError(t, err)
	_, _, err = service.PutRelationship(t.Context(), second)
	assert.ErrorIs(t, err, iamdomain.ErrRelationshipHierarchy)
	items, err := service.ListRelationships(t.Context(), "tenant", iamdomain.RelationshipFilter{})
	require.NoError(t, err)
	assert.Len(t, items, len(beforeCycle), "rejected cycle must roll back")

	nodes := make([]iamdomain.Entity, 17)
	for index := range nodes {
		nodes[index], err = service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "unit", Name: fmt.Sprintf("Unit %02d", index)})
		require.NoError(t, err)
	}
	_, _, err = service.PutRelationship(t.Context(), iamdomain.Relationship{
		TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "viewer", ResourceType: "unit", ResourceID: nodes[0].ID,
	})
	require.NoError(t, err)
	for index := 0; index < len(nodes)-1; index++ {
		_, _, err = service.PutRelationship(t.Context(), iamdomain.Relationship{
			TenantID: "tenant", SubjectType: "entity", SubjectID: nodes[index].ID, SubjectRelation: "viewer",
			Relation: "viewer", ResourceType: "unit", ResourceID: nodes[index+1].ID,
		})
		if index == len(nodes)-2 {
			assert.ErrorIs(t, err, iamdomain.ErrRelationshipHierarchy)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestRelationshipWriteRollsBackWhenAuditFails(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, "tenant"))
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err := service.PutTenantMember(t.Context(), "tenant", "principal", "Principal", "", "", iamdomain.MemberActive)
	require.NoError(t, err)
	entity, err := service.CreateEntity(t.Context(), iamdomain.Entity{TenantID: "tenant", Type: "store", Name: "Store"})
	require.NoError(t, err)
	entityGrant := iamdomain.Relationship{
		TenantID: "tenant", SubjectType: "principal", SubjectID: "principal", Relation: "viewer", ResourceType: "store", ResourceID: entity.ID,
	}
	resourceGrant := iamdomain.Relationship{
		TenantID: "tenant", EntityID: entity.ID, SubjectType: "principal", SubjectID: "principal",
		Relation: "viewer", ResourceType: "store", ResourceID: "business-store",
	}
	_, _, err = service.PutRelationship(t.Context(), entityGrant)
	require.NoError(t, err)
	_, _, err = service.PutRelationship(t.Context(), resourceGrant)
	require.NoError(t, err)
	revision, err := repo.policyRevision(t.Context(), "tenant")
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `CREATE TRIGGER reject_relationship_audit BEFORE INSERT ON iam_audit_events
		WHEN NEW.event_type = 'relationship_put' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`)
	require.NoError(t, err)
	endsAt := time.Now().UTC().Add(time.Hour)
	entityGrant.EndsAt = &endsAt
	entityGrant.Condition = json.RawMessage(`{"version":1,"gte":[{"context":"auth.acr"},{"value":2}]}`)
	_, _, err = service.PutRelationship(t.Context(), entityGrant)
	require.Error(t, err)
	resourceGrant.EndsAt = &endsAt
	resourceGrant.Condition = json.RawMessage(`{"version":1,"eq":[{"context":"client.id"},{"value":"console"}]}`)
	_, _, err = service.PutRelationship(t.Context(), resourceGrant)
	require.Error(t, err)
	items, listErr := service.ListRelationships(t.Context(), "tenant", iamdomain.RelationshipFilter{})
	require.NoError(t, listErr)
	require.Len(t, items, 1)
	assert.Nil(t, items[0].EndsAt)
	assert.Empty(t, items[0].Condition)
	resourceItems, listErr := service.ListRelationships(t.Context(), "tenant", iamdomain.RelationshipFilter{EntityID: entity.ID})
	require.NoError(t, listErr)
	require.Len(t, resourceItems, 1)
	assert.Nil(t, resourceItems[0].EndsAt)
	assert.Empty(t, resourceItems[0].Condition)
	revisionAfterFailure, err := repo.policyRevision(t.Context(), "tenant")
	require.NoError(t, err)
	assert.Equal(t, revision, revisionAfterFailure)

	resourceGrant.ResourceID = "business-store-new"
	_, _, err = service.PutRelationship(t.Context(), resourceGrant)
	require.Error(t, err)
	resourceItems, listErr = service.ListRelationships(t.Context(), "tenant", iamdomain.RelationshipFilter{EntityID: entity.ID})
	require.NoError(t, listErr)
	assert.Len(t, resourceItems, 1, "failed audit must roll back a new relationship")
}
