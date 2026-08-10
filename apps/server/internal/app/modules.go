package app

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/geoip"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/audit"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/federation"
	"github.com/chaos-plus/chaosplus/internal/modules/governance"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/oauth"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/chaos-plus/chaosplus/internal/modules/provisioning"
)

// buildModules is the composition root: the single place that constructs the
// application's modules and their dependencies, in registration order. Adding a
// feature means adding a module here - nothing else in the app changes.
func (app *App) buildModules() []any {
	mods := make([]any, 0, 9)

	if app.claimPlugins != nil {
		mods = append(mods, app.claimPlugins)
	}

	// Identity generation needs a writable database. Skipped when none exists so
	// the app can still serve endpoints that don't need one.
	if len(app.dbr.Writer) > 0 {
		mods = append(mods, guid.NewModule(app.dbr.Write(), time.Duration(app.cfg.WorkerLease)*time.Second))
	} else {
		slog.Warn("no writable database; skipping id generator")
	}

	if app.cfg.Authn.Enabled {
		mods = append(mods, authnmod.NewModule(app.authnRequest, app.authnWeb))
		if app.authnWeb != nil {
			mods = append(mods, oauth.NewModule(app.dbr.Write(), app.authnWeb, app.authzRegistrar))
		}
	}
	if app.authzRegistrar != nil {
		if app.authzRegistrar.IsDeclarationOnly() {
			mods = append(mods, audit.NewDeclarationOnlyModule(app.authzRegistrar))
			mods = append(mods, iam.NewDeclarationOnlyModule(app.authzRegistrar))
			mods = append(mods, organization.NewDeclarationOnlyModule(app.authzRegistrar))
			mods = append(mods, provisioning.NewDeclarationOnlyModule(app.authzRegistrar))
			mods = append(mods, governance.NewDeclarationOnlyModule(app.authzRegistrar))
			mods = append(mods, federation.NewDeclarationOnlyModule(app.authzRegistrar))
		} else {
			auditTrail := audit.NewService(app.dbr.Write())
			appendAudit := auditAppender(auditTrail)
			administratorGuard := iam.NewAdministratorGuard()
			dialect := app.dbr.Write().Dialect().Name().String()
			if dialect == "pg" {
				dialect = "postgres"
			}
			nextID := func() (string, error) {
				id, err := guid.Next()
				return strconv.FormatInt(id, 10), err
			}
			identityService := identity.NewService(app.dbr.Write(), appendAudit, administratorGuard)
			mods = append(mods, identity.NewModuleWithService(identityService, app.authzRegistrar))
			mods = append(mods, iam.NewModule(app.dbr.Write(), app.authzRegistrar, iam.NewAuthorizer(app.dbr.Write()), appendAudit, nextID))
			createInvitedPrincipal := func(ctx context.Context, db bun.IDB, tenantID, loginName, password, displayName, email string) (string, error) {
				id, err := identityService.CreateInvitedPrincipal(ctx, db, tenantID, loginName, password, displayName, email)
				switch {
				case errors.Is(err, identity.ErrLoginConflict):
					return "", organization.ErrInvitationLoginConflict
				case errors.Is(err, identity.ErrInvalid):
					return "", organization.ErrInvitationInvalid
				default:
					return id, err
				}
			}
			organizationModule := organization.NewModule(app.dbr.Write(), app.authzRegistrar, appendAudit, iam.NewMembershipChecker(app.dbr.Write()), administratorGuard, nextID, app.authnWeb, createInvitedPrincipal)
			mods = append(mods, organizationModule)
			mods = append(mods, provisioning.NewModule(app.dbr.Write(), app.authzRegistrar, appendAudit, nextID, identityService, organizationModule.ProvisioningGroups(), app.cfg.Provisioning, app.provisioningKey))
			mods = append(mods, governance.NewModule(app.dbr.Write(), app.authzRegistrar, appendAudit, governance.RoleGrantStore{
				Grant: iam.GrantTemporaryRole, Revoke: iam.RevokeTemporaryRole,
				RemovePermanent: func(ctx context.Context, db bun.IDB, tenantID, roleID, principalID string, createdAt int64) (bool, error) {
					changed, err := administratorGuard.RemoveRoleMember(ctx, db, dialect, tenantID, roleID, principalID, createdAt)
					if errors.Is(err, iam.ErrLastTenantAdministrator) {
						return false, governance.ErrReviewLastAdministrator
					}
					return changed, err
				},
				RemoveGroupMembership:    guardGroupPositionRemove(administratorGuard, dialect, iam.RemoveGroupMembership),
				RemovePositionMembership: guardGroupPositionRemove(administratorGuard, dialect, iam.RemovePositionMembership),
				RemoveEntityRoleBinding:  guardEntityRoleRemove(administratorGuard, dialect, iam.RemoveEntityRoleBinding),
			}, nextID))
			mods = append(mods, audit.NewModule(app.dbr.Write(), app.authzRegistrar, app.cfg.Audit))
			if app.cfg.Federation.Enabled && app.authnWeb != nil {
				mods = append(mods, federation.NewModule(app.dbr.Write(), app.authzRegistrar, appendAudit, identityService, app.authnWeb, app.cfg.Federation, app.federationKey))
			}
		}
	} else {
		slog.Warn("authorization stack disabled; skipping iam management API")
	}
	mods = append(mods, geoip.NewModule(app.cfg.GeoIP))

	return mods
}

func auditAppender(service *audit.Service) auditx.Appender {
	return func(ctx context.Context, db bun.IDB, event auditx.Event) error {
		_, err := service.AppendTo(ctx, db, audit.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID,
			EventType: event.EventType, TargetType: event.TargetType, TargetID: event.TargetID,
			Outcome: "success", Detail: event.Detail,
		})
		return err
	}
}

func registrationPrincipalCreator(ctx context.Context, db bun.IDB, email, passwordHash, displayName string, now time.Time) (string, error) {
	id, err := identity.CreatePendingPrincipal(ctx, db, email, passwordHash, displayName, now)
	switch {
	case errors.Is(err, identity.ErrLoginConflict):
		return "", authnext.ErrRegistrationConflict
	case errors.Is(err, identity.ErrInvalid):
		return "", authnext.ErrInvalidRegistration
	default:
		return id, err
	}
}

// guardGroupPositionRemove wraps a group or position membership removal with the
// administrator guard so an access review cannot revoke the last tenant
// administrator through a derived grant. The underlying removal function is
// called only after the guard confirms a durable administrator remains.
func guardGroupPositionRemove(
	guard *iam.AdministratorGuard, dialect string,
	remove func(context.Context, bun.IDB, string, string, string) (bool, error),
) func(context.Context, bun.IDB, string, string, string) (bool, error) {
	return func(ctx context.Context, db bun.IDB, tenantID, targetID, principalID string) (bool, error) {
		verify, err := guard.Protect(ctx, db, dialect, tenantID)
		if err != nil {
			return false, err
		}
		changed, err := remove(ctx, db, tenantID, targetID, principalID)
		if err != nil || !changed {
			return changed, err
		}
		if err := verify(); err != nil {
			if errors.Is(err, iam.ErrLastTenantAdministrator) {
				return false, governance.ErrReviewLastAdministrator
			}
			return false, err
		}
		return true, nil
	}
}

// guardEntityRoleRemove wraps an entity role binding removal with the
// administrator guard so an access review cannot revoke the last tenant
// administrator through a scoped entity role binding.
func guardEntityRoleRemove(
	guard *iam.AdministratorGuard, dialect string,
	remove func(context.Context, bun.IDB, string, string, string, string) (bool, error),
) func(context.Context, bun.IDB, string, string, string, string) (bool, error) {
	return func(ctx context.Context, db bun.IDB, tenantID, entityID, roleID, principalID string) (bool, error) {
		verify, err := guard.Protect(ctx, db, dialect, tenantID)
		if err != nil {
			return false, err
		}
		changed, err := remove(ctx, db, tenantID, entityID, roleID, principalID)
		if err != nil || !changed {
			return changed, err
		}
		if err := verify(); err != nil {
			if errors.Is(err, iam.ErrLastTenantAdministrator) {
				return false, governance.ErrReviewLastAdministrator
			}
			return false, err
		}
		return true, nil
	}
}
