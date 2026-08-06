package deployment

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/app"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/dlock"
	"github.com/chaos-plus/chaosplus/internal/infra/wuid"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/federation"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestOpenMigrationDBAcceptsSQLite(t *testing.T) {
	db, dialect, err := openMigrationDB(context.Background(), bunx.Datasource{
		Type: "sqlite3",
		Dsn:  ":memory:",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	assert.Equal(t, "sqlite", dialect)
}

func TestMigrateSQLite(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "chaosplus.db")
	cfg := app.Config{Bootstrap: app.BootstrapConfig{
		LockTimeout: time.Second,
		Database: bunx.Datasource{
			Type: "sqlite",
			Dsn:  dsn,
		},
	}}
	require.NoError(t, Migrate(context.Background(), cfg))

	db, err := (&bunx.Datasource{Type: "sqlite", Dsn: dsn}).Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.AssertMigrated(context.Background(), db))
	require.NoError(t, organization.AssertMigrated(context.Background(), db))
	require.NoError(t, federation.AssertMigrated(context.Background(), db))
}

func TestOpenRuntimeDBComparesCanonicalDialects(t *testing.T) {
	db, err := openRuntimeDB(context.Background(), map[string]bunx.Datasource{
		"primary": {
			Type:     "sqlite3",
			Dsn:      ":memory:",
			Writable: true,
		},
	}, "sqlite")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = openRuntimeDB(context.Background(), map[string]bunx.Datasource{
		"primary": {
			Type:     "sqlite",
			Dsn:      ":memory:",
			Writable: true,
		},
	}, "mysql")
	assert.ErrorContains(t, err, "dialects differ")
}

func TestBindInitialAdminIsIdempotent(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	require.NoError(t, bindInitialAdmin(context.Background(), db, "tenant", "principal", "Admin", "admin@example.com"))
	require.NoError(t, bindInitialAdmin(context.Background(), db, "tenant", "principal", "Admin", "admin@example.com"))
	var member iam.TenantMember
	_, err = iam.NewRepository(db, func() (string, error) { return "", nil }).GetMember(context.Background(), "tenant", "subject")
	require.Error(t, err)
	member, err = iam.NewRepository(db, func() (string, error) { return "", nil }).GetMember(context.Background(), "tenant", "principal")
	require.NoError(t, err)
	assert.Equal(t, iam.MemberActive, member.Status)
	allowed, err := iam.NewAuthorizer(db).Check(context.Background(), "tenant", "tenant_administer", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)
	var platformBindings int
	require.NoError(t, db.NewSelect().Table("iam_platform_administrators").ColumnExpr("COUNT(*)").Where("principal_id = ?", "principal").Scan(context.Background(), &platformBindings))
	assert.Equal(t, 1, platformBindings)
	menus, err := iam.NewRepository(db, func() (string, error) { return "", nil }).ListMenus(context.Background(), "tenant", false)
	require.NoError(t, err)
	assert.Len(t, menus, len(iam.DefaultMenus()))
}

func TestProvisionRealSQLiteInitialAdministrator(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "provision.db")
	datasource := bunx.Datasource{Type: "sqlite", Dsn: dsn, Writable: true}
	cfg := app.Config{
		Bootstrap: app.BootstrapConfig{
			LockTimeout: time.Second,
			Database:    datasource,
			InitialAdmin: app.BootstrapInitialAdmin{
				TenantID: "tenant", LoginName: "admin", Password: "correct horse battery staple",
				DisplayName: "System Admin", Email: "admin@example.com",
			},
		},
		Database: map[string]bunx.Datasource{"primary": datasource},
	}
	require.NoError(t, Migrate(context.Background(), cfg))
	require.NoError(t, Provision(context.Background(), cfg))
	require.NoError(t, Provision(context.Background(), cfg), "provisioning must be idempotent")

	db, err := datasource.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var principalID string
	require.NoError(t, db.NewSelect().Table("iam_principals").Column("id").Where("login_name = ?", "admin").Scan(context.Background(), &principalID))
	allowed, err := iam.NewAuthorizer(db).Check(context.Background(), "tenant", "tenant_administer", principalID)
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = iam.NewAuthorizer(db).CheckPlatform(context.Background(), "platform_administer", principalID)
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestRollbackRealSQLiteModules(t *testing.T) {
	for _, module := range []string{"dlock", "wuid", "iam", "organization", "provisioning", "governance", "federation"} {
		t.Run(module, func(t *testing.T) {
			dsn := filepath.Join(t.TempDir(), module+".db")
			cfg := app.Config{Bootstrap: app.BootstrapConfig{
				LockTimeout: time.Second,
				Database:    bunx.Datasource{Type: "sqlite", Dsn: dsn},
			}}
			require.NoError(t, Migrate(context.Background(), cfg))
			require.NoError(t, Rollback(context.Background(), cfg, module, nil))
			require.NoError(t, Migrate(context.Background(), cfg))
		})
	}

	dsn := filepath.Join(t.TempDir(), "unknown.db")
	cfg := app.Config{Bootstrap: app.BootstrapConfig{LockTimeout: time.Second, Database: bunx.Datasource{Type: "sqlite", Dsn: dsn}}}
	require.NoError(t, Migrate(context.Background(), cfg))
	assert.ErrorContains(t, Rollback(context.Background(), cfg, "unknown", nil), "unknown migration module")
}

func TestRuntimeDatabaseSelectionValidation(t *testing.T) {
	_, err := openRuntimeDB(context.Background(), nil, "sqlite")
	assert.ErrorContains(t, err, "exactly one")
	_, err = openRuntimeDB(context.Background(), map[string]bunx.Datasource{
		"one": {Type: "sqlite", Dsn: ":memory:", Writable: true},
		"two": {Type: "sqlite", Dsn: ":memory:", Writable: true},
	}, "sqlite")
	assert.ErrorContains(t, err, "exactly one")
	_, _, err = openMigrationDB(context.Background(), bunx.Datasource{Type: "unsupported", Dsn: "value"})
	assert.ErrorContains(t, err, "bootstrap database type")
}

func TestDeploymentFailurePaths(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "unmigrated.db")
	datasource := bunx.Datasource{Type: "sqlite", Dsn: dsn, Writable: true}
	cfg := app.Config{
		Bootstrap: app.BootstrapConfig{LockTimeout: time.Second, Database: datasource},
		Database:  map[string]bunx.Datasource{"primary": datasource},
	}
	assert.ErrorContains(t, Provision(t.Context(), cfg), "cannot read migrated IAM tables")
	assert.Error(t, Migrate(t.Context(), app.Config{Bootstrap: app.BootstrapConfig{Database: bunx.Datasource{Type: "sqlite"}}}))

	require.NoError(t, Migrate(t.Context(), cfg))
	cfg.Bootstrap.InitialAdmin = app.BootstrapInitialAdmin{
		TenantID: "tenant", LoginName: "admin", Password: "value", PasswordFile: filepath.Join(t.TempDir(), "password"),
	}
	assert.ErrorContains(t, Provision(t.Context(), cfg), "mutually exclusive")

	version := int64(0)
	require.NoError(t, Rollback(t.Context(), cfg, "iam", &version))
	require.NoError(t, Migrate(t.Context(), cfg))
	_, err := openRuntimeDB(t.Context(), map[string]bunx.Datasource{
		"primary": {Type: "unsupported", Dsn: "value", Writable: true},
	}, "sqlite")
	assert.ErrorContains(t, err, "runtime database type")
	_, err = openRuntimeDB(t.Context(), map[string]bunx.Datasource{
		"primary": {Type: "sqlite", Dsn: ":memory:", Writable: true},
	}, "unsupported")
	assert.ErrorContains(t, err, "migration database type")
}

func TestDeploymentLockTimeouts(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	lock, err := acquireAdvisoryLock(t.Context(), db, "sqlite", time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close(t.Context()) })

	datasource := bunx.Datasource{Type: "sqlite", Dsn: filepath.Join(t.TempDir(), "locked.db"), Writable: true}
	cfg := app.Config{
		Bootstrap: app.BootstrapConfig{LockTimeout: 10 * time.Millisecond, Database: datasource},
		Database:  map[string]bunx.Datasource{"primary": datasource},
	}
	assert.ErrorContains(t, Migrate(t.Context(), cfg), "acquire sqlite bootstrap lock")
	assert.ErrorContains(t, Rollback(t.Context(), cfg, "iam", nil), "acquire sqlite bootstrap lock")
	assert.ErrorContains(t, Provision(t.Context(), cfg), "acquire sqlite bootstrap lock")
}

func TestMigrationStageFailures(t *testing.T) {
	t.Run("dlock", func(t *testing.T) {
		db, err := bunxtest.Memory()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		_, err = db.ExecContext(t.Context(), "CREATE TABLE goose_dlock (bad TEXT)")
		require.NoError(t, err)
		assert.ErrorContains(t, migrate(t.Context(), db), "migrate dlock")
	})
	t.Run("wuid", func(t *testing.T) {
		db, err := bunxtest.Memory()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, dlock.Migrate(t.Context(), db))
		_, err = db.ExecContext(t.Context(), "CREATE TABLE goose_wuid (bad TEXT)")
		require.NoError(t, err)
		assert.ErrorContains(t, migrate(t.Context(), db), "migrate wuid")
	})
	t.Run("iam", func(t *testing.T) {
		db, err := bunxtest.Memory()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, dlock.Migrate(t.Context(), db))
		require.NoError(t, wuid.Migrate(t.Context(), db))
		_, err = db.ExecContext(t.Context(), "CREATE TABLE goose_iam (bad TEXT)")
		require.NoError(t, err)
		assert.ErrorContains(t, migrate(t.Context(), db), "migrate iam")
	})
	t.Run("organization", func(t *testing.T) {
		db, err := bunxtest.Memory()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, dlock.Migrate(t.Context(), db))
		require.NoError(t, wuid.Migrate(t.Context(), db))
		require.NoError(t, iam.Migrate(t.Context(), db))
		_, err = db.ExecContext(t.Context(), "CREATE TABLE goose_organization (bad TEXT)")
		require.NoError(t, err)
		assert.ErrorContains(t, migrate(t.Context(), db), "migrate organization")
	})
	t.Run("federation", func(t *testing.T) {
		db, err := bunxtest.Memory()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, dlock.Migrate(t.Context(), db))
		require.NoError(t, wuid.Migrate(t.Context(), db))
		require.NoError(t, iam.Migrate(t.Context(), db))
		require.NoError(t, organization.Migrate(t.Context(), db))
		_, err = db.ExecContext(t.Context(), "CREATE TABLE goose_federation (bad TEXT)")
		require.NoError(t, err)
		assert.ErrorContains(t, migrate(t.Context(), db), "migrate federation")
	})
}

func TestInitialAdministratorTransactionFailures(t *testing.T) {
	newIAMDB := func(t *testing.T) *bun.DB {
		t.Helper()
		db, err := bunxtest.Memory()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, iam.Migrate(t.Context(), db))
		require.NoError(t, organization.Migrate(t.Context(), db))
		return db
	}

	t.Run("list roles", func(t *testing.T) {
		db := newIAMDB(t)
		_, err := db.ExecContext(t.Context(), "ALTER TABLE iam_roles RENAME TO unavailable_roles")
		require.NoError(t, err)
		assert.ErrorContains(t, bindInitialAdmin(t.Context(), db, "tenant", "principal", "", ""), "list bootstrap roles")
	})
	t.Run("create role", func(t *testing.T) {
		db := newIAMDB(t)
		_, err := db.ExecContext(t.Context(), `CREATE TRIGGER deny_role_insert BEFORE INSERT ON iam_roles BEGIN SELECT RAISE(ABORT, 'role denied'); END`)
		require.NoError(t, err)
		assert.ErrorContains(t, bindInitialAdmin(t.Context(), db, "tenant", "principal", "Admin", ""), "create administrator role")
	})
	t.Run("grant permission", func(t *testing.T) {
		db := newIAMDB(t)
		repo := iam.NewRepository(db, bootstrapID)
		_, err := repo.CreateRole(t.Context(), "tenant", "System Administrator", "")
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `CREATE TRIGGER deny_permission_insert BEFORE INSERT ON iam_role_permissions BEGIN SELECT RAISE(ABORT, 'permission denied'); END`)
		require.NoError(t, err)
		assert.ErrorContains(t, bindInitialAdmin(t.Context(), db, "tenant", "principal", "Admin", ""), "grant administrator permission")
	})
	t.Run("bind member", func(t *testing.T) {
		db := newIAMDB(t)
		require.NoError(t, bindInitialAdmin(t.Context(), db, "tenant", "principal", "Admin", ""))
		_, err := db.ExecContext(t.Context(), "DELETE FROM iam_role_members")
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `CREATE TRIGGER deny_role_member_insert BEFORE INSERT ON iam_role_members BEGIN SELECT RAISE(ABORT, 'member denied'); END`)
		require.NoError(t, err)
		assert.ErrorContains(t, bindInitialAdmin(t.Context(), db, "tenant", "principal", "Admin", ""), "bind initial administrator")
	})
	t.Run("create default menu", func(t *testing.T) {
		db := newIAMDB(t)
		_, err := db.ExecContext(t.Context(), `CREATE TRIGGER deny_menu_insert BEFORE INSERT ON iam_menus BEGIN SELECT RAISE(ABORT, 'menu denied'); END`)
		require.NoError(t, err)
		assert.ErrorContains(t, bindInitialAdmin(t.Context(), db, "tenant", "principal", "Admin", ""), "create default menu")
	})
}
func TestProvisionAndLoginRealDialect(t *testing.T) {
	dialect := strings.ToLower(strings.TrimSpace(os.Getenv("IAM_DB_LIFECYCLE_TYPE")))
	if dialect == "" {
		t.Skip("set IAM_DB_LIFECYCLE_TYPE and IAM_DB_LIFECYCLE_ADMIN_DSN to test bootstrap and login on a real database")
	}
	dsn := newDeploymentLifecycleDatabase(t, dialect, os.Getenv("IAM_DB_LIFECYCLE_ADMIN_DSN"))
	datasource := bunx.Datasource{Type: dialect, Dsn: dsn, Writable: true}
	cfg := app.Config{
		Bootstrap: app.BootstrapConfig{
			LockTimeout: time.Second,
			Database:    datasource,
			InitialAdmin: app.BootstrapInitialAdmin{
				TenantID: "tenant", LoginName: "admin", Password: "correct horse battery staple",
				DisplayName: "System Admin", Email: "admin@example.com",
			},
		},
		Database: map[string]bunx.Datasource{"primary": datasource},
	}
	require.NoError(t, Migrate(context.Background(), cfg))
	require.NoError(t, Provision(context.Background(), cfg))
	require.NoError(t, Provision(context.Background(), cfg), "provisioning must be idempotent")

	db, err := datasource.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var principalID string
	require.NoError(t, db.NewSelect().Table("iam_principals").Column("id").Where("login_name = ?", "admin").Scan(context.Background(), &principalID))
	allowed, err := iam.NewAuthorizer(db).Check(context.Background(), "tenant", "tenant_administer", principalID)
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = iam.NewAuthorizer(db).CheckPlatform(context.Background(), "platform_administer", principalID)
	require.NoError(t, err)
	assert.True(t, allowed)

	service, err := authnmod.NewWebService(loginSmokeAuthConfig(), db)
	require.NoError(t, err)
	token, returnURL, err := service.Login(context.Background(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	assert.Equal(t, "https://app.example/", returnURL)
	claims, err := service.Authenticate(context.Background(), "", service.SessionCookie(token))
	require.NoError(t, err)
	assert.Equal(t, principalID, claims.Subject)
	assert.Equal(t, "admin", claims.PreferredUsername)
	assert.Equal(t, "admin@example.com", claims.Email)
	assert.True(t, claims.EmailVerified)

	// Token rotation: reconciling with a new password bumps the credential
	// version and revokes the previous session and access token.
	cookie := service.SessionCookie(token)
	oldAccess, _, err := service.IssueAccessToken(context.Background(), principalID, "api", "openid profile")
	require.NoError(t, err)
	_, err = service.Authenticate(context.Background(), "Bearer "+oldAccess, "")
	require.NoError(t, err)
	_, err = authnmod.EnsureBootstrapPrincipal(context.Background(), db, authnmod.BootstrapPrincipal{
		LoginName: "admin", Password: "rotated horse battery staple", DisplayName: "System Admin", Email: "admin@example.com",
	})
	require.NoError(t, err)
	_, err = service.Authenticate(context.Background(), "", cookie)
	assert.ErrorIs(t, err, authnext.ErrInvalidSession)
	_, err = service.Authenticate(context.Background(), "Bearer "+oldAccess, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	rotated, _, err := service.Login(context.Background(), "admin", "rotated horse battery staple", "")
	require.NoError(t, err)
	claims, err = service.Authenticate(context.Background(), "", service.SessionCookie(rotated))
	require.NoError(t, err)
	assert.Equal(t, principalID, claims.Subject)
}

func newDeploymentLifecycleDatabase(t *testing.T, dialect, adminDSN string) string {
	t.Helper()
	require.Contains(t, []string{"mysql", "postgres"}, dialect)
	require.NotEmpty(t, adminDSN)
	admin := (&bunx.Datasource{Type: dialect, Dsn: adminDSN}).NewDB()
	require.NotNil(t, admin)
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, admin.PingContext(t.Context()))

	name := fmt.Sprintf("chaosplus_deployment_%d", time.Now().UTC().UnixNano())
	quoted := `"` + name + `"`
	create := "CREATE DATABASE " + quoted
	if dialect == "mysql" {
		quoted = "`" + name + "`"
		create = "CREATE DATABASE " + quoted + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	}
	_, err := admin.ExecContext(t.Context(), create)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+quoted)
		assert.NoError(t, cleanupErr)
	})

	var targetDSN string
	if dialect == "mysql" {
		cfg, parseErr := mysql.ParseDSN(adminDSN)
		require.NoError(t, parseErr)
		cfg.DBName = name
		targetDSN = cfg.FormatDSN()
	} else {
		parsed, parseErr := url.Parse(adminDSN)
		require.NoError(t, parseErr)
		parsed.Path = "/" + name
		targetDSN = parsed.String()
	}
	db := (&bunx.Datasource{Type: dialect, Dsn: targetDSN}).NewDB()
	require.NotNil(t, db)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(t.Context()))
	return targetDSN
}

func loginSmokeAuthConfig() authnext.Config {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return authnext.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"},
		SigningKey: base64.RawStdEncoding.EncodeToString(seed), AccessTokenTTL: time.Minute,
		MFA:     authnext.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
		Passkey: authnext.PasskeyConfig{Enabled: false, RPID: "app.example", DisplayName: "Chaosplus", Origins: []string{"https://app.example"}},
		Web: authnext.WebConfig{
			Enabled: true, CookieName: "cp_session", SessionTTL: time.Hour, IdleTTL: 10 * time.Minute,
			PostLoginURL: "https://app.example/", PostLogoutURL: "https://app.example/login",
			AllowedReturnURLs: []string{"https://app.example/"}, AllowedOrigins: []string{"https://app.example"}, CookieSecure: true,
		},
	}
}
