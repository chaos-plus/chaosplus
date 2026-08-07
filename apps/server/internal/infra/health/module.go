package health

import (
	"context"
	"database/sql"

	"github.com/danielgtaylor/huma/v2"
	healthapi "github.com/chaos-plus/chaosplus/internal/infra/health/api"
)

// Module exposes liveness and readiness endpoints. It implements RESTRegistrar.
type Module struct {
	db *sql.DB
}

// NewModule builds a health module. db may be nil; when nil /ready reports
// "no database" rather than failing.
func NewModule(db *sql.DB) *Module {
	return &Module{db: db}
}

func (m *Module) RegisterREST(api huma.API) {
	healthapi.RegisterREST(api, func(ctx context.Context) error {
		if m.db == nil {
			return nil
		}
		return m.db.PingContext(ctx)
	})
}
