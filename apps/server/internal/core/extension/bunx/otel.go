package bunx

import (
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/extra/bunotel"
	"go.opentelemetry.io/otel"
)

func ApplyOtelHook(db *bun.DB, tracerName string) {
	db.AddQueryHook(bunotel.NewQueryHook(
		bunotel.WithDBName(tracerName),
		bunotel.WithTracerProvider(otel.GetTracerProvider()),
	))
}
