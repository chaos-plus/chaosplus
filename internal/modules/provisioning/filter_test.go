package provisioning

import (
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	scimfilter "github.com/scim2/filter-parser/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSCIMFilterASTCompilation(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	for _, filter := range []string{
		`userName sw "a" and (active eq true or emails[value co "@example.test"])`,
		`not (displayName eq "blocked")`,
		UserSchema + `:name.formatted pr`,
		`meta.created ge "2026-08-03T00:00:00Z"`,
	} {
		query := db.NewSelect().TableExpr("iam_scim_resources AS resource")
		require.NoError(t, applySCIMFilter(query, filter, ResourceUser, "sqlite"), filter)
	}
	query := db.NewSelect().TableExpr("iam_scim_resources AS resource")
	require.NoError(t, applySCIMFilter(query, `members[value eq "principal-1"]`, ResourceGroup, "sqlite"))

	for _, filter := range []string{
		`unknown eq "x"`,
		GroupSchema + `:displayName eq "x"`,
		`meta.created co "2026"`,
		`active gt true`,
		`emails[type eq "work"]`,
		strings.Repeat("x", maxFilterBytes+1),
	} {
		query := db.NewSelect().TableExpr("iam_scim_resources AS resource")
		assert.ErrorIs(t, applySCIMFilter(query, filter, ResourceUser, "sqlite"), ErrInvalidFilter, filter)
	}
}

func TestSCIMComparisonUsesDialectAndTimestamp(t *testing.T) {
	for dialect, expected := range map[string]string{"sqlite": "INSTR", "mysql": "LOCATE", "postgres": "STRPOS"} {
		condition, args, err := compileComparison(filterTarget{expression: "principal.login_name"}, scimfilter.CO, "ALICE", dialect)
		require.NoError(t, err)
		assert.Contains(t, condition, expected)
		assert.Equal(t, []any{"alice"}, args)
	}
	condition, args, err := compileComparison(filterTarget{expression: "resource.created_at", timestamp: true}, scimfilter.GE, "2026-08-03T00:00:00Z", "sqlite")
	require.NoError(t, err)
	assert.Equal(t, "resource.created_at >= ?", condition)
	assert.Equal(t, []any{time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC).UnixMilli()}, args)
	_, _, err = compileComparison(filterTarget{expression: "resource.created_at", timestamp: true}, scimfilter.EQ, "not-time", "sqlite")
	assert.ErrorIs(t, err, ErrInvalidFilter)

	condition, args, err = compileComparison(filterTarget{expression: "principal.email", caseExact: true}, scimfilter.EQ, nil, "sqlite")
	require.NoError(t, err)
	assert.Equal(t, "principal.email IS NULL", condition)
	assert.Empty(t, args)
	condition, _, err = compileComparison(filterTarget{expression: "principal.email", caseExact: true}, scimfilter.PR, nil, "sqlite")
	require.NoError(t, err)
	assert.Equal(t, "principal.email <> ''", condition)

	for operator, expected := range map[scimfilter.CompareOperator]string{
		scimfilter.EQ: "column = ?", scimfilter.NE: "column <> ?", scimfilter.GT: "column > ?",
		scimfilter.GE: "column >= ?", scimfilter.LT: "column < ?", scimfilter.LE: "column <= ?",
		scimfilter.SW: "SUBSTR(column, 1, LENGTH(?)) = ?", scimfilter.EW: "SUBSTR(column, -LENGTH(?)) = ?",
	} {
		condition, args, err = comparison("column", operator, "value", "sqlite")
		require.NoError(t, err)
		assert.Equal(t, expected, condition)
		assert.NotEmpty(t, args)
	}
	_, _, err = comparison("column", scimfilter.CompareOperator("invalid"), "value", "sqlite")
	assert.ErrorIs(t, err, ErrInvalidFilter)

	booleanTarget := filterTarget{expression: "active", boolean: true}
	condition, args, err = compileComparison(booleanTarget, scimfilter.PR, nil, "sqlite")
	require.NoError(t, err)
	assert.Equal(t, "1 = 1", condition)
	assert.Empty(t, args)
	condition, _, err = compileComparison(booleanTarget, scimfilter.EQ, true, "sqlite")
	require.NoError(t, err)
	assert.Equal(t, "active", condition)
	condition, _, err = compileComparison(booleanTarget, scimfilter.NE, true, "sqlite")
	require.NoError(t, err)
	assert.Equal(t, "NOT active", condition)
	_, _, err = compileComparison(booleanTarget, scimfilter.EQ, "true", "sqlite")
	assert.ErrorIs(t, err, ErrInvalidFilter)

	condition, _, err = compileComparison(filterTarget{expression: "email"}, scimfilter.NE, nil, "sqlite")
	require.NoError(t, err)
	assert.Equal(t, "email IS NOT NULL", condition)
	_, _, err = compileComparison(filterTarget{expression: "email"}, scimfilter.GT, nil, "sqlite")
	assert.ErrorIs(t, err, ErrInvalidFilter)
	_, _, err = compileComparison(filterTarget{expression: "email"}, scimfilter.EQ, true, "sqlite")
	assert.ErrorIs(t, err, ErrInvalidFilter)

	condition, args, err = compileMemberFilter(scimfilter.PR, nil)
	require.NoError(t, err)
	assert.Contains(t, condition, "EXISTS")
	assert.Empty(t, args)
	condition, args, err = compileMemberFilter(scimfilter.NE, "principal-1")
	require.NoError(t, err)
	assert.Contains(t, condition, "NOT EXISTS")
	assert.Equal(t, []any{"principal-1"}, args)
	_, _, err = compileMemberFilter(scimfilter.EQ, true)
	assert.ErrorIs(t, err, ErrInvalidFilter)
}
