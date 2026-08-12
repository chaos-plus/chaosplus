package app

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secure"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

// Extension adds product bounded contexts to the shared application host.
// Cross-cutting HTTP, IAM, database, ID, and lifecycle behavior remains owned
// by this package; extensions only declare permissions, locales, and modules.
type Extension struct {
	Name         string
	Actions      []authz.Action
	RegisterI18n func() error
	BuildModules func(ModuleDependencies) ([]any, error)
}

// ModuleDependencies are the shared owner capabilities available to product
// module factories after database and IAM bootstrap. They are deliberately
// narrow so an extension cannot replace the application host or IAM pipeline.
type ModuleDependencies struct {
	Context       context.Context
	Writer        *bun.DB
	Reader        *bun.DB
	Authorization *authz.Registrar
	NextID        func() (guid.ID, error)
	AppendAudit   auditx.Appender
	OriginPolicy  secure.OriginPolicy
}

func (app *App) buildAuthorizationRegistry() (*authz.Registry, error) {
	actions := authz.DefaultActions()
	for index, extension := range app.extensions {
		if extension.Name == "" {
			return nil, fmt.Errorf("application extension %d requires a name", index)
		}
		actions = append(actions, extension.Actions...)
	}
	registry, err := authz.NewRegistry(actions...)
	if err != nil {
		return nil, fmt.Errorf("build authorization registry: %w", err)
	}
	return registry, nil
}

func (app *App) buildExtensionModules() error {
	if len(app.extensions) == 0 {
		return nil
	}
	if app.dbr.Write() == nil {
		return fmt.Errorf("application extensions require a writable database")
	}
	if app.authzRegistrar == nil || app.authzRegistrar.IsDeclarationOnly() {
		return fmt.Errorf("application extensions require the production authorization stack")
	}
	appendAudit := auditAppender(auditService(app.dbr.Write(), nextGUID))
	originPolicy, err := secure.NewOriginPolicy(app.cfg.Authn.Web.AllowedOrigins)
	if err != nil {
		return fmt.Errorf("application extension origin policy: %w", err)
	}
	dependencies := ModuleDependencies{
		Context:       app.ctx,
		Writer:        app.dbr.Write(),
		Reader:        app.dbr.Read(),
		Authorization: app.authzRegistrar,
		NextID: func() (guid.ID, error) {
			id, err := guid.Next()
			return guid.ID(id), err
		},
		AppendAudit:  appendAudit,
		OriginPolicy: originPolicy,
	}
	for _, extension := range app.extensions {
		if extension.BuildModules == nil {
			continue
		}
		modules, err := extension.BuildModules(dependencies)
		if err != nil {
			return fmt.Errorf("build %s extension modules: %w", extension.Name, err)
		}
		for _, module := range modules {
			if module == nil {
				return fmt.Errorf("build %s extension modules: nil module", extension.Name)
			}
			app.mods = append(app.mods, module)
		}
	}
	return nil
}
