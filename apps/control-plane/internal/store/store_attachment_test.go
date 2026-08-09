package store

import (
	"context"
	"testing"
)

func TestAttachmentCRUDAndOwnerFilter(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	a := &Attachment{ID: "att-1", OwnerType: "work_item", OwnerID: "wi-1", Filename: "a.png", Mime: "image/png", SizeBytes: 10, StorePath: "/tmp/a.png"}
	b := &Attachment{ID: "att-2", OwnerType: "work_item", OwnerID: "wi-2", Filename: "b.jpg", Mime: "image/jpeg", SizeBytes: 20, StorePath: "/tmp/b.jpg"}
	if err := s.CreateAttachment(ctx, a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := s.CreateAttachment(ctx, b); err != nil {
		t.Fatalf("create b: %v", err)
	}

	got, err := s.GetAttachment(ctx, "att-1")
	if err != nil || got.Filename != "a.png" || got.Mime != "image/png" || got.SizeBytes != 10 {
		t.Fatalf("get wrong: %+v %v", got, err)
	}

	list, err := s.ListAttachments(ctx, "work_item", "wi-1")
	if err != nil || len(list) != 1 || list[0].ID != "att-1" {
		t.Fatalf("owner filter wrong: %+v %v", list, err)
	}

	if err := s.DeleteAttachment(ctx, "att-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetAttachment(ctx, "att-1"); err == nil {
		t.Fatal("deleted attachment still found")
	}
}
