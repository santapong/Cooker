package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveCommitPreservesSlashRefAndRejectsInvalidSHA(t *testing.T) {
	sha := strings.Repeat("c", 40)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/repos/acme/demo/commits/feature%2Fone" {
			t.Errorf("unexpected URL: %s", r.URL.EscapedPath())
		}
		fmt.Fprintf(w, `{"sha":%q}`, sha)
	}))
	defer s.Close()
	c := &AppClient{BaseURL: s.URL, HTTP: s.Client()}
	got, err := c.ResolveCommit(context.Background(), 0, "acme/demo", "feature/one")
	if err != nil || got != sha {
		t.Fatal(got, err)
	}
	sha = "short"
	if _, err := c.ResolveCommit(context.Background(), 0, "acme/demo", "feature/one"); err == nil {
		t.Fatal("accepted invalid commit")
	}
}
