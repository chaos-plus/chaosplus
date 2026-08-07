package audit

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	minioTestUser     = "chaosplus-minio"        //nolint:gosec // local-only MinIO root used inside tests
	minioTestPassword = "chaosplus-minio-secret" //nolint:gosec // local-only MinIO root used inside tests
)

func TestRootSignerRoundTrip(t *testing.T) {
	signer, err := NewRootSigner(base64.StdEncoding.EncodeToString(make([]byte, ed25519.SeedSize)))
	require.NoError(t, err)
	assert.Len(t, signer.keyID, 16)

	anchor := Anchor{Schema: anchorSchema, TenantID: "tenant", HeadSequence: 1, HeadHash: strings.Repeat("a", 64), AnchoredAt: time.Now().UTC()}
	anchor.AnchorHash = computeAnchorHash(anchor)
	require.NoError(t, signer.Sign(&anchor))
	assert.True(t, VerifyRootSignature(anchor))

	changed := anchor
	changed.AnchorHash = strings.Repeat("0", 64)
	assert.False(t, VerifyRootSignature(changed), "the signature must commit the anchor hash")

	unsigned := Anchor{Schema: anchorSchema, TenantID: "tenant", HeadSequence: 1, HeadHash: strings.Repeat("c", 64)}
	unsigned.AnchorHash = computeAnchorHash(unsigned)
	assert.False(t, VerifyRootSignature(unsigned), "unsigned anchors must not verify as signed")

	other, err := NewRootSigner(testSignerSeed(t))
	require.NoError(t, err)
	rekeyed := anchor
	rekeyed.RootPublicKey = base64.StdEncoding.EncodeToString(other.privateKey.Public().(ed25519.PublicKey))
	rekeyed.SigningKeyID = other.keyID
	assert.False(t, VerifyRootSignature(rekeyed), "a replaced public key without a matching signature must fail")

	corrupted := anchor
	corrupted.RootSignature = anchor.RootSignature[:len(anchor.RootSignature)-4] + "AAAA"
	assert.False(t, VerifyRootSignature(corrupted))

	_, err = NewRootSigner("not-base64")
	assert.ErrorIs(t, err, ErrInvalidSigningKey)
	_, err = NewRootSigner(base64.StdEncoding.EncodeToString([]byte("short")))
	assert.ErrorIs(t, err, ErrInvalidSigningKey)
	_, err = NewRootSigner("")
	assert.ErrorIs(t, err, ErrInvalidSigningKey)
}

func TestAnchorHashIsCanonical(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	anchor := Anchor{
		Schema: anchorSchema, TenantID: "tenant-a", HeadSequence: 7, HeadHash: strings.Repeat("a", 64),
		AnchoredAt: now, PreviousAnchorHash: strings.Repeat("b", 64),
	}
	anchor.AnchorHash = computeAnchorHash(anchor)
	require.NotEmpty(t, anchor.AnchorHash)
	assert.Len(t, anchor.AnchorHash, 64)
	assert.Equal(t, anchor.AnchorHash, computeAnchorHash(anchor), "hash must be deterministic")

	changed := anchor
	changed.HeadHash = strings.Repeat("c", 64)
	assert.NotEqual(t, anchor.AnchorHash, computeAnchorHash(changed), "head hash must be committed")

	reordered := anchor
	reordered.PreviousAnchorHash = ""
	assert.NotEqual(t, anchor.AnchorHash, computeAnchorHash(reordered), "previous anchor hash must be committed")
}

func TestAnchorStoreRejectsMissingConfiguration(t *testing.T) {
	_, err := NewAnchorStore(AnchorConfig{Enabled: true, Endpoint: "http://127.0.0.1:9000"})
	assert.ErrorIs(t, err, ErrAnchorMisconfigured)
	_, err = NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: "http://127.0.0.1:9000", Bucket: "audit",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	assert.NoError(t, err)
	_, err = NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: "https://s3.example.com", Bucket: "audit",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	assert.NoError(t, err, "https scheme must be accepted without dialing")
	_, err = NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: "http://", Bucket: "audit",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	assert.Error(t, err, "an endpoint without a host must be rejected")
	_, err = NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: "http://-bad.example.com", Bucket: "audit",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	assert.Error(t, err, "an endpoint that fails the S3 client's host validation must be rejected")
}

func TestAnchorStoreRealObjectLock(t *testing.T) {
	endpoint, client := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-anchor-lock", Region: "",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	anchor := Anchor{
		Schema: anchorSchema, TenantID: "tenant-lock", HeadSequence: 1,
		HeadHash: strings.Repeat("a", 64), AnchoredAt: time.Now().UTC(),
	}
	anchor.AnchorHash = computeAnchorHash(anchor)
	require.NoError(t, store.Put(t.Context(), anchor))

	key := store.objectKey("tenant-lock", 1)
	info, err := client.StatObject(t.Context(), "audit-anchor-lock", key, minio.StatObjectOptions{})
	require.NoError(t, err)
	assert.Greater(t, info.Size, int64(100), "anchor payload is the full JSON record")
	mode, until, err := client.GetObjectRetention(t.Context(), "audit-anchor-lock", key, "")
	require.NoError(t, err)
	require.NotNil(t, mode)
	require.NotNil(t, until)
	assert.Equal(t, minio.Compliance, *mode, "anchors must use compliance retention")
	assert.True(t, until.After(time.Now().Add(29*24*time.Hour)), "retention must cover the configured window")

	anchors, err := store.List(t.Context(), "tenant-lock")
	require.NoError(t, err)
	require.Len(t, anchors, 1)
	assert.Equal(t, anchor.AnchorHash, anchors[0].AnchorHash)

	var versionID string
	for object := range client.ListObjects(t.Context(), "audit-anchor-lock", minio.ListObjectsOptions{Prefix: key, Recursive: true, WithVersions: true}) {
		require.NoError(t, object.Err)
		versionID = object.VersionID
	}
	require.NotEmpty(t, versionID)
	err = client.RemoveObject(t.Context(), "audit-anchor-lock", key, minio.RemoveObjectOptions{VersionID: versionID})
	require.Error(t, err, "compliance-locked anchor versions must refuse deletion")

	err = store.Put(t.Context(), anchor)
	assert.ErrorIs(t, err, ErrAnchorAlreadyExists, "anchors are write-once")
}

func TestAuditAnchoringRealWORMLifecycle(t *testing.T) {
	endpoint, _ := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-worm-lifecycle",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)

	for i := 0; i < 3; i++ {
		_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-worm", EventType: "created", TargetType: "client", TargetID: "c", Outcome: "success"})
		require.NoError(t, err)
	}
	first, err := service.Anchor(t.Context(), "tenant-worm")
	require.NoError(t, err)
	assert.Equal(t, int64(3), first.HeadSequence)
	assert.Equal(t, "", first.PreviousAnchorHash)
	assert.Equal(t, first.AnchorHash, computeAnchorHash(first))

	same, err := service.Anchor(t.Context(), "tenant-worm")
	require.NoError(t, err)
	assert.Equal(t, first, same, "anchoring an already anchored head is idempotent")

	integrity, err := service.Verify(t.Context(), "tenant-worm")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.True(t, integrity.Anchor.Enabled)
	assert.True(t, integrity.Anchor.Valid)
	assert.Equal(t, int64(3), integrity.Anchor.Sequence)
	assert.Equal(t, first.AnchorHash, integrity.Anchor.Hash)

	for i := 0; i < 2; i++ {
		_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-worm", EventType: "updated", TargetType: "client", TargetID: "c", Outcome: "success"})
		require.NoError(t, err)
	}
	second, err := service.Anchor(t.Context(), "tenant-worm")
	require.NoError(t, err)
	assert.Equal(t, int64(5), second.HeadSequence)
	assert.Equal(t, first.AnchorHash, second.PreviousAnchorHash, "anchors must link into a chain")
	integrity, err = service.Verify(t.Context(), "tenant-worm")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.True(t, integrity.Anchor.Valid)
	assert.Equal(t, int64(5), integrity.Anchor.Sequence)

	anchors, err := store.List(t.Context(), "tenant-worm")
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 5}, []int64{anchors[0].HeadSequence, anchors[1].HeadSequence})

	secondStore, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-worm-lifecycle",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	anchors, err = secondStore.List(t.Context(), "tenant-worm")
	require.NoError(t, err, "a pre-existing locked bucket must be accepted")
	require.Len(t, anchors, 2)
}

func TestAuditAnchorStoreUnavailable(t *testing.T) {
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: "127.0.0.1:1", Bucket: "audit-dead",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)
	_, err = service.Append(t.Context(), EventInput{TenantID: "tenant", EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	_, err = service.Anchor(t.Context(), "tenant")
	assert.ErrorIs(t, err, ErrAnchorUnavailable)
	_, err = service.Verify(t.Context(), "tenant")
	assert.ErrorIs(t, err, ErrAnchorUnavailable)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := authz.TenantHeader + ": tenant"
	response := api.Post("/iam/audit-anchor", header)
	assert.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "audit_anchor_unavailable")
}

func TestAuditAnchorRequiresObjectLocking(t *testing.T) {
	endpoint, client := startTestMinIO(t)
	require.NoError(t, client.MakeBucket(t.Context(), "audit-plain", minio.MakeBucketOptions{}))
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-plain",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	err = store.Put(t.Context(), Anchor{Schema: anchorSchema, TenantID: "tenant-plain", HeadSequence: 1, HeadHash: strings.Repeat("a", 64), AnchoredAt: time.Now().UTC()})
	assert.Error(t, err, "a non-locked bucket must reject retained writes")
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)
	_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-lock", EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	_, err = service.Anchor(t.Context(), "tenant-lock")
	assert.ErrorIs(t, err, ErrAnchorUnavailable, "anchoring into a non-locked bucket must fail closed")
}

func TestAuditAnchorListSkipsForeignObjectsAndFailsClosedOnCorruption(t *testing.T) {
	endpoint, client := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-foreign",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)
	_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-foreign", EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	require.NoError(t, store.ensureBucket(t.Context()), "bucket must exist before raw foreign objects are written")

	_, err = client.PutObject(t.Context(), "audit-foreign", store.prefix+"/tenant-foreign/note.txt", strings.NewReader("not an anchor"), int64(len("not an anchor")), minio.PutObjectOptions{})
	require.NoError(t, err)
	_, err = client.PutObject(t.Context(), "audit-foreign", store.prefix+"/tenant-foreign/meta.json", strings.NewReader(`{"schema":"other"}`), int64(len(`{"schema":"other"}`)), minio.PutObjectOptions{})
	require.NoError(t, err)
	_, err = client.PutObject(t.Context(), "audit-foreign", store.prefix+"/tenant-foreign/999.json", strings.NewReader(`{"schema":"other"}`), int64(len(`{"schema":"other"}`)), minio.PutObjectOptions{})
	require.NoError(t, err)

	integrity, err := service.Verify(t.Context(), "tenant-foreign")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.True(t, integrity.Anchor.Valid, "foreign objects are skipped, not counted as anchors")

	_, err = client.PutObject(t.Context(), "audit-foreign", store.prefix+"/tenant-foreign/2.json", strings.NewReader("{broken json"), int64(len("{broken json")), minio.PutObjectOptions{})
	require.NoError(t, err)
	_, err = service.Verify(t.Context(), "tenant-foreign")
	assert.ErrorIs(t, err, ErrAnchorUnavailable, "a corrupted anchor object must fail closed")
}

func TestAuditAnchorDetectsLocalRollback(t *testing.T) {
	endpoint, _ := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-worm-rollback",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)
	for i := 0; i < 5; i++ {
		_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-rollback", EventType: "created", Outcome: "success"})
		require.NoError(t, err)
	}
	_, err = service.Anchor(t.Context(), "tenant-rollback")
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(), `DROP TRIGGER trg_iam_audit_events_no_delete`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `DROP TRIGGER trg_iam_audit_events_no_update`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `DELETE FROM iam_audit_events WHERE tenant_id = 'tenant-rollback' AND sequence > 3`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE iam_audit_heads SET sequence = 3, event_hash = (SELECT event_hash FROM iam_audit_events WHERE tenant_id = 'tenant-rollback' AND sequence = 3) WHERE tenant_id = 'tenant-rollback'`)
	require.NoError(t, err)

	_, err = service.Anchor(t.Context(), "tenant-rollback")
	assert.ErrorIs(t, err, ErrIntegrity, "a local chain rolled back behind its anchors must not be re-anchored")
	integrity, err := service.Verify(t.Context(), "tenant-rollback")
	require.NoError(t, err)
	assert.False(t, integrity.Anchor.Valid, "the rolled-back chain must no longer match its anchors")
}

func TestAuditAnchorDetectsDatabaseTampering(t *testing.T) {
	endpoint, _ := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-worm-tamper",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)
	event, err := service.Append(t.Context(), EventInput{TenantID: "tenant-tamper", EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	_, err = service.Anchor(t.Context(), "tenant-tamper")
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(), `DROP TRIGGER trg_iam_audit_events_no_update`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE iam_audit_events SET event_hash = 'tampered' WHERE id = ?`, event.ID)
	require.NoError(t, err)

	integrity, err := service.Verify(t.Context(), "tenant-tamper")
	require.NoError(t, err)
	assert.False(t, integrity.Valid, "tampered events must fail chain verification")
	assert.False(t, integrity.Anchor.Valid, "tampered events must fail anchor verification")
	_, err = service.Anchor(t.Context(), "tenant-tamper")
	assert.ErrorIs(t, err, ErrIntegrity, "tampered chains must not be re-anchored")
}

func TestAuditAnchorDetectsSubstitutedAnchor(t *testing.T) {
	endpoint, client := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-worm-substitution",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)
	for i := 0; i < 2; i++ {
		_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-sub", EventType: "created", Outcome: "success"})
		require.NoError(t, err)
	}
	anchored, err := service.Anchor(t.Context(), "tenant-sub")
	require.NoError(t, err)

	substituted := anchored
	substituted.HeadHash = strings.Repeat("0", 64)
	payload, err := json.Marshal(substituted)
	require.NoError(t, err)
	_, err = client.PutObject(t.Context(), "audit-worm-substitution", store.objectKey("tenant-sub", 2), strings.NewReader(string(payload)), int64(len(payload)), minio.PutObjectOptions{ContentType: "application/json"})
	require.NoError(t, err)

	integrity, err := service.Verify(t.Context(), "tenant-sub")
	require.NoError(t, err)
	assert.False(t, integrity.Anchor.Valid, "a substituted anchor must fail self-hash verification")
}

func TestAuditAnchorConflictObjectFailsClosed(t *testing.T) {
	endpoint, client := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-worm-conflict",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)
	_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-conflict", EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	require.NoError(t, store.ensureBucket(t.Context()), "bucket must exist before the conflicting object is written")
	_, err = client.PutObject(t.Context(), "audit-worm-conflict", store.objectKey("tenant-conflict", 1), strings.NewReader(`{"schema":"other"}`), int64(len(`{"schema":"other"}`)), minio.PutObjectOptions{})
	require.NoError(t, err)

	_, err = service.Anchor(t.Context(), "tenant-conflict")
	assert.ErrorIs(t, err, ErrAnchorUnavailable, "a non-anchor object squatting the anchor key must fail closed")
}

func TestAuditAnchorDisabledAndEmpty(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db)
	_, err = service.Anchor(t.Context(), "tenant")
	assert.ErrorIs(t, err, ErrAnchorDisabled)
	integrity, err := service.Verify(t.Context(), "tenant")
	require.NoError(t, err)
	assert.False(t, integrity.Anchor.Enabled)

	endpoint, _ := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-worm-empty",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	anchored := NewServiceWithAnchor(db, store)
	_, err = anchored.Anchor(t.Context(), "tenant-empty")
	assert.ErrorIs(t, err, ErrAnchorEmpty)
	integrity, err = anchored.Verify(t.Context(), "tenant-empty")
	require.NoError(t, err)
	assert.True(t, integrity.Anchor.Enabled)
	assert.True(t, integrity.Anchor.Valid, "no anchors yet is not an integrity failure")
	assert.NoError(t, anchorStoreError(nil), "nil store errors must pass through")

	anchoredService := NewServiceWithAnchor(db, store)
	_, err = anchoredService.Anchor(t.Context(), "")
	assert.ErrorIs(t, err, ErrInvalidFilter)
	_, err = anchoredService.Anchor(t.Context(), strings.Repeat("t", 129))
	assert.ErrorIs(t, err, ErrInvalidFilter)
}

func TestAuditAnchorAPIErrorMappings(t *testing.T) {
	mappings := []struct {
		err    error
		status int
		code   string
	}{
		{ErrAnchorDisabled, http.StatusServiceUnavailable, "audit_anchor_not_enabled"},
		{ErrAnchorEmpty, http.StatusUnprocessableEntity, "audit_anchor_empty"},
		{ErrAnchorAlreadyExists, http.StatusConflict, "audit_anchor_already_exists"},
		{ErrAnchorUnavailable, http.StatusServiceUnavailable, "audit_anchor_unavailable"},
		{ErrIntegrity, http.StatusConflict, "audit_integrity_failed"},
		{ErrInvalidEvent, http.StatusUnprocessableEntity, "invalid_audit_query"},
		{ErrInvalidFilter, http.StatusUnprocessableEntity, "invalid_audit_query"},
	}
	for _, mapping := range mappings {
		response := auditAPIError(mapping.err)
		status := 0
		var humaError huma.StatusError
		if errors.As(response, &humaError) {
			status = humaError.GetStatus()
		}
		assert.Equal(t, mapping.status, status, mapping.code)
		assert.Contains(t, response.Error(), mapping.code)
	}
}

func TestExportFilterAndValidationUnit(t *testing.T) {
	input := &exportInput{TenantID: "tenant"}
	_, err := exportFilter(input)
	require.NoError(t, err)
	input.From = "not-a-time"
	_, err = exportFilter(input)
	require.Error(t, err, "an invalid RFC3339 from must be rejected")
	input.From = ""
	input.To = "also-not-a-time"
	_, err = exportFilter(input)
	require.Error(t, err, "an invalid RFC3339 to must be rejected")

	assert.ErrorIs(t, validateFilter(Filter{TenantID: ""}, false), ErrInvalidFilter)
	assert.ErrorIs(t, validateFilter(Filter{TenantID: "tenant"}, true), ErrInvalidFilter, "pagination requires a positive limit")
	assert.ErrorIs(t, validateFilter(Filter{TenantID: "tenant", Limit: 50, Offset: -1}, true), ErrInvalidFilter)
}

func TestAuditAnchorHTTPContract(t *testing.T) {
	endpoint, _ := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-worm-api",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)
	_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-api", EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	header := authz.TenantHeader + ": tenant-api"

	response := api.Post("/iam/audit-anchor", header)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data Anchor `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, anchorSchema, body.Data.Schema)
	assert.Equal(t, int64(1), body.Data.HeadSequence)

	assert.Equal(t, http.StatusOK, api.Post("/iam/audit-anchor", header).Code, "idempotent re-anchor")
	integrity := api.Get("/iam/audit-integrity", header)
	require.Equal(t, http.StatusOK, integrity.Code, integrity.Body.String())
	assert.Contains(t, integrity.Body.String(), `"anchor":{"enabled":true`)

	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/iam/audit-anchor", authz.TenantHeader+": tenant-missing").Code)
}

func TestAuditAnchorHTTPDisabled(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	_, api := humatest.New(t)
	RegisterREST(api, NewService(db), authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	response := api.Post("/iam/audit-anchor", authz.TenantHeader+": tenant")
	assert.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "audit_anchor_not_enabled")
}

func TestAuditOpenAPIIncludesAnchorOperation(t *testing.T) {
	_, api := humatest.New(t)
	RegisterREST(api, nil, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	operation := api.OpenAPI().Paths["/iam/audit-anchor"].Post
	require.NotNil(t, operation)
	assert.Equal(t, "audit-anchor-current", operation.OperationID)
	assert.Equal(t, []string{"audit"}, operation.Tags)
	assert.Equal(t, "audit_event_anchor", operation.Extensions[authz.GuardExtensionKey])
	assert.Contains(t, operation.Responses, "503")
}

func startTestMinIO(t *testing.T) (string, *minio.Client) {
	t.Helper()
	if endpoint := os.Getenv("CHAOSPLUS_MINIO_ENDPOINT"); endpoint != "" {
		return endpoint, newRawClient(t, endpoint)
	}
	binary := os.Getenv("CHAOSPLUS_MINIO_BINARY")
	if binary == "" {
		_, file, _, _ := runtime.Caller(0)
		candidates := []string{
			filepath.Join(filepath.Dir(file), "..", "..", "..", ".local", "minio", "minio.exe"),
			"minio",
		}
		for _, candidate := range candidates {
			if path, err := exec.LookPath(candidate); err == nil {
				binary = path
				break
			}
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				binary = candidate
				break
			}
		}
	}
	if binary == "" {
		t.Skip("real MinIO binary not available; set CHAOSPLUS_MINIO_BINARY or CHAOSPLUS_MINIO_ENDPOINT")
	}
	ports := freePorts(t, 2)
	port, consolePort := ports[0], ports[1]
	dataDir := t.TempDir()
	command := exec.Command(binary, "server", dataDir, "--address", "127.0.0.1:"+port, "--console-address", "127.0.0.1:"+consolePort)
	command.Env = append(os.Environ(), "MINIO_ROOT_USER="+minioTestUser, "MINIO_ROOT_PASSWORD="+minioTestPassword)
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})
	endpoint := "127.0.0.1:" + port
	deadline := time.Now().Add(20 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", endpoint, 500*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("MinIO did not become ready on %s: %v", endpoint, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	client := newRawClient(t, endpoint)
	deadline = time.Now().Add(15 * time.Second)
	for {
		_, err := client.BucketExists(t.Context(), "probe")
		if err == nil {
			return endpoint, client
		}
		if time.Now().After(deadline) {
			t.Fatalf("MinIO S3 API did not become ready: %v", err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func newRawClient(t *testing.T, endpoint string) *minio.Client {
	t.Helper()
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioTestUser, minioTestPassword, ""),
		Secure: strings.HasPrefix(strings.ToLower(endpoint), "https://"),
	})
	require.NoError(t, err)
	return client
}

// freePorts reserves n distinct loopback ports. Every listener is held open
// until all ports have been chosen, which is what guarantees they differ.
//
// The previous helper derived MinIO's console port as "data port + 1" without
// ever checking it. Under a parallel `go test ./...` run the ephemeral ports
// handed out to sibling MinIO instances sit close together, so that guess
// regularly collided, MinIO exited during startup, and the anchor tests failed
// intermittently while passing in isolation.
func freePorts(t *testing.T, n int) []string {
	t.Helper()
	listeners := make([]net.Listener, 0, n)
	ports := make([]string, 0, n)
	for range n {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners = append(listeners, listener)
		ports = append(ports, strings.TrimPrefix(listener.Addr().String(), "127.0.0.1:"))
	}
	for _, listener := range listeners {
		require.NoError(t, listener.Close())
	}
	return ports
}
