// command control-plane runs the chaos.plus control-plane: connects to NATS,
// answers runner registrations, drives spawn/kill/switch-provider, and persists
// the runner event stream to the event-log StateStore (PRD §15.1).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/store"
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
	if dsn := os.Getenv("CONTROL_DB_DSN"); dsn != "" {
		st, err := store.Open(ctx, dsn)
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
