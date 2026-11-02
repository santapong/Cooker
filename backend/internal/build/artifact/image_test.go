package artifact

import (
	"encoding/json"
	"os"
	"testing"
)

func TestImageIsPrivateAndCleanupIsIdempotent(t *testing.T) {
	a, err := NewImage()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(a.OutputDir())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("artifact mode %o", info.Mode().Perm())
	}
	b, err := json.Marshal(a) //nolint:staticcheck // SA9005: prove private artifact fields cannot enter JSON.
	if err != nil || string(b) != "{}" {
		t.Fatalf("artifact leaks into JSON: %s (%v)", b, err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(a.OutputDir()); !os.IsNotExist(err) {
		t.Fatalf("artifact retained: %v", err)
	}
}
