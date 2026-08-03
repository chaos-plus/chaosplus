package authn

import (
	"context"

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

func (m *Module) Start(ctx context.Context) error {
	if service, ok := m.web.(interface{ StartNotificationWorker(context.Context) error }); ok {
		return service.StartNotificationWorker(ctx)
	}
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if service, ok := m.web.(interface{ StopNotificationWorker(context.Context) error }); ok {
		return service.StopNotificationWorker(ctx)
	}
	return nil
}
