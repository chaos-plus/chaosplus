// command server-ai runs the chaos.plus server-ai: connects to NATS,
// answers runner registrations, drives spawn/kill/switch-provider, and persists
// the runner event stream to the event-log StateStore (PRD §15.1).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/machine"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/server"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
	"github.com/chaos-plus/chaosplus/pkg/utils"
)

func main() {
	// Auth: read API token from env (NOT argv — /proc/<pid>/cmdline is world-readable).
	// Empty = desktop/localhost mode with no auth gate.
	server.AuthToken = os.Getenv("CONTROL_API_TOKEN")

	url := envOr("CONTROL_NATS_URL", "nats://127.0.0.1:4222")
	nc, err := nats.Connect(url, nats.Name("chaosplus-server-ai"), nats.Timeout(5*time.Second),
		nats.NoEcho()) // prevent self-receive of published run events
	if err != nil {
		log.Fatalf("connect NATS %s: %v", url, err)
	}
	defer nc.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g := gateway.New(nc)

	// Event-log persistence: optional (CONTROL_DB_DSN unset → in-memory only).
	var st *store.Store
	if dsn := os.Getenv("CONTROL_DB_DSN"); dsn != "" {
		var err error
		st, err = store.Open(ctx, dsn)
		if err != nil {
			log.Fatalf("open store: %v", err)
		}
		defer st.Close()
		// Crash recovery: runs left mid-flight by a dead process are surfaced via
		// LoadActiveRunDefinitions; no terminal flip here (merged base design).
		g.OnEvent(func(ev gateway.RunnerEvent) {
			payload, _ := json.Marshal(ev.Payload)
			_ = st.Append(ctx, store.Event{
				ID:             fmt.Sprintf("evt-%d-%s", ev.Seq, ev.RunnerID),
				InstanceID:     "desktop",
				Type:           ev.Type,
				IdempotencyKey: fmt.Sprintf("%s:%s:%d", ev.RunnerID, ev.Type, ev.Seq),
				PayloadJSON:    string(payload),
			})
		})
		log.Printf("server-ai persisting events to %s", dsn)
	}

	go func() {
		if err := g.Start(ctx); err != nil {
			log.Printf("gateway stopped: %v", err)
		}
	}()

	// Machine hub = NATS↔WS bridge for daemons; engine reaches daemons via the
	// NATS gateway (any instance), never through the bridge directly.
	hub := machine.NewHub(nc, machine.NewTokenStore(), st)
	// 重启后回灌 DB 里的长期 token hash,daemon 才能用旧 token 重连。
	if err := hub.LoadTokens(ctx); err != nil {
		log.Fatalf("load machine tokens: %v", err)
	}
	link := &workflow.NatsRunnerLink{G: g}
	rm := server.NewRunManager(nc, link, st, envOr("CONTROL_RUNNER_ID", ""))
	// Per-node machine dispatch: match node executor to machine runtimes.
	rm.SetMachinePicker(func(executorType string) string {
		for _, id := range hub.RegisteredRunners() {
			for _, rt := range hub.MachineRuntimes(id) {
				if rt == executorType {
					return id
				}
			}
		}
		return "" // fall back to default runnerID
	})
	if err := rm.Start(ctx); err != nil {
		log.Fatalf("run manager: %v", err)
	}
	if n, err := st.ReconcileStaleRunning(ctx); err != nil {
		slog.Warn("reconcile stale work items", "err", err)
	} else if n > 0 {
		slog.Info("reconciled work items stuck in_progress from a previous process", "count", n)
	}
	// Rehydrate runs from store so UI shows history across restarts (§15.1).
	if st != nil {
		rm.LoadFromStore(ctx)
	}

	var chat *server.ChatService
	if st != nil {
		chat = server.NewChatService(st, link, g, rm, envOr("CONTROL_RUNNER_ID", ""), envOr("CHAT_WORKSPACE_ROOT", defaultWorkspaceRoot()))
	}
	httpAddr := ":" + envOr("CONTROL_HTTP_PORT", "8081")
	hs := &http.Server{Addr: httpAddr, Handler: server.NewHandler(rm, hub, chat)}
	go func() {
		log.Printf("server-ai HTTP listening on %s", httpAddr)
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server: %v", err)
		}
	}()
	defer hs.Shutdown(context.Background())

	log.Printf("server-ai listening on NATS %s", url)
	<-ctx.Done()
	log.Printf("runners seen: %v", g.RegisteredRunners())
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fmt.Sprint(def)
}

// defaultWorkspaceRoot returns the CHAT_WORKSPACE_ROOT default:
// $XDG_CONFIG_HOME/<binary>/channels on Linux,
// ~/Library/Application Support/<binary>/channels on macOS,
// %AppData%/<binary>/channels on Windows.
func defaultWorkspaceRoot() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), utils.GetExecutableName(), "channels")
	}
	return filepath.Join(d, utils.GetExecutableName(), "channels")
}
