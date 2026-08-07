package iam

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

const policySnapshotAttempts = 3

// Authorizer evaluates tenant-scoped RBAC directly against the writable
// primary. Decisions are not cached, so committed revocations are immediately
// visible.
type Authorizer struct {
	db  *bun.DB
	now func() time.Time
}

type tenantGrant struct {
	PermissionCode string `bun:"permission_code"`
	RoleID         string `bun:"role_id"`
	SourceType     string `bun:"source_type"`
	SourceID       string `bun:"source_id"`
	DataScope      string `bun:"data_scope"`
	ConditionJSON  string `bun:"condition_json"`
}

type scopedGrant struct {
	PermissionCode string `bun:"permission_code"`
	RoleID         string `bun:"role_id"`
	Effect         string `bun:"effect"`
	ScopeID        string `bun:"scope_id"`
	EntityID       string `bun:"entity_id"`
	Depth          int    `bun:"depth"`
	ConditionJSON  string `bun:"condition_json"`
}

type authorizationSnapshot struct {
	revision           int64
	tenantActive       bool
	memberActive       bool
	entityIDs          []string
	tenantGrants       []tenantGrant
	scopedGrants       []scopedGrant
	relationshipGrants []relationshipGrant
	ownerIDs           []string
	departmentIDs      []string
}

func NewAuthorizer(db *bun.DB) *Authorizer {
	if db == nil {
		panic("iam authorizer requires database")
	}
	return &Authorizer{db: db, now: time.Now}
}

func (a *Authorizer) Check(ctx context.Context, tenantID, permission, subject string) (bool, error) {
	result, err := a.CheckBulk(ctx, tenantID, []string{permission}, subject)
	return result[permission], err
}

// CheckPlatform authorizes platform operations independently from tenant
// membership and tenant roles. The requested permission is honored: a full
// platform administrator holds every declared platform permission, while a
// restricted principal holds only its explicit iam_platform_grants rows.
// Mutable principal state is rechecked on every request so disabling a
// principal revokes platform access immediately.
func (a *Authorizer) CheckPlatform(ctx context.Context, permission, subject string) (bool, error) {
	if permission == "" || subject == "" || len(permission) > 128 || len(subject) > 255 {
		return false, fmt.Errorf("platform permission and subject are required")
	}
	// Fail closed for codes that are not declared platform actions. Route
	// registration already rejects them, so reaching this branch means a
	// caller bypassed the declaration gate.
	if action, ok := authz.DefaultRegistry().Find(permission); !ok || action.Scope != "platform" {
		return false, nil
	}
	count, err := a.db.NewSelect().TableExpr("iam_principals AS p").
		Where("p.id = ? AND p.status = 'active'", subject).
		Where("(EXISTS (SELECT 1 FROM iam_platform_administrators AS pa WHERE pa.principal_id = p.id)"+
			" OR EXISTS (SELECT 1 FROM iam_platform_grants AS pg WHERE pg.principal_id = p.id AND pg.permission_code = ?))", permission).
		Count(ctx)
	if err != nil {
		return false, fmt.Errorf("evaluate platform permission: %w", err)
	}
	return count == 1, nil
}

func (a *Authorizer) CheckBulk(ctx context.Context, tenantID string, permissions []string, subject string) (map[string]bool, error) {
	allowed := make(map[string]bool, len(permissions))
	if len(permissions) == 0 {
		return allowed, nil
	}
	tenantPermissions := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		if action, ok := authz.DefaultRegistry().Find(permission); !ok || action.Scope != "platform" {
			tenantPermissions = append(tenantPermissions, permission)
		}
	}
	requested, err := requestedPermissions(tenantPermissions...)
	if err != nil {
		return nil, err
	}
	memberActive, err := NewMembershipChecker(a.db).IsMemberActive(ctx, tenantID, subject)
	if err != nil {
		return nil, err
	}
	if !memberActive {
		return allowed, nil
	}
	trusted := policyx.TrustedFromContext(ctx, a.now())
	grants, err := a.loadTenantGrants(ctx, tenantID, subject, requested, trusted)
	if err != nil {
		return nil, err
	}
	grantSet := make(map[string]bool, len(grants))
	for _, grant := range grants {
		grantSet[grant.PermissionCode] = true
	}
	platformAdministrator, err := a.CheckPlatform(ctx, "platform_administer", subject)
	if err != nil {
		return nil, err
	}
	for _, permission := range permissions {
		if action, ok := authz.DefaultRegistry().Find(permission); ok && action.Scope == "platform" {
			allowed[permission] = platformAdministrator
		} else {
			allowed[permission] = grantSet[permission] || grantSet["tenant_administer"] || platformAdministrator
		}
	}
	return allowed, nil
}

// Constraint compiles tenant roles and inherited entity bindings into a
// parameter-only filter. ResourceIDs and DeniedIDs contain concrete entity IDs,
// so business repositories never need per-row authorization calls.
func (a *Authorizer) Constraint(ctx context.Context, tenantID, permission, subject string) (authz.DataConstraint, error) {
	if err := validateAuthorizationRequest(tenantID, "", permission, subject); err != nil {
		return authz.DataConstraint{}, err
	}
	trusted := policyx.TrustedFromContext(ctx, a.now())
	// Constraint returns concrete identifiers, so it genuinely needs the full
	// tenant enumeration.
	snapshot, err := a.snapshot(ctx, tenantID, "", permission, subject, true, trusted)
	if err != nil {
		return authz.DataConstraint{}, err
	}
	constraint := authz.DataConstraint{
		OwnerIDs: []string{}, ResourceIDs: []string{}, DepartmentIDs: []string{}, Ancestors: []authz.ResourceRef{}, DeniedIDs: []string{}, Revision: snapshot.revision,
	}
	if !snapshot.memberActive {
		return constraint, nil
	}
	constraint.OwnerIDs = snapshot.ownerIDs
	constraint.DepartmentIDs = snapshot.departmentIDs

	tenantRequested, tenantAdmin := tenantCapabilities(snapshot.tenantGrants, permission)
	action, _ := authz.DefaultRegistry().Find(permission)
	relationshipsByEntity := allowedRelationshipsByEntity(action, snapshot.relationshipGrants)
	constraint.AllowAll = tenantRequested || tenantAdmin
	ancestorSet := map[string]bool{}
	for _, grant := range snapshot.scopedGrants {
		if grant.Effect == "allow" && (grant.PermissionCode == permission || isAdministratorPermission(grant.PermissionCode)) {
			ancestorSet[grant.ScopeID] = true
		}
	}
	for id := range ancestorSet {
		constraint.Ancestors = append(constraint.Ancestors, authz.ResourceRef{Type: "entity", ID: id})
	}
	sort.Slice(constraint.Ancestors, func(i, j int) bool { return constraint.Ancestors[i].ID < constraint.Ancestors[j].ID })

	grantsByEntity := scopedByEntity(snapshot.scopedGrants)
	for _, entityID := range snapshot.entityIDs {
		allowed, explicitlyDenied, _ := decideEntity(permission, tenantRequested, tenantAdmin, grantsByEntity[entityID], relationshipsByEntity[entityID])
		if allowed {
			if !constraint.AllowAll {
				constraint.ResourceIDs = append(constraint.ResourceIDs, entityID)
			}
		} else if explicitlyDenied {
			constraint.DeniedIDs = append(constraint.DeniedIDs, entityID)
		}
	}
	return constraint, nil
}

// ExplainEntity returns the persisted matches and reason from the same
// evaluation used by CheckEntity.
func (a *Authorizer) ExplainEntity(ctx context.Context, tenantID, entityID, permission, subject string) (authz.Explanation, error) {
	if err := validateAuthorizationRequest(tenantID, entityID, permission, subject); err != nil {
		return authz.Explanation{}, err
	}
	trusted := policyx.TrustedFromContext(ctx, a.now())
	snapshot, err := a.snapshot(ctx, tenantID, entityID, permission, subject, false, trusted)
	if err != nil {
		return authz.Explanation{}, err
	}
	if !contains(snapshot.entityIDs, entityID) {
		return a.inactiveOrMissingEntity(ctx, tenantID, entityID, snapshot.revision)
	}
	return explainAuthorization(snapshot, tenantID, entityID, "", "", permission), nil
}

func (a *Authorizer) ExplainResource(ctx context.Context, tenantID, entityID, resourceType, resourceID, permission, subject string) (authz.Explanation, error) {
	if err := validateAuthorizationRequest(tenantID, entityID, permission, subject); err != nil || resourceType == "" || resourceID == "" || len(resourceType) > 64 || len(resourceID) > 255 {
		return authz.Explanation{}, fmt.Errorf("%w: invalid business resource authorization request", iamdomain.ErrInvalidArgument)
	}
	trusted := policyx.TrustedFromContext(ctx, a.now())
	trusted.Resource.Type = resourceType
	trusted.Resource.ID = resourceID
	snapshot, err := a.snapshot(ctx, tenantID, entityID, permission, subject, false, trusted)
	if err != nil {
		return authz.Explanation{}, err
	}
	if !contains(snapshot.entityIDs, entityID) {
		return a.inactiveOrMissingEntity(ctx, tenantID, entityID, snapshot.revision)
	}
	return explainAuthorization(snapshot, tenantID, entityID, resourceType, resourceID, permission), nil
}

func (a *Authorizer) inactiveOrMissingEntity(ctx context.Context, tenantID, entityID string, revision int64) (authz.Explanation, error) {
	count, err := a.db.NewSelect().Table("iam_entities").Where("tenant_id = ? AND id = ?", tenantID, entityID).Count(ctx)
	if err != nil {
		return authz.Explanation{}, fmt.Errorf("get authorization entity state: %w", err)
	}
	if count == 0 {
		return authz.Explanation{}, iamdomain.ErrEntityNotFound
	}
	return authz.Explanation{Reason: "inactive_resource", Revision: revision, Matches: []authz.DecisionMatch{}}, nil
}

func explainAuthorization(snapshot authorizationSnapshot, tenantID, entityID, resourceType, resourceID, permission string) authz.Explanation {
	explanation := authz.Explanation{Reason: "no_matching_grant", Revision: snapshot.revision, Matches: []authz.DecisionMatch{}}
	if !snapshot.memberActive {
		if !snapshot.tenantActive {
			explanation.Reason = "inactive_tenant"
		} else {
			explanation.Reason = "inactive_membership"
		}
		return explanation
	}
	tenantRequested, tenantAdmin := tenantCapabilities(snapshot.tenantGrants, permission)
	action, _ := authz.DefaultRegistry().Find(permission)
	for _, grant := range snapshot.tenantGrants {
		explanation.Matches = append(explanation.Matches, authz.DecisionMatch{
			PermissionCode: grant.PermissionCode, RoleID: grant.RoleID, SourceType: grant.SourceType, SourceID: grant.SourceID,
			ScopeType: "tenant", ScopeID: tenantID, Effect: "allow",
		})
	}
	scoped := scopedByEntity(snapshot.scopedGrants)[entityID]
	for _, grant := range scoped {
		explanation.Matches = append(explanation.Matches, authz.DecisionMatch{
			PermissionCode: grant.PermissionCode, RoleID: grant.RoleID, SourceType: "entity_binding", SourceID: grant.ScopeID,
			ScopeType: "entity", ScopeID: grant.ScopeID, Effect: grant.Effect, Inherited: grant.Depth > 0,
		})
	}
	relationshipGranted := false
	for _, grant := range snapshot.relationshipGrants {
		entityTarget := resourceType == "" && grant.EntityID == "" && grant.ResourceID == entityID
		resourceTarget := resourceType != "" && grant.EntityID == entityID && grant.ResourceType == resourceType && grant.ResourceID == resourceID
		if (!entityTarget && !resourceTarget) || !relationshipAllowed(action, grant) {
			continue
		}
		relationshipGranted = true
		explanation.Matches = append(explanation.Matches, authz.DecisionMatch{
			PermissionCode: permission, SourceType: "relationship", SourceID: grant.Path[0].SubjectID,
			ScopeType: grant.ResourceType, ScopeID: grant.ResourceID, Effect: "allow", Relation: grant.Relation, Path: grant.Path,
		})
	}
	explanation.Allowed, _, explanation.Reason = decideEntity(permission, tenantRequested, tenantAdmin, scoped, relationshipGranted)
	sort.Slice(explanation.Matches, func(i, j int) bool {
		left, right := explanation.Matches[i], explanation.Matches[j]
		if left.Effect != right.Effect {
			return left.Effect < right.Effect
		}
		if left.PermissionCode != right.PermissionCode {
			return left.PermissionCode < right.PermissionCode
		}
		if left.ScopeID != right.ScopeID {
			return left.ScopeID < right.ScopeID
		}
		return left.RoleID < right.RoleID
	})
	return explanation
}

func (a *Authorizer) CheckEntity(ctx context.Context, tenantID, entityID, permission, subject string) (bool, error) {
	explanation, err := a.ExplainEntity(ctx, tenantID, entityID, permission, subject)
	return explanation.Allowed, err
}

func (a *Authorizer) CheckResource(ctx context.Context, tenantID, entityID, resourceType, resourceID, permission, subject string) (bool, error) {
	explanation, err := a.ExplainResource(ctx, tenantID, entityID, resourceType, resourceID, permission, subject)
	return explanation.Allowed, err
}

// snapshot reads one consistent view of the policy state backing a decision.
// focusEntityID narrows the entity load to a single row: only Constraint needs
// the full tenant enumeration, while per-entity checks just need to know
// whether their target is active. Loading every entity for those was O(tenant
// entities) on the hot authorization path.
func (a *Authorizer) snapshot(ctx context.Context, tenantID, focusEntityID, permission, subject string, includeDataScope bool, trusted policyx.TrustedContext) (authorizationSnapshot, error) {
	requested, _ := requestedPermissions(permission)
	for range policySnapshotAttempts {
		before, err := policyx.Current(ctx, a.db, tenantID)
		if err != nil {
			return authorizationSnapshot{}, err
		}
		var snapshot authorizationSnapshot
		snapshot.tenantActive, snapshot.memberActive, err = NewMembershipChecker(a.db).stateOn(ctx, a.db, tenantID, subject)
		if err != nil {
			return authorizationSnapshot{}, err
		}
		entities := a.db.NewSelect().Table("iam_entities").Column("id").Where("tenant_id = ? AND status = ?", tenantID, iamdomain.EntityActive)
		if focusEntityID != "" {
			entities = entities.Where("id = ?", focusEntityID)
		}
		if err := entities.Order("id ASC").Scan(ctx, &snapshot.entityIDs); err != nil {
			return authorizationSnapshot{}, fmt.Errorf("list authorization entities: %w", err)
		}
		if snapshot.memberActive {
			snapshot.tenantGrants, err = a.loadTenantGrants(ctx, tenantID, subject, requested, trusted)
			if err != nil {
				return authorizationSnapshot{}, err
			}
			if includeDataScope {
				snapshot.ownerIDs, snapshot.departmentIDs, err = a.loadDataScopeFacts(ctx, tenantID, permission, subject, snapshot.tenantGrants)
				if err != nil {
					return authorizationSnapshot{}, err
				}
			}
			snapshot.scopedGrants, err = a.loadScopedGrants(ctx, tenantID, subject, requested, trusted)
			if err != nil {
				return authorizationSnapshot{}, err
			}
			if action, ok := authz.DefaultRegistry().Find(permission); ok && len(action.AllowedRelations) > 0 {
				snapshot.relationshipGrants, err = a.loadRelationshipGrants(ctx, tenantID, subject, trusted)
				if err != nil {
					return authorizationSnapshot{}, err
				}
			}
		}
		after, err := policyx.Current(ctx, a.db, tenantID)
		if err != nil {
			return authorizationSnapshot{}, err
		}
		if before == after {
			snapshot.revision = after
			return snapshot, nil
		}
	}
	return authorizationSnapshot{}, iamdomain.ErrAuthorizationChanged
}

func (a *Authorizer) loadTenantGrants(ctx context.Context, tenantID, subject string, requested []string, trusted policyx.TrustedContext) ([]tenantGrant, error) {
	if tenantID == "" || subject == "" {
		return nil, fmt.Errorf("tenant and subject are required")
	}
	if len(requested) == 0 {
		return []tenantGrant{}, nil
	}
	now := trusted.Time.UTC().UnixMilli()
	grants := []tenantGrant{}
	err := a.db.NewRaw(`
WITH effective_roles(role_id, source_type, source_id) AS (
    SELECT m.role_id, 'role_member', m.user_subject
    FROM iam_role_members m
    JOIN iam_tenant_members tm
      ON tm.tenant_id = m.tenant_id AND tm.user_subject = m.user_subject AND tm.status = ?
    WHERE m.tenant_id = ? AND m.user_subject = ?
    UNION
	SELECT grants.role_id, 'temporary_role', grants.id
	FROM iam_temporary_role_grants grants
	JOIN iam_tenant_members tm
	  ON tm.tenant_id = grants.tenant_id AND tm.user_subject = grants.principal_id AND tm.status = ?
	WHERE grants.tenant_id = ? AND grants.principal_id = ?
	  AND grants.starts_at <= ? AND grants.ends_at > ?
	UNION
    SELECT b.role_id, 'group', b.group_id
    FROM iam_group_role_bindings b
    JOIN iam_groups g
      ON g.tenant_id = b.tenant_id AND g.id = b.group_id AND g.status = 'active' AND g.group_type = 'static'
    JOIN iam_group_members gm
      ON gm.tenant_id = b.tenant_id AND gm.group_id = b.group_id
    JOIN iam_tenant_members tm
      ON tm.tenant_id = gm.tenant_id AND tm.user_subject = gm.principal_id AND tm.status = ?
    WHERE b.tenant_id = ? AND gm.principal_id = ?
      AND (gm.starts_at = 0 OR gm.starts_at <= ?)
      AND (gm.ends_at = 0 OR gm.ends_at > ?)
    UNION
    SELECT b.role_id, 'position', b.position_id
    FROM iam_position_role_bindings b
    JOIN iam_positions p
      ON p.tenant_id = b.tenant_id AND p.id = b.position_id AND p.status = 'active'
    JOIN iam_position_members pm
      ON pm.tenant_id = b.tenant_id AND pm.position_id = b.position_id
    JOIN iam_tenant_members tm
      ON tm.tenant_id = pm.tenant_id AND tm.user_subject = pm.principal_id AND tm.status = ?
    WHERE b.tenant_id = ? AND pm.principal_id = ?
      AND (pm.starts_at = 0 OR pm.starts_at <= ?)
      AND (pm.ends_at = 0 OR pm.ends_at > ?)
)
SELECT DISTINCT rp.permission_code, er.role_id, er.source_type, er.source_id, COALESCE(ds.scope_type, 'all') AS data_scope, rp.condition_json
FROM iam_role_permissions rp
JOIN effective_roles er ON er.role_id = rp.role_id
LEFT JOIN iam_role_data_scopes ds ON ds.tenant_id = rp.tenant_id AND ds.role_id = rp.role_id
WHERE rp.tenant_id = ? AND rp.permission_code IN (?)`,
		MemberActive, tenantID, subject,
		MemberActive, tenantID, subject, now, now,
		MemberActive, tenantID, subject, now, now,
		MemberActive, tenantID, subject, now, now,
		tenantID, bun.List(requested)).Scan(ctx, &grants)
	if err != nil {
		return nil, fmt.Errorf("evaluate tenant permissions: %w", err)
	}
	dynamicGroupIDs, err := matchingDynamicGroupIDs(ctx, a.db, tenantID, subject)
	if err != nil {
		return nil, err
	}
	if len(dynamicGroupIDs) > 0 {
		dynamicGrants := make([]tenantGrant, 0)
		if err := a.db.NewSelect().TableExpr("iam_group_role_bindings AS b").
			ColumnExpr("rp.permission_code, b.role_id, 'group' AS source_type, b.group_id AS source_id, COALESCE(ds.scope_type, 'all') AS data_scope, rp.condition_json").
			Join("JOIN iam_role_permissions AS rp ON rp.tenant_id = b.tenant_id AND rp.role_id = b.role_id").
			Join("LEFT JOIN iam_role_data_scopes AS ds ON ds.tenant_id = b.tenant_id AND ds.role_id = b.role_id").
			Where("b.tenant_id = ? AND b.group_id IN (?) AND rp.permission_code IN (?)", tenantID, bun.List(dynamicGroupIDs), bun.List(requested)).
			Scan(ctx, &dynamicGrants); err != nil {
			return nil, fmt.Errorf("evaluate dynamic group permissions: %w", err)
		}
		grants = append(grants, dynamicGrants...)
	}
	return matchingTenantGrants(grants, trusted)
}

func (a *Authorizer) loadDataScopeFacts(ctx context.Context, tenantID, permission, subject string, grants []tenantGrant) ([]string, []string, error) {
	ownerSet := map[string]struct{}{}
	selectedRoles := map[string]struct{}{}
	needDepartment, needDescendants := false, false
	for _, grant := range grants {
		if grant.PermissionCode != permission {
			continue
		}
		switch DataScope(grant.DataScope) {
		case DataScopeSelf:
			ownerSet[subject] = struct{}{}
		case DataScopeDepartment:
			needDepartment = true
		case DataScopeDepartmentAndDescendants:
			needDescendants = true
		case DataScopeSelectedDepartments:
			selectedRoles[grant.RoleID] = struct{}{}
		}
	}
	departmentSet := map[string]struct{}{}
	if needDepartment || needDescendants {
		var departmentID string
		err := a.db.NewSelect().TableExpr("iam_member_departments AS md").ColumnExpr("md.department_id").
			Join("JOIN iam_departments AS d ON d.tenant_id = md.tenant_id AND d.id = md.department_id AND d.status = 'active'").
			Where("md.tenant_id = ? AND md.principal_id = ?", tenantID, subject).Scan(ctx, &departmentID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("get authorization member department: %w", err)
		}
		if departmentID != "" {
			if needDepartment {
				departmentSet[departmentID] = struct{}{}
			}
			if needDescendants {
				ids := make([]string, 0)
				if err := a.db.NewSelect().TableExpr("iam_department_closure AS c").ColumnExpr("c.descendant_id").
					Join("JOIN iam_departments AS d ON d.tenant_id = c.tenant_id AND d.id = c.descendant_id AND d.status = 'active'").
					Where("c.tenant_id = ? AND c.ancestor_id = ?", tenantID, departmentID).Scan(ctx, &ids); err != nil {
					return nil, nil, fmt.Errorf("list authorization department descendants: %w", err)
				}
				for _, id := range ids {
					departmentSet[id] = struct{}{}
				}
			}
		}
	}
	if len(selectedRoles) > 0 {
		roleIDs := make([]string, 0, len(selectedRoles))
		for roleID := range selectedRoles {
			roleIDs = append(roleIDs, roleID)
		}
		ids := make([]string, 0)
		if err := a.db.NewSelect().TableExpr("iam_role_scope_departments AS rsd").ColumnExpr("rsd.department_id").Distinct().
			Join("JOIN iam_departments AS d ON d.tenant_id = rsd.tenant_id AND d.id = rsd.department_id AND d.status = 'active'").
			Where("rsd.tenant_id = ? AND rsd.role_id IN (?)", tenantID, bun.List(roleIDs)).Scan(ctx, &ids); err != nil {
			return nil, nil, fmt.Errorf("list authorization selected departments: %w", err)
		}
		for _, id := range ids {
			departmentSet[id] = struct{}{}
		}
	}
	owners := mapKeys(ownerSet)
	departments := mapKeys(departmentSet)
	return owners, departments, nil
}

func mapKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (a *Authorizer) loadScopedGrants(ctx context.Context, tenantID, subject string, requested []string, trusted policyx.TrustedContext) ([]scopedGrant, error) {
	grants := []scopedGrant{}
	err := a.db.NewRaw(`
WITH RECURSIVE expanded(permission_code, role_id, effect, scope_id, condition_json, entity_id, depth) AS (
    SELECT p.permission_code, b.role_id, b.effect, b.scope_id, p.condition_json, e.id, 0
    FROM iam_role_bindings b
    JOIN iam_tenant_members tm
      ON tm.tenant_id = b.tenant_id AND tm.user_subject = b.principal_id AND tm.status = ?
    JOIN iam_role_permissions p
      ON p.tenant_id = b.tenant_id AND p.role_id = b.role_id
    JOIN iam_entities e
      ON e.tenant_id = b.tenant_id AND e.id = b.scope_id AND e.status = 'active'
    WHERE b.tenant_id = ? AND b.principal_id = ? AND b.scope_type = 'entity'
      AND (b.expires_at = 0 OR b.expires_at > ?)
      AND p.permission_code IN (?)
    UNION ALL
    SELECT x.permission_code, x.role_id, x.effect, x.scope_id, x.condition_json, e.id, x.depth + 1
    FROM expanded x
    JOIN iam_entities e ON e.tenant_id = ? AND e.parent_id = x.entity_id AND e.status = 'active'
    WHERE x.depth < 15
)
SELECT DISTINCT permission_code, role_id, effect, scope_id, condition_json, entity_id, depth
FROM expanded`, MemberActive, tenantID, subject, trusted.Time.UTC().UnixMilli(), bun.List(requested), tenantID).Scan(ctx, &grants)
	if err != nil {
		return nil, fmt.Errorf("evaluate scoped entity permissions: %w", err)
	}
	return matchingScopedGrants(grants, trusted)
}

func matchingTenantGrants(grants []tenantGrant, trusted policyx.TrustedContext) ([]tenantGrant, error) {
	matched := make([]tenantGrant, 0, len(grants))
	for _, grant := range grants {
		ok, err := policyx.EvaluateCondition(json.RawMessage(grant.ConditionJSON), trusted)
		if err != nil {
			return nil, fmt.Errorf("evaluate role permission condition %s/%s: %w", grant.RoleID, grant.PermissionCode, err)
		}
		if ok {
			matched = append(matched, grant)
		}
	}
	return matched, nil
}

func matchingScopedGrants(grants []scopedGrant, trusted policyx.TrustedContext) ([]scopedGrant, error) {
	matched := make([]scopedGrant, 0, len(grants))
	for _, grant := range grants {
		ok, err := policyx.EvaluateCondition(json.RawMessage(grant.ConditionJSON), trusted)
		if err != nil {
			return nil, fmt.Errorf("evaluate scoped role permission condition %s/%s: %w", grant.RoleID, grant.PermissionCode, err)
		}
		if ok {
			matched = append(matched, grant)
		}
	}
	return matched, nil
}

func requestedPermissions(permissions ...string) ([]string, error) {
	requested := make([]string, 0, len(permissions)+2)
	seen := map[string]bool{}
	for _, permission := range append(append([]string{}, permissions...), "tenant_administer") {
		if permission == "" {
			return nil, fmt.Errorf("permission is empty")
		}
		if !seen[permission] {
			seen[permission] = true
			requested = append(requested, permission)
		}
	}
	return requested, nil
}

func validateAuthorizationRequest(tenantID, entityID, permission, subject string) error {
	if tenantID == "" || permission == "" || subject == "" || len(tenantID) > 128 || len(permission) > 128 || len(subject) > 255 {
		return fmt.Errorf("%w: tenant, permission, and subject are required", iamdomain.ErrInvalidArgument)
	}
	if entityID != "" && len(entityID) > 64 {
		return fmt.Errorf("%w: invalid entity id", iamdomain.ErrInvalidArgument)
	}
	return nil
}

func tenantCapabilities(grants []tenantGrant, permission string) (requested, administrator bool) {
	for _, grant := range grants {
		requested = requested || (grant.PermissionCode == permission && grant.DataScope == string(DataScopeAll))
		administrator = administrator || isAdministratorPermission(grant.PermissionCode)
	}
	return requested, administrator
}

func scopedByEntity(grants []scopedGrant) map[string][]scopedGrant {
	result := make(map[string][]scopedGrant)
	for _, grant := range grants {
		result[grant.EntityID] = append(result[grant.EntityID], grant)
	}
	return result
}

func decideEntity(permission string, tenantRequested, tenantAdmin bool, grants []scopedGrant, relationshipAllow bool) (allowed, explicitlyDenied bool, reason string) {
	requestedAllow, administratorAllow := tenantRequested, tenantAdmin
	requestedDeny, administratorDeny := false, false
	for _, grant := range grants {
		requested := grant.PermissionCode == permission
		administrator := isAdministratorPermission(grant.PermissionCode)
		if grant.Effect == "deny" {
			requestedDeny = requestedDeny || requested
			administratorDeny = administratorDeny || administrator
		} else {
			requestedAllow = requestedAllow || requested
			administratorAllow = administratorAllow || administrator
		}
	}
	if requestedDeny {
		return false, true, "explicit_deny"
	}
	if requestedAllow {
		return true, false, "permission_grant"
	}
	if relationshipAllow {
		return true, false, "relationship_grant"
	}
	if administratorDeny && administratorAllow {
		return false, true, "administrator_scope_denied"
	}
	if administratorAllow {
		return true, false, "administrator_grant"
	}
	return false, false, "no_matching_grant"
}

func allowedRelationshipsByEntity(action authz.Action, grants []relationshipGrant) map[string]bool {
	result := make(map[string]bool)
	for _, grant := range grants {
		if relationshipAllowed(action, grant) {
			result[grant.ResourceID] = true
		}
	}
	return result
}

func isAdministratorPermission(permission string) bool {
	return permission == "tenant_administer"
}

func contains(values []string, expected string) bool {
	index := sort.SearchStrings(values, expected)
	return index < len(values) && values[index] == expected
}
