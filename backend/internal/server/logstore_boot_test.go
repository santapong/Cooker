package server

import (
	"context"
	"testing"

	"github.com/santapong/cooker/internal/logstore"
)

func TestBootLogStoreExplicitSelectionFailsClosed(t *testing.T) {
	for _, backend := range []string{"postgres", "postgress"} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv("COOKER_LOGSTORE_BACKEND", backend)
			if _, close, err := bootLogStore(context.Background(), ""); err == nil {
				close()
				t.Fatal("invalid durable configuration silently became memory")
			}
		})
	}
}

func TestBootLogStoreMemoryDoesNotDialDatabase(t *testing.T) {
	t.Setenv("COOKER_LOGSTORE_BACKEND", "memory")
	store, close, err := bootLogStore(context.Background(), "invalid database URL")
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if _, ok := store.(*logstore.Memory); !ok {
		t.Fatal("explicit memory backend was not selected")
	}
}
