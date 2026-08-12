package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func clearNATSEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CONTROL_NATS_CONFIG", "CONTROL_NATS_HOST", "CONTROL_NATS_PORT",
		"CONTROL_NATS_MONITOR_HOST", "CONTROL_NATS_MONITOR_PORT", "CONTROL_NATS_TOKEN",
		"CONTROL_NATS_JETSTREAM", "CONTROL_NATS_STORE_DIR", "CONTROL_NATS_SERVER_NAME",
		"CONTROL_NATS_TLS_CERT", "CONTROL_NATS_TLS_KEY", "CONTROL_NATS_TLS_CA",
		"CONTROL_NATS_TLS_VERIFY",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadOptionsSafeDefaults(t *testing.T) {
	clearNATSEnv(t)
	opts, err := loadOptions()
	if err != nil {
		t.Fatal(err)
	}
	if opts.Host != "127.0.0.1" || opts.Port != 4222 {
		t.Fatalf("client listener = %s:%d", opts.Host, opts.Port)
	}
	if opts.HTTPHost != "127.0.0.1" || opts.HTTPPort != 8222 {
		t.Fatalf("monitor listener = %s:%d", opts.HTTPHost, opts.HTTPPort)
	}
	if !opts.JetStream || opts.StoreDir == "" || !opts.NoSigs {
		t.Fatalf("durability/signal defaults not applied: %#v", opts)
	}
}

func TestLoadOptionsRequiresAuthOffLoopback(t *testing.T) {
	clearNATSEnv(t)
	t.Setenv("CONTROL_NATS_HOST", "0.0.0.0")
	if _, err := loadOptions(); err == nil {
		t.Fatal("expected an authentication error")
	}
	t.Setenv("CONTROL_NATS_TOKEN", "test-token")
	if _, err := loadOptions(); err != nil {
		t.Fatalf("token-authenticated listener rejected: %v", err)
	}
}

func TestLoadOptionsRejectsPartialTLS(t *testing.T) {
	clearNATSEnv(t)
	t.Setenv("CONTROL_NATS_TLS_CERT", "server.pem")
	if _, err := loadOptions(); err == nil {
		t.Fatal("expected partial TLS configuration error")
	}
}

func TestEmbeddedServerAcceptsAuthenticatedConnection(t *testing.T) {
	storeDir := t.TempDir()
	opts := &natsserver.Options{
		Host:          "127.0.0.1",
		Port:          -1,
		HTTPPort:      -1,
		Authorization: "test-token",
		JetStream:     true,
		StoreDir:      filepath.Join(storeDir, "jetstream"),
		NoLog:         true,
		NoSigs:        true,
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	t.Cleanup(func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	})
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded NATS did not become ready")
	}

	if _, err := nats.Connect(ns.ClientURL(), nats.Timeout(time.Second)); err == nil {
		t.Fatal("unauthenticated connection unexpectedly succeeded")
	}
	nc, err := nats.Connect(ns.ClientURL(), nats.Token("test-token"), nats.Timeout(time.Second))
	if err != nil {
		t.Fatalf("authenticated connection failed: %v", err)
	}
	defer nc.Close()
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(storeDir, "jetstream")); err != nil {
		t.Fatalf("JetStream store was not created: %v", err)
	}
}
