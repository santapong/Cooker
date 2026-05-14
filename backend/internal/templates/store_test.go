package templates

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestMemoryStoreCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	schema := json.RawMessage(`{"stages":[]}`)
	if err := s.Create(ctx, Template{ID: "t1", Name: "Hello", Schema: schema, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, Template{ID: "t1", Name: "dup"}); err == nil {
		t.Fatal("want duplicate-id error")
	}
	got, err := s.Get(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Hello" {
		t.Fatalf("Name=%q", got.Name)
	}
	list, _ := s.List(ctx)
	if len(list) != 1 {
		t.Fatalf("List=%d", len(list))
	}
	got.Name = "Renamed"
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Get(ctx, "t1")
	if after.Name != "Renamed" {
		t.Fatalf("after Update Name=%q", after.Name)
	}
	if err := s.Delete(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMemoryStoreListSkipsDisabled(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_ = s.Create(ctx, Template{ID: "on", Name: "On", Enabled: true})
	_ = s.Create(ctx, Template{ID: "off", Name: "Off", Enabled: false})
	list, _ := s.List(ctx)
	if len(list) != 1 || list[0].ID != "on" {
		t.Fatalf("list=%+v want only 'on'", list)
	}
}
