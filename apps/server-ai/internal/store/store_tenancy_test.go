package store

import (
	"context"
	"testing"
)

func TestWorkflowIdentityAndDeleteAreTenantScoped(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, instanceID := range []string{"tenant-a", "tenant-b"} {
		if err := st.SaveWorkflow(ctx, WorkflowDefModel{ID: "same-id", Version: "1", Name: instanceID, DefJSON: `{}`, InstanceID: instanceID}); err != nil {
			t.Fatal(err)
		}
	}
	listA, err := st.ListWorkflows(ctx, "tenant-a")
	if err != nil || len(listA) != 1 || listA[0].Name != "tenant-a" {
		t.Fatalf("tenant-a list = (%+v, %v)", listA, err)
	}
	ctxA := WithEntity(ctx, "tenant-a")
	if err := st.DeleteWorkflow(ctxA, "same-id"); err != nil {
		t.Fatal(err)
	}
	listA, _ = st.ListWorkflows(ctx, "tenant-a")
	listB, _ := st.ListWorkflows(ctx, "tenant-b")
	if len(listA) != 0 || len(listB) != 1 || listB[0].Name != "tenant-b" {
		t.Fatalf("scoped delete leaked: tenant-a=%+v tenant-b=%+v", listA, listB)
	}
}
