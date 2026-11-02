//go:build integration

package gitea

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/source/gittransport"
)

// TestGiteaLiveSourceAcceptance creates only its own loopback disposable server.
// Run the compiled test binary on a host with Docker/Git; no existing accounts,
// host credential files, or provider services are used.
func TestGiteaLiveSourceAcceptance(t *testing.T) {
	if os.Getenv("COOKER_GITEA_ACCEPTANCE") != "true" {
		t.Skip("set COOKER_GITEA_ACCEPTANCE=true for a disposable Gitea server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(random)
	name := "cooker-source-gitea-" + suffix[:8]
	password := "fixture-" + suffix
	user := "fixture"
	image := "gitea/gitea:1.26.4-rootless"
	docker := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if _, err := docker("image", "inspect", image); err != nil {
		t.Fatal("pull the pinned disposable Gitea image first")
	}
	if _, err := docker("run", "-d", "--rm", "--name", name, "--label", "cooker.acceptance=source-gitea", "--cpus", "2", "--memory", "768m", "-p", "127.0.0.1::3000", "-e", "GITEA__database__DB_TYPE=sqlite3", "-e", "GITEA__security__INSTALL_LOCK=true", "-e", "GITEA__service__DISABLE_REGISTRATION=true", "-e", "GITEA__server__ROOT_URL=http://localhost:3000/", "-e", "GITEA__server__DISABLE_SSH=true", "-e", "GITEA__log__LEVEL=Error", image); err != nil {
		t.Fatal("cannot create disposable Gitea", err)
	}
	t.Cleanup(func() {
		cleanup, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		if out, err := exec.CommandContext(cleanup, "docker", "rm", "-f", "-v", name).CombinedOutput(); err != nil {
			t.Errorf("remove fixture %s: %v %s", name, err, out)
		}
	})
	port, err := docker("port", name, "3000/tcp")
	if err != nil || !strings.HasPrefix(port, "127.0.0.1:") {
		t.Fatal("fixture is not loopback bound", err)
	}
	base := "http://" + port
	setupClient := &http.Client{Timeout: 10 * time.Second}
	ready := false
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); {
		resp, err := setupClient.Get(base + "/api/v1/version")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("Gitea fixture did not become ready")
	}
	// Password is generated for this disposable account and never printed.
	if _, err := docker("exec", name, "gitea", "admin", "user", "create", "--username", user, "--password", password, "--email", "fixture@example.test", "--admin", "--must-change-password=false"); err != nil {
		t.Fatal("cannot initialize disposable account", err)
	}
	request := func(method, path string, payload any, result any) {
		t.Helper()
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, method, base+"/api/v1"+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth(user, password)
		req.Header.Set("Content-Type", "application/json")
		resp, err := setupClient.Do(req)
		if err != nil {
			t.Fatal("fixture setup request failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Fatalf("fixture setup %s %s HTTP %d", method, path, resp.StatusCode)
		}
		if result != nil {
			if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
				t.Fatal("invalid fixture setup response", err)
			}
		}
	}
	var version struct {
		Version string `json:"version"`
	}
	request("GET", "/version", nil, &version)
	request("POST", "/user/repos", map[string]any{"name": "repo", "private": true, "auto_init": true, "default_branch": "main"}, nil)
	var created struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
		Content struct {
			SHA string `json:"sha"`
		} `json:"content"`
	}
	request("POST", "/repos/fixture/repo/contents/marker.txt", map[string]any{"content": base64.StdEncoding.EncodeToString([]byte("accepted fixture source\n")), "branch": "main", "message": "accepted source"}, &created)
	accepted := created.Commit.SHA
	if err := gittransport.ValidateCommit(accepted); err != nil {
		t.Fatal(err)
	}
	request("POST", "/repos/fixture/repo/branches", map[string]any{"new_branch_name": "release/v1", "old_ref_name": accepted}, nil)
	request("POST", "/repos/fixture/repo/tags", map[string]any{"tag_name": "release/v1", "target": accepted, "message": "annotated fixture tag"}, nil)
	var token struct {
		ID   int64  `json:"id"`
		SHA1 string `json:"sha1"`
	}
	request("POST", "/users/fixture/tokens", map[string]any{"name": "source-acceptance", "scopes": []string{"read:repository"}}, &token)
	if token.SHA1 == "" {
		t.Fatal("fixture token was not created")
	}
	target, _ := url.Parse(base)
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(target))
	defer proxy.Close()
	config := gittransport.Config{Origin: proxy.URL, APIOrigin: proxy.URL, AllowPrivateNetwork: true, CAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxy.Certificate().Raw}))}
	client, err := NewClient(config, gittransport.Credentials{Username: user, Token: token.SHA1})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := client.Repository(ctx, "fixture/repo")
	if err != nil {
		t.Fatal(err)
	}
	branch, err := client.ResolveRevision(ctx, repo, gittransport.Ref{Kind: "branch", Name: "main"})
	if err != nil || branch.Commit != accepted {
		t.Fatal("accepted branch resolution failed", branch, err)
	}
	tag, err := client.ResolveRevision(ctx, repo, gittransport.Ref{Kind: "tag", Name: "release/v1"})
	if err != nil || tag.Commit != accepted || tag.ObjectSHA == accepted {
		t.Fatal("annotated tag peeling failed", tag, err)
	}
	// Move the branch after accepting its immutable commit; retain the original tag.
	var changed struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	request("PUT", "/repos/fixture/repo/contents/marker.txt", map[string]any{"content": base64.StdEncoding.EncodeToString([]byte("moved fixture source\n")), "sha": created.Content.SHA, "branch": "main", "message": "move after acceptance"}, &changed)
	// The same short name exists as branch and annotated tag. Update the
	// existing branch using the contents API; Gitea refuses re-creating a
	// deleted branch whose name collides with a tag (HTTP 409).
	var branchMoved struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	request("PUT", "/repos/fixture/repo/contents/marker.txt", map[string]any{"content": base64.StdEncoding.EncodeToString([]byte("branch moved independently\n")), "sha": created.Content.SHA, "branch": "release/v1", "message": "move collision branch"}, &branchMoved)
	same, err := client.ResolveRevision(ctx, repo, gittransport.Ref{Kind: "branch", Name: "release/v1"})
	if err != nil || same.Commit == tag.Commit || same.Commit != branchMoved.Commit.SHA {
		t.Fatal("typed branch/tag collision failed", same, err)
	}
	dir, err := client.Checkout(ctx, repo, branch.Commit, io.Discard)
	if err != nil {
		t.Fatal("private pinned checkout failed", err)
	}
	defer os.RemoveAll(dir)
	marker, err := os.ReadFile(filepath.Join(dir, "marker.txt"))
	if err != nil || string(marker) != "accepted fixture source\n" {
		t.Fatal("moving branch substituted source", err)
	}
	if head, err := gittransport.HeadCommit(ctx, dir); err != nil || head != accepted {
		t.Fatal("HEAD mismatch", head, err)
	}
	gitConfig, _ := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if bytes.Contains(gitConfig, []byte(token.SHA1)) {
		t.Fatal("persisted provider token")
	}
	request("DELETE", fmt.Sprintf("/users/fixture/tokens/%d", token.ID), nil, nil)
	if _, err := client.Repository(ctx, repo.ID); err == nil {
		t.Fatal("revoked credential retained API access")
	}
	t.Logf("Gitea %s accepted repository=%s branch/tag collision, annotated tag peeling, private token checkout commit=%s after branch advanced=%s; revoked token rejected", version.Version, repo.ID, accepted, changed.Commit.SHA)
}
