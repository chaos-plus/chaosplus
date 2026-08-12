package websec

import (
	"net/http/httptest"
	"testing"
)

func TestOriginAllowed(t *testing.T) {
	t.Setenv("CONTROL_ALLOWED_ORIGINS", "https://admin.example.com")
	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{name: "non-browser", host: "control.example.com", want: true},
		{name: "same-origin", host: "control.example.com", origin: "https://control.example.com", want: true},
		{name: "configured", host: "control.example.com", origin: "https://admin.example.com", want: true},
		{name: "cross-origin", host: "control.example.com", origin: "https://evil.example.com", want: false},
		{name: "malformed", host: "control.example.com", origin: "://", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://"+tt.host+"/ws", nil)
			r.Header.Set("Origin", tt.origin)
			if got := OriginAllowed(r); got != tt.want {
				t.Fatalf("OriginAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}
