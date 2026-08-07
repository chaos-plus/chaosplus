package authzsql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
)

type constrainedRow struct {
	bun.BaseModel `bun:"table:constraint_rows"`
	TenantID      string `bun:"tenant_id"`
	EntityID      string `bun:"entity_id"`
	ID            string `bun:"id"`
}

func TestApplyEntityConstraintUsesTenantAndParameters(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(t.Context(), `CREATE TABLE constraint_rows (tenant_id TEXT NOT NULL, entity_id TEXT NOT NULL, id TEXT NOT NULL)`)
	require.NoError(t, err)
	rows := []constrainedRow{
		{TenantID: "tenant", EntityID: "allowed", ID: "one"},
		{TenantID: "tenant", EntityID: "denied", ID: "two"},
		{TenantID: "tenant", EntityID: "x') OR 1=1 --", ID: "injection-shaped"},
		{TenantID: "other", EntityID: "allowed", ID: "foreign"},
	}
	_, err = db.NewInsert().Model(&rows).Exec(t.Context())
	require.NoError(t, err)

	var filtered []constrainedRow
	query := db.NewSelect().Model(&filtered).Order("id ASC")
	constraint := authz.DataConstraint{ResourceIDs: []string{"allowed", "denied", "x') OR 1=1 --"}, DeniedIDs: []string{"denied"}}
	require.NoError(t, ApplyEntityConstraint(query, "tenant", constraint).Scan(t.Context()))
	assert.Equal(t, []string{"injection-shaped", "one"}, []string{filtered[0].ID, filtered[1].ID})

	filtered = nil
	require.NoError(t, ApplyEntityConstraint(db.NewSelect().Model(&filtered), "tenant", authz.DataConstraint{}).Scan(t.Context()))
	assert.Empty(t, filtered)

	filtered = nil
	require.NoError(t, ApplyEntityConstraint(db.NewSelect().Model(&filtered).Order("id ASC"), "tenant", authz.DataConstraint{AllowAll: true, DeniedIDs: []string{"denied"}}).Scan(t.Context()))
	assert.Equal(t, []string{"injection-shaped", "one"}, []string{filtered[0].ID, filtered[1].ID})
}
