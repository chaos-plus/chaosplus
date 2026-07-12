package app

import "github.com/chaos-plus/chaosplus/internal/core/extension/bunx"

func (app *App) SetupDebug() {
	if !app.cfg.Debug {
		return
	}

	if len(app.cfg.Database) != 0 {
		return
	}

	app.cfg.Database = map[string]bunx.Datasource{
		"debug": {Type: "sqlite", Dsn: ":memory:", Writable: true, Readable: true, MaxOpenConns: 1, MaxIdleConns: 1},
	}
}
