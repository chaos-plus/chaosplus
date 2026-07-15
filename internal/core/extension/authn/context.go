// Package authn verifies Zitadel/OIDC bearer tokens and stores the authenticated
// subject in request context. Authorization remains separate and is checked
// against SpiceDB.
package authn

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
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

// SubjectFromContext returns the SpiceDB subject represented by the token.
func SubjectFromContext(ctx context.Context) (spicedbx.SubjectRef, bool) {
	claims, ok := FromContext(ctx)
	if !ok {
		return spicedbx.SubjectRef{}, false
	}
	return claims.SubjectRef(), true
}
