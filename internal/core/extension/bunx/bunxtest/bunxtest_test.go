package bunxtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryDatabaseIsUsable(t *testing.T) {
	db, err := Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Ping())
	var value int
	require.NoError(t, db.NewRaw("SELECT 42").Scan(t.Context(), &value))
	require.Equal(t, 42, value)
}
