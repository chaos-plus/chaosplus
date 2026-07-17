package deployment

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/uptrace/bun"
)

const (
	postgresLockID int64 = 0x4348414f53504c55
	mysqlLockName        = "chaosplus-production-bootstrap-v1"
)

type advisoryLock struct {
	conn    *sql.Conn
	dialect string
}

func acquireAdvisoryLock(ctx context.Context, db *bun.DB, dialect string, timeout time.Duration) (*advisoryLock, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	lockCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
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
				conn.Close()
				return nil, fmt.Errorf("acquire postgres bootstrap lock: %w", err)
			}
			if !acquired {
				select {
				case <-lockCtx.Done():
					conn.Close()
					return nil, fmt.Errorf("acquire postgres bootstrap lock: %w", lockCtx.Err())
				case <-time.After(250 * time.Millisecond):
				}
			}
		}
	case "mysql":
		seconds := int(math.Ceil(timeout.Seconds()))
		var result sql.NullInt64
		if err := conn.QueryRowContext(lockCtx, "SELECT GET_LOCK(?, ?)", mysqlLockName, seconds).Scan(&result); err != nil {
			conn.Close()
			return nil, fmt.Errorf("acquire mysql bootstrap lock: %w", err)
		}
		acquired = result.Valid && result.Int64 == 1
	default:
		conn.Close()
		return nil, fmt.Errorf("unsupported bootstrap database dialect %q", dialect)
	}
	if !acquired {
		conn.Close()
		return nil, fmt.Errorf("bootstrap lock was not acquired")
	}
	return lock, nil
}

func (l *advisoryLock) Close(ctx context.Context) error {
	if l == nil || l.conn == nil {
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
