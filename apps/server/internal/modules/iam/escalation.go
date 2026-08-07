package iam

import (
	"context"
	"fmt"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
)

// requireGrantablePermissions refuses a write that would hand out a permission
// the acting principal does not itself hold. Without it, role_grant_permission,
// role_manage_member, role_manage_assignee, and entity_manage_binding are each
// silently equivalent to tenant_administer: the holder can grant itself every
// tenant permission and take over the tenant.
//
// Requests without verified claims are system flows (deployment bootstrap,
// migrations) that run before any principal exists, so they are exempt. Every
// HTTP path authenticates before reaching a service method.
func (s *Service) requireGrantablePermissions(ctx context.Context, tenantID string, codes ...string) error {
	claims, ok := authnext.FromContext(ctx)
	if !ok || claims == nil || claims.Subject == "" {
		return nil
	}
	wanted := make([]string, 0, len(codes))
	seen := map[string]bool{}
	for _, code := range codes {
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		wanted = append(wanted, code)
	}
	if len(wanted) == 0 {
		return nil
	}
	allowed, err := s.checker.CheckBulk(ctx, tenantID, wanted, claims.Subject)
	if err != nil {
		return err
	}
	for _, code := range wanted {
		if !allowed[code] {
			return fmt.Errorf("%w: %s", ErrPrivilegeEscalation, code)
		}
	}
	return nil
}

// requireGrantableRole applies requireGrantablePermissions to every permission
// a role currently carries. Assigning a principal to a role, or binding a group
// or position to it, confers exactly that permission set.
func (s *Service) requireGrantableRole(ctx context.Context, tenantID, roleID string) error {
	if claims, ok := authnext.FromContext(ctx); !ok || claims == nil || claims.Subject == "" {
		return nil
	}
	codes, err := s.repo.ListPermissions(ctx, tenantID, roleID)
	if err != nil {
		return err
	}
	return s.requireGrantablePermissions(ctx, tenantID, codes...)
}
