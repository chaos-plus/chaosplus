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
	t.Setenv("EMAIL_WEBHOOK_SECRET", "")
	t.Setenv("CONTROL_AUTH_HARDENED", "")
	server.AuthToken = ""
	err := validateRuntimeConfig("127.0.0.1:8081")
	if err == nil || !strings.Contains(err.Error(), "CONTROL_DB_DSN") {
		t.Fatalf("incomplete production config error = %v", err)
	}

	t.Setenv("CONTROL_API_TOKEN", "secret")
	t.Setenv("CONTROL_DB_DSN", "control.db")
	t.Setenv("EMAIL_WEBHOOK_SECRET", "webhook-secret")
	t.Setenv("CONTROL_AUTH_HARDENED", "1")
	server.AuthToken = "secret"
	if err := validateRuntimeConfig("0.0.0.0:8081"); err != nil {
		t.Fatalf("complete production config rejected: %v", err)
	}
}
