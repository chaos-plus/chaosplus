package iam

import (
	"context"
	"fmt"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

func (s *Service) AuthorizationConstraint(ctx context.Context, tenantID guid.ID, permission string, subject guid.ID) (authz.DataConstraint, error) {
	permission = strings.TrimSpace(permission)
	if err := s.validateDataPermission(permission); err != nil {
		return authz.DataConstraint{}, err
	}
	return s.checker.Constraint(ctx, tenantID, permission, subject)
}

func (s *Service) ExplainEntityAuthorization(ctx context.Context, tenantID, entityID guid.ID, permission string, subject guid.ID) (authz.Explanation, error) {
	permission = strings.TrimSpace(permission)
	if err := s.validateDataPermission(permission); err != nil {
		return authz.Explanation{}, err
	}
	return s.checker.ExplainEntity(ctx, tenantID, entityID, permission, subject)
}

func (s *Service) CheckEntityAuthorization(ctx context.Context, tenantID, entityID guid.ID, permission string, subject guid.ID) (authz.Explanation, error) {
	return s.ExplainEntityAuthorization(ctx, tenantID, entityID, permission, subject)
}

func (s *Service) ExplainResourceAuthorization(ctx context.Context, tenantID, entityID guid.ID, resourceType string, resourceID guid.ID, permission string, subject guid.ID) (authz.Explanation, error) {
	permission, resourceType = strings.TrimSpace(permission), strings.TrimSpace(resourceType)
	if err := s.validateDataPermission(permission); err != nil {
		return authz.Explanation{}, err
	}
	action, _ := s.registry.Find(permission)
	if action.Resource != resourceType || entityID.Zero() || resourceID.Zero() {
		return authz.Explanation{}, fmt.Errorf("%w: permission and business resource type must match", domain.ErrInvalidResourceAuthorization)
	}
	return s.checker.ExplainResource(ctx, tenantID, entityID, resourceType, resourceID, permission, subject)
}

func (s *Service) CheckResourceAuthorization(ctx context.Context, tenantID, entityID guid.ID, resourceType string, resourceID guid.ID, permission string, subject guid.ID) (authz.Explanation, error) {
	return s.ExplainResourceAuthorization(ctx, tenantID, entityID, resourceType, resourceID, permission, subject)
}

func (s *Service) validateDataPermission(permission string) error {
	action, ok := s.registry.Find(permission)
	if !ok {
		return fmt.Errorf("%w: %s", domain.ErrPermissionNotFound, permission)
	}
	if !action.DataScoped {
		return fmt.Errorf("%w: permission %s is not data scoped", domain.ErrInvalidArgument, permission)
	}
	return nil
}
