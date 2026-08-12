package machine

import (
	"context"
	"errors"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
)

var Actions = []authz.Action{
	{Resource: "machine", Verb: "create", Scope: "entity", Summary: "onboard machine runners", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "machine", Verb: "view", Scope: "entity", Summary: "view machine runners", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
	{Resource: "machine", Verb: "update", Scope: "entity", Summary: "manage machine runners", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "machine", Verb: "delete", Scope: "entity", Summary: "remove machine runners", AllowedRelations: []string{"owner"}, DataScoped: true},
}

type entityInput struct{}

type machineInput struct {
	entityInput
	ID coreid.ID `path:"id"`
}

type machineTokenInput struct {
	machineInput
	Body struct {
		Token string `json:"token" minLength:"1" maxLength:"512"`
	}
}

type bodyResponse[T any] struct {
	Body T
}

type tokenResponse struct {
	Token     string    `json:"token"`
	MachineID coreid.ID `json:"machineId"`
	LongTerm  bool      `json:"longTerm"`
	ExpiresAt int64     `json:"expiresAt,omitempty"`
}

type machineSummary struct {
	ID              coreid.ID `json:"id"`
	Name            string    `json:"name"`
	Address         string    `json:"address"`
	Status          Status    `json:"status"`
	Online          bool      `json:"online"`
	LastHeartbeatAt int64     `json:"lastHeartbeatAt"`
	AgentCount      int       `json:"agentCount"`
	Runtimes        []string  `json:"runtimes"`
}

type machineDetail struct {
	ID              coreid.ID      `json:"id"`
	Name            string         `json:"name"`
	Address         string         `json:"address"`
	Status          Status         `json:"status"`
	Online          bool           `json:"online"`
	OS              string         `json:"os"`
	RegisteredAt    int64          `json:"registeredAt"`
	LastHeartbeatAt int64          `json:"lastHeartbeatAt"`
	Runtimes        []string       `json:"runtimes"`
	Agents          []ManagedAgent `json:"agents"`
}

type onboardingStatus struct {
	State     string `json:"state"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

func (m *Module) RegisterREST(api huma.API) {
	registerMachineEntity(m, api, huma.Operation{OperationID: "machine-issue-onboarding-token", Method: http.MethodPost, Path: "/api/machines/tokens", Summary: "Issue a machine onboarding token", Tags: []string{"machine"}}, "create", m.issueToken)
	registerMachineWebSocket(api, m.hub)
	registerMachineEntity(m, api, huma.Operation{OperationID: "machine-list", Method: http.MethodGet, Path: "/api/machines", Summary: "List machine runners", Tags: []string{"machine"}}, "view", m.listMachines)
	registerMachineEntity(m, api, huma.Operation{OperationID: "machine-get", Method: http.MethodGet, Path: "/api/machines/{id}", Summary: "Get a machine runner", Tags: []string{"machine"}, Errors: []int{http.StatusNotFound}}, "view", m.getMachine)
	registerMachineEntity(m, api, huma.Operation{OperationID: "machine-confirm", Method: http.MethodPost, Path: "/api/machines/{id}/confirm", Summary: "Confirm machine onboarding", Tags: []string{"machine"}}, "update", m.confirmMachine)
	registerMachineEntity(m, api, huma.Operation{OperationID: "machine-onboarding-status", Method: http.MethodPost, Path: "/api/machines/{id}/onboarding-status", Summary: "Get machine onboarding status", Tags: []string{"machine"}, Errors: []int{http.StatusNotFound}}, "view", m.getOnboardingStatus)
	registerMachineEntity(m, api, huma.Operation{OperationID: "machine-delete", Method: http.MethodDelete, Path: "/api/machines/{id}", Summary: "Remove a machine runner", Tags: []string{"machine"}, Errors: []int{http.StatusNotFound}}, "delete", m.deleteMachine)
	registerMachineEntity(m, api, huma.Operation{OperationID: "machine-rotate-token", Method: http.MethodPost, Path: "/api/machines/{id}/refresh-token", Summary: "Rotate a machine runner token", Tags: []string{"machine"}, Errors: []int{http.StatusNotFound}}, "update", m.rotateToken)
}

func registerMachineEntity[I, O any](m *Module, api huma.API, operation huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, operation, authz.Guard{Resource: "machine", Verb: verb}, handler)
}

func registerMachineWebSocket(api huma.API, hub *Hub) {
	operation := huma.Operation{
		OperationID: "machine-daemon-websocket", Method: http.MethodGet, Path: "/api/machines/ws",
		Summary: "Connect a machine daemon", Tags: []string{"machine"},
		Parameters: []*huma.Param{{Name: "token", In: "query", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MinLength: intPointer(1), MaxLength: intPointer(512)}}},
		Errors:     []int{http.StatusUnauthorized},
	}
	authz.Public(&operation)
	operation.Security = []map[string][]string{}
	api.OpenAPI().AddOperation(&operation)
	api.Adapter().Handle(&operation, func(ctx huma.Context) {
		request, writer := humachi.Unwrap(ctx)
		hub.HandleWS(writer, request)
	})
}

func (m *Module) issueToken(ctx context.Context, _ *entityInput) (*bodyResponse[tokenResponse], error) {
	id, token, expiresAt, err := m.hub.IssueTokenFor(ctx)
	if err != nil {
		return nil, machineAPIError(err)
	}
	return &bodyResponse[tokenResponse]{Body: tokenResponse{Token: token, MachineID: id, ExpiresAt: expiresAt.UTC().UnixMilli()}}, nil
}

func (m *Module) listMachines(ctx context.Context, _ *entityInput) (*bodyResponse[[]machineSummary], error) {
	items, err := m.hub.ListMachines(ctx)
	if err != nil {
		return nil, machineAPIError(err)
	}
	counts := map[coreid.ID]int{}
	if m.agents != nil {
		counts, err = m.agents.CountByMachine(ctx)
		if err != nil {
			return nil, machineAPIError(err)
		}
	}
	out := make([]machineSummary, 0, len(items))
	for _, item := range items {
		name := m.hub.MachineName(item.ID)
		if name == "" {
			name = item.ID.String()
		}
		runtimes := []string{}
		if m.hub.IsConnected(item.ID) {
			runtimes = m.hub.MachineRuntimes(item.ID)
		}
		out = append(out, machineSummary{ID: item.ID, Name: name, Address: item.Address, Status: item.Status, Online: m.hub.IsConnected(item.ID), LastHeartbeatAt: item.LastHeartbeatAt, AgentCount: counts[item.ID], Runtimes: runtimes})
	}
	return &bodyResponse[[]machineSummary]{Body: out}, nil
}

func (m *Module) getMachine(ctx context.Context, input *machineInput) (*bodyResponse[machineDetail], error) {
	items, err := m.hub.ListMachines(ctx)
	if err != nil {
		return nil, machineAPIError(err)
	}
	for _, item := range items {
		if item.ID != input.ID {
			continue
		}
		name := m.hub.MachineName(item.ID)
		if name == "" {
			name = item.ID.String()
		}
		agents := []ManagedAgent{}
		if m.agents != nil {
			agents, err = m.agents.ListByMachine(ctx, item.ID)
			if err != nil {
				return nil, machineAPIError(err)
			}
		}
		return &bodyResponse[machineDetail]{Body: machineDetail{ID: item.ID, Name: name, Address: item.Address, Status: item.Status, Online: m.hub.IsConnected(item.ID), OS: item.OS, RegisteredAt: item.RegisteredAt, LastHeartbeatAt: item.LastHeartbeatAt, Runtimes: m.hub.MachineRuntimes(item.ID), Agents: agents}}, nil
	}
	return nil, huma.Error404NotFound("machine.not_found")
}

func (m *Module) confirmMachine(ctx context.Context, input *machineTokenInput) (*bodyResponse[okResponse], error) {
	if err := m.hub.Confirm(ctx, input.ID, input.Body.Token); err != nil {
		return nil, machineAPIError(err)
	}
	return &bodyResponse[okResponse]{Body: okResponse{OK: true}}, nil
}

func (m *Module) getOnboardingStatus(ctx context.Context, input *machineTokenInput) (*bodyResponse[onboardingStatus], error) {
	if !m.hub.CanAccess(ctx, input.ID) {
		return nil, huma.Error404NotFound("machine.not_found")
	}
	state, expiresAt := m.hub.OnboardingStatus(input.ID, input.Body.Token)
	response := onboardingStatus{State: state}
	if !expiresAt.IsZero() {
		response.ExpiresAt = expiresAt.UTC().UnixMilli()
	}
	return &bodyResponse[onboardingStatus]{Body: response}, nil
}

func (m *Module) deleteMachine(ctx context.Context, input *machineInput) (*bodyResponse[okResponse], error) {
	if err := m.hub.Cancel(ctx, input.ID); err != nil {
		return nil, machineAPIError(err)
	}
	return &bodyResponse[okResponse]{Body: okResponse{OK: true}}, nil
}

func (m *Module) rotateToken(ctx context.Context, input *machineInput) (*bodyResponse[tokenResponse], error) {
	token, err := m.hub.RefreshToken(ctx, input.ID)
	if err != nil {
		return nil, machineAPIError(err)
	}
	return &bodyResponse[tokenResponse]{Body: tokenResponse{Token: token, MachineID: input.ID, LongTerm: true}}, nil
}

func machineAPIError(err error) error {
	switch {
	case errors.Is(err, ErrMachineNotFound):
		return huma.Error404NotFound("machine.not_found")
	case errors.Is(err, ErrMachineNotConnected), errors.Is(err, ErrTokenInvalid), errors.Is(err, ErrTokenExpired):
		return huma.Error422UnprocessableEntity("machine.invalid_onboarding")
	default:
		return huma.Error500InternalServerError("machine.unavailable")
	}
}

func intPointer(value int) *int { return &value }
