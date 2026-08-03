package organization

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupHTTPWorkflow(t *testing.T) {
	db, service := newGroupService(t)
	addTenantMember(t, db, "tenant-a", "principal/a", "Alice", iam.MemberActive)
	_, api := humatest.New(t)
	RegisterGroupREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": tenant-a"

	created := api.Post("/iam/groups", tenant, map[string]any{"name": "Operators", "description": "Operators", "sort_order": 10})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	group := decodeGroup(t, created.Body.Bytes())
	assert.Equal(t, GroupTypeStatic, group.Type)
	assert.Equal(t, http.StatusOK, api.Get("/iam/groups/"+group.ID, tenant).Code)
	assert.Equal(t, http.StatusConflict, api.Post("/iam/groups", tenant, map[string]any{"name": "OPERATORS"}).Code)

	listed := api.Get("/iam/groups", tenant)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var groups struct {
		Data []Group `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &groups))
	require.Len(t, groups.Data, 1)

	updated := api.Patch("/iam/groups/"+group.ID, tenant, map[string]any{"name": "Security Operators", "status": "disabled", "version": group.Version})
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	group = decodeGroup(t, updated.Body.Bytes())
	assert.Equal(t, int64(2), group.Version)
	assert.Equal(t, http.StatusConflict, api.Patch("/iam/groups/"+group.ID, tenant, map[string]any{"name": "Stale", "version": 1}).Code)

	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	memberResponse := api.Put("/iam/groups/"+group.ID+"/members/principal%2Fa", tenant, map[string]any{"starts_at": start, "ends_at": end})
	require.Equal(t, http.StatusOK, memberResponse.Code, memberResponse.Body.String())
	members := api.Get("/iam/groups/"+group.ID+"/members", tenant)
	require.Equal(t, http.StatusOK, members.Code, members.Body.String())
	var memberEnvelope struct {
		Data []GroupMember `json:"data"`
	}
	require.NoError(t, json.Unmarshal(members.Body.Bytes(), &memberEnvelope))
	require.Len(t, memberEnvelope.Data, 1)
	assert.Equal(t, "principal/a", memberEnvelope.Data[0].PrincipalID)
	assert.Equal(t, http.StatusConflict, api.Delete("/iam/groups/"+group.ID+"?version=2", tenant).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/groups/"+group.ID+"/members/principal%2Fa", tenant).Code)
	assert.Equal(t, http.StatusOK, api.Delete("/iam/groups/"+group.ID+"?version=2", tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/groups/"+group.ID, tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/groups/"+group.ID, authz.TenantHeader+": tenant-b").Code)
}

func TestGroupHTTPEmptyCollectionsAndErrors(t *testing.T) {
	db, service := newGroupService(t)
	_, api := humatest.New(t)
	RegisterGroupREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": tenant"

	response := api.Get("/iam/groups", tenant)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	assert.JSONEq(t, `[]`, string(envelope.Data))
	invalidDynamic := api.Post("/iam/groups", tenant, map[string]any{"name": "Group", "type": "dynamic"})
	assert.Equal(t, http.StatusUnprocessableEntity, invalidDynamic.Code)
	assert.Contains(t, invalidDynamic.Body.String(), "invalid_group_membership_rule")
	dynamic := api.Post("/iam/groups", tenant, map[string]any{"name": "Dynamic", "type": "dynamic", "membership_rule": map[string]any{"version": 1, "match": "all", "conditions": []any{map[string]any{"field": "member.status", "operator": "in", "values": []string{"active"}}}}})
	require.Equal(t, http.StatusCreated, dynamic.Code, dynamic.Body.String())
	dynamicGroup := decodeGroup(t, dynamic.Body.Bytes())
	assert.Equal(t, GroupTypeDynamic, dynamicGroup.Type)
	assert.JSONEq(t, `{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["active"]}]}`, string(dynamicGroup.Rule))
	manualMember := api.Put("/iam/groups/"+dynamicGroup.ID+"/members/principal", tenant, map[string]any{})
	assert.Equal(t, http.StatusConflict, manualMember.Code)
	assert.Contains(t, manualMember.Body.String(), "dynamic_group_members_computed")
	assert.Equal(t, http.StatusUnprocessableEntity, api.Delete("/iam/groups/missing?version=0", tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Put("/iam/groups/missing/members/missing", tenant, map[string]any{}).Code)

	require.NoError(t, db.Close())
	assert.Equal(t, http.StatusInternalServerError, api.Get("/iam/groups", tenant).Code)
}

func TestGroupHTTPPreservesLastAdministrator(t *testing.T) {
	db, service := newGroupService(t)
	addLocalPrincipal(t, db, "administrator")
	addTenantMember(t, db, "tenant", "administrator", "Administrator", iam.MemberActive)
	_, api := humatest.New(t)
	RegisterGroupREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": tenant"
	created := api.Post("/iam/groups", tenant, map[string]any{"name": "Administrators"})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	group := decodeGroup(t, created.Body.Bytes())
	assigned := api.Put("/iam/groups/"+group.ID+"/members/administrator", tenant, map[string]any{})
	require.Equal(t, http.StatusOK, assigned.Code, assigned.Body.String())
	bindDirectoryAdministrator(t, db, "tenant", "group", group.ID)

	for _, response := range []*httptest.ResponseRecorder{
		api.Patch("/iam/groups/"+group.ID, tenant, map[string]any{"status": "disabled", "version": group.Version}),
		api.Put("/iam/groups/"+group.ID+"/members/administrator", tenant, map[string]any{"ends_at": time.Now().UTC().Add(time.Hour)}),
		api.Delete("/iam/groups/"+group.ID+"/members/administrator", tenant),
	} {
		assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), "last_tenant_administrator")
	}
}

func TestGroupErrorMapping(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
	}{
		{ErrGroupNotFound, http.StatusNotFound},
		{ErrGroupNameConflict, http.StatusConflict},
		{ErrGroupVersionConflict, http.StatusConflict},
		{ErrGroupHasMembers, http.StatusConflict},
		{ErrGroupRoleBound, http.StatusConflict},
		{ErrGroupRelationshipBound, http.StatusConflict},
		{ErrGroupMemberInactive, http.StatusConflict},
		{ErrDynamicGroupMembers, http.StatusConflict},
		{ErrGroupRuleType, http.StatusUnprocessableEntity},
		{ErrGroupRuleInvalid, http.StatusUnprocessableEntity},
		{ErrGroupInvalid, http.StatusUnprocessableEntity},
		{ErrGroupMemberNotFound, http.StatusInternalServerError},
	} {
		statusError, ok := groupError(item.err).(huma.StatusError)
		require.True(t, ok)
		assert.Equal(t, item.status, statusError.GetStatus())
	}
}

func TestDynamicGroupErrorsAreLocalized(t *testing.T) {
	_, service := newGroupService(t)
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	router := chi.NewMux()
	router.Use(respx.Timing, respx.Locale)
	config := huma.DefaultConfig("group-test", "1.0.0")
	config.Transformers = append(config.Transformers, respx.LocalizeMessage)
	respx.Install()
	api := humachi.New(router, config)
	RegisterGroupREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	for _, item := range []struct {
		locale  string
		message string
	}{
		{"en-US", "The dynamic membership rule is invalid. Use version 1, choose all or any matching, and provide 1 to 16 supported attribute conditions."},
		{"zh-CN", "动态成员规则无效。请使用版本 1，选择满足全部或任一条件，并配置 1 到 16 个受支持的属性条件。"},
		{"ms-MY", "Peraturan keahlian dinamik tidak sah. Gunakan versi 1, pilih padanan semua atau mana-mana, dan berikan 1 hingga 16 syarat atribut yang disokong."},
	} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/iam/groups", strings.NewReader(`{"name":"Dynamic","type":"dynamic"}`))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept-Language", item.locale)
		request.Header.Set(authz.TenantHeader, "tenant")
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode)
		var envelope struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(data, &envelope))
		assert.Equal(t, item.message, envelope.Message)
		assert.NotContains(t, envelope.Message, "invalid_group_membership_rule")
	}
}

func decodeGroup(t *testing.T, body []byte) Group {
	t.Helper()
	var envelope struct {
		Data Group `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return envelope.Data
}
