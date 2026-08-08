package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/machine"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true }, // local dev single-user
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// NewHandler wires all control-plane HTTP routes.
func NewHandler(m *RunManager, hub *machine.Hub) http.Handler {
	mux := http.NewServeMux()

	// machines — runner onboarding (PRD §5.3.1).
	mux.HandleFunc("POST /api/machines/tokens", func(w http.ResponseWriter, r *http.Request) {
		machineID, token := hub.IssueToken()
		writeJSON(w, 201, map[string]any{"token": token, "machineId": machineID, "expiresIn": 300})
	})
	mux.HandleFunc("GET /api/machines/ws", hub.HandleWS)
	mux.HandleFunc("GET /api/machines", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := hub.ListMachines(r.Context())
		type msum struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			Address         string `json:"address"`
			Status          string `json:"status"`
			Online          bool   `json:"online"`
			LastHeartbeatAt int64  `json:"lastHeartbeatAt"`
		}
		out := []msum{}
		for _, m := range ms {
			name := hub.MachineName(m.ID)
			if name == "" {
				name = m.ID
			}
			out = append(out, msum{ID: m.ID, Name: name, Address: m.Address, Status: m.Status, Online: hub.IsConnected(m.ID), LastHeartbeatAt: m.LastHeartbeatAt})
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("POST /api/machines/{id}/confirm", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		if err := hub.Confirm(r.Context(), id, body.Token, r.RemoteAddr); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("DELETE /api/machines/{id}", func(w http.ResponseWriter, r *http.Request) {
		hub.Cancel(r.PathValue("id"))
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/machines/{id}/refresh-token", func(w http.ResponseWriter, r *http.Request) {
		token, err := hub.RefreshToken(r.PathValue("id"))
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"token": token, "expiresIn": 300})
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(uiHTML))
	})
	mux.HandleFunc("POST /api/runs", func(w http.ResponseWriter, r *http.Request) {
		var req LaunchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		// The run outlives the HTTP request: tie it to the process lifetime, not
		// r.Context() (which cancels when this handler returns and would kill a
		// run parked on a human-approval gate).
		run, err := m.Launch(context.Background(), req)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"runId": run.ID})
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		type sum struct {
			ID        string    `json:"id"`
			Status    RunStatus `json:"status"`
			Nodes     int       `json:"nodes"`
			CreatedAt string    `json:"createdAt"`
		}
		out := []sum{}
		for _, run := range m.List() {
			out = append(out, sum{ID: run.ID, Status: run.Status(), Nodes: len(run.Def.Nodes), CreatedAt: run.created.Format("2006-01-02 15:04:05")})
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /api/runs/{id}/events", m.handleWS)
	mux.HandleFunc("POST /api/runs/{id}/approvals/{node}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		node := r.PathValue("node")
		var body struct {
			Approve bool   `json:"approve"`
			Reason  string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "bad request: "+err.Error())
			return
		}
		if _, ok := m.Get(id); !ok {
			writeErr(w, 404, "run not found")
			return
		}
		if err := m.Approve(id, node, body.Approve, body.Reason); err != nil {
			writeErr(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	return mux
}

// handleWS upgrades to WebSocket, replays buffered events, then streams live.
func (m *RunManager) handleWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok := m.Get(id)
	if !ok {
		writeErr(w, 404, "run not found")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sub, hist, unsub := run.subscribe()
	defer unsub()
	for _, ev := range hist { // replay buffered
		if err := conn.WriteJSON(ev); err != nil {
			return
		}
	}
	for ev := range sub {
		if err := conn.WriteJSON(ev); err != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
