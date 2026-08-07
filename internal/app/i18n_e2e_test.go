package app

import (
	"testing"

	"github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/federation"
	"github.com/chaos-plus/chaosplus/internal/modules/provisioning"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
)

// TestRegisteredLocalesResolveKeyMessages verifies that once the global i18n
// instance has been initialized and the module bundles registered (mirroring
// the bootstrap ordering), error keys used by handlers resolve to a real
// translated message instead of falling back to the raw key.
func TestRegisteredLocalesResolveKeyMessages(t *testing.T) {
	if err := i18n.Init("en-US", "../../pkg/i18n/locales"); err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	for _, register := range []func() error{
		audit.RegisterI18n,
		federation.RegisterI18n,
		provisioning.RegisterI18n,
	} {
		if err := register(); err != nil {
			t.Fatalf("register locales: %v", err)
		}
	}

	for _, locale := range []string{"en-US", "zh-CN", "ms-MY"} {
		i18n.SetLocale(locale)
		for _, key := range []string{
			"federation_provider_disabled",
			"federation_saml_invalid_request",
			"federation_saml_sp_not_found",
			"scim_target_disabled",
			"scim_target_not_found",
			"audit_retention_invalid",
			"not_found",
		} {
			if got := i18n.T(key); got == key {
				t.Errorf("[%s] key %q fell back to the raw key (no translation)", locale, key)
			} else if placeholderMark.MatchString(got) {
				t.Errorf("[%s] key %q resolved to a placeholder: %q", locale, key, got)
			}
		}
	}
}
