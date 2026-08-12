// Package authn stores the authenticated local subject in request context.
package authn

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type contextKey struct{}

// WithClaims stores verified claims in ctx.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, contextKey{}, claims)
}

// FromContext returns verified claims from ctx.
func FromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(contextKey{}).(*Claims)
	return claims, ok
}

// SubjectFromContext returns the verified token subject. Callers that require
// a human principal must also check Claims.SubjectType.
func SubjectFromContext(ctx context.Context) (string, bool) {
	claims, ok := FromContext(ctx)
	if !ok {
		return "", false
	}
	return claims.Subject, true
}

func TenantIDFromContext(ctx context.Context) guid.ID {
	claims, _ := FromContext(ctx)
	if claims == nil {
		return 0
	}
	return claims.TenantID
}

func EntityIDFromContext(ctx context.Context) guid.ID {
	claims, _ := FromContext(ctx)
	if claims == nil {
		return 0
	}
	return claims.EntityID
}

func PrincipalIDFromContext(ctx context.Context) guid.ID {
	claims, _ := FromContext(ctx)
	if claims == nil {
		return 0
	}
	return claims.PrincipalID
}
