package gitea

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

func TestAdapterExactRefAnnotatedTagIdentityAndToken(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tagSHA := strings.Repeat("b", 40)
	objectSHA := strings.Repeat("c", 40)
	cycle := false
	revoked := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-token" || revoked {
			w.WriteHeader(401)
			return
		}
		switch r.URL.EscapedPath() {
		case "/api/v1/repos/team/repo", "/api/v1/repositories/42":
			fmt.Fprint(w, `{"id":42,"full_name":"team/repo","clone_url":"https://evil.invalid/repo.git"}`)
		case "/api/v1/repos/team/repo/git/refs/heads%2Frelease%2Fv1":
			fmt.Fprintf(w, `[{"ref":"refs/heads/release/v1-other","object":{"type":"commit","sha":%q}},{"ref":"refs/heads/release/v1","object":{"type":"commit","sha":%q}}]`, tagSHA, sha)
		case "/api/v1/repos/team/repo/git/refs/tags%2Frelease%2Fv1":
			fmt.Fprintf(w, `[{"ref":"refs/tags/release/v1","object":{"type":"tag","sha":%q}}]`, objectSHA)
		case "/api/v1/repos/team/repo/git/refs/tags%2Flight":
			fmt.Fprintf(w, `[{"ref":"refs/tags/light","object":{"type":"commit","sha":%q}}]`, tagSHA)
		case "/api/v1/repos/team/repo/git/tags/" + objectSHA:
			kind, target := "commit", tagSHA
			if cycle {
				kind, target = "tag", objectSHA
			}
			fmt.Fprintf(w, `{"sha":%q,"object":{"type":%q,"sha":%q}}`, objectSHA, kind, target)
		case "/api/v1/repos/team/repo/git/commits/" + sha:
			fmt.Fprintf(w, `{"sha":%q}`, sha)
		case "/api/v1/repos/team/repo/branches":
			fmt.Fprint(w, `[{"name":"release/v1"}]`)
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	config := gittransport.Config{Origin: server.URL, APIOrigin: server.URL, AllowPrivateNetwork: true, CAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))}
	c, err := NewClient(config, gittransport.Credentials{Username: "fixture", Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo, err := c.Repository(ctx, "team/repo")
	if err != nil || repo.ID != "42" {
		t.Fatal(repo, err)
	}
	branch, err := c.ResolveRevision(ctx, repo, gittransport.Ref{Kind: "branch", Name: "release/v1"})
	if err != nil || branch.Commit != sha {
		t.Fatal(branch, err)
	}
	tag, err := c.ResolveRevision(ctx, repo, gittransport.Ref{Kind: "tag", Name: "release/v1"})
	if err != nil || tag.Commit != tagSHA || tag.ObjectSHA != objectSHA {
		t.Fatal(tag, err)
	}
	light, err := c.ResolveRevision(ctx, repo, gittransport.Ref{Kind: "tag", Name: "light"})
	if err != nil || light.Commit != tagSHA || light.ObjectSHA != tagSHA {
		t.Fatal(light, err)
	}
	page, err := c.ListRevisions(ctx, repo, "branch", 1)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if got, err := c.VerifyCommit(ctx, repo, sha); err != nil || got != sha {
		t.Fatal(got, err)
	}
	cycle = true
	if _, err := c.ResolveRevision(ctx, repo, gittransport.Ref{Kind: "tag", Name: "release/v1"}); err == nil {
		t.Fatal("tag cycle accepted")
	}
	wrong := repo
	wrong.Origin = "https://another.example"
	if _, err := c.VerifyCommit(ctx, wrong, sha); err == nil {
		t.Fatal("cross-origin identity accepted")
	}
	revoked = true
	if _, err := c.Repository(ctx, "42"); err == nil {
		t.Fatal("revoked token accepted")
	}
}
