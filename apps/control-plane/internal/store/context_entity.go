package store

import "context"

type entityKey struct{}
type ownerKey struct{}

// WithEntity binds the caller's instance(实体) scope to the context.
func WithEntity(ctx context.Context, entityID string) context.Context {
	return context.WithValue(ctx, entityKey{}, entityID)
}

// EntityOf returns the caller's instance scope ("" = unbound → no filter).
func EntityOf(ctx context.Context) string {
	e, _ := ctx.Value(entityKey{}).(string)
	return e
}

// WithOwner binds the calling human(subject) to the context.
func WithOwner(ctx context.Context, ownerID string) context.Context {
	return context.WithValue(ctx, ownerKey{}, ownerID)
}

// OwnerOf returns the calling human ("" = unbound → no filter).
func OwnerOf(ctx context.Context) string {
	o, _ := ctx.Value(ownerKey{}).(string)
	return o
}
