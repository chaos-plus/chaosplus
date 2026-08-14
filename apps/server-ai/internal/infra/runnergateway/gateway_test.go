package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/infra/runnertransport"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/machine"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/nats-io/nats.go"
)

func TestGatewayRejectsDuplicateOutOfOrderAndStaleFencedEvents(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_NATS_URL"))
	if url == "" {
		t.Skip("set TEST_NATS_URL to a real NATS service")
	}
	subscriberConnection, err := nats.Connect(url, nats.Name("gateway-instance-b-test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(subscriberConnection.Close)
	publisherConnection, err := nats.Connect(url, nats.Name("hub-instance-a-test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisherConnection.Close)

	database, err := (&bunx.Datasource{Type: "sqlite", Dsn: filepath.Join(t.TempDir(), "gateway.db"), Writable: true}).Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := machine.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	directory := machine.NewRepository(database)
	claimsContext := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	routeA, err := directory.AcquireRoute(claimsContext, 41, 51, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	gateway := New(runnertransport.NewNATS(subscriberConnection))
	gateway.SetDirectory(directory)
	workerContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := gateway.Start(workerContext); err != nil {
		t.Fatal(err)
	}
	received := make(chan int, 8)
	gateway.OnEvent(func(event RunnerEvent) { received <- event.Seq })
	publisher := runnertransport.NewNATS(publisherConnection)
	for _, sequence := range []int64{2, 1, 2, 3} {
		if err := publisher.PublishEvent(t.Context(), machine.EventEnvelope{Route: routeA, Event: machine.Event{Seq: sequence, Type: "heartbeat"}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, expected := range []int{2, 3} {
		select {
		case actual := <-received:
			if actual != expected {
				t.Fatalf("accepted sequence = %d, want %d", actual, expected)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for sequence %d", expected)
		}
	}
	if err := directory.ReleaseRoute(claimsContext, routeA); err != nil {
		t.Fatal(err)
	}
	routeB, err := directory.AcquireRoute(claimsContext, 41, 52, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishEvent(t.Context(), machine.EventEnvelope{Route: routeA, Event: machine.Event{Seq: 4, Type: "heartbeat"}}); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishEvent(t.Context(), machine.EventEnvelope{Route: routeB, Event: machine.Event{Seq: 1, Type: "heartbeat"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case actual := <-received:
		if actual != 1 {
			t.Fatalf("takeover sequence = %d, want 1", actual)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for takeover event")
	}
	select {
	case unexpected := <-received:
		t.Fatalf("stale or duplicate event was delivered: %d", unexpected)
	case <-time.After(150 * time.Millisecond):
	}

	commands, err := publisher.SubscribeCommands(t.Context(), routeB, func(_ machine.CommandEnvelope, respond machine.ReplyFunc) {
		if err := directory.ReleaseRoute(claimsContext, routeB); err != nil {
			t.Errorf("release route B during command: %v", err)
			return
		}
		if _, err := directory.AcquireRoute(claimsContext, 41, 53, time.Minute); err != nil {
			t.Errorf("acquire takeover route during command: %v", err)
			return
		}
		_ = respond(machine.ReplyEnvelope{Route: routeB, Reply: machine.Reply{OK: true}})
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = commands.Close() })
	_, err = gateway.RunCmd(claimsContext, routeB.MachineID.String(), "spawn-1", "echo stale", 1000)
	if err == nil || !errors.Is(err, machine.ErrRouteStale) && !strings.Contains(err.Error(), "fenced") {
		t.Fatalf("reply from superseded route = %v, want fenced rejection", err)
	}
}
