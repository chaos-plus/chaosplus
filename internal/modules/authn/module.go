package authn

import (
	"github.com/danielgtaylor/huma/v2"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	authnapi "github.com/chaos-plus/chaosplus/internal/modules/authn/api"
)

type Module struct {
	verifier *authnext.Verifier
}

func NewModule(verifier *authnext.Verifier) *Module {
	return &Module{verifier: verifier}
}

func (m *Module) RegisterREST(api huma.API) {
	if m.verifier == nil {
		return
	}
	authnapi.RegisterREST(api, m.verifier)
}
