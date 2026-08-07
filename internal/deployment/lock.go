package deployment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/uptrace/bun"
)

const (
	postgresLockID int64 = 0x4348414f53504c55
	mysqlLockName        = "chaosplus-production-bootstrap-v1"
)

// SQLite is an embedded database and the supported deployment model has one
// application process per database file. Serialize deployment operations in
// that process without holding a SQLite write transaction, which would block
// Goose when it obtains a different pooled connection.
var sqliteDeploymentLock = make(chan struct{}, 1)

type advisoryLock struct {
	conn    *sql.Conn
	dialect string
	once    sync.Once
}

func acquireAdvisoryLock(ctx context.Context, db *bun.DB, dialect string, timeout time.Duration) (*advisoryLock, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	lockCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if dialect == "sqlite" {
		select {
		case sqliteDeploymentLock <- struct{}{}:
			return &advisoryLock{dialect: dialect}, nil
		case <-lockCtx.Done():
			return nil, fmt.Errorf("acquire sqlite bootstrap lock: %w", lockCtx.Err())
		}
	}
	conn, err := db.DB.Conn(lockCtx)
	if err != nil {
		return nil, fmt.Errorf("open bootstrap lock connection: %w", err)
	}
	lock := &advisoryLock{conn: conn, dialect: dialect}
	var acquired bool
	switch dialect {
	case "postgres":
		for !acquired {
			if err := conn.QueryRowContext(lockCtx, "SELECT pg_try_advisory_lock($1)", postgresLockID).Scan(&acquired); err != nil {
				return nil, errors.Join(fmt.Errorf("acquire postgres bootstrap lock: %w", err), conn.Close())
			}
			if !acquired {
				select {
				case <-lockCtx.Done():
					return nil, errors.Join(fmt.Errorf("acquire postgres bootstrap lock: %w", lockCtx.Err()), conn.Close())
				case <-time.After(250 * time.Millisecond):
				}
			}
		}
	case "mysql":
		seconds := int(math.Ceil(timeout.Seconds()))
		var result sql.NullInt64
		if err := conn.QueryRowContext(lockCtx, "SELECT GET_LOCK(?, ?)", mysqlLockName, seconds).Scan(&result); err != nil {
			return nil, errors.Join(fmt.Errorf("acquire mysql bootstrap lock: %w", err), conn.Close())
		}
		acquired = result.Valid && result.Int64 == 1
	default:
		return nil, errors.Join(fmt.Errorf("unsupported bootstrap database dialect %q", dialect), conn.Close())
	}
	if !acquired {
		return nil, errors.Join(fmt.Errorf("bootstrap lock was not acquired"), conn.Close())
	}
	return lock, nil
}

func (l *advisoryLock) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if l.dialect == "sqlite" {
		l.once.Do(func() { <-sqliteDeploymentLock })
		return nil
	}
	if l.conn == nil {
		return nil
	}
	var err error
	switch l.dialect {
	case "postgres":
		var released bool
		err = l.conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", postgresLockID).Scan(&released)
		if err == nil && !released {
			err = fmt.Errorf("postgres bootstrap lock was not held")
		}
	case "mysql":
		var released sql.NullInt64
		err = l.conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", mysqlLockName).Scan(&released)
		if err == nil && (!released.Valid || released.Int64 != 1) {
			err = fmt.Errorf("mysql bootstrap lock was not held")
		}
	}
	closeErr := l.conn.Close()
	if err != nil {
		return err
	}
	return closeErr
}
