package workspace

import "net/http"

// Module is the workspace bounded context's composition boundary.
type Module struct {
	service *Service
}

func NewModule(repo Repository, executor Executor, notifier Notifier, artifactRoot string) *Module {
	return &Module{service: NewService(repo, executor, notifier, artifactRoot)}
}

func (m *Module) RegisterREST(mux *http.ServeMux) { RegisterREST(mux, m.service) }

func (m *Module) Service() *Service { return m.service }
