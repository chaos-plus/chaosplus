package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestIDGenerator() func() (guid.ID, error) {
	var next atomic.Int64
	return func() (guid.ID, error) { return guid.ID(next.Add(1)), nil }
}

var claimsWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x11, 0x03, 0x60, 0x01, 0x7f, 0x01, 0x7f, 0x60, 0x02, 0x7f, 0x7f, 0x00, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
	0x03, 0x04, 0x03, 0x00, 0x01, 0x02,
	0x05, 0x03, 0x01, 0x00, 0x01,
	0x07, 0x25, 0x04, 0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, 0x05, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x00,
	0x07, 0x64, 0x65, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x01, 0x06, 0x63, 0x6c, 0x61, 0x69, 0x6d, 0x73, 0x00, 0x02,
	0x0a, 0x0f, 0x03, 0x05, 0x00, 0x41, 0x80, 0x08, 0x0b, 0x02, 0x00, 0x0b, 0x04, 0x00, 0x42, 0x0f, 0x0b,
	0x0b, 0x15, 0x01, 0x00, 0x41, 0x00, 0x0b, 0x0f, 0x7b, 0x22, 0x72, 0x65, 0x67, 0x69, 0x6f, 0x6e, 0x22, 0x3a, 0x22, 0x75, 0x73, 0x22, 0x7d,
}

func TestManifestSignatureAndLimits(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	manifest := Manifest{
		Name: "crm_claims", Version: "1.0.0", ABI: claimsABI, Hook: claimsHook,
		Module: "claims.wasm", SHA256: strings.Repeat("a", 64), KeyID: "release-2026",
	}
	manifest.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestPayload(manifest)))
	config := Config{TrustedKeys: map[string]string{"release-2026": base64.RawStdEncoding.EncodeToString(publicKey)}}
	assert.NoError(t, validateManifest(manifest, config))

	manifest.Version = "1.0.1"
	assert.ErrorContains(t, validateManifest(manifest, config), "signature")
	manifest.Signature = ""
	assert.ErrorContains(t, validateManifest(manifest, config), "unsigned")
	config.AllowUnsigned = true
	assert.NoError(t, validateManifest(manifest, config))
}

func TestClaimOutputValidation(t *testing.T) {
	assert.NoError(t, validateClaims(map[string]any{
		"region": "us-west", "level": float64(3), "features": []any{"orders", "billing"},
	}))
	assert.Error(t, validateClaims(map[string]any{"organization_id": map[string]any{"override": true}}))
	assert.Error(t, validateClaims(map[string]any{"Bad-Key": true}))
}

func TestPluginModulePathConfinement(t *testing.T) {
	base := t.TempDir()
	resolved, err := confinedModulePath(base, "modules/claims.wasm")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(resolved, base))
	_, err = confinedModulePath(base, "../escape.wasm")
	assert.Error(t, err)
	_, err = confinedModulePath(base, filepath.Join(base, "claims.wasm"))
	assert.ErrorContains(t, err, "must be relative")
}

func TestSignedWASMClaimsEnrichRealToken(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	directory := t.TempDir()
	modulePath := filepath.Join(directory, "claims.wasm")
	require.NoError(t, os.WriteFile(modulePath, claimsWASM, 0o600))
	digest := sha256.Sum256(claimsWASM)
	manifest := Manifest{
		Name: "crm_claims", Version: "1.0.0", ABI: claimsABI, Hook: claimsHook,
		Module: filepath.Base(modulePath), SHA256: hex.EncodeToString(digest[:]), KeyID: "release", FailureMode: failureDeny,
	}
	manifest.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestPayload(manifest)))
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestPath := filepath.Join(directory, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, manifestData, 0o600))
	chain, err := LoadClaims(Config{
		Enabled: true, Manifests: []string{manifestPath},
		TrustedKeys: map[string]string{"release": base64.RawStdEncoding.EncodeToString(publicKey)},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = chain.Close() })

	claims, err := chain.Enrich(context.Background(), authnmod.ClaimContext{TokenType: "access_token", Subject: "alice", TenantID: "tenant"})
	require.NoError(t, err)
	assert.Equal(t, "us", claims["crm_claims"].(map[string]any)["region"])

	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	seed := make([]byte, ed25519.SeedSize)
	issuer, err := authnmod.NewWebService(authnext.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"}, SigningKey: base64.RawStdEncoding.EncodeToString(seed), AccessTokenTTL: time.Hour,
	}, db, authnmod.WithClaimEnricher(chain), authnmod.WithIDGenerator(newTestIDGenerator()))
	require.NoError(t, err)
	token, _, err := issuer.IssueTenantSubjectToken(context.Background(), "alice", "tenant", "api", "orders.read", "Alice", "")
	require.NoError(t, err)
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var tokenClaims map[string]any
	require.NoError(t, json.Unmarshal(payload, &tokenClaims))
	extensions := tokenClaims["ext"].(map[string]any)
	assert.Equal(t, "us", extensions["crm_claims"].(map[string]any)["region"])
}

func TestPluginConfigurationAndManifestValidation(t *testing.T) {
	chain, err := LoadClaims(Config{})
	require.NoError(t, err)
	assert.Nil(t, chain)
	_, err = LoadClaims(Config{Enabled: true})
	assert.ErrorContains(t, err, "no manifests")

	base := Manifest{Name: "claims", Version: "1.0.0", ABI: claimsABI, Hook: claimsHook, Module: "claims.wasm", SHA256: strings.Repeat("a", 64)}
	config := Config{AllowUnsigned: true}
	for name, mutate := range map[string]func(*Manifest){
		"name":    func(value *Manifest) { value.Name = "Invalid-Name" },
		"version": func(value *Manifest) { value.Version = "" },
		"abi":     func(value *Manifest) { value.ABI++ },
		"hook":    func(value *Manifest) { value.Hook = "other" },
		"module":  func(value *Manifest) { value.Module = "" },
		"digest":  func(value *Manifest) { value.SHA256 = "short" },
		"failure": func(value *Manifest) { value.FailureMode = "continue" },
		"timeout": func(value *Manifest) { value.TimeoutMS = int(maxTimeout.Milliseconds()) + 1 },
		"memory":  func(value *Manifest) { value.MemoryPages = maxMemory + 1 },
		"output":  func(value *Manifest) { value.MaxOutputBytes = defaultOutput + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			manifest := base
			mutate(&manifest)
			assert.Error(t, validateManifest(manifest, config))
		})
	}
	assert.ErrorContains(t, validateManifest(base, Config{}), "unsigned")
	base.Signature = base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	base.KeyID = "unknown"
	assert.ErrorContains(t, validateManifest(base, Config{TrustedKeys: map[string]string{}}), "untrusted")
	assert.ErrorContains(t, validateManifest(base, Config{TrustedKeys: map[string]string{"unknown": "invalid"}}), "public key")
}

func TestPluginLoadingFailuresAndFailureModes(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing.json")
	_, err := LoadClaims(Config{Enabled: true, Manifests: []string{missing}, AllowUnsigned: true})
	assert.ErrorContains(t, err, "read plugin manifest")

	manifestPath := filepath.Join(directory, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`{"unknown":true}`), 0o600))
	_, err = LoadClaims(Config{Enabled: true, Manifests: []string{manifestPath}, AllowUnsigned: true})
	assert.ErrorContains(t, err, "unknown field")

	modulePath := filepath.Join(directory, "claims.wasm")
	require.NoError(t, os.WriteFile(modulePath, claimsWASM, 0o600))
	digest := sha256.Sum256(claimsWASM)
	manifest := Manifest{
		Name: "claims", Version: "1.0.0", ABI: claimsABI, Hook: claimsHook,
		Module: "claims.wasm", SHA256: hex.EncodeToString(digest[:]), FailureMode: failureIgnore,
	}
	writeManifest := func(value Manifest) {
		data, marshalErr := json.Marshal(value)
		require.NoError(t, marshalErr)
		require.NoError(t, os.WriteFile(manifestPath, data, 0o600))
	}

	badDigest := manifest
	badDigest.SHA256 = strings.Repeat("0", 64)
	writeManifest(badDigest)
	_, err = LoadClaims(Config{Enabled: true, Manifests: []string{manifestPath}, AllowUnsigned: true})
	assert.ErrorContains(t, err, "digest mismatch")

	writeManifest(manifest)
	chain, err := LoadClaims(Config{Enabled: true, Manifests: []string{manifestPath}, AllowUnsigned: true})
	require.NoError(t, err)
	chain.plugins[0].manifest.MaxOutputBytes = 2
	chain.plugins[0].manifest.TimeoutMS = 10
	claims, err := chain.Enrich(t.Context(), authnmod.ClaimContext{Subject: "alice"})
	require.NoError(t, err)
	assert.Empty(t, claims)
	chain.plugins[0].manifest.FailureMode = failureDeny
	_, err = chain.Enrich(t.Context(), authnmod.ClaimContext{Subject: "alice"})
	assert.ErrorContains(t, err, `claim plugin "claims"`)
	require.NoError(t, chain.Stop(t.Context()))
}

func TestClaimValueLimits(t *testing.T) {
	claims := make(map[string]any, 33)
	for index := range 33 {
		claims[fmt.Sprintf("claim_%d", index)] = true
	}
	assert.ErrorContains(t, validateClaims(claims), "more than 32")
	assert.True(t, claimValue(nil))
	assert.True(t, claimValue([]any{"value", float64(1), true, nil}))
	assert.False(t, claimValue([]any{map[string]any{"nested": true}}))
	assert.False(t, claimValue(make([]any, 65)))
	assert.False(t, claimValue(map[string]any{}))
	decoded, err := decodeBase64(base64.StdEncoding.EncodeToString([]byte("value")))
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), decoded)
	assert.Equal(t, failureDeny, (&claimPlugin{}).failureMode())
}

func TestPluginModuleLoadingFailures(t *testing.T) {
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "manifest.json")

	missing := Manifest{Name: "claims", Version: "1", ABI: claimsABI, Hook: claimsHook, Module: "missing.wasm", SHA256: strings.Repeat("0", 64)}
	data, err := json.Marshal(missing)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, data, 0o600))
	_, err = LoadClaims(Config{Enabled: true, Manifests: []string{manifestPath}, AllowUnsigned: true})
	assert.ErrorContains(t, err, "read plugin")

	module := []byte("not-wasm")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "claims.wasm"), module, 0o600))
	digest := sha256.Sum256(module)
	invalid := Manifest{Name: "claims", Version: "1", ABI: claimsABI, Hook: claimsHook, Module: "claims.wasm", SHA256: hex.EncodeToString(digest[:])}
	data, err = json.Marshal(invalid)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, data, 0o600))
	_, err = LoadClaims(Config{Enabled: true, Manifests: []string{manifestPath}, AllowUnsigned: true})
	assert.ErrorContains(t, err, "instantiate plugin")
}
