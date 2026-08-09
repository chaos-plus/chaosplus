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
	Code            string    `json:"code,omitempty"` // 注册邮箱验证码(6 位)
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
// forwardToSMTP 把通知邮件转发到 SMTP(如 10.0.0.100 的 MailHog),渲染为品牌 HTML。
func forwardToSMTP(n notificationPayload) {
	host := os.Getenv("MAILBRIDGE_SMTP")
	if host == "" {
		return
	}
	title, heading, body, btnText, btnURL := "", "", "", "", ""
	switch {
	case n.VerificationURL != "" || n.Code != "":
		title, heading = "邮箱验证", "验证你的邮箱"
		body = "欢迎使用 chaos.plus。请输入下面的验证码完成注册/登录:"
		btnText, btnURL = "完成验证", n.VerificationURL
	case n.RecoveryURL != "":
		title, heading = "重置密码", "重置你的密码"
		body = "点击下面的按钮重置密码。链接 15 分钟内有效。"
		btnText, btnURL = "重置密码", n.RecoveryURL
	default:
		return
	}
	codeBlock := ""
	if n.Code != "" {
		codeBlock = "<p style=\"margin:0 0 8px;font-size:13px;color:#6b7280\">验证码:</p><p style=\"margin:0 0 24px;font-size:36px;font-weight:800;letter-spacing:12px;color:#6366f1\">" + n.Code + "</p>"
	}
	btnBlock := ""
	if btnText != "" {
		btnBlock = "<a href=\"" + btnURL + "\" style=\"display:inline-block;background:#6366f1;color:#ffffff;padding:12px 28px;border-radius:8px;font-size:14px;font-weight:600;text-decoration:none\">" + btnText + "</a>"
	}
	html := "<!doctype html><html lang=\"zh\"><body style=\"margin:0;padding:0;background:#f5f3ff;font-family:Inter,-apple-system,Segoe UI,Roboto,sans-serif\">" +
		"<table role=\"presentation\" width=\"100%\" cellpadding=\"0\" cellspacing=\"0\" style=\"background:#f5f3ff;padding:32px 16px\"><tr><td align=\"center\">" +
		"<table role=\"presentation\" width=\"480\" cellpadding=\"0\" cellspacing=\"0\" style=\"max-width:480px;width:100%;background:#ffffff;border-radius:16px;box-shadow:0 8px 32px rgba(99,102,241,.12);overflow:hidden\">" +
		"<tr><td style=\"background:linear-gradient(135deg,#6366f1,#8b5cf6);padding:28px 32px\">" +
		"<p style=\"margin:0;color:#ffffff;font-size:20px;font-weight:700\">chaos.plus</p>" +
		"<p style=\"margin:6px 0 0;color:#e0e7ff;font-size:13px\">" + title + "</p></td></tr>" +
		"<tr><td style=\"padding:32px\">" +
		"<h1 style=\"margin:0 0 8px;font-size:22px;color:#312e81;font-weight:700\">" + heading + "</h1>" +
		"<p style=\"margin:0 0 20px;font-size:14px;line-height:1.6;color:#6b7280\">" + body + "</p>" +
		codeBlock + btnBlock +
		"<p style=\"margin:24px 0 0;font-size:12px;color:#9ca3af\">如果这不是你操作,请忽略这封邮件。</p>" +
		"</td></tr></table></td></tr></table></body></html>"
	msg := "From: chaosplus@local\r\nTo: " + n.Recipient + "\r\nSubject: " + title +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n" + html
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
