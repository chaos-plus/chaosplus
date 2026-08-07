package timezone

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimezoneLifecycle(t *testing.T) {
	require.NoError(t, SetTimezone("America/New_York"))
	t.Cleanup(func() { _ = SetTimezone("UTC") })

	now := Now()
	assert.Equal(t, time.UTC, now.Location())
	assert.Equal(t, "America/New_York", time.Local.String())

	local := time.Date(2026, time.July, 31, 12, 30, 0, 0, time.Local)
	utc := ToUTC(local)
	assert.Equal(t, time.UTC, utc.Location())

	formatted := FormatUTC(utc)
	parsed, err := ParseUTC(formatted)
	require.NoError(t, err)
	assert.True(t, parsed.Equal(utc.Truncate(time.Second)))
	assert.Error(t, SetTimezone("Invalid/Timezone"))
	require.NoError(t, SetTimezone(""))
	assert.Equal(t, time.UTC, time.Local)
	_, err = ParseUTC("not-a-timestamp")
	assert.Error(t, err)
}
