package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
)

func TestLegacySourceAccessPreservesTypedBranchAndTagRefs(t *testing.T) {
	branchSHA, tagObject, tagCommit := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/repos/acme/shop/git/ref/heads/release/one":
			fmt.Fprintf(w, `{"ref":"refs/heads/release/one","object":{"type":"commit","sha":%q}}`, branchSHA)
		case "/repos/acme/shop/git/ref/tags/release/one":
			fmt.Fprintf(w, `{"ref":"refs/tags/release/one","object":{"type":"tag","sha":%q}}`, tagObject)
		case "/repos/acme/shop/git/tags/" + tagObject:
			fmt.Fprintf(w, `{"sha":%q,"object":{"type":"commit","sha":%q}}`, tagObject, tagCommit)
		case "/repos/acme/shop/git/ref/tags/tag-only":
			fmt.Fprintf(w, `{"ref":"refs/tags/tag-only","object":{"type":"commit","sha":%q}}`, tagCommit)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	source := &SourceAccess{GitHub: &github.AppClient{BaseURL: server.URL}}
	app := &model.App{GitHubRepo: "acme/shop"}
	for _, tc := range []struct{ ref, want string }{
		{"release/one", branchSHA}, {"refs/heads/release/one", branchSHA},
		{"refs/tags/release/one", tagCommit}, {"refs/tags/tag-only", tagCommit},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			got, err := source.ResolveApp(context.Background(), app, tc.ref)
			if err != nil || got != tc.want {
				t.Fatalf("ResolveApp(%q) = %s, %v; want %s", tc.ref, got, err, tc.want)
			}
		})
	}
	if _, err := source.ResolveApp(context.Background(), app, "tag-only"); err == nil {
		t.Fatal("bare branch name fell back to a same-named tag")
	}
	before := len(calls)
	for _, ref := range []string{"refs/remotes/origin/main", "refs/tags/", "main~1", ""} {
		if _, err := source.ResolveApp(context.Background(), app, ref); err == nil {
			t.Fatalf("unsupported ref %q accepted", ref)
		}
	}
	if len(calls) != before {
		t.Fatal("invalid refs reached the provider")
	}
}
