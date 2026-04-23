package tsnet

import (
	"context"
	"errors"
	"testing"
)

// TestStubReturnsErrDisabled runs in the default build. When the
// binary is built with -tags tsnet this file is not excluded (the
// stub file is), but New() actually dials tailnet, which we never
// want in unit tests — so the real build has its own test that
// only checks constructor input validation.
func TestStubReturnsErrDisabled(t *testing.T) {
	_, err := New(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error")
	}
	// In the stub build this is ErrDisabled. In the real build it
	// is "tsnet: AuthKey is required" — either is acceptable.
	if !errors.Is(err, ErrDisabled) && err.Error() != "tsnet: AuthKey is required" {
		t.Errorf("unexpected error: %v", err)
	}
}
