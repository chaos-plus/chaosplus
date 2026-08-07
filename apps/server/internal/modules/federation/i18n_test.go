package federation

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFederationI18nRegistration(t *testing.T) {
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	for _, locale := range []string{"en-US", "zh-CN", "ms-MY"} {
		message := i18n.TContext(i18n.WithLocale(context.Background(), locale), "federation_unavailable")
		assert.NotEmpty(t, message, locale)
		assert.NotEqual(t, "federation_unavailable", message, locale)
	}
	base := i18n.TContext(i18n.WithLocale(context.Background(), "en-US"), "federation_unavailable")
	translated := i18n.TContext(i18n.WithLocale(context.Background(), "zh-CN"), "federation_unavailable")
	assert.NotEqual(t, base, translated)
}
