package bunx

import (
	"fmt"
	"strings"
)

// NowMillisExpr returns a SQL expression that evaluates to the database server's
// current time as Unix milliseconds (BIGINT), for the given bun dialect name
// (db.Dialect().Name().String()).
//
// Lease-based coordinators such as dlock and wuid embed this in their queries so
// expiry is judged by the database clock rather than each node's local clock —
// nodes with skewed clocks then never reclaim a lease early. The expression is
// dialect-specific because SQL has no portable "now in milliseconds".
//
// An unsupported dialect panics: it means the process is wired to a database it
// was never built to coordinate on, a configuration error that must fail fast
// rather than silently compute against the wrong clock.
func NowMillisExpr(dialect string) string {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "sqlite", "sqlite3":
		// julianday('now') is UTC with sub-second precision; 2440587.5 is the
		// Julian date of the Unix epoch; *86400000 converts days to millis.
		return "CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)"
	case "mysql":
		// NOW(3) carries millisecond precision; UNIX_TIMESTAMP yields seconds.
		return "CAST(ROUND(UNIX_TIMESTAMP(NOW(3)) * 1000) AS SIGNED)"
	case "pg", "postgres", "postgresql":
		// clock_timestamp() is the real wall clock (it advances within a
		// transaction), unlike now()/transaction_timestamp().
		return "CAST(EXTRACT(EPOCH FROM clock_timestamp()) * 1000 AS BIGINT)"
	default:
		panic(fmt.Sprintf("bunx: NowMillisExpr unsupported dialect %q", dialect))
	}
}
