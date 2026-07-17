package authn

import (
	"github.com/danielgtaylor/huma/v2"

	authnapi "github.com/chaos-plus/chaosplus/internal/modules/authn/api"
)

type Module struct {
	authenticator authnapi.Authenticator
	web           authnapi.WebService
}

func NewModule(authenticator authnapi.Authenticator, web authnapi.WebService) *Module {
	return &Module{authenticator: authenticator, web: web}
}

func (m *Module) RegisterREST(api huma.API) {
	if m.authenticator == nil {
		return
	}
	authnapi.RegisterREST(api, m.authenticator, m.web)
}
