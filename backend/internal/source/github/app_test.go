package github

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGitHubAppReadOnlyScopesAndRepositoryOwnership(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var tokenCalls, repoCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/app/installations/7", "/repos/acme/shop/installation", "/repos/other/shop/installation":
			token, err := jwt.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), func(token *jwt.Token) (any, error) {
				if token.Method != jwt.SigningMethodRS256 {
					t.Error("JWT method")
				}
				return &key.PublicKey, nil
			}, jwt.WithIssuer("42"))
			if err != nil || !token.Valid {
				t.Errorf("invalid GitHub App JWT: %v", err)
			}
			exp, _ := token.Claims.GetExpirationTime()
			if exp.After(time.Now().Add(10 * time.Minute)) {
				t.Error("JWT too long lived")
			}
			id := 7
			if strings.HasPrefix(r.URL.Path, "/repos/other/") {
				id = 8
			}
			fmt.Fprintf(w, `{"id":%d,"account":{"login":"acme"}}`, id)
		case "/app/installations/7/access_tokens":
			tokenCalls++
			var body struct {
				Permissions  map[string]string `json:"permissions"`
				Repositories []string          `json:"repositories"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Permissions) != 1 || body.Permissions["contents"] != "read" {
				t.Error("token has broader permissions")
			}
			if len(body.Repositories) > 0 && (len(body.Repositories) != 1 || body.Repositories[0] != "shop") {
				t.Error("wrong repository token scope")
			}
			fmt.Fprint(w, `{"token":"fake-short-lived-token"}`)
		case "/installation/repositories":
			repoCalls++
			if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "100" {
				t.Error("pagination missing")
			}
			if r.Header.Get("Authorization") != "Bearer fake-short-lived-token" {
				t.Error("missing installation auth")
			}
			fmt.Fprint(w, `{"repositories":[{"full_name":"acme/shop","private":true,"default_branch":"develop"}]}`)
		case "/repos/acme/shop/tags":
			fmt.Fprint(w, `[{"name":"v1","commit":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}]`)
		default:
			t.Errorf("unexpected GitHub request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := &AppClient{AppID: "42", Key: key, InstallationIDs: []int64{7}, BaseURL: server.URL}
	ctx := context.Background()
	if _, err := c.Token(ctx, 8, "acme/shop"); err == nil {
		t.Fatal("unapproved installation accepted")
	}
	if _, err := c.Token(ctx, 7, "other/shop"); err == nil {
		t.Fatal("wrong owner installation accepted")
	}
	if tokenCalls != 0 {
		t.Fatal("minted token before checking ownership")
	}
	items, err := c.Installations(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("installations: %+v %v", items, err)
	}
	repos, err := c.Repositories(ctx, 7, 2)
	if err != nil || len(repos) != 1 || !repos[0].Private || repoCalls != 1 {
		t.Fatalf("repos: %+v %v", repos, err)
	}
	revisions, err := c.Revisions(ctx, 7, "acme/shop", "tags", 1)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("revisions: %+v %v", revisions, err)
	}
}

func TestGitHubErrorsNeverEchoTokenOrResponseBody(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, "secret-from-response-body")
	}))
	defer s.Close()
	c := &AppClient{BaseURL: s.URL}
	err := c.request(context.Background(), "GET", "/repos/acme/shop", "secret-installation-token", nil, &map[string]any{})
	if err == nil || strings.Contains(err.Error(), "secret-") {
		t.Fatalf("unsafe API error: %v", err)
	}
}

func TestCloneCredentialIsMemoryOnlyAndPinReachesGit(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "http.extraheader")
	t.Setenv("GIT_CONFIG_VALUE_0", "inherited-header")
	root := t.TempDir()
	argsFile := filepath.Join(root, "args")
	envFile := filepath.Join(root, "auth-check")
	t.Setenv("COOKER_TEST_ARGS", argsFile)
	t.Setenv("COOKER_TEST_AUTH", envFile)
	bin := filepath.Join(root, "git-test")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$COOKER_TEST_ARGS"
if [ "$1" = clone ] || [ "$3" = fetch ]; then
  case "$GIT_CONFIG_VALUE_1" in 'Authorization: Basic '*) printf 'scoped\n' >> "$COOKER_TEST_AUTH";; *) exit 10;; esac
fi
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	sha := strings.Repeat("a", 40)
	dir, err := Clone(context.Background(), CloneOptions{Repo: "acme/shop", Branch: "main", Commit: sha, InstallationID: 7, Token: "fake-private-token", GitBin: bin, LogWriter: &logs})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	args, _ := os.ReadFile(argsFile)
	auth, _ := os.ReadFile(envFile)
	if !strings.Contains(string(args), sha) || !strings.Contains(string(args), "checkout\n--detach") || len(auth) == 0 {
		t.Fatal("commit pin or scoped authorization missing")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("x-access-token:fake-private-token"))
	for _, s := range []string{"fake-private-token", encoded, "inherited-header"} {
		if strings.Contains(string(args)+logs.String(), s) {
			t.Fatal("credentials escaped into command/logs")
		}
	}
	joined := strings.Join(cloneEnvironment(""), "\n")
	if strings.Contains(joined, "inherited-header") || !strings.Contains(joined, "GIT_TERMINAL_PROMPT=0") {
		t.Fatal("clone inherited credentials or permits prompting")
	}
}
