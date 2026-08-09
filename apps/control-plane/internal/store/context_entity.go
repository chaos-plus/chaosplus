package store

import "context"

type entityKey struct{}

// WithEntity binds the caller's instance(实体) scope to the context.
func WithEntity(ctx context.Context, entityID string) context.Context {
	return context.WithValue(ctx, entityKey{}, entityID)
}

// EntityOf returns the caller's instance scope ("" = unbound → no filter).
func EntityOf(ctx context.Context) string {
	e, _ := ctx.Value(entityKey{}).(string)
	return e
}
