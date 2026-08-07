package respx

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/chaos-plus/chaosplus/pkg/i18n"
)

// WriteError writes a localized {code,message,meta,data} error envelope directly
// to w. Use it from chi middleware that runs before the huma stack — where
// huma.NewError (patched by Install) is not in play — so those responses keep the
// same shape and localization as handler errors. key is an i18n key resolved from
// the request locale set by the Locale middleware; retryAfter, when > 0, sets the
// Retry-After header (whole seconds, rounded up).
func WriteError(w http.ResponseWriter, r *http.Request, status int, key string, retryAfter time.Duration) {
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
	}
	body := errorEnvelope{
		status:  status,
		Code:    status,
		Message: i18n.TContext(r.Context(), key),
		Meta:    Meta{RequestAt: time.Now().UTC()},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
