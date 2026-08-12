package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/machine"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
)

// TestMachineOnboardingFlow covers PRD §5.3.1 end-to-end over HTTP + WS:
// issue token → daemon connects → confirm → listed online → cancel → gone.
func TestMachineOnboardingFlow(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, nil, "r") // link unused: this test never launches a run
	hub := machine.NewHub(nc, machine.NewTokenStore(), st)
	ts := httptest.NewServer(NewHandler(m, hub, nil))
	defer ts.Close()

	// 1. 签发一次性 token。
	resp, err := http.Post(ts.URL+"/api/machines/tokens", "application/json", nil)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("issue status = %d", resp.StatusCode)
	}
	var issued struct {
		Token     string `json:"token"`
		MachineID string `json:"machineId"`
		LongTerm  bool   `json:"longTerm"`
		ExpiresAt int64  `json:"expiresAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()
	if issued.LongTerm || issued.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("issued onboarding token must be temporary: %+v", issued)
	}

	// 2. 列表为空(未确认)。
	ms := listMachines(t, ts.URL)
	if len(ms) != 0 {
		t.Fatalf("expected empty machine list, got %+v", ms)
	}

	// 3. daemon 用 token 连 WS。
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/machines/ws?token=" + issued.Token + "&name=box"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = ws.Close() }()
	var ready map[string]any
	if err := ws.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}
	statusBody, _ := json.Marshal(map[string]any{"token": issued.Token})
	statusResp, err := http.Post(ts.URL+"/api/machines/"+issued.MachineID+"/onboarding-status", "application/json", bytes.NewReader(statusBody))
	if err != nil {
		t.Fatalf("onboarding status: %v", err)
	}
	var onboarding struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(statusResp.Body).Decode(&onboarding); err != nil {
		t.Fatal(err)
	}
	_ = statusResp.Body.Close()
	if onboarding.State != "connected" {
		t.Fatalf("onboarding state = %q, want connected", onboarding.State)
	}

	// 4. confirm。
	body, _ := json.Marshal(map[string]any{"token": issued.Token})
	req, _ := http.NewRequest("POST", ts.URL+"/api/machines/"+issued.MachineID+"/confirm", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	cresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if cresp.StatusCode != 200 {
		t.Fatalf("confirm status = %d", cresp.StatusCode)
	}
	_ = cresp.Body.Close()

	// 5. 列表 1 条且 online。
	ms = listMachines(t, ts.URL)
	if len(ms) != 1 || ms[0].ID != issued.MachineID || !ms[0].Online {
		t.Fatalf("after confirm, got %+v, want [%s online]", ms, issued.MachineID)
	}

	// 6. cancel → 消失。
	del, _ := http.NewRequest("DELETE", ts.URL+"/api/machines/"+issued.MachineID, nil)
	dresp, err := http.DefaultClient.Do(del)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	_ = dresp.Body.Close()
	ms = listMachines(t, ts.URL)
	if len(ms) != 0 {
		t.Fatalf("after cancel, got %+v, want []", ms)
	}
}

type machineSummary struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Online bool   `json:"online"`
}

func listMachines(t *testing.T, base string) []machineSummary {
	t.Helper()
	resp, err := http.Get(base + "/api/machines")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var ms []machineSummary
	if err := json.NewDecoder(resp.Body).Decode(&ms); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return ms
}

func TestMachineMutationEndpointsAreTenantScoped(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	nc := startTestNATS(t)
	hub := machine.NewHub(nc, machine.NewTokenStore(), st)
	m := NewRunManager(nc, nil, nil, "r")
	ts := httptest.NewServer(NewHandler(m, hub, nil))
	defer ts.Close()

	request := func(method, path, entity string, body []byte) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Entity", entity)
		if len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	issued := request(http.MethodPost, "/api/machines/tokens", "tenant-a", nil)
	if issued.StatusCode != http.StatusCreated {
		t.Fatalf("issue status = %d", issued.StatusCode)
	}
	var token struct {
		Token     string `json:"token"`
		MachineID string `json:"machineId"`
	}
	if err := json.NewDecoder(issued.Body).Decode(&token); err != nil {
		t.Fatal(err)
	}
	_ = issued.Body.Close()
	statusBody, _ := json.Marshal(map[string]string{"token": token.Token})

	for _, tc := range []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/api/machines/" + token.MachineID + "/onboarding-status", statusBody},
		{http.MethodGet, "/api/machines/" + token.MachineID + "/token", nil},
		{http.MethodPost, "/api/machines/" + token.MachineID + "/refresh-token", nil},
		{http.MethodDelete, "/api/machines/" + token.MachineID, nil},
	} {
		resp := request(tc.method, tc.path, "tenant-b", tc.body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s as tenant-b = %d, want 404", tc.method, tc.path, resp.StatusCode)
		}
	}

	// The unauthorized delete must not revoke tenant-a's pending token.
	status := request(http.MethodPost, "/api/machines/"+token.MachineID+"/onboarding-status", "tenant-a", statusBody)
	defer func() { _ = status.Body.Close() }()
	if status.StatusCode != http.StatusOK {
		t.Fatalf("tenant-a onboarding status = %d, want 200", status.StatusCode)
	}
	var state struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(status.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.State != "waiting" {
		t.Fatalf("tenant-a state = %q, want waiting", state.State)
	}

	confirmedID := "m-confirmed-tenant-a"
	if err := st.UpsertMachine(ctx, store.Machine{
		ID: confirmedID, EntityID: "tenant-a", Status: "confirmed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.RefreshToken(confirmedID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/machines/" + confirmedID + "/token"},
		{http.MethodPost, "/api/machines/" + confirmedID + "/refresh-token"},
		{http.MethodDelete, "/api/machines/" + confirmedID},
	} {
		resp := request(tc.method, tc.path, "tenant-b", nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("confirmed %s %s as tenant-b = %d, want 404", tc.method, tc.path, resp.StatusCode)
		}
	}
	visible := request(http.MethodGet, "/api/machines/"+confirmedID+"/token", "tenant-a", nil)
	defer func() { _ = visible.Body.Close() }()
	if visible.StatusCode != http.StatusOK {
		t.Fatalf("tenant-a confirmed token status = %d, want 200", visible.StatusCode)
	}
}
