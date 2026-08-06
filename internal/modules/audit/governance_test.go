package audit

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetentionPolicyDefaultAndUpsert(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db)

	policy, err := service.GetRetentionPolicy(t.Context(), "tenant-ret")
	require.NoError(t, err)
	assert.Equal(t, "tenant-ret", policy.TenantID)
	assert.Equal(t, defaultMinRetentionDays, policy.MinDays)
	assert.Equal(t, defaultArchiveAfterDays, policy.ArchiveAfterDays)

	policy, err = service.SetRetentionPolicy(t.Context(), "tenant-ret", 90, 180)
	require.NoError(t, err)
	assert.Equal(t, 90, policy.MinDays)
	assert.Equal(t, 180, policy.ArchiveAfterDays)
	assert.False(t, policy.UpdatedAt.IsZero())

	again, err := service.GetRetentionPolicy(t.Context(), "tenant-ret")
	require.NoError(t, err)
	assert.Equal(t, policy, again, "the upserted policy must round-trip")

	updated, err := service.SetRetentionPolicy(t.Context(), "tenant-ret", 30, 365)
	require.NoError(t, err)
	assert.Equal(t, 30, updated.MinDays)
	assert.Equal(t, 365, updated.ArchiveAfterDays)

	other, err := service.GetRetentionPolicy(t.Context(), "tenant-other")
	require.NoError(t, err)
	assert.Equal(t, defaultMinRetentionDays, other.MinDays, "another tenant must keep the platform default")
}

func TestSetRetentionPolicyValidation(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db)

	_, err = service.SetRetentionPolicy(t.Context(), "tenant", 0, 10)
	assert.ErrorIs(t, err, ErrRetentionInvalid)
	_, err = service.SetRetentionPolicy(t.Context(), "tenant", 40000, 40001)
	assert.ErrorIs(t, err, ErrRetentionInvalid)
	_, err = service.SetRetentionPolicy(t.Context(), "tenant", 365, 30)
	assert.ErrorIs(t, err, ErrRetentionInvalid, "an archive threshold before the minimum retention must be refused")
	_, err = service.GetRetentionPolicy(t.Context(), "")
	assert.ErrorIs(t, err, ErrRetentionInvalid)
}

func TestGovernanceReportsRetentionAndStats(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewService(db)
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-gov", EventType: "created", Outcome: "success"})
		require.NoError(t, err)
	}
	_, err = service.SetRetentionPolicy(t.Context(), "tenant-gov", 30, 90)
	require.NoError(t, err)

	report, err := service.Governance(t.Context(), "tenant-gov")
	require.NoError(t, err)
	assert.Equal(t, int64(3), report.TotalEvents)
	assert.Equal(t, int64(0), report.ArchiveReady)
	assert.Zero(t, report.Anchored)
	assert.Nil(t, report.Anchors)
	assert.True(t, report.Integrity.Valid)
	assert.False(t, report.Integrity.Anchor.Enabled)

	service.now = func() time.Time { return now.AddDate(0, 0, 120) }
	report, err = service.Governance(t.Context(), "tenant-gov")
	require.NoError(t, err)
	assert.Equal(t, int64(3), report.ArchiveReady, "events past the archive threshold must be counted")
}

func testSignerSeed(t *testing.T) string {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(seed)
}

func TestSignedRootRealWORMLifecycle(t *testing.T) {
	endpoint, _ := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-signed-roots",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	signer, err := NewRootSigner(testSignerSeed(t))
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchorAndSigner(db, store, signer)

	for i := 0; i < 3; i++ {
		_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-signed", EventType: "created", Outcome: "success"})
		require.NoError(t, err)
	}
	root, err := service.SignRoot(t.Context(), "tenant-signed")
	require.NoError(t, err)
	assert.Equal(t, int64(3), root.HeadSequence)
	assert.NotEmpty(t, root.SigningKeyID)
	assert.NotEmpty(t, root.RootPublicKey)
	assert.NotEmpty(t, root.RootSignature)
	assert.Equal(t, signer.keyID, root.SigningKeyID)
	assert.True(t, VerifyRootSignature(root), "the returned anchor must carry a valid root signature")

	integrity, err := service.Verify(t.Context(), "tenant-signed")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.True(t, integrity.Anchor.Valid)
	assert.True(t, integrity.Anchor.Signed)
	assert.True(t, integrity.Anchor.SignatureValid)

	again, err := service.SignRoot(t.Context(), "tenant-signed")
	require.NoError(t, err)
	assert.Equal(t, root, again, "signing an already signed root must be idempotent")

	report, err := service.Governance(t.Context(), "tenant-signed")
	require.NoError(t, err)
	assert.Equal(t, int64(3), report.Anchored)
	require.Len(t, report.Anchors, 1)
	assert.True(t, VerifyRootSignature(report.Anchors[0]), "governance must expose the signed anchor chain")
}

func TestSignedRootRejectsTamperedAnchor(t *testing.T) {
	endpoint, _ := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-signed-tamper",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	signer, err := NewRootSigner(testSignerSeed(t))
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchorAndSigner(db, store, signer)
	_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-tamper", EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	root, err := service.SignRoot(t.Context(), "tenant-tamper")
	require.NoError(t, err)
	require.True(t, VerifyRootSignature(root))

	tampered := root
	tampered.AnchorHash = strings.Repeat("0", 64)
	assert.False(t, VerifyRootSignature(tampered), "a changed anchor hash must invalidate the signature")

	rekeyed := root
	otherSigner, err := NewRootSigner(testSignerSeed(t))
	require.NoError(t, err)
	rekeyed.RootPublicKey = base64.StdEncoding.EncodeToString(otherSigner.privateKey.Public().(ed25519.PublicKey))
	rekeyed.SigningKeyID = otherSigner.keyID
	assert.False(t, VerifyRootSignature(rekeyed), "swapping the public key without re-signing must invalidate the signature")

	corrupted := root
	corrupted.RootSignature = root.RootSignature[:len(root.RootSignature)-4] + "AAAA"
	assert.False(t, VerifyRootSignature(corrupted), "corrupted signature bytes must invalidate the signature")
}

func TestSignRootWithoutSignerFailsClosed(t *testing.T) {
	endpoint, _ := startTestMinIO(t)
	store, err := NewAnchorStore(AnchorConfig{
		Enabled: true, Endpoint: endpoint, Bucket: "audit-unsigned-roots",
		AccessKey: minioTestUser, SecretKey: minioTestPassword, RetentionDays: 30,
	})
	require.NoError(t, err)
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	service := NewServiceWithAnchor(db, store)
	_, err = service.Append(t.Context(), EventInput{TenantID: "tenant-unsigned", EventType: "created", Outcome: "success"})
	require.NoError(t, err)
	anchor, err := service.Anchor(t.Context(), "tenant-unsigned")
	require.NoError(t, err)
	assert.Empty(t, anchor.RootSignature)

	_, err = service.SignRoot(t.Context(), "tenant-unsigned")
	assert.ErrorIs(t, err, ErrRootSigningDisabled, "an anchor committed before signing was enabled must fail closed")

	integrity, err := service.Verify(t.Context(), "tenant-unsigned")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.True(t, integrity.Anchor.Valid)
	assert.False(t, integrity.Anchor.Signed, "unsigned anchors must be reported as unsigned")
}
