package guid

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

type widget struct {
	bun.BaseModel `bun:"table:widgets"`
	Base
	Name string `bun:"name"`
}

func TestBase_FillsIDAndTimestamps(t *testing.T) {
	ctx := context.Background()
	g, err := New(9)
	require.NoError(t, err)
	SetDefault(g)

	db := newDB(t)
	_, err = db.NewCreateTable().Model((*widget)(nil)).Exec(ctx)
	require.NoError(t, err)

	w := &widget{Name: "gadget"}
	_, err = db.NewInsert().Model(w).Exec(ctx)
	require.NoError(t, err)

	assert.False(t, w.ID.Zero(), "id auto-filled from the generator")
	assert.False(t, w.CreatedAt.IsZero())
	assert.False(t, w.UpdatedAt.IsZero())

	prev := w.UpdatedAt
	w.Name = "gizmo"
	_, err = db.NewUpdate().Model(w).WherePK().Exec(ctx)
	require.NoError(t, err)
	assert.False(t, w.UpdatedAt.Before(prev), "updated_at bumped on update")
}
