package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func mkToken(id, sub, hash string, created time.Time, expires *time.Time) *model.APIToken {
	return &model.APIToken{
		ID:             id,
		Name:           "tok-" + id,
		DisplayPrefix:  "ck_" + id,
		Hash:           hash,
		Role:           "operator",
		CreatedBySub:   sub,
		CreatedByEmail: sub + "@x.com",
		CreatedAt:      created,
		ExpiresAt:      expires,
	}
}

func TestAPITokenStore_CreateGetByHashListDelete(t *testing.T) {
	st := New().APITokens
	ctx := context.Background()
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Insert two for alice (out of time order) and one for bob.
	tokens := []*model.APIToken{
		mkToken("a1", "alice", "hash-a1", t0, nil),
		mkToken("a2", "alice", "hash-a2", t0.Add(time.Hour), nil),
		mkToken("b1", "bob", "hash-b1", t0.Add(2*time.Hour), nil),
	}
	for _, tok := range tokens {
		if err := st.Create(ctx, tok); err != nil {
			t.Fatalf("create %s: %v", tok.ID, err)
		}
	}

	// GetByHash is the auth-path lookup.
	got, err := st.GetByHash(ctx, "hash-a2")
	if err != nil || got.ID != "a2" {
		t.Fatalf("GetByHash(hash-a2) = %v, %v", got, err)
	}
	if _, err := st.GetByHash(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetByHash(nope) should be ErrNotFound; got %v", err)
	}

	// ListByCreator scopes to the creator, newest-first.
	mine, err := st.ListByCreator(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 2 || mine[0].ID != "a2" || mine[1].ID != "a1" {
		t.Fatalf("ListByCreator(alice) not newest-first own-only: %+v", idsOf(mine))
	}

	// ListAll returns everything newest-first.
	all, err := st.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].ID != "b1" {
		t.Fatalf("ListAll not newest-first: %+v", idsOf(all))
	}

	// Delete one; it's gone from both lookups.
	if err := st.Delete(ctx, "a1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.Get(ctx, "a1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(a1) after delete should be ErrNotFound; got %v", err)
	}
	if err := st.Delete(ctx, "a1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("double delete should be ErrNotFound; got %v", err)
	}
}

func TestAPITokenStore_HashUniqueness(t *testing.T) {
	st := New().APITokens
	ctx := context.Background()
	if err := st.Create(ctx, mkToken("x1", "alice", "dup", time.Now(), nil)); err != nil {
		t.Fatal(err)
	}
	// A second token with the same hash conflicts (mirrors the Postgres
	// unique index on hash).
	if err := st.Create(ctx, mkToken("x2", "bob", "dup", time.Now(), nil)); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate hash should be ErrConflict; got %v", err)
	}
}

func TestAPITokenStore_TouchLastUsed(t *testing.T) {
	st := New().APITokens
	ctx := context.Background()
	if err := st.Create(ctx, mkToken("t1", "alice", "h1", time.Now(), nil)); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(ctx, "t1")
	if before.LastUsedAt != nil {
		t.Fatalf("LastUsedAt should start nil")
	}
	ts := time.Date(2026, 6, 5, 9, 0, 0, 0, time.UTC)
	if err := st.TouchLastUsed(ctx, "t1", ts); err != nil {
		t.Fatalf("touch: %v", err)
	}
	after, _ := st.Get(ctx, "t1")
	if after.LastUsedAt == nil || !after.LastUsedAt.Equal(ts) {
		t.Fatalf("LastUsedAt = %v, want %v", after.LastUsedAt, ts)
	}
	if err := st.TouchLastUsed(ctx, "missing", ts); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("touch missing should be ErrNotFound; got %v", err)
	}
}

// TestAPITokenStore_ReturnsCopies confirms mutating a returned token does
// not corrupt the stored row (the store hands out copies).
func TestAPITokenStore_ReturnsCopies(t *testing.T) {
	st := New().APITokens
	ctx := context.Background()
	exp := time.Now().Add(time.Hour)
	if err := st.Create(ctx, mkToken("c1", "alice", "h", time.Now(), &exp)); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(ctx, "c1")
	got.Role = "admin"
	*got.ExpiresAt = time.Unix(0, 0)
	fresh, _ := st.Get(ctx, "c1")
	if fresh.Role == "admin" {
		t.Errorf("store returned a mutable reference (role)")
	}
	if fresh.ExpiresAt.Equal(time.Unix(0, 0)) {
		t.Errorf("store returned a mutable reference (expiresAt)")
	}
}

func idsOf(ts []*model.APIToken) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}
