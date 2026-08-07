package iam_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestIAMManagementHTTPFlow(t *testing.T) {
	db, api, authorization := newIAMAPI(t)
	header := []string{authorization.header, authz.TenantHeader + ": tenant"}

	for _, path := range []string{"/iam/permission-catalog", "/iam/scope-model", "/iam/menu-catalog", "/iam/entities", "/iam/roles", "/iam/members?limit=50", "/iam/menus"} {
		response := api.Get(path, header[0], header[1])
		assert.Equal(t, http.StatusOK, response.Code, path+": "+response.Body.String())
	}
	dataScopePath := api.OpenAPI().Paths["/iam/roles/{role_id}/data-scope"]
	require.NotNil(t, dataScopePath)
	assert.Equal(t, "iam-get-role-data-scope", dataScopePath.Get.OperationID)
	assert.Equal(t, "iam-set-role-data-scope", dataScopePath.Put.OperationID)
	emptyMenus := api.Get("/iam/me/menus", header[0], header[1])
	require.Equal(t, http.StatusOK, emptyMenus.Code, emptyMenus.Body.String())
	var emptyMenuEnvelope struct {
		Data []iam.MenuItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(emptyMenus.Body.Bytes(), &emptyMenuEnvelope))
	assert.NotNil(t, emptyMenuEnvelope.Data)
	assert.Empty(t, emptyMenuEnvelope.Data)

	createdRole := api.Post("/iam/roles", header[0], header[1], map[string]any{"name": "Managers", "description": "Tenant managers"})
	require.Equal(t, http.StatusCreated, createdRole.Code, createdRole.Body.String())
	roleID := responseID(t, createdRole.Body.Bytes())
	require.NotEmpty(t, roleID)
	assert.Equal(t, http.StatusOK, api.Get("/iam/roles/"+roleID, header[0], header[1]).Code)
	now := time.Now().UTC().UnixMilli()
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_departments
		(tenant_id,id,parent_id,name,name_key,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','department','','Operations','operations','active',0,1,?,?)`, now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_department_closure
		(tenant_id,ancestor_id,descendant_id,depth) VALUES ('tenant','department','department',0)`)
	require.NoError(t, err)
	setDataScope := api.Put("/iam/roles/"+roleID+"/data-scope", header[0], header[1], map[string]any{
		"scope": "selected_departments", "department_ids": []string{"department"},
	})
	require.Equal(t, http.StatusOK, setDataScope.Code, setDataScope.Body.String())
	assert.Contains(t, setDataScope.Body.String(), `"scope":"selected_departments"`)
	assert.Contains(t, setDataScope.Body.String(), `"department_ids":["department"]`)
	assert.Equal(t, http.StatusOK, api.Get("/iam/roles/"+roleID+"/data-scope", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Patch("/iam/roles/"+roleID, header[0], header[1], map[string]any{"name": "Operators"}).Code)
	assert.Equal(t, http.StatusOK, api.Put("/iam/roles/"+roleID+"/permissions/user_view", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Get("/iam/roles/"+roleID+"/permissions", header[0], header[1]).Code)
	setPermissionCondition := api.Put("/iam/roles/"+roleID+"/permissions/user_view/condition", header[0], header[1], map[string]any{
		"condition": map[string]any{"version": 1, "gte": []any{map[string]any{"context": "auth.acr"}, map[string]any{"value": 1}}},
	})
	require.Equal(t, http.StatusOK, setPermissionCondition.Code, setPermissionCondition.Body.String())
	assert.Contains(t, setPermissionCondition.Body.String(), `"permission_code":"user_view"`)
	assert.Contains(t, setPermissionCondition.Body.String(), `"condition":{"gte":`)
	permissionGrants := api.Get("/iam/roles/"+roleID+"/permission-grants", header[0], header[1])
	require.Equal(t, http.StatusOK, permissionGrants.Code, permissionGrants.Body.String())
	assert.Contains(t, permissionGrants.Body.String(), `"condition":{"gte":`)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','group','Operators','operators','static','','active',0,1,?,?)`, now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_positions
		(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','position','operator','Operator','active',0,1,?,?)`, now, now)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, api.Put("/iam/roles/"+roleID+"/directory-bindings/group/group", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Put("/iam/roles/"+roleID+"/directory-bindings/position/position", header[0], header[1]).Code)
	directoryBindings := api.Get("/iam/roles/"+roleID+"/directory-bindings", header[0], header[1])
	require.Equal(t, http.StatusOK, directoryBindings.Code, directoryBindings.Body.String())
	assert.Contains(t, directoryBindings.Body.String(), `"assignee_type":"group"`)
	assert.Contains(t, directoryBindings.Body.String(), `"assignee_type":"position"`)

	member := api.Post("/iam/members", header[0], header[1], map[string]any{
		"subject": "external-subject", "display_name": "External User", "email": "external@example.com", "department_id": "department", "status": "active",
	})
	require.Equal(t, http.StatusCreated, member.Code, member.Body.String())
	assert.Contains(t, member.Body.String(), `"department_id":"department"`)
	assert.Equal(t, http.StatusOK, api.Get("/iam/members/external-subject", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Patch("/iam/members/external-subject", header[0], header[1], map[string]any{"status": "disabled"}).Code)
	assert.Equal(t, http.StatusOK, api.Put("/iam/roles/"+roleID+"/members/"+authorization.principalID, header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Get("/iam/roles/"+roleID+"/members", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Get("/iam/members/"+authorization.principalID+"/roles", header[0], header[1]).Code)

	rootEntity := api.Post("/iam/entities", header[0], header[1], map[string]any{
		"type": "company", "name": "Acme", "metadata": map[string]any{"region": "west"},
	})
	require.Equal(t, http.StatusCreated, rootEntity.Code, rootEntity.Body.String())
	rootEntityID := responseID(t, rootEntity.Body.Bytes())
	childEntity := api.Post("/iam/entities", header[0], header[1], map[string]any{
		"parent_id": rootEntityID, "type": "store", "name": "Main",
	})
	require.Equal(t, http.StatusCreated, childEntity.Code, childEntity.Body.String())
	childEntityID := responseID(t, childEntity.Body.Bytes())
	assert.Equal(t, http.StatusOK, api.Get("/iam/entities/"+childEntityID, header[0], header[1]).Code)
	relationshipMember := api.Post("/iam/members", header[0], header[1], map[string]any{
		"subject": "relationship-user", "display_name": "Relationship User", "status": "active",
	})
	require.Equal(t, http.StatusCreated, relationshipMember.Code, relationshipMember.Body.String())
	directRelationship := map[string]any{
		"subject_type": "principal", "subject_id": "relationship-user", "relation": "owner",
		"resource_type": "company", "resource_id": rootEntityID,
		"starts_at": time.Now().UTC().Add(-time.Minute), "ends_at": time.Now().UTC().Add(time.Hour),
		"condition": map[string]any{"version": 1, "gte": []any{map[string]any{"context": "auth.acr"}, map[string]any{"value": 1}}},
	}
	directRelationshipResponse := api.Post("/iam/relationships", header[0], header[1], directRelationship)
	require.Equal(t, http.StatusOK, directRelationshipResponse.Code, directRelationshipResponse.Body.String())
	assert.Contains(t, directRelationshipResponse.Body.String(), `"starts_at":`)
	assert.Contains(t, directRelationshipResponse.Body.String(), `"ends_at":`)
	assert.Contains(t, directRelationshipResponse.Body.String(), `"condition":{"gte":`)
	invalidRelationshipWindow := map[string]any{
		"subject_type": "principal", "subject_id": "relationship-user", "relation": "viewer",
		"resource_type": "store", "resource_id": childEntityID, "ends_at": time.Now().UTC().Add(-time.Minute),
	}
	invalidWindowResponse := api.Post("/iam/relationships", header[0], header[1], invalidRelationshipWindow)
	require.Equal(t, http.StatusUnprocessableEntity, invalidWindowResponse.Code, invalidWindowResponse.Body.String())
	assert.Contains(t, invalidWindowResponse.Body.String(), i18n.TContext(t.Context(), "invalid_relationship_window"))
	assert.NotContains(t, invalidWindowResponse.Body.String(), `"message":"invalid_relationship_window"`)
	inheritedRelationship := map[string]any{
		"subject_type": "entity", "subject_id": rootEntityID, "subject_relation": "owner", "relation": "viewer",
		"resource_type": "store", "resource_id": childEntityID,
	}
	require.Equal(t, http.StatusOK, api.Post("/iam/relationships", header[0], header[1], inheritedRelationship).Code)
	listedRelationships := api.Get("/iam/relationships?resource_id="+childEntityID, header[0], header[1])
	require.Equal(t, http.StatusOK, listedRelationships.Code, listedRelationships.Body.String())
	assert.Contains(t, listedRelationships.Body.String(), `"subject_relation":"owner"`)
	checkRelationship := api.Post("/iam/authorization/check", header[0], header[1], map[string]any{
		"entity_id": childEntityID, "permission_code": "store_view", "subject": "relationship-user",
	})
	require.Equal(t, http.StatusOK, checkRelationship.Code, checkRelationship.Body.String())
	assert.Contains(t, checkRelationship.Body.String(), `"allowed":true`)
	assert.Contains(t, checkRelationship.Body.String(), `"reason":"relationship_grant"`)
	resourceRelationship := map[string]any{
		"entity_id": childEntityID, "subject_type": "entity", "subject_id": rootEntityID, "subject_relation": "owner",
		"relation": "viewer", "resource_type": "store", "resource_id": "business-store-1",
	}
	require.Equal(t, http.StatusOK, api.Post("/iam/relationships", header[0], header[1], resourceRelationship).Code)
	listedResourceRelationships := api.Get("/iam/relationships?entity_id="+childEntityID+"&resource_type=store", header[0], header[1])
	require.Equal(t, http.StatusOK, listedResourceRelationships.Code, listedResourceRelationships.Body.String())
	assert.Contains(t, listedResourceRelationships.Body.String(), `"resource_id":"business-store-1"`)
	checkResource := api.Post("/iam/authorization/check", header[0], header[1], map[string]any{
		"entity_id": childEntityID, "resource_type": "store", "resource_id": "business-store-1",
		"permission_code": "store_view", "subject": "relationship-user",
	})
	require.Equal(t, http.StatusOK, checkResource.Code, checkResource.Body.String())
	assert.Contains(t, checkResource.Body.String(), `"allowed":true`)
	resourceExplanation := api.Post("/iam/authorization/explain", header[0], header[1], map[string]any{
		"entity_id": childEntityID, "resource_type": "store", "resource_id": "business-store-1",
		"permission_code": "store_view", "subject": "relationship-user",
	})
	require.Equal(t, http.StatusOK, resourceExplanation.Code, resourceExplanation.Body.String())
	assert.Contains(t, resourceExplanation.Body.String(), `"entity_id":"`+childEntityID+`"`)
	for _, invalidResourceAuthorization := range []map[string]any{
		{"entity_id": childEntityID, "resource_type": "store", "permission_code": "store_view", "subject": "relationship-user"},
		{"entity_id": childEntityID, "resource_id": "business-store-1", "permission_code": "store_view", "subject": "relationship-user"},
		{"entity_id": childEntityID, "resource_type": "store", "resource_id": "business-store-1", "permission_code": "merchant_view", "subject": "relationship-user"},
	} {
		response := api.Post("/iam/authorization/check", header[0], header[1], invalidResourceAuthorization)
		require.Equal(t, http.StatusUnprocessableEntity, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), i18n.TContext(t.Context(), "invalid_resource_authorization"))
		assert.NotContains(t, response.Body.String(), `"message":"invalid_resource_authorization"`)
	}
	missingResourceParent := map[string]any{
		"entity_id": "missing", "subject_type": "principal", "subject_id": "relationship-user",
		"relation": "viewer", "resource_type": "store", "resource_id": "business-store-2",
	}
	assert.Equal(t, http.StatusNotFound, api.Post("/iam/relationships", header[0], header[1], missingResourceParent).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Get("/iam/relationships?resource_type=bad%20type", header[0], header[1]).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/relationships", header[0], header[1], map[string]any{
		"subject_type": "principal", "subject_id": "relationship-user", "subject_relation": "member", "relation": "viewer",
		"resource_type": "store", "resource_id": childEntityID,
	}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Delete("/iam/relationships", header[0], header[1]).Code)
	assert.Equal(t, http.StatusNotFound, api.Post("/iam/authorization/check", header[0], header[1], map[string]any{
		"entity_id": "missing", "permission_code": "store_view", "subject": "relationship-user",
	}).Code)
	assert.Equal(t, http.StatusOK, api.Patch("/iam/entities/"+childEntityID, header[0], header[1], map[string]any{
		"name": "Flagship", "status": "disabled", "metadata": map[string]any{"tier": 1},
	}).Code)
	expiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	entityBindingPath := "/iam/entities/" + childEntityID + "/role-bindings/" + roleID + "/" + authorization.principalID
	binding := api.Put(entityBindingPath, header[0], header[1], map[string]any{"effect": "deny", "expires_at": expiresAt})
	require.Equal(t, http.StatusOK, binding.Code, binding.Body.String())
	assert.Contains(t, binding.Body.String(), `"effect":"deny"`)
	constraint := api.Post("/iam/authorization/constraints", header[0], header[1], map[string]any{
		"permission_code": "user_view", "subject": authorization.principalID,
	})
	require.Equal(t, http.StatusOK, constraint.Code, constraint.Body.String())
	assert.Contains(t, constraint.Body.String(), `"allow_all":true`)
	assert.NotContains(t, constraint.Body.String(), childEntityID)
	explanation := api.Post("/iam/authorization/explain", header[0], header[1], map[string]any{
		"entity_id": childEntityID, "permission_code": "user_view", "subject": authorization.principalID,
	})
	require.Equal(t, http.StatusOK, explanation.Code, explanation.Body.String())
	assert.Contains(t, explanation.Body.String(), `"allowed":false`)
	assert.Contains(t, explanation.Body.String(), `"reason":"inactive_resource"`)
	assert.Equal(t, http.StatusOK, api.Get("/iam/entities/"+childEntityID+"/role-bindings", header[0], header[1]).Code)
	assert.Equal(t, http.StatusConflict, api.Delete("/iam/entities/"+childEntityID, header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete(entityBindingPath, header[0], header[1]).Code)
	idempotentDelete := api.Delete(entityBindingPath, header[0], header[1])
	require.Equal(t, http.StatusOK, idempotentDelete.Code, idempotentDelete.Body.String())
	assert.Contains(t, idempotentDelete.Body.String(), `"changed":false`)
	deleteInherited := "/iam/relationships?subject_type=entity&subject_id=" + rootEntityID + "&subject_relation=owner&relation=viewer&resource_type=store&resource_id=" + childEntityID
	deleteDirect := "/iam/relationships?subject_type=principal&subject_id=relationship-user&relation=owner&resource_type=company&resource_id=" + rootEntityID
	deleteResource := "/iam/relationships?entity_id=" + childEntityID + "&subject_type=entity&subject_id=" + rootEntityID + "&subject_relation=owner&relation=viewer&resource_type=store&resource_id=business-store-1"
	assert.Equal(t, http.StatusOK, api.Delete(deleteResource, header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete(deleteInherited, header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete(deleteDirect, header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/entities/"+childEntityID, header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/entities/"+rootEntityID, header[0], header[1]).Code)

	menu := api.Post("/iam/menus", header[0], header[1], map[string]any{
		"label": "Users", "route": "/iam/users", "permission_code": "user_view", "status": "active",
	})
	require.Equal(t, http.StatusCreated, menu.Code, menu.Body.String())
	menuID := responseID(t, menu.Body.Bytes())
	assert.Equal(t, http.StatusOK, api.Get("/iam/menus/"+menuID, header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Patch("/iam/menus/"+menuID, header[0], header[1], map[string]any{
		"parent_id": "", "label": "People", "route": "/iam/people", "icon": "users", "sort_order": 10,
		"permission_code": "user_view", "status": "disabled",
	}).Code)
	assert.Equal(t, http.StatusOK, api.Get("/iam/menus", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Get("/iam/me/menus", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/menus/"+menuID, header[0], header[1]).Code)
	auditResponse := api.Get("/iam/audit-events?limit=200", header[0], header[1])
	require.Equal(t, http.StatusOK, auditResponse.Code, auditResponse.Body.String())
	var auditEnvelope struct {
		Data []auditmod.Event `json:"data"`
	}
	require.NoError(t, json.Unmarshal(auditResponse.Body.Bytes(), &auditEnvelope))
	require.NotEmpty(t, auditEnvelope.Data)
	for _, event := range auditEnvelope.Data {
		if event.TenantID == "tenant" {
			assert.Equal(t, authorization.principalID, event.PrincipalID)
		}
	}
	_, err = db.ExecContext(t.Context(), "DELETE FROM iam_platform_administrators WHERE principal_id = ?", authorization.principalID)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, api.Delete("/iam/roles/"+roleID+"/members/"+authorization.principalID, header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/roles/"+roleID+"/directory-bindings/group/group", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/roles/"+roleID+"/directory-bindings/position/position", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/roles/"+roleID+"/permissions/user_view", header[0], header[1]).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/roles/"+roleID, header[0], header[1]).Code)
	lastAdministrator := api.Delete("/iam/roles/administrator/members/"+authorization.principalID, header[0], header[1])
	assert.Equal(t, http.StatusConflict, lastAdministrator.Code, lastAdministrator.Body.String())
	assert.Contains(t, lastAdministrator.Body.String(), i18n.TContext(t.Context(), "last_tenant_administrator"))
	assert.NotContains(t, lastAdministrator.Body.String(), `"message":"last_tenant_administrator"`)
	backupID, err := authnmod.EnsureBootstrapPrincipal(t.Context(), db, authnmod.BootstrapPrincipal{
		LoginName: "backup-admin", Password: "backup administrator password", DisplayName: "Backup Admin",
	})
	require.NoError(t, err)
	backupMember := api.Post("/iam/members", header[0], header[1], map[string]any{
		"subject": backupID, "display_name": "Backup Admin", "status": "active",
	})
	require.Equal(t, http.StatusCreated, backupMember.Code, backupMember.Body.String())
	backupRole := api.Put("/iam/roles/administrator/members/"+backupID, header[0], header[1])
	require.Equal(t, http.StatusOK, backupRole.Code, backupRole.Body.String())
	assert.Equal(t, http.StatusOK, api.Delete("/iam/roles/administrator/members/"+authorization.principalID, header[0], header[1]).Code)
	assert.Equal(t, http.StatusForbidden, api.Get("/iam/menus", header[0], header[1]).Code)
	rolelessMenus := api.Get("/iam/me/menus", header[0], header[1])
	require.Equal(t, http.StatusOK, rolelessMenus.Code, rolelessMenus.Body.String())
}

func TestIAMDataScopeHTTPErrorsAndLocales(t *testing.T) {
	db, api, authorization := newIAMAPI(t)
	header := []string{authorization.header, authz.TenantHeader + ": tenant"}
	role := api.Post("/iam/roles", header[0], header[1], map[string]any{"name": "Scoped"})
	require.Equal(t, http.StatusCreated, role.Code, role.Body.String())
	roleID := responseID(t, role.Body.Bytes())
	now := time.Now().UTC().UnixMilli()
	for _, department := range []struct{ id, status string }{{"active", "active"}, {"disabled", "disabled"}} {
		_, err := db.ExecContext(t.Context(), `INSERT INTO iam_departments
			(tenant_id,id,parent_id,name,name_key,status,sort_order,version,created_at,updated_at)
			VALUES ('tenant',?,?,?,?,?,0,1,?,?)`, department.id, "", department.id, department.id, department.status, now, now)
		require.NoError(t, err)
	}

	tests := []struct {
		name, path, key string
		status          int
		body            map[string]any
	}{
		{"selected departments required", "/iam/roles/" + roleID + "/data-scope", "role_data_scope_department_required", http.StatusUnprocessableEntity, map[string]any{"scope": "selected_departments"}},
		{"departments rejected for all", "/iam/roles/" + roleID + "/data-scope", "role_data_scope_departments_not_allowed", http.StatusUnprocessableEntity, map[string]any{"scope": "all", "department_ids": []string{"active"}}},
		{"scope department missing", "/iam/roles/" + roleID + "/data-scope", "role_scope_department_not_found", http.StatusNotFound, map[string]any{"scope": "selected_departments", "department_ids": []string{"missing"}}},
		{"scope department inactive", "/iam/roles/" + roleID + "/data-scope", "role_scope_department_inactive", http.StatusConflict, map[string]any{"scope": "selected_departments", "department_ids": []string{"disabled"}}},
		{"member department missing", "/iam/members", "member_department_not_found", http.StatusNotFound, map[string]any{"subject": "missing-member", "display_name": "Missing", "department_id": "missing", "status": "active"}},
		{"member department inactive", "/iam/members", "member_department_inactive", http.StatusConflict, map[string]any{"subject": "inactive-member", "display_name": "Inactive", "department_id": "disabled", "status": "active"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := api.Put
			if test.path == "/iam/members" {
				request = api.Post
			}
			for _, locale := range i18n.Supported() {
				message := i18n.TContext(i18n.WithLocale(t.Context(), locale.Code), test.key)
				assert.NotEmpty(t, message, locale.Code)
				assert.NotEqual(t, test.key, message, locale.Code)
				response := request(test.path, header[0], header[1], "Accept-Language: "+locale.Code, test.body)
				assert.Equal(t, test.status, response.Code, response.Body.String())
				assert.Contains(t, response.Body.String(), message)
				assert.NotContains(t, response.Body.String(), `"message":"`+test.key+`"`)
			}
		})
	}

	invalidCondition := map[string]any{
		"subject_type": "principal", "subject_id": authorization.principalID, "relation": "viewer",
		"resource_type": "store", "resource_id": "missing", "condition": map[string]any{
			"version": 2, "eq": []any{map[string]any{"context": "auth.acr"}, map[string]any{"value": 1}},
		},
	}
	for _, locale := range i18n.Supported() {
		response := api.Post("/iam/relationships", header[0], header[1], "Accept-Language: "+locale.Code, invalidCondition)
		require.Equal(t, http.StatusUnprocessableEntity, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), i18n.TContext(i18n.WithLocale(t.Context(), locale.Code), "invalid_relationship_condition"))
		assert.NotContains(t, response.Body.String(), `"message":"invalid_relationship_condition"`)
		roleConditionResponse := api.Put("/iam/roles/"+roleID+"/permissions/user_view/condition", header[0], header[1], "Accept-Language: "+locale.Code, map[string]any{
			"condition": map[string]any{"version": 2, "gte": []any{map[string]any{"context": "auth.acr"}, map[string]any{"value": 1}}},
		})
		require.Equal(t, http.StatusUnprocessableEntity, roleConditionResponse.Code, roleConditionResponse.Body.String())
		assert.Contains(t, roleConditionResponse.Body.String(), i18n.TContext(i18n.WithLocale(t.Context(), locale.Code), "invalid_role_permission_condition"))
		assert.NotContains(t, roleConditionResponse.Body.String(), `"message":"invalid_role_permission_condition"`)
	}
}

func TestIAMManagementHTTPAuditFailureRollsBack(t *testing.T) {
	db, api, authorization := newIAMAPI(t)
	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_role_created_audit BEFORE INSERT ON iam_audit_events
		WHEN NEW.event_type = 'role_created' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`)
	require.NoError(t, err)

	response := api.Post("/iam/roles", authorization.header, authz.TenantHeader+": tenant", map[string]any{"name": "Must Roll Back"})
	assert.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	roles, err := db.NewSelect().Table("iam_roles").Where("tenant_id = ? AND name = ?", "tenant", "Must Roll Back").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, roles)
	revisions, err := db.NewSelect().Table("iam_policy_revisions").Where("tenant_id = ?", "tenant").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, revisions)
}

func TestIAMManagementHTTPFailures(t *testing.T) {
	db, api, authorization := newIAMAPI(t)
	authorizationHeader, tenant := authorization.header, authz.TenantHeader+": tenant"
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/roles", authorizationHeader, tenant, map[string]any{"name": ""}).Code)
	assert.Equal(t, http.StatusConflict, api.Post("/iam/roles", authorizationHeader, tenant, map[string]any{"name": "Administrator"}).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/roles/missing", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/entities/missing", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Patch("/iam/entities/missing", authorizationHeader, tenant, map[string]any{"name": "Missing"}).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/entities/missing", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Post("/iam/entities", authorizationHeader, tenant, map[string]any{"parent_id": "missing", "type": "store", "name": "Missing parent"}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/entities", authorizationHeader, tenant, map[string]any{"type": "Company", "name": "Invalid"}).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/entities/missing/role-bindings", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Put("/iam/entities/missing/role-bindings/administrator/"+authorization.principalID, authorizationHeader, tenant, map[string]any{"effect": "allow"}).Code)
	assert.Equal(t, http.StatusNotFound, api.Post("/iam/authorization/explain", authorizationHeader, tenant, map[string]any{"entity_id": "missing", "permission_code": "user_view", "subject": authorization.principalID}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/authorization/constraints", authorizationHeader, tenant, map[string]any{"permission_code": "role_view", "subject": authorization.principalID}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/authorization/constraints", authorizationHeader, tenant, map[string]any{"permission_code": "missing", "subject": authorization.principalID}).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/roles/missing/directory-bindings", authorizationHeader, tenant).Code)
	now := time.Now().UTC().UnixMilli()
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','disabled-group','Disabled','disabled','static','','disabled',0,1,?,?)`, now, now)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, api.Put("/iam/roles/administrator/directory-bindings/group/missing", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusConflict, api.Put("/iam/roles/administrator/directory-bindings/group/disabled-group", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Put("/iam/roles/administrator/directory-bindings/dynamic/disabled-group", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/members/missing", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/menus/missing", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Patch("/iam/roles/missing", authorizationHeader, tenant, map[string]any{"name": "Missing"}).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/roles/missing", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/roles/missing/permissions", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/roles/missing/permission-grants", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Put("/iam/roles/missing/permissions/user_view", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Put("/iam/roles/missing/permissions/user_view/condition", authorizationHeader, tenant, map[string]any{"condition": map[string]any{"version": 1, "gte": []any{map[string]any{"context": "auth.acr"}, map[string]any{"value": 1}}}}).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/roles/missing/permissions/user_view", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/roles/missing/permissions/user_view/condition", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/roles/missing/members", authorizationHeader, tenant).Code)
	// The escalation guard resolves the role before the member, so a missing
	// role now reports 404 instead of leaking a membership conflict.
	assert.Equal(t, http.StatusNotFound, api.Put("/iam/roles/missing/members/subject", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/roles/missing/members/subject", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Patch("/iam/members/missing", authorizationHeader, tenant, map[string]any{"display_name": "Missing"}).Code)
	assert.Equal(t, http.StatusNotFound, api.Patch("/iam/menus/missing", authorizationHeader, tenant, map[string]any{"label": "Missing"}).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/menus/missing", authorizationHeader, tenant).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Put("/iam/roles/administrator/permissions/not_declared", authorizationHeader, tenant).Code)
	emptyRole := api.Post("/iam/roles", authorizationHeader, tenant, map[string]any{"name": "No Permissions"})
	require.Equal(t, http.StatusCreated, emptyRole.Code, emptyRole.Body.String())
	assert.Equal(t, http.StatusNotFound, api.Put("/iam/roles/"+responseID(t, emptyRole.Body.Bytes())+"/permissions/user_view/condition", authorizationHeader, tenant, map[string]any{"condition": map[string]any{"version": 1, "gte": []any{map[string]any{"context": "auth.acr"}, map[string]any{"value": 1}}}}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/menus", authorizationHeader, tenant, map[string]any{
		"label": "Invalid", "route": "/invalid", "permission_code": "not_declared", "status": "active",
	}).Code)
	assert.Equal(t, http.StatusForbidden, api.Get("/iam/roles", authorizationHeader, authz.TenantHeader+": other").Code)
	assert.Equal(t, http.StatusUnauthorized, api.Get("/iam/roles", tenant).Code)
}

type iamAuthorization struct {
	principalID string
	header      string
}

func newIAMAPI(t *testing.T) (*bun.DB, humatest.TestAPI, iamAuthorization) {
	t.Helper()
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, iam.RegisterI18n())
	respx.Install()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	require.NoError(t, organization.EnsureTenant(context.Background(), db, "tenant"))
	seed := make([]byte, ed25519.SeedSize)
	web, err := authnmod.NewWebService(authnext.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"}, SigningKey: base64.RawStdEncoding.EncodeToString(seed),
		MFA: authnext.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
		Web: authnext.WebConfig{Enabled: true, CookieName: "session", SessionTTL: time.Hour, IdleTTL: time.Minute, PostLoginURL: "https://app.example/", AllowedReturnURLs: []string{"https://app.example/"}},
	}, db)
	require.NoError(t, err)
	principalID, err := authnmod.EnsureBootstrapPrincipal(context.Background(), db, authnmod.BootstrapPrincipal{LoginName: "admin", Password: "correct horse battery staple", DisplayName: "Admin"})
	require.NoError(t, err)
	registry := authz.DefaultRegistry()
	now := time.Now().UTC().UnixMilli()
	_, err = db.ExecContext(context.Background(), "INSERT INTO iam_platform_administrators (principal_id, created_at) VALUES (?, ?)", principalID, now)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), `INSERT INTO iam_tenant_members
 (tenant_id,user_subject,display_name,email,status,created_at,updated_at,disabled_at) VALUES (?,?,?,'','active',?,?,0)`, "tenant", principalID, "Admin", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), "INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,?,?,?)", "tenant", "administrator", "Administrator", "", now, now)
	require.NoError(t, err)
	// Platform-scope permissions are deliberately excluded: the service refuses
	// to grant them to a tenant role, so seeding them would build a state
	// production can never reach.
	for _, action := range registry.All() {
		if action.Scope == "platform" {
			continue
		}
		_, err = db.ExecContext(context.Background(), "INSERT INTO iam_role_permissions (tenant_id,role_id,permission_code,created_at) VALUES (?,?,?,?)", "tenant", "administrator", action.Code, now)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(context.Background(), "INSERT INTO iam_role_members (tenant_id,role_id,user_subject,created_at) VALUES (?,?,?,?)", "tenant", "administrator", principalID, now)
	require.NoError(t, err)

	authorizer := iam.NewAuthorizer(db)
	registrar := authz.NewRegistrar(registry, web, authorizer, iam.NewMembershipChecker(db))
	var sequence atomic.Int64
	repository := iam.NewRepository(db, func() (string, error) { return fmt.Sprintf("id-%d", sequence.Add(1)), nil })
	auditTrail := auditmod.NewService(db)
	service := iam.NewService(registry, repository, authorizer, func(ctx context.Context, executor bun.IDB, event auditx.Event) error {
		_, err := auditTrail.AppendTo(ctx, executor, auditmod.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID,
			EventType: event.EventType, TargetType: event.TargetType, TargetID: event.TargetID,
			Outcome: "success", Detail: event.Detail,
		})
		return err
	})
	config := huma.DefaultConfig("IAM Test API", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	router, api := humatest.New(t, config)
	router.(interface {
		Use(...func(http.Handler) http.Handler)
	}).Use(respx.Locale)
	iam.RegisterREST(api, service, registrar)
	auditmod.RegisterREST(api, auditTrail, registrar)
	token, _, err := web.IssueAccessToken(context.Background(), principalID, "api", "openid")
	require.NoError(t, err)
	return db, api, iamAuthorization{principalID: principalID, header: "Authorization: Bearer " + token}
}

func responseID(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return strings.TrimSpace(envelope.Data.ID)
}
