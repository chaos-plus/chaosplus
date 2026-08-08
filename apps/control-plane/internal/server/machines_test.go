package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/machine"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
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
	ts := httptest.NewServer(NewHandler(m, hub))
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
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

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
	defer ws.Close()
	var ready map[string]any
	if err := ws.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
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
	cresp.Body.Close()

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
	dresp.Body.Close()
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
	defer resp.Body.Close()
	var ms []machineSummary
	if err := json.NewDecoder(resp.Body).Decode(&ms); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return ms
}
