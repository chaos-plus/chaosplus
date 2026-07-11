package guid

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_Generates(t *testing.T) {
	g, err := New(7)
	require.NoError(t, err)

	id1, err := g.Next()
	require.NoError(t, err)
	assert.Positive(t, id1)

	id2 := g.MustNext()
	assert.Positive(t, id2)
	assert.NotEqual(t, id1, id2)
}

func TestNew_Uniqueness(t *testing.T) {
	g, err := New(1)
	require.NoError(t, err)

	const n = 20000
	seen := make(map[int64]struct{}, n)
	for i := 0; i < n; i++ {
		id := g.MustNext()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id generated: %d", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNew_DistinctWorkersDoNotCollide(t *testing.T) {
	g1, err := New(1)
	require.NoError(t, err)
	g2, err := New(2)
	require.NoError(t, err)

	seen := make(map[int64]struct{})
	for i := 0; i < 1000; i++ {
		for _, id := range []int64{g1.MustNext(), g2.MustNext()} {
			if _, dup := seen[id]; dup {
				t.Fatalf("collision across workers: %d", id)
			}
			seen[id] = struct{}{}
		}
	}
}

func TestNew_ErrorWhenEpochInFuture(t *testing.T) {
	orig := epoch
	epoch = time.Now().Add(time.Hour)
	defer func() { epoch = orig }()

	_, err := New(1)
	assert.Error(t, err)
}

func TestDefault(t *testing.T) {
	// Reset the process-wide default so the "not initialized" paths are exercised.
	SetDefault(nil)
	assert.Nil(t, Default())

	_, err := Next()
	assert.ErrorIs(t, err, ErrNotInitialized)
	assert.Panics(t, func() { MustNext() })

	g, err := New(3)
	require.NoError(t, err)
	SetDefault(g)
	assert.Same(t, g, Default())

	id, err := Next()
	require.NoError(t, err)
	assert.Positive(t, id)
	assert.Positive(t, MustNext())

	SetDefault(nil) // leave the package clean for other tests
}
