package gitlab

import (
	"context"
	"encoding/pem"
	"fmt"
	"github.com/santapong/cooker/internal/source/gittransport"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdapterNestedRepositoryTypedRefsAndPagination(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tagSHA := strings.Repeat("b", 40)
	objectSHA := strings.Repeat("c", 40)
	revoked := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" || revoked {
			w.WriteHeader(401)
			return
		}
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/group%2Fsub%2Frepo", "/api/v4/projects/42":
			fmt.Fprint(w, `{"id":42,"path_with_namespace":"group/sub/repo","http_url_to_repo":"https://evil.invalid/repo.git"}`)
		case "/api/v4/projects/42/repository/branches/release%2Fv1":
			fmt.Fprintf(w, `{"name":"release/v1","commit":{"id":%q}}`, sha)
		case "/api/v4/projects/42/repository/tags/release%2Fv1":
			fmt.Fprintf(w, `{"name":"release/v1","target":%q,"commit":{"id":%q}}`, objectSHA, tagSHA)
		case "/api/v4/projects/42/repository/branches":
			if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "50" {
				t.Error("page not bounded")
			}
			w.Header().Set("X-Next-Page", "3")
			fmt.Fprintf(w, `[{"name":"release/v1","commit":{"id":%q}}]`, sha)
		case "/api/v4/projects/42/repository/commits/" + sha:
			fmt.Fprintf(w, `{"id":%q}`, sha)
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	config := gittransport.Config{Origin: server.URL, APIOrigin: server.URL, AllowPrivateNetwork: true, CAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))}
	c, err := NewClient(config, gittransport.Credentials{Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo, err := c.Repository(ctx, "group/sub/repo")
	if err != nil || repo.ID != "42" || repo.Origin != server.URL {
		t.Fatal(repo, err)
	}
	branch, err := c.ResolveRevision(ctx, repo, gittransport.Ref{Kind: "branch", Name: "release/v1"})
	if err != nil || branch.Commit != sha || branch.FullRef != "refs/heads/release/v1" {
		t.Fatal(branch, err)
	}
	tag, err := c.ResolveRevision(ctx, repo, gittransport.Ref{Kind: "tag", Name: "release/v1"})
	if err != nil || tag.Commit != tagSHA || tag.ObjectSHA != objectSHA {
		t.Fatal(tag, err)
	}
	page, err := c.ListRevisions(ctx, repo, "branch", 2)
	if err != nil || len(page.Items) != 1 || page.NextPage != 3 {
		t.Fatal(page, err)
	}
	if got, err := c.VerifyCommit(ctx, repo, sha); err != nil || got != sha {
		t.Fatal(got, err)
	}
	other := repo
	other.Origin = "https://another.example"
	if _, err := c.ResolveRevision(ctx, other, gittransport.Ref{Kind: "branch", Name: "release/v1"}); err == nil {
		t.Fatal("cross-origin repo accepted")
	}
	other = repo
	other.Path = "group/renamed"
	if _, err := c.VerifyCommit(ctx, other, sha); err == nil {
		t.Fatal("mismatched repository identity accepted")
	}
	revoked = true
	if _, err := c.Repository(ctx, "42"); err == nil {
		t.Fatal("revoked token accepted")
	}
}
