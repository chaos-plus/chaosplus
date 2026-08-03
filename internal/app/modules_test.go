package app

import (
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/require"
)

func TestAuditAppenderUsesCallerDatabase(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))

	appendAudit := auditAppender(audit.NewService(db))
	event := auditx.NewEvent(t.Context(), "tenant", "tested", "module", "modules")
	require.NoError(t, appendAudit(t.Context(), db, event))

	var count int
	require.NoError(t, db.NewSelect().Table("iam_audit_events").ColumnExpr("COUNT(*)").Where("tenant_id = ? AND event_type = ?", "tenant", "tested").Scan(t.Context(), &count))
	require.Equal(t, 1, count)
}

func TestRegistrationPrincipalCreatorMapsIdentityErrors(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))

	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	id, err := registrationPrincipalCreator(t.Context(), db, "user@example.com", "password-hash", "User", now)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	_, err = registrationPrincipalCreator(t.Context(), db, "user@example.com", "password-hash", "User", now)
	require.ErrorIs(t, err, authnext.ErrRegistrationConflict)
	_, err = registrationPrincipalCreator(t.Context(), db, "invalid", "password-hash", "User", now)
	require.ErrorIs(t, err, authnext.ErrInvalidRegistration)
}
