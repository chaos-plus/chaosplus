package bunx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNowMillisExpressions(t *testing.T) {
	for _, dialect := range []string{"sqlite", "sqlite3", "mysql", "pg", "pgsql", "postgres", "postgresql"} {
		assert.NotEmpty(t, NowMillisExpr(dialect), dialect)
	}
	assert.Panics(t, func() { NowMillisExpr("unsupported") })
}
