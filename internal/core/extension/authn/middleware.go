package authn

import (
	"errors"
	"net/http"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
)

var (
	ErrMissingBearer = errors.New("missing bearer token")
	ErrInvalidBearer = errors.New("invalid bearer token")
)

// Middleware verifies Authorization: Bearer tokens and injects claims into the
// request context. Invalid or missing tokens fail closed with 401.
func Middleware(verifier *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := verifier.VerifyAuthorization(r.Context(), r.Header.Get("Authorization"))
			if err != nil {
				respx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", 0)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
		})
	}
}

func bearerToken(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", ErrMissingBearer
	}
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return "", ErrInvalidBearer
	}
	return fields[1], nil
}
