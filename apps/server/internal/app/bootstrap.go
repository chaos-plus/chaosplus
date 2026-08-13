package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/uptrace/bun"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/plugin"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/federation"
	"github.com/chaos-plus/chaosplus/internal/modules/governance"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	identitymod "github.com/chaos-plus/chaosplus/internal/modules/identity"
	oauthmod "github.com/chaos-plus/chaosplus/internal/modules/oauth"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/chaos-plus/chaosplus/internal/modules/provisioning"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/chaos-plus/chaosplus/pkg/timezone"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// Bootstrap wires up cross-cutting dependencies before the servers start: base
// services (timezone, logging, database), then the application modules through
// their migrate and start phases. It returns an error so a failed migration or
// module start aborts startup instead of leaving the app half-initialised.
// bootstrapTenantForVerifiedUser 给注册激活的用户自动创建一个租户,并把该用户
// 挂为成员并授予租户管理员角色(PRD:每个注册用户是一个租户)。幂等:slug 冲突或
// 成员已存在即跳过。所有 ID 均由共享雪花生成器产生。
func bootstrapTenantForVerifiedUser(ctx context.Context, db *bun.DB, principalID guid.ID, email string) error {
	if db == nil {
		return errors.New("bootstrap tenant: database is required")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	now := time.Now().UTC().UnixMilli()
	base := email
	if i := strings.IndexByte(email, '@'); i > 0 {
		base = email[:i]
	}
	randBytes := make([]byte, 3)
	if _, err := rand.Read(randBytes); err != nil {
		return fmt.Errorf("bootstrap tenant: rand: %w", err)
	}
	slug := fmt.Sprintf("%s-%x", base, randBytes)
	id, err := guid.Next()
	if err != nil {
		return fmt.Errorf("bootstrap tenant: id: %w", err)
	}
	tenantID := guid.ID(id)
	roleRaw, err := guid.Next()
	if err != nil {
		return fmt.Errorf("bootstrap tenant: role id: %w", err)
	}
	roleID := guid.ID(roleRaw)
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		tenant := struct {
			bun.BaseModel `bun:"table:iam_tenants"`
			ID            guid.ID
			Slug          string
			Name          string
			Status        string
			Version       int64
			CreatedAt     int64
			UpdatedAt     int64
		}{ID: tenantID, Slug: slug, Name: email, Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(&tenant).Ignore().Exec(ctx); err != nil {
			return fmt.Errorf("bootstrap tenant: create: %w", err)
		}
		member := struct {
			bun.BaseModel `bun:"table:iam_tenant_members"`
			TenantID      guid.ID
			PrincipalID   guid.ID
			DisplayName   string
			Email         string
			Status        string
			CreatedAt     int64
			UpdatedAt     int64
		}{TenantID: tenantID, PrincipalID: principalID, DisplayName: email, Email: email, Status: "active", CreatedAt: now, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(&member).Ignore().Exec(ctx); err != nil {
			return fmt.Errorf("bootstrap tenant: member: %w", err)
		}
		role := struct {
			bun.BaseModel `bun:"table:iam_roles"`
			TenantID      guid.ID
			ID            guid.ID
			Name          string
			Description   string
			CreatedAt     int64
			UpdatedAt     int64
		}{TenantID: tenantID, ID: roleID, Name: "Owner", Description: "自动创建的租户所有者", CreatedAt: now, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(&role).Ignore().Exec(ctx); err != nil {
			return fmt.Errorf("bootstrap tenant: role: %w", err)
		}
		perm := struct {
			bun.BaseModel  `bun:"table:iam_role_permissions"`
			TenantID       guid.ID
			RoleID         guid.ID
			PermissionCode string
			ConditionJSON  string
			CreatedAt      int64
		}{TenantID: tenantID, RoleID: roleID, PermissionCode: "tenant_administer", ConditionJSON: "", CreatedAt: now}
		if _, err := tx.NewInsert().Model(&perm).Ignore().Exec(ctx); err != nil {
			return fmt.Errorf("bootstrap tenant: permission: %w", err)
		}
		roleMember := struct {
			bun.BaseModel `bun:"table:iam_role_members"`
			TenantID      guid.ID
			RoleID        guid.ID
			PrincipalID   guid.ID
			CreatedAt     int64
		}{TenantID: tenantID, RoleID: roleID, PrincipalID: principalID, CreatedAt: now}
		if _, err := tx.NewInsert().Model(&roleMember).Ignore().Exec(ctx); err != nil {
			return fmt.Errorf("bootstrap tenant: role member: %w", err)
		}
		return nil
	})
}

func (app *App) Bootstrap() error {

	// init timezone: timestamps are UTC end to end (DB, API), and only the
	// frontend converts to a display timezone. Fail fast on an invalid config.
	if err := timezone.SetTimezone(app.cfg.Timezone); err != nil {
		return fmt.Errorf("set timezone %q: %w", app.cfg.Timezone, err)
	}

	// init i18n: load the global locale bundle so response messages can be
	// localized from their i18n keys (see respx.LocalizeMessage).
	if err := initModuleI18n(); err != nil {
		return fmt.Errorf("init i18n: %w", err)
	}

	// init logger: always to stdout, optionally also to a file when configured.
	var handlers []slog.Handler
	handlers = append(handlers, slog.NewJSONHandler(os.Stdout, nil))
	handlers = append(handlers, otelslog.NewHandler(app.name))
	slog.SetDefault(slog.New(slog.NewMultiHandler(handlers...)))

	// init db: a single sqlite connection keeps the private ":memory:" database
	// alive and consistent for the process lifetime (see SetupDebug).
	app.dbr = bunx.NewDatasourceRouter(app.name, app.cfg.Debug, app.cfg.Database)

	// init redis: created lazily (no startup ping) so the rate limiter can fail
	// open if Redis is briefly unavailable. The universal client selects
	// standalone/sentinel/cluster from the options. Absent when no address is set.
	if len(app.cfg.Redis.Addrs) > 0 {
		password, err := secretx.Resolve("redis.password", app.cfg.Redis.Password, app.cfg.Redis.PasswordFile, 4096)
		if err != nil {
			return err
		}
		app.cfg.Redis.Password = password
		app.redis = redis.NewUniversalClient(&redis.UniversalOptions{
			Addrs:      app.cfg.Redis.Addrs,
			MasterName: app.cfg.Redis.MasterName,
			Username:   app.cfg.Redis.Username,
			Password:   app.cfg.Redis.Password,
			DB:         app.cfg.Redis.DB,
		})
	}

	registry, err := app.buildAuthorizationRegistry()
	if err != nil {
		return err
	}
	app.authzRegistry = registry
	for _, extension := range app.extensions {
		if extension.RegisterI18n != nil {
			if err := extension.RegisterI18n(); err != nil {
				return fmt.Errorf("register %s locales: %w", extension.Name, err)
			}
		}
	}
	if app.resourceProfile {
		if !app.cfg.Authn.Enabled || !app.cfg.Authz.Enabled {
			return fmt.Errorf("resource application requires authentication and authorization")
		}
		if len(app.dbr.Writer) == 0 {
			return fmt.Errorf("resource application requires a writable business database with IAM projections")
		}
		if app.cfg.Authn.Web.Enabled {
			web, err := authnmod.NewWebService(app.cfg.Authn, app.dbr.Write(), authnmod.WithIDGenerator(nextGUID))
			if err != nil {
				return fmt.Errorf("init resource browser authentication: %w", err)
			}
			app.authnWeb = web
			app.authnRequest = web
		} else {
			verifier, err := authnext.NewVerifier(app.cfg.Authn)
			if err != nil {
				return fmt.Errorf("init resource authentication: %w", err)
			}
			app.authnRequest = verifier
		}
		app.authzRegistrar = authz.NewRegistrar(registry, app.authnRequest, iam.NewAuthorizerWithRegistry(app.dbr.Write(), registry), iam.NewMembershipChecker(app.dbr.Write()))
	} else if app.cfg.Authn.Enabled {
		if len(app.dbr.Writer) == 0 {
			return fmt.Errorf("local authentication requires a writable database")
		}
		claimPlugins, err := plugin.LoadClaims(app.cfg.Plugins)
		if err != nil {
			return fmt.Errorf("load claim plugins: %w", err)
		}
		app.claimPlugins = claimPlugins
		options := []authnmod.WebOption{
			authnmod.WithRegistrationPrincipalCreator(registrationPrincipalCreator),
			authnmod.WithIDGenerator(nextGUID),
		}
		// 每个注册用户激活后自动拥有一个租户(PRD:注册用户=租户,租户下多 instance)。
		options = append(options, authnmod.WithVerifiedHook(func(ctx context.Context, principalID guid.ID, email string) error {
			return bootstrapTenantForVerifiedUser(ctx, app.dbr.Write(), principalID, email)
		}))
		if claimPlugins != nil {
			options = append(options, authnmod.WithClaimEnricher(claimPlugins))
		}
		// Brand fields follow the configured app name so a deployment can
		// rebrand by changing config.name alone.
		if app.cfg.Authn.MFA.Issuer == "" {
			app.cfg.Authn.MFA.Issuer = app.cfg.Name
		}
		if app.cfg.Authn.Passkey.DisplayName == "" {
			app.cfg.Authn.Passkey.DisplayName = app.cfg.Name
		}
		web, err := authnmod.NewWebService(app.cfg.Authn, app.dbr.Write(), options...)
		if err != nil {
			return fmt.Errorf("init local authn: %w", err)
		}
		app.authnWeb = web
		app.authnRequest = web

	}

	if !app.resourceProfile && app.cfg.Authz.Enabled {
		if !app.cfg.Authn.Enabled {
			return fmt.Errorf("authorization requires authentication to be enabled")
		}
		if len(app.dbr.Writer) == 0 {
			return fmt.Errorf("authorization requires a writable database")
		}
		app.authzRegistrar = authz.NewRegistrar(registry, app.authnRequest, iam.NewAuthorizerWithRegistry(app.dbr.Write(), registry), iam.NewMembershipChecker(app.dbr.Write()))
	}

	if !app.resourceProfile && app.cfg.Federation.Enabled {
		if !app.cfg.Authn.Enabled || app.authnWeb == nil {
			return fmt.Errorf("federation requires authentication to be enabled")
		}
		key, err := federation.ResolveEncryptionKey(app.cfg.Federation)
		if err != nil {
			return fmt.Errorf("init federation: %w", err)
		}
		app.federationKey = key
	}
	if !app.resourceProfile {
		provisioningKey, err := provisioning.ResolveEncryptionKey(app.cfg.Provisioning)
		if err != nil {
			return fmt.Errorf("init provisioning: %w", err)
		}
		app.provisioningKey = provisioningKey
	}

	// build modules, then run the migrate and start phases in order.
	app.mods = app.buildModules()
	if err := app.buildExtensionModules(); err != nil {
		return err
	}
	if app.cfg.Migrations.Auto {
		if err := app.migrateModules(app.ctx); err != nil {
			return err
		}
	} else if app.authzRegistrar != nil && !app.resourceProfile {
		if err := iam.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
			return err
		}
		if err := organization.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
			return err
		}
		if err := provisioning.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
			return err
		}
		if err := governance.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
			return err
		}
		if app.cfg.Federation.Enabled {
			if err := federation.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
				return err
			}
		}
	}
	if app.resourceProfile {
		if err := iam.AssertMigrated(app.ctx, app.dbr.Write()); err != nil {
			return fmt.Errorf("resource authorization dependency: %w", err)
		}
	}
	if err := app.startModules(app.ctx); err != nil {
		return err
	}

	return nil
}

func initModuleI18n() error {
	if err := i18n.InitEmbedded(i18n.Base); err != nil {
		return err
	}
	modules := []struct {
		name     string
		register func() error
	}{
		{"audit", auditmod.RegisterI18n},
		{"authn", authnmod.RegisterI18n},
		{"governance", governance.RegisterI18n},
		{"iam", iam.RegisterI18n},
		{"identity", identitymod.RegisterI18n},
		{"oauth", oauthmod.RegisterI18n},
		{"organization", organization.RegisterI18n},
		{"provisioning", provisioning.RegisterI18n},
		{"federation", federation.RegisterI18n},
	}
	for _, module := range modules {
		if err := module.register(); err != nil {
			return fmt.Errorf("register %s locales: %w", module.name, err)
		}
	}
	return nil
}
