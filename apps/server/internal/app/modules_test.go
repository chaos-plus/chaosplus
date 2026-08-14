package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/federation"
	"github.com/chaos-plus/chaosplus/internal/modules/governance"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/chaos-plus/chaosplus/internal/modules/provisioning"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// seedGuardTenant builds a tenant whose only administrator is reachable through
// the returned role, which is what the access-review guards must protect.
func seedGuardTenant(t *testing.T, db *bun.DB) (*iam.Repository, iam.Role) {
	t.Helper()
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))
	repo := iam.NewRepository(db, newTestIDGenerator())
	// The guard only counts administrators that resolve to an active principal,
	// so the principal rows are part of a realistic tenant.
	for _, principalID := range []guid.ID{testID("root"), testID("backup")} {
		_, err := db.ExecContext(t.Context(), `INSERT INTO iam_principals
			(id, login_name, email, display_name, status, created_at, updated_at, disabled_at)
			VALUES (?, ?, ?, ?, 'active', ?, ?, 0)`,
			principalID, principalID, principalID.String()+"@example.test", principalID.String(), 1_700_000_000_000, 1_700_000_000_000)
		require.NoError(t, err)
	}
	_, err := repo.PutMember(t.Context(), iam.TenantMember{TenantID: testID("tenant"), PrincipalID: testID("root"), DisplayName: "Root", Status: iam.MemberActive})
	require.NoError(t, err)
	role, err := repo.CreateRole(t.Context(), testID("tenant"), "Administrators", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(t.Context(), testID("tenant"), role.ID, "tenant_administer")
	require.NoError(t, err)
	_, err = repo.AddMember(t.Context(), testID("tenant"), role.ID, testID("root"))
	require.NoError(t, err)
	return repo, role
}

func TestGuardGroupPositionRemoveProtectsLastAdministrator(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo, role := seedGuardTenant(t, db)

	// A removal that changes nothing is passed straight through.
	noop := guardGroupPositionRemove(iam.NewAdministratorGuard(), "sqlite",
		func(context.Context, bun.IDB, guid.ID, guid.ID, guid.ID) (bool, error) { return false, nil })
	changed, err := noop(t.Context(), db, testID("tenant"), testID("group"), testID("root"))
	require.NoError(t, err)
	assert.False(t, changed)

	// A removal that reports an error is propagated verbatim.
	failing := guardGroupPositionRemove(iam.NewAdministratorGuard(), "sqlite",
		func(context.Context, bun.IDB, guid.ID, guid.ID, guid.ID) (bool, error) {
			return false, errors.New("removal exploded")
		})
	_, err = failing(t.Context(), db, testID("tenant"), testID("group"), testID("root"))
	assert.ErrorContains(t, err, "removal exploded")

	// Removing the only path to tenant_administer is converted into the
	// review-specific refusal instead of silently stranding the tenant.
	stripping := guardGroupPositionRemove(iam.NewAdministratorGuard(), "sqlite",
		func(ctx context.Context, executor bun.IDB, tenantID, _, principalID guid.ID) (bool, error) {
			_, removeErr := executor.NewDelete().Table("iam_role_members").
				Where("tenant_id = ? AND principal_id = ?", tenantID, principalID).Exec(ctx)
			return true, removeErr
		})
	_, err = stripping(t.Context(), db, testID("tenant"), testID("group"), testID("root"))
	assert.ErrorIs(t, err, governance.ErrReviewLastAdministrator)

	// With a second administrator present the removal is allowed to complete.
	_, err = repo.PutMember(t.Context(), iam.TenantMember{TenantID: testID("tenant"), PrincipalID: testID("backup"), DisplayName: "Backup", Status: iam.MemberActive})
	require.NoError(t, err)
	_, err = repo.AddMember(t.Context(), testID("tenant"), role.ID, testID("backup"))
	require.NoError(t, err)
	_, err = repo.AddMember(t.Context(), testID("tenant"), role.ID, testID("root"))
	require.NoError(t, err)
	changed, err = stripping(t.Context(), db, testID("tenant"), testID("group"), testID("root"))
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestGuardEntityRoleRemoveProtectsLastAdministrator(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	seedGuardTenant(t, db)

	noop := guardEntityRoleRemove(iam.NewAdministratorGuard(), "sqlite",
		func(context.Context, bun.IDB, guid.ID, guid.ID, guid.ID, guid.ID) (bool, error) { return false, nil })
	changed, err := noop(t.Context(), db, testID("tenant"), testID("entity"), testID("role"), testID("root"))
	require.NoError(t, err)
	assert.False(t, changed)

	failing := guardEntityRoleRemove(iam.NewAdministratorGuard(), "sqlite",
		func(context.Context, bun.IDB, guid.ID, guid.ID, guid.ID, guid.ID) (bool, error) {
			return false, errors.New("binding removal exploded")
		})
	_, err = failing(t.Context(), db, testID("tenant"), testID("entity"), testID("role"), testID("root"))
	assert.ErrorContains(t, err, "binding removal exploded")

	stripping := guardEntityRoleRemove(iam.NewAdministratorGuard(), "sqlite",
		func(ctx context.Context, executor bun.IDB, tenantID, _, _, principalID guid.ID) (bool, error) {
			_, removeErr := executor.NewDelete().Table("iam_role_members").
				Where("tenant_id = ? AND principal_id = ?", tenantID, principalID).Exec(ctx)
			return true, removeErr
		})
	_, err = stripping(t.Context(), db, testID("tenant"), testID("entity"), testID("role"), testID("root"))
	assert.ErrorIs(t, err, governance.ErrReviewLastAdministrator)
}

func TestAuditAppenderUsesCallerDatabase(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))

	appendAudit := auditAppender(audit.NewService(db, newTestIDGenerator()))
	event := auditx.NewEvent(t.Context(), testID("tenant"), "tested", "module", testID("modules"))
	require.NoError(t, appendAudit(t.Context(), db, event))

	var count int
	require.NoError(t, db.NewSelect().Table("iam_audit_events").ColumnExpr("COUNT(*)").Where("tenant_id = ? AND event_type = ?", testID("tenant"), "tested").Scan(t.Context(), &count))
	require.Equal(t, 1, count)
}

func TestRegistrationPrincipalCreatorMapsIdentityErrors(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))

	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	id, err := registrationPrincipalCreator(t.Context(), db, "user@example.com", "password-hash", "User", now, newTestIDGenerator())
	require.NoError(t, err)
	require.NotEmpty(t, id)

	_, err = registrationPrincipalCreator(t.Context(), db, "user@example.com", "password-hash", "User", now, newTestIDGenerator())
	require.ErrorIs(t, err, authnext.ErrRegistrationConflict)
	_, err = registrationPrincipalCreator(t.Context(), db, "invalid", "password-hash", "User", now, newTestIDGenerator())
	require.ErrorIs(t, err, authnext.ErrInvalidRegistration)
}

// placeholderMark matches a run of two or more '?' characters, which is how
// untranslated placeholder strings appear in locale files (e.g. "??????").
var placeholderMark = regexp.MustCompile(`\?\?`)

// localeFiles returns every embedded locale JSON under the repo's i18n/locales
// directories (pkg and all modules), keyed by bundle directory then locale.
func localeFiles(t *testing.T) map[string]map[string]map[string]string {
	t.Helper()
	// Tests run with the package directory as cwd; the repo root is two levels up.
	root := "../../"
	bundles := make(map[string]map[string]map[string]string)
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") ||
			!strings.Contains(path, string(filepath.Separator)+"i18n"+string(filepath.Separator)+"locales") {
			return err
		}
		base := filepath.Dir(path)
		locale := strings.TrimSuffix(filepath.Base(path), ".json")
		if bundles[base] == nil {
			bundles[base] = make(map[string]map[string]string)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			t.Errorf("invalid JSON in %s: %v", path, err)
			return nil
		}
		bundles[base][locale] = msgs
		return nil
	})
	require.NoError(t, err)
	return bundles
}

// TestLocaleBundlesHaveNoPlaceholders guards against untranslated '?' runs in
// any locale file, which surface to clients as raw question marks.
func TestLocaleBundlesHaveNoPlaceholders(t *testing.T) {
	for base, locales := range localeFiles(t) {
		for locale, msgs := range locales {
			for key, value := range msgs {
				if placeholderMark.MatchString(value) {
					t.Errorf("%s [%s] key %q contains untranslated placeholder: %q", base, locale, key, value)
				}
			}
		}
	}
}

// TestLocaleBundlesAreAlignedAcrossLanguages verifies every bundle defines the
// same key set in en-US, zh-CN, and ms-MY so a request in any supported locale
// resolves every message without falling back to the raw key.
func TestLocaleBundlesAreAlignedAcrossLanguages(t *testing.T) {
	for base, locales := range localeFiles(t) {
		en, ok := locales["en-US"]
		if !ok {
			continue
		}
		enKeys := make(map[string]bool, len(en))
		for k := range en {
			enKeys[k] = true
		}
		for locale, msgs := range locales {
			if locale == "en-US" {
				continue
			}
			for key := range enKeys {
				if msgs[key] == "" {
					t.Errorf("%s [%s] is missing key %q present in en-US", base, locale, key)
				}
			}
			for key := range msgs {
				if !enKeys[key] {
					t.Errorf("%s [%s] has key %q missing from en-US", base, locale, key)
				}
			}
		}
	}
}

// TestRegisteredLocalesResolveKeyMessages verifies that once the global i18n
// instance has been initialized and the module bundles registered (mirroring
// the bootstrap ordering), error keys used by handlers resolve to a real
// translated message instead of falling back to the raw key.
func TestRegisteredLocalesResolveKeyMessages(t *testing.T) {
	if err := i18n.Init("en-US", "../../pkg/i18n/locales"); err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	for _, register := range []func() error{
		audit.RegisterI18n,
		federation.RegisterI18n,
		provisioning.RegisterI18n,
	} {
		if err := register(); err != nil {
			t.Fatalf("register locales: %v", err)
		}
	}

	for _, locale := range []string{"en-US", "zh-CN", "ms-MY"} {
		i18n.SetLocale(locale)
		for _, key := range []string{
			"federation_provider_disabled",
			"federation_saml_invalid_request",
			"federation_saml_sp_not_found",
			"scim_target_disabled",
			"scim_target_not_found",
			"audit_retention_invalid",
			"not_found",
		} {
			if got := i18n.T(key); got == key {
				t.Errorf("[%s] key %q fell back to the raw key (no translation)", locale, key)
			} else if placeholderMark.MatchString(got) {
				t.Errorf("[%s] key %q resolved to a placeholder: %q", locale, key, got)
			}
		}
	}
}
