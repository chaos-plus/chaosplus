package authn

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

type Module struct {
	authenticator Authenticator
	web           *WebService
}

func NewModule(authenticator Authenticator, web *WebService) *Module {
	return &Module{authenticator: authenticator, web: web}
}

func (m *Module) RegisterREST(api huma.API) {
	if m.authenticator == nil {
		return
	}
	RegisterREST(api, m.authenticator, m.web)
}

func (m *Module) Start(ctx context.Context) error {
	if m.web != nil {
		return m.web.StartNotificationWorker(ctx)
	}
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.web != nil {
		return m.web.StopNotificationWorker(ctx)
	}
	return nil
}
