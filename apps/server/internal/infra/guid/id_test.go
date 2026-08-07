package guid

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestID_JSON(t *testing.T) {
	b, err := json.Marshal(ID(123))
	require.NoError(t, err)
	assert.Equal(t, `"123"`, string(b))

	var got struct {
		ID ID `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"id":"456"}`), &got))
	assert.Equal(t, ID(456), got.ID)
	require.NoError(t, json.Unmarshal([]byte(`{"id":789}`), &got))
	assert.Equal(t, ID(789), got.ID)
	require.NoError(t, json.Unmarshal([]byte(`{"id":null}`), &got))
	assert.Equal(t, ID(0), got.ID)
	assert.Error(t, json.Unmarshal([]byte(`{"id":"abc"}`), &got))
}

func TestID_Text(t *testing.T) {
	b, err := ID(42).MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "42", string(b))

	var id ID
	require.NoError(t, id.UnmarshalText([]byte("99")))
	assert.Equal(t, ID(99), id)
}

func TestID_SQL(t *testing.T) {
	v, err := ID(7).Value()
	require.NoError(t, err)
	assert.Equal(t, int64(7), v)

	var id ID
	require.NoError(t, id.Scan(int64(11)))
	assert.Equal(t, ID(11), id)
	require.NoError(t, id.Scan([]byte("12")))
	assert.Equal(t, ID(12), id)
	require.NoError(t, id.Scan("13"))
	assert.Equal(t, ID(13), id)
	require.NoError(t, id.Scan(nil))
	assert.Equal(t, ID(0), id)

	assert.Error(t, id.Scan(3.14))
	assert.Error(t, id.Scan([]byte("nope")))
	assert.Error(t, id.Scan("nope"))
}

func TestID_Misc(t *testing.T) {
	assert.Equal(t, "5", ID(5).String())
	assert.Equal(t, int64(5), ID(5).Int64())
	assert.True(t, ID(0).Zero())
	assert.False(t, ID(1).Zero())
	assert.Equal(t, "string", string(ID(0).Schema(nil).Type))
}

func TestParse(t *testing.T) {
	for _, in := range []string{"", "null", `""`, `"  "`} {
		id, err := Parse(in)
		require.NoError(t, err, "input %q", in)
		assert.Equal(t, ID(0), id)
	}
	id, err := Parse(`"123"`)
	require.NoError(t, err)
	assert.Equal(t, ID(123), id)

	_, err = Parse("nope")
	assert.Error(t, err)
}
