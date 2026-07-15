package spicedbx_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

func TestRemoteSpiceDBSmoke(t *testing.T) {
	if os.Getenv("SPICEDB_SMOKE") != "1" {
		t.Skip("set SPICEDB_SMOKE=1 to run against a real SpiceDB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := spicedbx.Open(spicedbx.Config{
		Endpoint: os.Getenv("SPICEDB_ENDPOINT"),
		Token:    os.Getenv("SPICEDB_TOKEN"),
		Insecure: true,
	})
	require.NoError(t, err)
	defer client.Close()

	_, err = client.WriteSchema(ctx, authz.DefaultSchema())
	require.NoError(t, err)
	token, err := client.WriteRelationships(ctx, []spicedbx.Relationship{{
		Resource: spicedbx.ObjectRef{Type: "tenant", ID: "smoke_tenant"},
		Relation: "admin",
		Subject:  spicedbx.SubjectRef{Object: spicedbx.ObjectRef{Type: "user", ID: "smoke_user"}},
	}})
	require.NoError(t, err)

	allowed, err := client.Check(ctx, spicedbx.ObjectRef{Type: "tenant", ID: "smoke_tenant"}, "administer", spicedbx.SubjectRef{Object: spicedbx.ObjectRef{Type: "user", ID: "smoke_user"}}, token)
	require.NoError(t, err)
	require.True(t, allowed)
}
