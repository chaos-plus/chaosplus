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
	"github.com/uptrace/bun/schema"
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
	var sqldb *sql.DB
	var dialect schema.Dialect

	switch d.Type {
	case "sqlite":
		conn, err := sql.Open("sqliteshim", d.Dsn)
		if err != nil {
			slog.Error("failed to open sqlite", "error", err)
			return nil
		}
		sqldb, dialect = conn, sqlitedialect.New()
	case "mysql":
		conn, err := sql.Open("mysql", d.Dsn)
		if err != nil {
			slog.Error("failed to open mysql", "error", err)
			return nil
		}
		sqldb, dialect = conn, mysqldialect.New()
	case "postgres":
		sqldb = sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(d.Dsn)))
		dialect = pgdialect.New()
	default:
		slog.Error("unsupported datasource type", "type", d.Type)
		return nil
	}

	d.applyPool(sqldb)
	return bun.NewDB(sqldb, dialect)
}

// applyPool applies the configured connection-pool limits. Each is applied only
// when set (> 0) so a zero value leaves database/sql's default in place rather
// than, say, capping the pool at zero connections.
func (d *Datasource) applyPool(sqldb *sql.DB) {
	if d.MaxOpenConns > 0 {
		sqldb.SetMaxOpenConns(d.MaxOpenConns)
	}
	if d.MaxIdleConns > 0 {
		sqldb.SetMaxIdleConns(d.MaxIdleConns)
	}
	if d.ConnMaxLifetime > 0 {
		sqldb.SetConnMaxLifetime(d.ConnMaxLifetime)
	}
	if d.ConnMaxIdleTime > 0 {
		sqldb.SetConnMaxIdleTime(d.ConnMaxIdleTime)
	}
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

func NewDatasourceRouter(tracerName string, debug bool, datasources map[string]Datasource) DatasourceRouter {
	writer := make([]*bun.DB, 0)
	reader := make([]*bun.DB, 0)
	for _, datasource := range datasources {
		db := datasource.NewDB()
		if db == nil {
			slog.Error("failed to create db", "datasource", datasource)
			continue
		}
		// Verbose per-query logging is opt-in via debug mode; it is far too noisy
		// (and adds overhead) for production.
		if debug {
			ApplyDebugHook(db)
		}
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
