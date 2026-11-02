package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTypedRefsPeelAnnotatedTagsAndKeepBranchesDistinct(t *testing.T) {
	object, commit := strings.Repeat("a", 40), strings.Repeat("b", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("connection credential missing")
		}
		switch r.URL.Path {
		case "/repos/acme/demo":
			fmt.Fprint(w, `{"id":123,"full_name":"acme/demo"}`)
		case "/repos/acme/demo/git/ref/tags/release/one":
			fmt.Fprintf(w, `{"ref":"refs/tags/release/one","object":{"type":"tag","sha":%q}}`, object)
		case "/repos/acme/demo/git/tags/" + object:
			fmt.Fprintf(w, `{"sha":%q,"object":{"type":"commit","sha":%q}}`, object, commit)
		case "/repos/acme/demo/git/ref/heads/release/one":
			fmt.Fprintf(w, `{"ref":"refs/heads/release/one","object":{"type":"commit","sha":%q}}`, object)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &AppClient{BaseURL: srv.URL, HTTP: srv.Client(), AccessToken: "fixture-token"}
	ctx := context.Background()
	id, err := c.RepositoryIdentity(ctx, 0, "acme/demo")
	if err != nil || id.ID != "123" {
		t.Fatal(id, err)
	}
	r, err := c.ResolveRef(ctx, 0, "acme/demo", "refs/tags/release/one")
	if err != nil || r.CommitSHA != commit || r.ObjectSHA != object {
		t.Fatal(r, err)
	}
	r, err = c.ResolveRef(ctx, 0, "acme/demo", "refs/heads/release/one")
	if err != nil || r.CommitSHA != object {
		t.Fatal(r, err)
	}
	if _, err = c.ResolveRef(ctx, 0, "acme/demo", "release/one"); err == nil {
		t.Fatal("ambiguous ref accepted")
	}
}
