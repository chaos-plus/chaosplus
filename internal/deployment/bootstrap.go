package deployment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
	"github.com/chaos-plus/chaosplus/internal/infra/dlock"
	"github.com/chaos-plus/chaosplus/internal/infra/wuid"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/uptrace/bun"
)

type identity struct {
	Subject     string
	DisplayName string
	Email       string
}

func Run(ctx context.Context, cfg app.Config) (runErr error) {
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

	if err := migrate(ctx, migrationDB); err != nil {
		return err
	}
	if err := assertRuntimeAccess(ctx, runtimeDB); err != nil {
		return err
	}

	var provisioner *zitadelProvisioner
	if cfg.Bootstrap.Zitadel.Enabled {
		provisioner, err = newZitadelProvisioner(ctx, cfg.Authn.Issuer, cfg.Bootstrap.Zitadel)
		if err != nil {
			return err
		}
		defer provisioner.Close()
		resources, err := provisioner.EnsureResources(ctx)
		if err != nil {
			return fmt.Errorf("provision Zitadel resources: %w", err)
		}
		if err := authn.WriteRuntimeResources(cfg.Bootstrap.Zitadel.ResourcesOutputFile, resources); err != nil {
			return err
		}
	}

	var spice *spicedbx.AuthzedClient
	if cfg.Authz.SpiceDB.Enabled {
		spice, err = spicedbx.Open(cfg.Authz.SpiceDB)
		if err != nil {
			return fmt.Errorf("connect SpiceDB: %w", err)
		}
		defer spice.Close()
		// Bootstrap owns schema rollout; apply_schema only controls API startup.
		if _, err := spice.WriteSchema(ctx, authz.GenerateSchema(authz.DefaultRegistry().All())); err != nil {
			return fmt.Errorf("apply SpiceDB schema: %w", err)
		}
	}

	adminCfg := cfg.Bootstrap.InitialAdmin
	if adminCfg.TenantID != "" {
		if spice == nil {
			return fmt.Errorf("initial admin requires SpiceDB to be enabled")
		}
		admin := identity{Subject: strings.TrimSpace(adminCfg.Subject), DisplayName: strings.TrimSpace(adminCfg.DisplayName), Email: strings.TrimSpace(adminCfg.Email)}
		if admin.Subject == "" {
			if provisioner == nil {
				return fmt.Errorf("initial admin subject is required when Zitadel provisioning is disabled")
			}
			admin, err = provisioner.FindHuman(ctx, adminCfg.LoginName)
			if err != nil {
				return fmt.Errorf("resolve initial admin: %w", err)
			}
		}
		if admin.DisplayName == "" {
			admin.DisplayName = admin.Subject
		}
		if err := bindInitialAdmin(ctx, runtimeDB, spice, adminCfg.TenantID, admin); err != nil {
			return err
		}
	}

	slog.Info("production bootstrap completed")
	return nil
}

func openMigrationDB(ctx context.Context, datasource bunx.Datasource) (*bun.DB, string, error) {
	if datasource.Type != "postgres" && datasource.Type != "mysql" {
		return nil, "", fmt.Errorf("bootstrap database type must be postgres or mysql")
	}
	db, err := datasource.Open()
	if err != nil {
		return nil, "", fmt.Errorf("open migration database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, "", fmt.Errorf("ping migration database: %w", err)
	}
	return db, datasource.Type, nil
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
		copy := datasource
		selected = &copy
	}
	if selected == nil {
		return nil, fmt.Errorf("production bootstrap requires exactly one writable runtime database")
	}
	if selected.Type != dialect {
		return nil, fmt.Errorf("migration and runtime database dialects differ")
	}
	db, err := selected.Open()
	if err != nil {
		return nil, fmt.Errorf("open runtime database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping runtime database: %w", err)
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
	return nil
}

func assertRuntimeAccess(ctx context.Context, db *bun.DB) error {
	if err := iam.AssertMigrated(ctx, db); err != nil {
		return fmt.Errorf("runtime database cannot read migrated IAM tables: %w", err)
	}
	return nil
}

func bindInitialAdmin(ctx context.Context, db *bun.DB, spice spicedbx.Client, tenantID string, admin identity) error {
	repo := iam.NewRepository(db, func() (string, error) { return "", fmt.Errorf("ID generation is unavailable during bootstrap") })
	if _, err := repo.PutMember(ctx, iam.TenantMember{TenantID: tenantID, Subject: admin.Subject, DisplayName: admin.DisplayName, Email: admin.Email, Status: iam.MemberActive}); err != nil {
		return fmt.Errorf("upsert initial tenant member: %w", err)
	}
	rel := spicedbx.Relationship{
		Resource: spicedbx.ObjectRef{Type: "tenant", ID: tenantID},
		Relation: "admin",
		Subject:  spicedbx.SubjectRef{Object: spicedbx.ObjectRef{Type: "user", ID: admin.Subject}},
	}
	token, err := spice.WriteRelationships(ctx, []spicedbx.Relationship{rel})
	if err != nil {
		return fmt.Errorf("write initial admin relationship: %w", err)
	}
	allowed, err := spice.Check(ctx, rel.Resource, "administer", rel.Subject, token)
	if err != nil {
		return fmt.Errorf("verify initial admin relationship: %w", err)
	}
	if !allowed {
		return fmt.Errorf("initial admin relationship verification was denied")
	}
	return nil
}
