package authz

import (
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

const TenantHeader = "X-Tenant-Id"

// TenantIDFunc extracts the active tenant/scope from a request. The default
// implementation reads X-Tenant-Id until signed session scope lands.
type TenantIDFunc func(*http.Request) string

func HeaderTenantID(r *http.Request) string {
	return r.Header.Get(TenantHeader)
}

// TenantMiddleware enforces a catalog guard against tenant:<id> in SpiceDB. It
// assumes authentication middleware already placed verified claims in context.
func TenantMiddleware(checker spicedbx.Client, guard Guard, tenantID TenantIDFunc) func(http.Handler) http.Handler {
	if tenantID == nil {
		tenantID = HeaderTenantID
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject, ok := authn.SubjectFromContext(r.Context())
			if !ok {
				respx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", 0)
				return
			}
			tid := tenantID(r)
			if tid == "" {
				respx.WriteError(w, r, http.StatusForbidden, "forbidden", 0)
				return
			}
			allowed, err := checker.Check(r.Context(), spicedbx.ObjectRef{Type: "tenant", ID: tid}, guard.Code(), subject, "")
			if err != nil || !allowed {
				respx.WriteError(w, r, http.StatusForbidden, "forbidden", 0)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
