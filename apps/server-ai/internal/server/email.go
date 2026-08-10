// email.go — SMTP email sending with DB-backed templates (PRD §authn notification).
// IAM webhook → template lookup (DB with defaults) → SMTP.
package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
)

// ── default email templates (overridable via email_templates table) ──

const (
	defaultSubjectVerification  = "验证你的邮箱"
	defaultSubjectPasswordReset = "重置你的密码"
	defaultSubjectInvite        = "邀请你加入 chaos.plus 实例"
	defaultSubjectCode          = "验证你的邮箱"

	defaultBodyVerification = `<!doctype html><html lang="zh"><body style="margin:0;background:#f5f3ff;font-family:Inter,sans-serif">
<table style="max-width:480px;margin:32px auto;background:#fff;border-radius:16px;box-shadow:0 8px 32px rgba(99,102,241,.12)"><tr><td style="padding:28px 32px;background:linear-gradient(135deg,#6366f1,#8b5cf6)"><p style="margin:0;color:#fff;font-size:20px;font-weight:700">chaos.plus</p><p style="margin:6px 0 0;color:#e0e7ff;font-size:13px">邮箱验证</p></td></tr>
<tr><td style="padding:32px"><h1 style="margin:0 0 8px;font-size:22px;color:#312e81">验证你的邮箱</h1>
<p style="margin:0 0 20px;font-size:14px;color:#6b7280">欢迎使用 chaos.plus。请输入下面的验证码完成注册/登录:</p>
<p style="margin:0 0 24px;font-size:36px;font-weight:800;letter-spacing:12px;color:#6366f1">{{.Code}}</p>
<a href="{{.URL}}" style="display:inline-block;background:#6366f1;color:#fff;padding:12px 28px;border-radius:8px;font-size:14px;font-weight:600;text-decoration:none">完成验证</a>
<p style="margin:24px 0 0;font-size:12px;color:#9ca3af">如果这不是你操作,请忽略这封邮件。</p></td></tr></table></body></html>`

	defaultBodyPasswordReset = `<!doctype html><html lang="zh"><body style="margin:0;background:#f5f3ff;font-family:Inter,sans-serif">
<table style="max-width:480px;margin:32px auto;background:#fff;border-radius:16px;box-shadow:0 8px 32px rgba(99,102,241,.12)"><tr><td style="padding:28px 32px;background:linear-gradient(135deg,#6366f1,#8b5cf6)"><p style="margin:0;color:#fff;font-size:20px;font-weight:700">chaos.plus</p><p style="margin:6px 0 0;color:#e0e7ff;font-size:13px">重置密码</p></td></tr>
<tr><td style="padding:32px"><h1 style="margin:0 0 8px;font-size:22px;color:#312e81">重置你的密码</h1>
<p style="margin:0 0 20px;font-size:14px;color:#6b7280">点击下面的按钮重置密码。链接 15 分钟内有效。</p>
<a href="{{.URL}}" style="display:inline-block;background:#6366f1;color:#fff;padding:12px 28px;border-radius:8px;font-size:14px;font-weight:600;text-decoration:none">重置密码</a>
<p style="margin:24px 0 0;font-size:12px;color:#9ca3af">如果这不是你操作,请忽略这封邮件。</p></td></tr></table></body></html>`

	defaultBodyInvite = `<!doctype html><html lang="zh"><body style="margin:0;background:#f5f3ff;font-family:Inter,sans-serif">
<table style="max-width:480px;margin:32px auto;background:#fff;border-radius:16px;box-shadow:0 8px 32px rgba(99,102,241,.12)"><tr><td style="padding:28px 32px;background:linear-gradient(135deg,#6366f1,#8b5cf6)"><p style="margin:0;color:#fff;font-size:20px;font-weight:700">chaos.plus</p></td></tr>
<tr><td style="padding:32px"><h1 style="margin:0 0 8px;font-size:22px;color:#312e81">你被邀请加入实例</h1>
<p style="margin:0 0 20px;font-size:14px;color:#6b7280">{{.EntityName}} 邀请你加入,点击按钮开始协作。</p>
<a href="{{.URL}}" style="display:inline-block;background:#6366f1;color:#fff;padding:12px 28px;border-radius:8px;font-size:14px;font-weight:600;text-decoration:none">加入实例</a>
<p style="margin:24px 0 0;font-size:12px;color:#9ca3af">如果这不是你操作,请忽略这封邮件。</p></td></tr></table></body></html>`
)

// notificationPayload matches IAM's authn notification webhook contract.
type notificationPayload struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Recipient       string `json:"recipient"`
	RecoveryURL     string `json:"recovery_url,omitempty"`
	VerificationURL string `json:"verification_url,omitempty"`
	Code            string `json:"code,omitempty"`
}

// emailRenderer resolves a template by key: DB row wins, constant default falls back.
type emailRenderer struct {
	st *store.Store
}

func (r *emailRenderer) resolve(ctx context.Context, key string) (subject, html string) {
	if r.st != nil {
		if t, err := r.st.GetEmailTemplate(ctx, key); err == nil && t != nil {
			if t.Subject != "" {
				subject = t.Subject
			}
			if t.HTMLBody != "" {
				html = t.HTMLBody
			}
		}
	}
	if html == "" {
		subject, html = defaultTemplate(key)
	}
	return
}

func defaultTemplate(key string) (subject, html string) {
	switch key {
	case "verification":
		return defaultSubjectVerification, defaultBodyVerification
	case "password_reset":
		return defaultSubjectPasswordReset, defaultBodyPasswordReset
	case "invite":
		return defaultSubjectInvite, defaultBodyInvite
	default:
		return defaultSubjectCode, defaultBodyVerification // ponytail: generic code template
	}
}

// sendEmailSMTP sends a HTML email to recipient via SMTP_HOST.
func sendEmailSMTP(to, subject, html string) error {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		host = os.Getenv("MAILBRIDGE_SMTP") // legacy env
	}
	if host == "" {
		return nil // silently skip when no SMTP configured (local dev without MailHog)
	}
	msg := "From: " + smtpFrom() + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n" + html
	return smtp.SendMail(host, nil, smtpFrom(), []string{to}, []byte(msg))
}

func smtpFrom() string {
	if f := os.Getenv("SMTP_FROM"); f != "" {
		return f
	}
	return "chaosplus@local"
}

// ── IAM notification webhook handler ──

func (cs *ChatService) registerEmailRoutes(mux *http.ServeMux) {
	er := &emailRenderer{st: cs.st}

	mux.HandleFunc("POST /api/email/notification", func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		var n notificationPayload
		if err := json.Unmarshal(body, &n); err != nil {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"bad payload"}`))
			return
		}
		if n.Recipient == "" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"recipient required"}`))
			return
		}

		var tmplKey string
		var tmplData map[string]string
		switch {
		case n.VerificationURL != "" || n.Code != "":
			tmplKey = "verification"
			tmplData = map[string]string{"Code": n.Code, "URL": n.VerificationURL}
		case n.RecoveryURL != "":
			tmplKey = "password_reset"
			tmplData = map[string]string{"URL": n.RecoveryURL}
		default:
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"unknown notification type"}`))
			return
		}

		subject, tmpl := er.resolve(req.Context(), tmplKey)
		html := renderTemplate(tmpl, tmplData)

		log.Printf("email %s -> %s (%s)", tmplKey, n.Recipient, n.ID)
		if err := sendEmailSMTP(n.Recipient, subject, html); err != nil {
			log.Printf("email send failed: %v", err)
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":"send failed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func renderTemplate(tmpl string, data map[string]string) string {
	s := tmpl
	for k, v := range data {
		s = strings.ReplaceAll(s, "{{."+k+"}}", v)
	}
	return s
}

// SendInviteEmail sends an HTML invite email via SMTP.
// Template: "invite" from DB, falls back to defaultBodyInvite.
func (cs *ChatService) SendInviteEmail(ctx context.Context, email, entityName, inviteURL string) error {
	r := &emailRenderer{st: cs.st}
	subject, tmpl := r.resolve(ctx, "invite")
	html := renderTemplate(tmpl, map[string]string{"EntityName": entityName, "URL": inviteURL})
	return sendEmailSMTP(email, subject, html)
}
