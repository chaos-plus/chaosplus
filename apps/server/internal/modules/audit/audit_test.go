package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
)

func TestAuditAppendQueryIntegrityAndImmutability(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	ids := []guid.ID{testID("event-1"), testID("event-2"), testID("event-3")}
	service.nextID = func() (guid.ID, error) { id := ids[0]; ids = ids[1:]; return id, nil }

	first, err := service.Append(t.Context(), EventInput{TenantID: testID("tenant-a"), PrincipalID: testID("principal"), EventType: "oauth_client_created", TargetType: "oauth_client", TargetID: testID("client"), Outcome: "success", Detail: map[string]any{"public": false}})
	require.NoError(t, err)
	second, err := service.Append(t.Context(), EventInput{TenantID: testID("tenant-a"), PrincipalID: testID("principal"), EventType: "oauth_client_rotated", TargetType: "oauth_client", TargetID: testID("client"), Outcome: "success"})
	require.NoError(t, err)
	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant-b"), EventType: "login", Outcome: "denied"})
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Sequence)
	require.Equal(t, int64(2), second.Sequence)
	require.Equal(t, first.EventHash, second.PreviousHash)
	require.JSONEq(t, `{}`, string(second.Detail))

	events, total, err := service.List(t.Context(), Filter{TenantID: testID("tenant-a"), TargetType: "oauth_client", Offset: 0, Limit: 50})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Equal(t, []string{wireID("event-2"), wireID("event-1")}, []string{guidString(events[1].ID), guidString(events[0].ID)})
	_, total, err = service.List(t.Context(), Filter{TenantID: testID("tenant-b"), Offset: 0, Limit: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	filtered, total, err := service.List(t.Context(), Filter{
		TenantID: testID("tenant-a"), PrincipalID: testID("principal"), EventType: "oauth_client_created",
		Outcome: "success", TargetType: "oauth_client", TargetID: testID("client"),
		From: time.Unix(99, 0), To: time.Unix(101, 0), Offset: 0, Limit: 50,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, first.ID, filtered[0].ID)
	got, err := service.Get(t.Context(), testID("tenant-a"), first.ID)
	require.NoError(t, err)
	require.Equal(t, first.EventHash, got.EventHash)
	_, err = service.Get(t.Context(), testID("tenant-b"), first.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)

	integrity, err := service.Verify(t.Context(), testID("tenant-a"))
	require.NoError(t, err)
	require.True(t, integrity.Valid)
	require.Equal(t, int64(2), integrity.VerifiedEvents)
	_, err = db.NewUpdate().Table("iam_audit_events").Set("outcome = 'failure'").Where("id = ?", first.ID).Exec(context.Background())
	require.ErrorContains(t, err, "append-only")
	_, err = db.NewDelete().Table("iam_audit_events").Where("id = ?", first.ID).Exec(context.Background())
	require.ErrorContains(t, err, "append-only")
}

func TestAuditValidationAndEmptyIntegrity(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	_, err = service.Append(t.Context(), EventInput{EventType: "", Outcome: "success"})
	require.ErrorIs(t, err, ErrInvalidEvent)
	_, err = service.AppendTo(t.Context(), nil, EventInput{TenantID: testID("tenant"), EventType: "event", Outcome: "success"})
	require.ErrorIs(t, err, ErrInvalidEvent)
	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant"), EventType: "event", Outcome: "success", Detail: map[string]any{"unsupported": make(chan int)}})
	require.ErrorContains(t, err, "encode audit detail")
	_, _, err = service.List(t.Context(), Filter{TenantID: testID("tenant"), Limit: 0})
	require.ErrorIs(t, err, ErrInvalidFilter)
	_, _, err = service.List(t.Context(), Filter{TenantID: testID("tenant"), Offset: -1, Limit: 50})
	require.ErrorIs(t, err, ErrInvalidFilter)
	_, _, err = service.List(t.Context(), Filter{TenantID: testID("tenant"), Limit: 201})
	require.ErrorIs(t, err, ErrInvalidFilter)
	_, _, err = service.List(t.Context(), Filter{TenantID: testID("tenant"), Outcome: "unknown", Limit: 50})
	require.ErrorIs(t, err, ErrInvalidFilter)
	_, err = service.Verify(t.Context(), 0)
	require.NoError(t, err)
	integrity, err := service.Verify(t.Context(), testID("tenant"))
	require.NoError(t, err)
	require.True(t, integrity.Valid)
	require.Zero(t, integrity.VerifiedEvents)
	assertInvalidEvents(t)
	assert.Panics(t, func() { NewService(nil, nil) })
}

func TestAuditExportIsVerifiedTenantSnapshot(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	service.now = func() time.Time { return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC) }
	ids := []guid.ID{testID("event-1"), testID("event-other"), testID("event-later")}
	service.nextID = func() (guid.ID, error) { id := ids[0]; ids = ids[1:]; return id, nil }

	first, err := service.Append(t.Context(), EventInput{TenantID: testID("tenant-a"), PrincipalID: testID("principal-a"), EventType: "principal_created", TargetType: "principal", TargetID: testID("principal-a"), Outcome: "success"})
	require.NoError(t, err)
	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant-b"), EventType: "tenant_event", Outcome: "success"})
	require.NoError(t, err)
	snapshot, err := service.PrepareExport(t.Context(), Filter{TenantID: testID("tenant-a"), PrincipalID: testID("principal-a")})
	require.NoError(t, err)
	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant-a"), PrincipalID: testID("principal-a"), EventType: "principal_updated", Outcome: "success"})
	require.NoError(t, err)

	var output bytes.Buffer
	require.NoError(t, service.WriteExport(t.Context(), snapshot, &output))
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 3)
	var manifest exportManifest
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &manifest))
	assert.Equal(t, "audit-export.v1", manifest.Schema)
	assert.Equal(t, testID("tenant-a"), manifest.TenantID)
	assert.Equal(t, int64(1), manifest.HeadSequence)
	assert.Equal(t, first.EventHash, manifest.HeadHash)
	assert.Equal(t, testID("principal-a"), manifest.Filter.PrincipalID)

	var record exportEvent
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &record))
	assert.Equal(t, first.ID, record.Event.ID)
	assert.NotContains(t, output.String(), "event-other")
	assert.NotContains(t, output.String(), "event-later")

	var complete exportComplete
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &complete))
	assert.Equal(t, int64(1), complete.ExportedEvents)
	contentHash := sha256.Sum256([]byte(lines[1] + "\n"))
	assert.Equal(t, hex.EncodeToString(contentHash[:]), complete.ContentSHA256)
}

func TestAuditExportValidationAndFailures(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	_, err = service.PrepareExport(t.Context(), Filter{TenantID: testID("tenant"), From: time.Unix(2, 0), To: time.Unix(1, 0)})
	require.ErrorIs(t, err, ErrInvalidFilter)
	require.ErrorIs(t, service.WriteExport(t.Context(), ExportSnapshot{}, nil), ErrInvalidFilter)

	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant"), EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	snapshot, err := service.PrepareExport(t.Context(), Filter{TenantID: testID("tenant")})
	require.NoError(t, err)
	require.NoError(t, db.Close())
	var output bytes.Buffer
	require.ErrorContains(t, service.WriteExport(t.Context(), snapshot, &output), "read audit export")
	assert.NotContains(t, output.String(), `"type":"complete"`)
	_, err = service.PrepareExport(t.Context(), Filter{TenantID: testID("tenant")})
	require.ErrorContains(t, err, "read audit head")
}

func TestAuditExportEmptyFilteredAndTamperedSnapshots(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	empty, err := service.PrepareExport(t.Context(), Filter{TenantID: testID("tenant")})
	require.NoError(t, err)
	var output bytes.Buffer
	require.NoError(t, service.WriteExport(t.Context(), empty, &output))
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)
	var complete exportComplete
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &complete))
	assert.Zero(t, complete.ExportedEvents)
	assert.Equal(t, hex.EncodeToString(sha256.New().Sum(nil)), complete.ContentSHA256)

	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	event, err := service.Append(t.Context(), EventInput{TenantID: testID("tenant"), EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	filtered, err := service.PrepareExport(t.Context(), Filter{
		TenantID: testID("tenant"), EventType: "missing", From: time.Unix(99, 0), To: time.Unix(101, 0),
	})
	require.NoError(t, err)
	output.Reset()
	require.NoError(t, service.WriteExport(t.Context(), filtered, &output))
	lines = strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)
	var manifest exportManifest
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &manifest))
	assert.Equal(t, "1970-01-01T00:01:39Z", manifest.Filter.From)
	assert.Equal(t, "1970-01-01T00:01:41Z", manifest.Filter.To)

	full, err := service.PrepareExport(t.Context(), Filter{TenantID: testID("tenant")})
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `DROP TRIGGER trg_iam_audit_events_no_update`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE iam_audit_events SET event_hash = 'tampered' WHERE id = ?`, event.ID)
	require.NoError(t, err)
	output.Reset()
	require.ErrorIs(t, service.WriteExport(t.Context(), full, &output), ErrIntegrity)
	assert.NotContains(t, output.String(), `"type":"complete"`)
}

func TestAuditExportWriterAndEncodingFailures(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	snapshot, err := service.PrepareExport(t.Context(), Filter{TenantID: testID("tenant")})
	require.NoError(t, err)
	closed, err := os.CreateTemp(t.TempDir(), "audit-export-*.ndjson")
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	require.ErrorContains(t, service.WriteExport(t.Context(), snapshot, closed), "write audit export manifest")
	require.Error(t, writeJSONLine(os.Stdout, make(chan int)))
}

func TestAuditLegacyEventsAndBrokenChains(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db, newTestIDGenerator())
	first, err := service.Append(t.Context(), EventInput{TenantID: testID("tenant-a"), EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant-b"), EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_audit_events (id, tenant_id, principal_id, target_id, event_type, outcome, detail, created_at, sequence, previous_hash, event_hash) VALUES (?, ?, 0, 0, 'legacy_event', 'success', '{}', 1, 0, '', '')`, testID("legacy"), testID("tenant-c"))
	require.NoError(t, err)
	events, total, err := service.List(t.Context(), Filter{TenantID: testID("tenant-c"), Offset: 0, Limit: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Zero(t, events[0].Sequence)
	integrity, err := service.Verify(t.Context(), testID("tenant-c"))
	require.NoError(t, err)
	require.True(t, integrity.Valid)
	require.Zero(t, integrity.VerifiedEvents)

	_, err = db.ExecContext(t.Context(), `DROP TRIGGER trg_iam_audit_events_no_update`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE iam_audit_events SET event_hash = 'broken' WHERE id = ?`, first.ID)
	require.NoError(t, err)
	integrity, err = service.Verify(t.Context(), testID("tenant-a"))
	require.NoError(t, err)
	require.False(t, integrity.Valid)
	_, err = service.PrepareExport(t.Context(), Filter{TenantID: testID("tenant-a")})
	require.ErrorIs(t, err, ErrIntegrity)

	_, err = db.ExecContext(t.Context(), `UPDATE iam_audit_heads SET event_hash = 'broken' WHERE tenant_id = ?`, testID("tenant-b"))
	require.NoError(t, err)
	integrity, err = service.Verify(t.Context(), testID("tenant-b"))
	require.NoError(t, err)
	require.False(t, integrity.Valid)
}

func TestAuditFailsClosedWithoutSchema(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := NewService(db, newTestIDGenerator())
	_, err = service.Append(t.Context(), EventInput{TenantID: testID("tenant"), EventType: "event", Outcome: "success"})
	require.ErrorContains(t, err, "ensure audit head")
	_, _, err = service.List(t.Context(), Filter{TenantID: testID("tenant"), Limit: 50})
	require.ErrorContains(t, err, "count audit events")
	_, err = service.Get(t.Context(), testID("tenant"), testID("event"))
	require.Error(t, err)
	_, err = service.Verify(t.Context(), testID("tenant"))
	require.Error(t, err)
}

func assertInvalidEvents(t *testing.T) {
	t.Helper()
	for _, input := range []EventInput{
		{TenantID: testID("tenant"), EventType: "", Outcome: "success"},
		{TenantID: testID("tenant"), EventType: strings.Repeat("e", 65), Outcome: "success"},
		{TenantID: testID("tenant"), EventType: "event", Outcome: "unknown"},
		{TenantID: testID("tenant"), EventType: "event", TargetType: strings.Repeat("x", 65), Outcome: "success"},
		{TenantID: testID("tenant"), EventType: "event", IPAddress: strings.Repeat("i", 65), Outcome: "success"},
		{TenantID: testID("tenant"), EventType: "event", UserAgent: strings.Repeat("u", 513), Outcome: "success"},
	} {
		require.ErrorIs(t, validate(input), ErrInvalidEvent)
	}
}
