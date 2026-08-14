package deployment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
	"github.com/chaos-plus/chaosplus/internal/infra/dlock"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/infra/wuid"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/federation"
	"github.com/chaos-plus/chaosplus/internal/modules/governance"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/chaos-plus/chaosplus/internal/modules/provisioning"
	"github.com/uptrace/bun"
)

// Migrate applies every pending embedded Goose migration. It is safe to call
// before every server start; Goose records module versions and skips applied SQL.
func Migrate(ctx context.Context, cfg app.Config) (runErr error) {
	migrationDB, dialect, err := openMigrationDB(ctx, cfg.Bootstrap.Database)
	if err != nil {
		return err
	}
	defer migrationDB.Close()

	lock, err := acquireAdvisoryLock(ctx, migrationDB, dialect, cfg.Bootstrap.LockTimeout)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, lock.Close(context.Background()))
	}()

	if err := migrate(ctx, migrationDB); err != nil {
		return err
	}
	slog.Info("database migrations completed")
	return nil
}

// Rollback rolls one module back by one migration, or to an explicit version.
// Module selection is mandatory because each module owns a separate Goose table.
func Rollback(ctx context.Context, cfg app.Config, module string, target *int64) (runErr error) {
	migrationDB, dialect, err := openMigrationDB(ctx, cfg.Bootstrap.Database)
	if err != nil {
		return err
	}
	defer migrationDB.Close()

	lock, err := acquireAdvisoryLock(ctx, migrationDB, dialect, cfg.Bootstrap.LockTimeout)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, lock.Close(context.Background()))
	}()

	down := func(one func(context.Context, *bun.DB) error, to func(context.Context, *bun.DB, int64) error) error {
		if target == nil {
			return one(ctx, migrationDB)
		}
		return to(ctx, migrationDB, *target)
	}
	switch module {
	case "iam":
		err = down(iam.MigrateDown, iam.MigrateDownTo)
	case "organization":
		err = down(organization.MigrateDown, organization.MigrateDownTo)
	case "governance":
		err = down(governance.MigrateDown, governance.MigrateDownTo)
	case "federation":
		err = down(federation.MigrateDown, federation.MigrateDownTo)
	case "provisioning":
		err = down(provisioning.MigrateDown, provisioning.MigrateDownTo)
	case "wuid":
		err = down(wuid.MigrateDown, wuid.MigrateDownTo)
	case "dlock":
		err = down(dlock.MigrateDown, dlock.MigrateDownTo)
	default:
		return fmt.Errorf("unknown migration module %q (want dlock, wuid, iam, organization, provisioning, governance, or federation)", module)
	}
	if err != nil {
		return fmt.Errorf("rollback %s: %w", module, err)
	}
	targetValue := any("previous")
	if target != nil {
		targetValue = *target
	}
	slog.Info("database rollback completed", "module", module, "target", targetValue)
	return nil
}

// Provision reconciles the initial local principal and administrator role after
// migrations succeed. The advisory lock makes concurrent bootstrap idempotent.
func Provision(ctx context.Context, cfg app.Config) (runErr error) {
	migrationDB, dialect, err := openMigrationDB(ctx, cfg.Bootstrap.Database)
	if err != nil {
		return err
	}
	defer migrationDB.Close()

	lock, err := acquireAdvisoryLock(ctx, migrationDB, dialect, cfg.Bootstrap.LockTimeout)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, lock.Close(context.Background()))
	}()

	runtimeDB, err := openRuntimeDB(ctx, cfg.Database, dialect)
	if err != nil {
		return err
	}
	defer runtimeDB.Close()

	if err := assertRuntimeAccess(ctx, runtimeDB); err != nil {
		return err
	}

	// Provision runs before the application's guid module starts, so the
	// package-level generator is not yet installed. Lease a worker and install
	// it here or EnsureBootstrapPrincipal fails with "default generator not
	// initialized" and desktop first-launch (J1) can never create its admin.
	if err := ensureGUIDGenerator(ctx, runtimeDB); err != nil {
		return fmt.Errorf("provision guid generator: %w", err)
	}

	adminCfg := cfg.Bootstrap.InitialAdmin
	if adminCfg.TenantID != "" {
		password, err := secretx.Resolve("bootstrap.initial_admin.password", adminCfg.Password, adminCfg.PasswordFile, 4096)
		if err != nil {
			return err
		}
		tenantID, err := guid.Parse(adminCfg.TenantID)
		if err != nil {
			return fmt.Errorf("provision initial tenant id: %w", err)
		}
		principalID, err := authnmod.EnsureBootstrapPrincipal(ctx, runtimeDB, authnmod.BootstrapPrincipal{
			LoginName: adminCfg.LoginName, Password: password, DisplayName: adminCfg.DisplayName, Email: adminCfg.Email,
		}, nextGUID)
		if err != nil {
			return fmt.Errorf("provision initial principal: %w", err)
		}
		if err := bindInitialAdmin(ctx, runtimeDB, tenantID, principalID, adminCfg.DisplayName, adminCfg.Email); err != nil {
			return err
		}
	}

	slog.Info("deployment resources provisioned")
	return nil
}

// ensureGUIDGenerator installs the package-level guid generator against a
// worker leased from the runtime database, if it is not already installed.
// Provision runs before the application lifecycle starts the guid module, so
// this is what makes bootstrap-provided ids (initial admin etc.) possible.
func ensureGUIDGenerator(ctx context.Context, db *bun.DB) error {
	if guid.Default() != nil {
		return nil
	}
	worker, err := wuid.Open(ctx, db)
	if err != nil {
		return fmt.Errorf("lease bootstrap worker id: %w", err)
	}
	defer func() {
		_ = worker.Close(context.Background())
	}()
	generator, err := guid.New(worker.ID())
	if err != nil {
		return fmt.Errorf("init bootstrap guid generator: %w", err)
	}
	guid.SetDefault(generator)
	return nil
}

func openMigrationDB(ctx context.Context, datasource bunx.Datasource) (*bun.DB, string, error) {
	dialect, err := bunx.NormalizeDialect(datasource.Type)
	if err != nil {
		return nil, "", fmt.Errorf("bootstrap database type: %w", err)
	}
	db, err := datasource.Open()
	if err != nil {
		return nil, "", fmt.Errorf("open migration database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, "", errors.Join(fmt.Errorf("ping migration database: %w", err), db.Close())
	}
	return db, dialect, nil
}

func openRuntimeDB(ctx context.Context, datasources map[string]bunx.Datasource, dialect string) (*bun.DB, error) {
	var selected *bunx.Datasource
	for _, datasource := range datasources {
		if !datasource.Writable {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("production bootstrap requires exactly one writable runtime database")
		}
		datasourceCopy := datasource
		selected = &datasourceCopy
	}
	if selected == nil {
		return nil, fmt.Errorf("production bootstrap requires exactly one writable runtime database")
	}
	runtimeDialect, err := bunx.NormalizeDialect(selected.Type)
	if err != nil {
		return nil, fmt.Errorf("runtime database type: %w", err)
	}
	migrationDialect, err := bunx.NormalizeDialect(dialect)
	if err != nil {
		return nil, fmt.Errorf("migration database type: %w", err)
	}
	if runtimeDialect != migrationDialect {
		return nil, fmt.Errorf("migration and runtime database dialects differ")
	}
	db, err := selected.Open()
	if err != nil {
		return nil, fmt.Errorf("open runtime database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("ping runtime database: %w", err), db.Close())
	}
	return db, nil
}

func migrate(ctx context.Context, db *bun.DB) error {
	if err := dlock.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate dlock: %w", err)
	}
	if err := wuid.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate wuid: %w", err)
	}
	if err := iam.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate iam: %w", err)
	}
	if err := organization.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate organization: %w", err)
	}
	if err := provisioning.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate provisioning: %w", err)
	}
	if err := governance.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate governance: %w", err)
	}
	if err := federation.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate federation: %w", err)
	}
	return nil
}

func assertRuntimeAccess(ctx context.Context, db *bun.DB) error {
	if err := iam.AssertMigrated(ctx, db); err != nil {
		return fmt.Errorf("runtime database cannot read migrated IAM tables: %w", err)
	}
	if err := organization.AssertMigrated(ctx, db); err != nil {
		return fmt.Errorf("runtime database cannot read migrated organization tables: %w", err)
	}
	if err := provisioning.AssertMigrated(ctx, db); err != nil {
		return fmt.Errorf("runtime database cannot read migrated provisioning tables: %w", err)
	}
	if err := governance.AssertMigrated(ctx, db); err != nil {
		return fmt.Errorf("runtime database cannot read migrated governance tables: %w", err)
	}
	if err := federation.AssertMigrated(ctx, db); err != nil {
		return fmt.Errorf("runtime database cannot read migrated federation tables: %w", err)
	}
	return nil
}

func bindInitialAdmin(ctx context.Context, db *bun.DB, tenantID, principalID guid.ID, displayName, email string) error {
	if err := organization.EnsureTenant(ctx, db, tenantID); err != nil {
		return fmt.Errorf("ensure initial tenant: %w", err)
	}
	repo := iam.NewRepository(db, nextGUID)
	if _, err := repo.GrantPlatformAdministrator(ctx, principalID); err != nil {
		return err
	}
	if displayName == "" {
		displayName = principalID.String()
	}
	if _, err := repo.PutMember(ctx, iam.TenantMember{TenantID: tenantID, PrincipalID: principalID, DisplayName: displayName, Email: email, Status: iam.MemberActive}); err != nil {
		return fmt.Errorf("upsert initial tenant member: %w", err)
	}
	var role iam.Role
	roles, err := repo.ListRoles(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list bootstrap roles: %w", err)
	}
	for _, candidate := range roles {
		if candidate.Name == "System Administrator" {
			role = candidate
			break
		}
	}
	if role.ID.Zero() {
		role, err = repo.CreateRole(ctx, tenantID, "System Administrator", "Built-in tenant administrator")
		if err != nil {
			return fmt.Errorf("create administrator role: %w", err)
		}
	}
	for _, action := range authz.DefaultRegistry().All() {
		if action.Scope == "platform" {
			continue
		}
		if _, err := repo.GrantPermission(ctx, tenantID, role.ID, action.Code); err != nil {
			return fmt.Errorf("grant administrator permission %s: %w", action.Code, err)
		}
	}
	if _, err := repo.AddMember(ctx, tenantID, role.ID, principalID); err != nil {
		return fmt.Errorf("bind initial administrator: %w", err)
	}
	allowed, err := iam.NewAuthorizer(db).Check(ctx, tenantID, "tenant_administer", principalID)
	if err != nil {
		return fmt.Errorf("verify initial administrator: %w", err)
	}
	if !allowed {
		return fmt.Errorf("initial administrator verification was denied")
	}
	if err := ensureDefaultMenus(ctx, repo, tenantID); err != nil {
		return err
	}
	return nil
}

func ensureDefaultMenus(ctx context.Context, repo *iam.Repository, tenantID guid.ID) error {
	existing, err := repo.ListMenus(ctx, tenantID, false)
	if err != nil {
		return fmt.Errorf("list bootstrap menus: %w", err)
	}
	routes := make(map[string]struct{}, len(existing))
	for _, menu := range existing {
		routes[menu.Route] = struct{}{}
	}
	for _, menu := range iam.DefaultMenus() {
		if _, ok := routes[menu.Route]; ok {
			continue
		}
		menu.ID = 0
		menu.TenantID = tenantID
		if _, err := repo.CreateMenu(ctx, menu); err != nil {
			return fmt.Errorf("create default menu %s: %w", menu.Route, err)
		}
	}
	return nil
}

func nextGUID() (guid.ID, error) {
	id, err := guid.Next()
	return guid.ID(id), err
}
