package iam

import (
	"context"
	"fmt"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

func (s *Service) AuthorizationConstraint(ctx context.Context, tenantID, permission, subject string) (authz.DataConstraint, error) {
	permission, subject = strings.TrimSpace(permission), strings.TrimSpace(subject)
	if err := s.validateDataPermission(permission); err != nil {
		return authz.DataConstraint{}, err
	}
	return s.checker.Constraint(ctx, tenantID, permission, subject)
}

func (s *Service) ExplainEntityAuthorization(ctx context.Context, tenantID, entityID, permission, subject string) (authz.Explanation, error) {
	permission, subject, entityID = strings.TrimSpace(permission), strings.TrimSpace(subject), strings.TrimSpace(entityID)
	if err := s.validateDataPermission(permission); err != nil {
		return authz.Explanation{}, err
	}
	return s.checker.ExplainEntity(ctx, tenantID, entityID, permission, subject)
}

func (s *Service) CheckEntityAuthorization(ctx context.Context, tenantID, entityID, permission, subject string) (authz.Explanation, error) {
	return s.ExplainEntityAuthorization(ctx, tenantID, entityID, permission, subject)
}

func (s *Service) ExplainResourceAuthorization(ctx context.Context, tenantID, entityID, resourceType, resourceID, permission, subject string) (authz.Explanation, error) {
	permission, subject = strings.TrimSpace(permission), strings.TrimSpace(subject)
	entityID, resourceType, resourceID = strings.TrimSpace(entityID), strings.TrimSpace(resourceType), strings.TrimSpace(resourceID)
	if err := s.validateDataPermission(permission); err != nil {
		return authz.Explanation{}, err
	}
	action, _ := s.registry.Find(permission)
	if action.Resource != resourceType || entityID == "" || resourceID == "" {
		return authz.Explanation{}, fmt.Errorf("%w: permission and business resource type must match", domain.ErrInvalidResourceAuthorization)
	}
	return s.checker.ExplainResource(ctx, tenantID, entityID, resourceType, resourceID, permission, subject)
}

func (s *Service) CheckResourceAuthorization(ctx context.Context, tenantID, entityID, resourceType, resourceID, permission, subject string) (authz.Explanation, error) {
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
