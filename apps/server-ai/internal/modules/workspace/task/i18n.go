package task

import (
	"embed"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
)

//go:embed i18n/locales/*.json
var locales embed.FS

func RegisterI18n() error { return i18n.RegisterFS(locales, "i18n/locales") }
