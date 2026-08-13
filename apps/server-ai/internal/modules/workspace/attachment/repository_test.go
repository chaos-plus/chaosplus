package attachment

import (
	"context"
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
)

func TestRepositoryUsesWorkspaceTableForListAndDelete(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	repository := NewRepository(db)
	value := &Attachment{ID: 41, TenantID: 11, EntityID: 21, OwnerID: 31, ResourceType: ResourceTask, ResourceID: 51, Filename: "result.txt", ContentType: "text/plain", SizeBytes: 6, Checksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ObjectKey: "workspace/11/21/41"}
	if err := repository.Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	items, err := repository.List(ctx, ResourceTask, 51)
	if err != nil || len(items) != 1 || items[0].ID != value.ID {
		t.Fatalf("list attachments = %+v, %v", items, err)
	}
	deleting, err := repository.MarkDeleting(ctx, value.ID, value.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteDelete(ctx, value.ID, deleting.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, value.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted attachment = %v, want ErrNotFound", err)
	}
}
