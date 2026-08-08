// command control-plane runs the chaos.plus control-plane: connects to NATS,
// answers runner registrations, drives spawn/kill/switch-provider, and persists
// the runner event stream to the event-log StateStore (PRD §15.1).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/machine"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/server"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

func main() {
	url := envOr("CONTROL_NATS_URL", "nats://127.0.0.1:4222")
	nc, err := nats.Connect(url, nats.Name("chaosplus-control-plane"), nats.Timeout(5*time.Second))
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
		log.Printf("control-plane persisting events to %s", dsn)
	}

	go func() {
		if err := g.Start(ctx); err != nil {
			log.Printf("gateway stopped: %v", err)
		}
	}()

	// Machine hub = NATS↔WS bridge for daemons; engine reaches daemons via the
	// NATS gateway (any instance), never through the bridge directly.
	hub := machine.NewHub(nc, machine.NewTokenStore(), st)
	link := &workflow.NatsRunnerLink{G: g}
	rm := server.NewRunManager(nc, link, st, envOr("CONTROL_RUNNER_ID", ""))
	if err := rm.Start(ctx); err != nil {
		log.Fatalf("run manager: %v", err)
	}
	chat := server.NewChatService(st, link, envOr("CHAT_WORKSPACE_ROOT", "C:/tmp/chaos-channels"))
	httpAddr := ":" + envOr("CONTROL_HTTP_PORT", "8081")
	hs := &http.Server{Addr: httpAddr, Handler: server.NewHandler(rm, hub, chat)}
	go func() {
		log.Printf("control-plane HTTP listening on %s", httpAddr)
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server: %v", err)
		}
	}()
	defer hs.Shutdown(context.Background())

	log.Printf("control-plane listening on NATS %s", url)
	<-ctx.Done()
	log.Printf("runners seen: %v", g.RegisteredRunners())
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fmt.Sprint(def)
}
