package oauth

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
)

func TestModuleRegistersOAuthRoutes(t *testing.T) {
	service, authentication, _ := newOAuthTestService(t)
	module := NewModule(service.db, authentication, nil)
	_, api := humatest.New(t)
	module.RegisterREST(api)

	assert.Equal(t, http.StatusOK, api.Get("/.well-known/openid-configuration").Code)
	assert.NotNil(t, api.OpenAPI().Paths["/oauth/token"].Post)
}
