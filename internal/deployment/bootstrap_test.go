package deployment

import (
	"context"
	"fmt"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBindInitialAdminIsIdempotent(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	spice := &fakeSpice{}
	admin := identity{Subject: "subject", DisplayName: "Admin", Email: "admin@example.com"}
	require.NoError(t, bindInitialAdmin(context.Background(), db, spice, "tenant", admin))
	require.NoError(t, bindInitialAdmin(context.Background(), db, spice, "tenant", admin))
	member, err := iam.NewRepository(db, func() (string, error) { return "", nil }).GetMember(context.Background(), "tenant", "subject")
	require.NoError(t, err)
	assert.Equal(t, iam.MemberActive, member.Status)
	assert.Equal(t, 2, spice.writes)
}

func TestBindInitialAdminFailsClosed(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	spice := &fakeSpice{allowed: new(bool)}
	err = bindInitialAdmin(context.Background(), db, spice, "tenant", identity{Subject: "subject", DisplayName: "Admin"})
	assert.ErrorContains(t, err, "denied")
}

type fakeSpice struct {
	writes  int
	allowed *bool
}

func (f *fakeSpice) Check(context.Context, spicedbx.ObjectRef, string, spicedbx.SubjectRef, spicedbx.ZedToken) (bool, error) {
	if f.allowed == nil {
		return true, nil
	}
	return *f.allowed, nil
}
func (f *fakeSpice) CheckBulk(context.Context, spicedbx.ObjectRef, []string, spicedbx.SubjectRef, spicedbx.ZedToken) (map[string]bool, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeSpice) WriteRelationships(context.Context, []spicedbx.Relationship) (spicedbx.ZedToken, error) {
	f.writes++
	return "token", nil
}
func (f *fakeSpice) WriteRelationshipUpdates(context.Context, []spicedbx.RelationshipUpdate) (spicedbx.ZedToken, error) {
	return "", fmt.Errorf("not implemented")
}
func (f *fakeSpice) LookupResources(context.Context, string, string, spicedbx.SubjectRef) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeSpice) LookupSubjects(context.Context, spicedbx.ObjectRef, string, string) ([]spicedbx.SubjectRef, error) {
	return nil, fmt.Errorf("not implemented")
}
