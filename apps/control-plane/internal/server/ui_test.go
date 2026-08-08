package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUIServesPage(t *testing.T) {
	nc := startTestNATS(t)
	m := NewRunManager(nc, nil, nil, "r")
	ts := httptest.NewServer(NewHandler(m))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"chaos.plus", "approve", "WebSocket"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("UI page missing %q", want)
		}
	}
}
