package gittransport

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkPolicyAndOriginValidation(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://user:secret@example.com", "https://", "https://example.com/", "https://example.com?", "https://example.com/a", "https://example.com#x", "https://EXAMPLE.com", "https://example.com.", "https://169.254.169.254", "https://168.63.129.16", "https://100.100.100.200", "https://[fd00:ec2::254]", "https://[::ffff:169.254.169.254]"} {
		if ValidateOrigin(raw, true) == nil {
			t.Errorf("accepted invalid origin %q", raw)
		}
	}
	for _, addr := range []string{"127.0.0.1", "10.1.1.1", "172.16.1.1", "192.168.1.1", "::1", "fd01::1"} {
		ip := netip.MustParseAddr(addr)
		if allowedAddress(ip, false) || !allowedAddress(ip, true) {
			t.Errorf("private policy incorrect for %s", addr)
		}
	}
	if !allowedAddress(netip.MustParseAddr("8.8.8.8"), false) {
		t.Fatal("public destination blocked")
	}
	for _, addr := range []string{"0.0.0.0", "169.254.169.254", "fe80::1", "ff00::1", "224.0.0.1", "64:ff9b::a9fe:a9fe", "2002:a9fe:a9fe::1"} {
		if allowedAddress(netip.MustParseAddr(addr), true) {
			t.Errorf("private approval permits forbidden %s", addr)
		}
	}
}
func configForTLS(s *httptest.Server) Config {
	return Config{Origin: s.URL, APIOrigin: s.URL, AllowPrivateNetwork: true, CAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}))}
}
func TestHTTPTrustRedirectAndBodyBoundaries(t *testing.T) {
	received := false
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true }))
	defer other.Close()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			fmt.Fprint(w, `{"id":1}`)
		case "/large":
			fmt.Fprint(w, strings.Repeat(" ", 2<<20)+"{}")
		default:
			http.Redirect(w, r, other.URL, http.StatusFound)
		}
	}))
	defer s.Close()
	cfg := configForTLS(s)
	client, err := NewHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if _, err := GetJSON(context.Background(), client, cfg.APIOrigin, "/ok", nil, &out); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/redirect", "/large"} {
		if _, err := GetJSON(context.Background(), client, cfg.APIOrigin, path, http.Header{"Authorization": []string{"secret-fixture"}}, &out); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	if received {
		t.Fatal("followed redirect with a source credential")
	}
	cfg.AllowPrivateNetwork = false
	client, err = NewHTTPClient(cfg)
	if err == nil {
		_, err = GetJSON(context.Background(), client, cfg.APIOrigin, "/ok", nil, &out)
	}
	if err == nil {
		t.Fatal("private host accepted without policy")
	}
	cfg = configForTLS(s)
	cfg.CAPEM = ""
	client, err = NewHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = GetJSON(context.Background(), client, cfg.APIOrigin, "/ok", nil, &out); err == nil {
		t.Fatal("untrusted TLS accepted")
	}
}
func TestTypedRefsAndSourcePaths(t *testing.T) {
	for _, ref := range []Ref{{"branch", "-main"}, {"branch", "../main"}, {"tag", "v1.lock"}, {"tag", "v1\n"}, {"branch", "release//one"}, {"branch", "a@{b"}, {"commit", "abc"}} {
		if _, err := ref.FullName(); err == nil {
			t.Errorf("accepted %+v", ref)
		}
	}
	for _, kind := range []string{"branch", "tag"} {
		full, err := (Ref{kind, "release/v1"}).FullName()
		if err != nil || !strings.HasSuffix(full, "release/v1") {
			t.Fatal(full, err)
		}
	}
	if ValidateRepositoryPath("team/subteam/repo", true) != nil || ValidateRepositoryPath("team/subteam/repo", false) == nil {
		t.Fatal("nested repository policy")
	}
	for _, p := range []string{"../repo", "team/repo.git", "team/repo?token=x", "/team/repo", "team//repo", "team/-repo"} {
		if ValidateRepositoryPath(p, true) == nil {
			t.Errorf("accepted %s", p)
		}
	}
}
func TestGitEnvironmentDoesNotInheritSecretsOrHelpers(t *testing.T) {
	t.Setenv("COOKER_DATABASE_PASSWORD", "do-not-inherit")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "!malicious")
	t.Setenv("HTTPS_PROXY", "https://bad.example")
	env := strings.Join(isolatedEnvironment("/tmp/private", nil), "\n")
	for _, bad := range []string{"do-not-inherit", "!malicious", "bad.example", "COOKER_DATABASE_PASSWORD"} {
		if strings.Contains(env, bad) {
			t.Fatal("ambient value leaked into Git")
		}
	}
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "credential.helper", "http.followRedirects", "protocol.allow", "core.hooksPath"} {
		if !strings.Contains(env, want) {
			t.Fatal("missing isolation setting", want)
		}
	}
}
func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(isolatedEnvironment(t.TempDir(), []setting{{"protocol.file.allow", "always"}}), "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func TestCloneExactSmartHTTPAndUnsupportedSources(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	// This is a real Git smart-HTTPS fixture, not a hosted-provider acceptance.
	root := t.TempDir()
	bare := filepath.Join(root, "team", "repo.git")
	if err := os.MkdirAll(bare, 0700); err != nil {
		t.Fatal(err)
	}
	testGit(t, bare, "init", "--bare")
	work := t.TempDir()
	testGit(t, work, "init")
	os.WriteFile(filepath.Join(work, "marker.txt"), []byte("accepted"), 0600)
	testGit(t, work, "add", ".")
	testGit(t, work, "commit", "-m", "accepted")
	accepted := testGit(t, work, "rev-parse", "HEAD")
	testGit(t, work, "push", bare, "HEAD:refs/heads/main")
	os.WriteFile(filepath.Join(work, "marker.txt"), []byte("moved"), 0600)
	testGit(t, work, "commit", "-am", "moved")
	testGit(t, work, "push", bare, "HEAD:refs/heads/main")
	path := strings.TrimSpace(testGit(t, work, "--exec-path"))
	handler := &cgi.Handler{Path: filepath.Join(path, "git-http-backend"), Dir: root, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "PATH=/usr/bin:/bin"}}
	token := "private-fixture-token"
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("fixture:"+token))
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			w.WriteHeader(401)
			return
		}
		requests++
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	opts := CloneOptions{Config: configForTLS(server), Credentials: Credentials{Username: "fixture", Token: token}, Repository: "team/repo", Commit: accepted}
	dir, err := CloneExact(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	got, err := os.ReadFile(filepath.Join(dir, "marker.txt"))
	if err != nil || string(got) != "accepted" {
		t.Fatal("did not preserve the accepted commit", err)
	}
	if head, err := HeadCommit(context.Background(), dir); err != nil || head != accepted {
		t.Fatal("HEAD mismatch", head, err)
	}
	config, _ := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if strings.Contains(string(config), token) || strings.Contains(string(config), base64.StdEncoding.EncodeToString([]byte("fixture:"+token))) {
		t.Fatal("persisted credentials")
	}
	if requests < 2 {
		t.Fatal("smart HTTP was not exercised")
	}
	// A gitlink is rejected before checkout; no submodule endpoint is contacted.
	testGit(t, work, "update-index", "--add", "--cacheinfo", "160000,"+accepted+",external")
	testGit(t, work, "commit", "-m", "submodule")
	opts.Commit = testGit(t, work, "rev-parse", "HEAD")
	testGit(t, work, "push", bare, "HEAD:refs/heads/submodule")
	if dir, err := CloneExact(context.Background(), opts); err == nil || dir != "" || !strings.Contains(err.Error(), "submodules") {
		os.RemoveAll(dir)
		t.Fatal("gitlink not rejected", err)
	}
	testGit(t, work, "reset", "--hard", accepted)
	os.WriteFile(filepath.Join(work, ".gitattributes"), []byte("*.txt filter=lfs\n"), 0600)
	testGit(t, work, "add", ".")
	testGit(t, work, "commit", "-m", "lfs")
	opts.Commit = testGit(t, work, "rev-parse", "HEAD")
	testGit(t, work, "push", bare, "HEAD:refs/heads/lfs")
	if dir, err := CloneExact(context.Background(), opts); err == nil || dir != "" || !strings.Contains(err.Error(), "filters") {
		os.RemoveAll(dir)
		t.Fatal("LFS not rejected", err)
	}
	opts.Credentials.Token = "revoked-fixture"
	opts.Commit = accepted
	if dir, err := CloneExact(context.Background(), opts); err == nil || dir != "" {
		os.RemoveAll(dir)
		t.Fatal("revoked credential was accepted")
	}
}

func TestGitOutputLimitCannotBeBypassedByIOCopy(t *testing.T) {
	var out boundedOutput
	_, err := io.Copy(&out, strings.NewReader(strings.Repeat("x", (16<<20)+1)))
	if err == nil || len(out.Bytes()) > 16<<20 {
		t.Fatal("source output exceeded capture limit")
	}
}
