package store

import (
	"context"
	"database/sql"
	"time"
)

// EmailTemplate is a row in email_templates (PRD §authn notification).
type EmailTemplate struct {
	TemplateKey string `bun:"template_key,pk"`
	Subject     string `bun:"subject,notnull,default:''"`
	HTMLBody    string `bun:"html_body,notnull,default:''"`
	UpdatedAt   int64  `bun:"updated_at,notnull,default:0"`
}

// GetEmailTemplate returns the template for key, or nil if not in DB.
func (s *Store) GetEmailTemplate(ctx context.Context, key string) (*EmailTemplate, error) {
	var t EmailTemplate
	err := s.db.NewSelect().Model(&t).Where("template_key = ?", key).Scan(ctx)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// SaveEmailTemplate upserts an email template.
func (s *Store) SaveEmailTemplate(ctx context.Context, key, subject, html string) error {
	t := &EmailTemplate{
		TemplateKey: key,
		Subject:     subject,
		HTMLBody:    html,
		UpdatedAt:   time.Now().UnixMilli(),
	}
	_, err := s.db.NewInsert().Model(t).On("CONFLICT (template_key) DO UPDATE").
		Set("subject = EXCLUDED.subject").
		Set("html_body = EXCLUDED.html_body").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}
