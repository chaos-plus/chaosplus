package bunx

import (
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type Datasource struct {
	Type string `mapstructure:"type" description:"type" default:"mysql"`
	Dsn  string `mapstructure:"dsn" description:"dsn"`

	Writable bool `mapstructure:"writable" description:"writable" default:"true"`
	Readable bool `mapstructure:"readable" description:"readable" default:"true"`

	MaxOpenConns    int           `mapstructure:"max_open_conns" description:"max open conns" default:"25"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns" description:"max idle conns" default:"10"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime" description:"conn max lifetime" default:"5m"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time" description:"conn max idle time" default:"5m"`
}

func (d *Datasource) NewDB() *bun.DB {
	if d.Type == "sqlite" {
		sqldb, err := sql.Open("sqliteshim", d.Dsn)
		if err != nil {
			slog.Error("failed to open sqlite", "error", err)
			return nil
		}
		return bun.NewDB(sqldb, sqlitedialect.New())
	}
	if d.Type == "mysql" {
		sqldb, err := sql.Open("mysql", d.Dsn)
		if err != nil {
			slog.Error("failed to open mysql", "error", err)
			return nil
		}
		return bun.NewDB(sqldb, mysqldialect.New())
	}
	if d.Type == "postgres" {
		sqldb := sql.OpenDB(pgdriver.NewConnector(
			pgdriver.WithDSN(d.Dsn),
		))
		return bun.NewDB(sqldb, pgdialect.New())
	}

	return nil
}

type DatasourceRouter struct {
	Writer []*bun.DB
	Reader []*bun.DB
}

func (r *DatasourceRouter) Read() *bun.DB {
	if len(r.Reader) == 0 {
		return r.Writer[0]
	}
	tick := time.Now().Unix()
	return r.Reader[tick%int64(len(r.Reader))]
}

func (r *DatasourceRouter) Write() *bun.DB {
	tick := time.Now().Unix()
	return r.Writer[tick%int64(len(r.Writer))]
}

// Close closes every underlying writer and reader connection pool, joining any
// errors so a single failing pool does not hide the others.
func (r *DatasourceRouter) Close() error {
	var errs []error
	for _, db := range r.Writer {
		if db != nil {
			if err := db.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, db := range r.Reader {
		if db != nil {
			if err := db.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func NewDatasourceRouter(tracerName string, datasources map[string]Datasource) DatasourceRouter {
	writer := make([]*bun.DB, 0)
	reader := make([]*bun.DB, 0)
	for _, datasource := range datasources {
		db := datasource.NewDB()
		if db == nil {
			slog.Error("failed to create db", "datasource", datasource)
			continue
		}
		ApplyDebugHook(db)
		ApplySlogHook(db)
		ApplyOtelHook(db, tracerName)
		if datasource.Writable {
			writer = append(writer, db)
		}
		if datasource.Readable {
			reader = append(reader, db)
		}
	}
	return DatasourceRouter{
		Writer: writer,
		Reader: reader,
	}
}
