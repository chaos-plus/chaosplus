package iam

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIamI18nRegistration(t *testing.T) {
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	for _, locale := range []string{"en-US", "zh-CN", "ms-MY"} {
		message := i18n.TContext(i18n.WithLocale(context.Background(), locale), "iam_role_name_conflict")
		assert.NotEmpty(t, message, locale)
		assert.NotEqual(t, "iam_role_name_conflict", message, locale)
	}
}
