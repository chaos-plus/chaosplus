// mailbridge is a dev mail catcher for the IAM authn notification webhook.
// It writes each notification to disk, serves them back over HTTP (so the
// verification link can be clicked in a real browser), and optionally forwards
// them to an SMTP server (e.g. MailHog) via MAILBRIDGE_SMTP.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"time"
)

type notificationPayload struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"`
	Recipient       string    `json:"recipient"`
	RecoveryURL     string    `json:"recovery_url,omitempty"`
	VerificationURL string    `json:"verification_url,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
}

var dir = func() string {
	d := os.Getenv("MAILBRIDGE_DIR")
	if d == "" {
		d = filepath.Join(os.TempDir(), "mailbridge")
	}
	return d
}()

// forwardToSMTP 把通知邮件转发到 SMTP(如 10.0.0.100 的 MailHog),真实可查。
func forwardToSMTP(n notificationPayload) {
	host := os.Getenv("MAILBRIDGE_SMTP")
	if host == "" {
		return
	}
	var body string
	switch {
	case n.VerificationURL != "":
		body = "请点击以下链接完成验证:\n\n" + n.VerificationURL + "\n"
	case n.RecoveryURL != "":
		body = "请点击以下链接重置密码:\n\n" + n.RecoveryURL + "\n"
	default:
		return
	}
	msg := "From: chaosplus@local\r\nTo: " + n.Recipient + "\r\nSubject: " + n.Type +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	if err := smtp.SendMail(host, nil, "chaosplus@local", []string{n.Recipient}, []byte(msg)); err != nil {
		log.Printf("smtp forward failed: %v", err)
	}
}

func main() {
	addr := os.Getenv("MAILBRIDGE_ADDR")
	if addr == "" {
		addr = ":19999"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("POST /hook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var n notificationPayload
		if err := json.Unmarshal(body, &n); err != nil {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"bad payload"}`))
			return
		}
		fn := filepath.Join(dir, fmt.Sprintf("%s.json", n.ID))
		if err := os.WriteFile(fn, body, 0o644); err != nil {
			w.WriteHeader(500)
			return
		}
		kind, url := n.Type, n.VerificationURL
		if n.RecoveryURL != "" {
			url = n.RecoveryURL
			kind = "recovery"
		}
		log.Printf("mail %s -> %s\n  %s\n", kind, n.Recipient, url)
		forwardToSMTP(n)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	http.HandleFunc("GET /inbox", func(w http.ResponseWriter, r *http.Request) {
		entries, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		type item struct {
			ID          string `json:"id"`
			Type        string `json:"type"`
			Recipient   string `json:"recipient"`
			RecoveryURL string `json:"recoveryUrl,omitempty"`
			VerifyURL   string `json:"verificationUrl,omitempty"`
		}
		out := []item{}
		for _, f := range entries {
			raw, _ := os.ReadFile(f)
			var n notificationPayload
			if json.Unmarshal(raw, &n) != nil {
				continue
			}
			out = append(out, item{ID: n.ID, Type: n.Type, Recipient: n.Recipient, RecoveryURL: n.RecoveryURL, VerifyURL: n.VerificationURL})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	log.Printf("mailbridge listening on %s, inbox dir %s", addr, dir)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
