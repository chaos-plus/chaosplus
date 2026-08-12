package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/plugin"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleI18nRegistersEverySupportedLocale(t *testing.T) {
	require.NoError(t, initModuleI18n())
	keys := []string{
		"audit_event_not_found", "invalid_login", "entity_not_found",
		"principal_not_found", "oauth_invalid_request", "tenant_not_found",
		"registration_request_rejected", "invalid_registration", "registration_unavailable",
		"invalid_relationship_window",
		"invalid_relationship_condition",
		"scim_invalid_syntax",
		"scim_unauthorized",
		"provisioning_unavailable",
	}
	for _, locale := range i18n.Supported() {
		ctx := i18n.WithLocale(t.Context(), locale.Code)
		for _, key := range keys {
			message := i18n.TContext(ctx, key)
			assert.NotEqual(t, key, message, "%s %s", locale.Code, key)
			assert.NotEmpty(t, message, "%s %s", locale.Code, key)
		}
	}
}

func TestEveryFeatureModuleOwnsCompleteErrorLocales(t *testing.T) {
	require.NoError(t, initModuleI18n())
	base := readLocaleCatalog(t, filepath.Join("..", "..", "pkg", "i18n", "locales", i18n.Base+".json"))
	modules, err := os.ReadDir(filepath.Join("..", "modules"))
	require.NoError(t, err)
	for _, module := range modules {
		if !module.IsDir() {
			continue
		}
		t.Run(module.Name(), func(t *testing.T) {
			root := filepath.Join("..", "modules", module.Name())
			require.FileExists(t, filepath.Join(root, "i18n.go"))
			catalogs := make(map[string]map[string]string, len(i18n.Supported()))
			for _, locale := range i18n.Supported() {
				catalogs[locale.Code] = readLocaleCatalog(t, filepath.Join(root, "i18n", "locales", locale.Code+".json"))
			}
			for _, locale := range i18n.Supported() {
				require.Equal(t, len(catalogs[i18n.Base]), len(catalogs[locale.Code]), "%s locale key count", locale.Code)
				for key, value := range catalogs[i18n.Base] {
					require.NotEmpty(t, strings.TrimSpace(value), "%s %s", i18n.Base, key)
					require.NotEmpty(t, strings.TrimSpace(catalogs[locale.Code][key]), "%s %s", locale.Code, key)
					if locale.Code != i18n.Base {
						assert.NotEqual(t, value, catalogs[locale.Code][key], "%s %s is not translated", locale.Code, key)
					}
					assert.NotEqual(t, key, i18n.TContext(i18n.WithLocale(t.Context(), locale.Code), key), "%s %s is not registered", locale.Code, key)
				}
			}

			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return walkErr
				}
				file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
				if parseErr != nil {
					return parseErr
				}
				ast.Inspect(file, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok || len(call.Args) == 0 {
						return true
					}
					keyIndex := -1
					keyPrefix := ""
					switch function := call.Fun.(type) {
					case *ast.SelectorExpr:
						pkg, ok := function.X.(*ast.Ident)
						if !ok {
							return true
						}
						switch {
						case pkg.Name == "huma" && strings.HasPrefix(function.Sel.Name, "Error"):
							keyIndex = 0
						case pkg.Name == "huma" && function.Sel.Name == "NewError" && len(call.Args) > 1:
							keyIndex = 1
						case pkg.Name == "respx" && function.Sel.Name == "Err" && len(call.Args) > 2:
							keyIndex = 2
						}
					case *ast.Ident:
						switch function.Name {
						case "oauthError":
							keyIndex, keyPrefix = 2, "oauth_"
						case "newSCIMError":
							keyIndex = 3
						}
					}
					if keyIndex < 0 || keyIndex >= len(call.Args) {
						return true
					}
					literal, literalOK := call.Args[keyIndex].(*ast.BasicLit)
					if !literalOK || literal.Kind != token.STRING {
						return true
					}
					key, unquoteErr := strconv.Unquote(literal.Value)
					require.NoError(t, unquoteErr)
					key = keyPrefix + key
					_, moduleKey := catalogs[i18n.Base][key]
					_, baseKey := base[key]
					assert.True(t, moduleKey || baseKey, "%s uses unregistered public error key %q", path, key)
					return true
				})
				return nil
			})
			require.NoError(t, err)
		})
	}
}

func readLocaleCatalog(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var catalog map[string]string
	require.NoError(t, json.Unmarshal(data, &catalog))
	return catalog
}

func TestBootstrapRealSQLiteApplication(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index + 1)
	}
	dsn := filepath.Join(t.TempDir(), "application.db")
	cfg := Config{
		Name: "bootstrap-test", Timezone: "UTC", WorkerLease: 30,
		Database: map[string]bunx.Datasource{
			"primary": {Type: "sqlite", Dsn: dsn, Writable: true},
		},
		Migrations: Migrations{Auto: true},
		Authn: authn.Config{
			Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"},
			SigningKey: base64.RawStdEncoding.EncodeToString(seed), AccessTokenTTL: time.Minute,
			MFA: authn.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
			Web: authn.WebConfig{Enabled: true, CookieName: "session", SessionTTL: time.Hour, IdleTTL: time.Minute},
		},
		Authz: Authz{Enabled: true},
	}
	application := withCtx(NewApp(cfg))
	require.NoError(t, application.Bootstrap())
	require.NotNil(t, application.authnWeb)
	require.NotNil(t, application.authzRegistrar)
	require.GreaterOrEqual(t, len(application.mods), 6)

	_, api := humatest.New(t)
	application.registerREST(api)
	require.NoError(t, authz.ValidateOperations(api, application.authzRegistrar.Registry()))
	assert.Contains(t, api.OpenAPI().Paths, "/iam/principals")
	assert.Contains(t, api.OpenAPI().Paths, "/oauth/token")
	require.NoError(t, application.shutdown())
}

func TestBootstrapRejectsInvalidDependencyConfigurations(t *testing.T) {
	t.Run("timezone", func(t *testing.T) {
		application := NewApp(Config{Timezone: "Invalid/Timezone"})
		assert.ErrorContains(t, application.Bootstrap(), "set timezone")
	})
	t.Run("authentication database", func(t *testing.T) {
		application := NewApp(Config{Timezone: "UTC", Authn: authn.Config{Enabled: true}})
		assert.ErrorContains(t, application.Bootstrap(), "writable database")
	})
	t.Run("authorization dependency", func(t *testing.T) {
		application := NewApp(Config{Timezone: "UTC", Authz: Authz{Enabled: true}})
		assert.ErrorContains(t, application.Bootstrap(), "requires authentication")
	})
	t.Run("resource application security", func(t *testing.T) {
		application := NewResourceApp(Config{Timezone: "UTC"}, Extension{Name: "resource"})
		assert.ErrorContains(t, application.Bootstrap(), "requires authentication and authorization")
	})
}

func TestBootstrapRedisAndPluginConfiguration(t *testing.T) {
	application := withCtx(NewApp(Config{
		Timezone: "UTC", Redis: Redis{Addrs: []string{"127.0.0.1:1"}},
	}))
	require.NoError(t, application.Bootstrap())
	require.NotNil(t, application.redis)
	require.NoError(t, application.shutdown())

	seed := make([]byte, ed25519.SeedSize)
	invalidPlugins := withCtx(NewApp(Config{
		Timezone: "UTC",
		Database: map[string]bunx.Datasource{"primary": {Type: "sqlite", Dsn: ":memory:", Writable: true}},
		Authn:    authn.Config{Enabled: true, Issuer: "https://iam.example", SigningKey: base64.RawStdEncoding.EncodeToString(seed)},
		Plugins:  plugin.Config{Enabled: true},
	}))
	assert.ErrorContains(t, invalidPlugins.Bootstrap(), "no manifests")
	require.NoError(t, invalidPlugins.shutdown())
}

func TestBootstrapDependencyFailurePropagation(t *testing.T) {
	t.Run("redis password file", func(t *testing.T) {
		application := withCtx(NewApp(Config{
			Timezone: "UTC",
			Redis:    Redis{Addrs: []string{"127.0.0.1:6379"}, PasswordFile: filepath.Join(t.TempDir(), "missing")},
		}))
		assert.ErrorContains(t, application.Bootstrap(), "redis.password")
		require.NoError(t, application.shutdown())
	})

	t.Run("authentication signing key", func(t *testing.T) {
		application := withCtx(NewApp(Config{
			Timezone: "UTC",
			Database: map[string]bunx.Datasource{"primary": {Type: "sqlite", Dsn: ":memory:", Writable: true}},
			Authn:    authn.Config{Enabled: true, Issuer: "https://iam.example", SigningKey: "short"},
		}))
		assert.ErrorContains(t, application.Bootstrap(), "init local authn")
		require.NoError(t, application.shutdown())
	})

	t.Run("authorization schema", func(t *testing.T) {
		seed := make([]byte, ed25519.SeedSize)
		application := withCtx(NewApp(Config{
			Timezone: "UTC",
			Database: map[string]bunx.Datasource{"primary": {Type: "sqlite", Dsn: ":memory:", Writable: true}},
			Authn:    authn.Config{Enabled: true, Issuer: "https://iam.example", SigningKey: base64.RawStdEncoding.EncodeToString(seed)},
			Authz:    Authz{Enabled: true},
		}))
		assert.Error(t, application.Bootstrap())
		require.NoError(t, application.shutdown())
	})

	t.Run("organization schema", func(t *testing.T) {
		seed := make([]byte, ed25519.SeedSize)
		dsn := filepath.Join(t.TempDir(), "iam-only.db")
		datasource := bunx.Datasource{Type: "sqlite", Dsn: dsn, Writable: true}
		db, err := datasource.Open()
		require.NoError(t, err)
		require.NoError(t, iam.Migrate(context.Background(), db))
		require.NoError(t, db.Close())

		application := withCtx(NewApp(Config{
			Timezone:   "UTC",
			Database:   map[string]bunx.Datasource{"primary": datasource},
			Migrations: Migrations{Auto: false},
			Authn: authn.Config{
				Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"},
				SigningKey: base64.RawStdEncoding.EncodeToString(seed), AccessTokenTTL: time.Minute,
				MFA: authn.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
				Web: authn.WebConfig{Enabled: true, CookieName: "session", SessionTTL: time.Hour, IdleTTL: time.Minute},
			},
			Authz: Authz{Enabled: true},
		}))
		assert.ErrorContains(t, application.Bootstrap(), "organization schema is not ready")
		require.NoError(t, application.shutdown())
	})
}
