package main

import (
	"strings"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/server"
)

func TestValidateRuntimeConfig(t *testing.T) {
	oldToken := server.AuthToken
	t.Cleanup(func() { server.AuthToken = oldToken })
	t.Setenv("CONTROL_ENV", "")
	server.AuthToken = ""
	if err := validateRuntimeConfig("127.0.0.1:8081"); err != nil {
		t.Fatalf("loopback desktop config rejected: %v", err)
	}
	if err := validateRuntimeConfig(":8081"); err == nil {
		t.Fatal("unauthenticated all-interface listener must be rejected")
	}
	server.AuthToken = "secret"
	if err := validateRuntimeConfig(":8081"); err != nil {
		t.Fatalf("authenticated listener rejected: %v", err)
	}
}

func TestValidateProductionConfig(t *testing.T) {
	oldToken := server.AuthToken
	t.Cleanup(func() { server.AuthToken = oldToken })
	t.Setenv("CONTROL_ENV", "production")
	t.Setenv("CONTROL_API_TOKEN", "")
	t.Setenv("CONTROL_DB_DSN", "")
	t.Setenv("CONTROL_EMAIL_WEBHOOK_SECRET", "")
	t.Setenv("CONTROL_AUTH_HARDENED", "")
	t.Setenv("CONTROL_NATS_URL", "")
	t.Setenv("CONTROL_NATS_TOKEN", "")
	t.Setenv("CONTROL_NATS_TLS_CA", "")
	t.Setenv("CONTROL_NATS_DEPLOYMENT", "")
	server.AuthToken = ""
	err := validateRuntimeConfig("127.0.0.1:8081")
	if err == nil || !strings.Contains(err.Error(), "CONTROL_DB_DSN") {
		t.Fatalf("incomplete production config error = %v", err)
	}

	t.Setenv("CONTROL_API_TOKEN", "secret")
	t.Setenv("CONTROL_DB_DSN", "control.db")
	t.Setenv("CONTROL_EMAIL_WEBHOOK_SECRET", "webhook-secret")
	t.Setenv("CONTROL_AUTH_HARDENED", "1")
	t.Setenv("CONTROL_NATS_URL", "tls://nats:4222")
	t.Setenv("CONTROL_NATS_TOKEN", "nats-secret")
	t.Setenv("CONTROL_NATS_TLS_CA", "/run/secrets/nats-ca.pem")
	t.Setenv("CONTROL_NATS_DEPLOYMENT", "official-image")
	server.AuthToken = "secret"
	if err := validateRuntimeConfig("0.0.0.0:8081"); err != nil {
		t.Fatalf("complete production config rejected: %v", err)
	}
	t.Setenv("CONTROL_NATS_DEPLOYMENT", "embedded")
	if err := validateRuntimeConfig("0.0.0.0:8081"); err == nil || !strings.Contains(err.Error(), "official-image") {
		t.Fatalf("embedded production NATS accepted: %v", err)
	}
	t.Setenv("CONTROL_NATS_DEPLOYMENT", "official-image")
	for _, invalidURL := range []string{
		"nats://nats:4222",
		"tls://token@nats:4222",
		"tls://nats:4222?token=secret",
		"tls:///missing-host",
	} {
		t.Setenv("CONTROL_NATS_URL", invalidURL)
		if err := validateRuntimeConfig("0.0.0.0:8081"); err == nil || !strings.Contains(err.Error(), "valid tls://") {
			t.Errorf("invalid production NATS URL %q accepted: %v", invalidURL, err)
		}
	}
}
