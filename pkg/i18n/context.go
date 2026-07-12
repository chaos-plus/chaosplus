package i18n

import "context"

type ctxKey struct{}

// WithLocale returns a context with the given locale.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, ctxKey{}, locale)
}

// LocaleFromContext returns the locale from context, or empty string if not set.
func LocaleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}
