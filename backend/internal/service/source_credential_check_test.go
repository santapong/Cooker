package service

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/source/github"
)

func credentialProbeFixture(t *testing.T, provider string, server *httptest.Server, repos []string) (*connection.Service, *connection.Connection, *SourceAccess) {
	t.Helper()
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	cs := &connection.Service{Store: connection.NewMemory(), Codec: codec}
	c := &connection.Connection{Name: "Owned credential check", Provider: provider, AuthMode: "token", Username: "owned-user", Repositories: repos, Enabled: false}
	a := &SourceAccess{Connections: cs}
	if provider == "github" {
		a.GitHub = &github.AppClient{BaseURL: server.URL, HTTP: server.Client()}
	} else {
		c.Origin, c.APIOrigin, c.AllowPrivateNetwork = server.URL, server.URL, true
		c.CAPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	}
	if provider == "gitlab" {
		c.WebhookAuthMode = "legacy_token"
	}
	if err := cs.CreateWithCredentials(context.Background(), c, connection.Credentials{Token: "synthetic-old-token", WebhookSecret: strings.Repeat("h", 32)}); err != nil {
		t.Fatal(err)
	}
	cs.TokenProbe = a.CheckCredentialReplacement
	return cs, c, a
}

func TestCredentialReplacementChecksEachProviderWithNewCredential(t *testing.T) {
	for _, provider := range []string{"github", "gitlab", "gitea"} {
		t.Run(provider, func(t *testing.T) {
			var reads, bad atomic.Int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				if r.Method != "GET" {
					bad.Add(1)
				}
				header, want := r.Header.Get("Authorization"), "Bearer synthetic-new-token"
				if provider == "gitlab" {
					header, want = r.Header.Get("PRIVATE-TOKEN"), "synthetic-new-token"
				}
				if provider == "gitea" {
					want = "token synthetic-new-token"
				}
				if header != want {
					bad.Add(1)
					w.WriteHeader(401)
					return
				}
				path := r.URL.Path
				prefix := "/repos/"
				if provider == "gitea" {
					prefix = "/api/v1/repos/"
				}
				if provider == "gitlab" {
					prefix = "/api/v4/projects/"
				}
				if !strings.HasPrefix(path, prefix) {
					bad.Add(1)
					w.WriteHeader(404)
					return
				}
				repo, err := url.PathUnescape(strings.TrimPrefix(path, prefix))
				if err != nil {
					bad.Add(1)
				}
				field := "full_name"
				if provider == "gitlab" {
					field = "path_with_namespace"
				}
				fmt.Fprintf(w, `{"id":31,%q:%q}`, field, repo)
			}))
			defer srv.Close()
			repos := []string{"team/one", "team/two"}
			if provider == "gitlab" {
				repos = []string{"team/sub/one", "team/sub/two"}
			}
			cs, c, _ := credentialProbeFixture(t, provider, srv, repos)
			after, err := cs.ReplaceToken(context.Background(), c.ID, c.Version, "synthetic-new-token", nil)
			if err != nil || after.Enabled || reads.Load() != 2 || bad.Load() != 0 {
				t.Fatal("provider check did not use exact allowed repositories and replacement credential")
			}
			value, _ := cs.OpenCredentials(after)
			if value.WebhookSecret != strings.Repeat("h", 32) {
				t.Fatal("provider check replaced webhook key")
			}
		})
	}
}

func TestCredentialReplacementBoundsConcurrentRepositoryChecks(t *testing.T) {
	var active, peak, reads atomic.Int32
	entered := make(chan struct{}, 12)
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		reads.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprintf(w, `{"id":31,"full_name":%q}`, strings.TrimPrefix(r.URL.Path, "/api/v1/repos/"))
	}))
	defer srv.Close()
	repos := make([]string, 12)
	for i := range repos {
		repos[i] = fmt.Sprintf("team/repo%d", i)
	}
	cs, c, _ := credentialProbeFixture(t, "gitea", srv, repos)
	result := make(chan error, 1)
	go func() {
		_, err := cs.ReplaceToken(context.Background(), c.ID, c.Version, "synthetic-new-token", nil)
		result <- err
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("bounded workers did not start")
		}
	}
	select {
	case <-entered:
		close(release)
		t.Fatal("more than four simultaneous checks")
	default:
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("allowed checks failed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("checks did not finish")
	}
	if peak.Load() != 4 || reads.Load() != 12 {
		t.Fatal("repository checks were unbounded or incomplete")
	}
}

func TestCredentialReplacementRejectsProviderFailuresIdentityMismatchAndRedirect(t *testing.T) {
	for _, mode := range []string{"denied", "invalid-id", "different-allowed-repository", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			var redirected atomic.Int32
			dest := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
			defer dest.Close()
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "denied":
					w.WriteHeader(403)
					io.WriteString(w, "synthetic-private-provider-body")
				case "invalid-id":
					io.WriteString(w, `{"id":0,"full_name":"team/one"}`)
				case "different-allowed-repository":
					io.WriteString(w, `{"id":31,"full_name":"team/two"}`)
				case "redirect":
					http.Redirect(w, r, dest.URL+"/api/v1/repos/team/one", 307)
				}
			}))
			defer srv.Close()
			cs, c, _ := credentialProbeFixture(t, "gitea", srv, []string{"team/one", "team/two"})
			before, _ := cs.Store.Get(context.Background(), c.ID)
			_, err := cs.ReplaceToken(context.Background(), c.ID, c.Version, "synthetic-new-token", nil)
			if !errors.Is(err, connection.ErrCredentialCheck) || strings.Contains(err.Error(), "synthetic-private-provider-body") || redirected.Load() != 0 {
				t.Fatal("unsafe provider result accepted or leaked")
			}
			after, _ := cs.Store.Get(context.Background(), c.ID)
			if before.Version != after.Version || string(before.Credential) != string(after.Credential) {
				t.Fatal("failed provider check saved replacement")
			}
		})
	}
}

func TestCredentialReplacementCancellationAndOldAdmissionRefuseBeforeSourceAccess(t *testing.T) {
	var reads atomic.Int32
	entered := make(chan struct{}, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	cs, c, access := credentialProbeFixture(t, "gitea", srv, []string{"team/one"})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := cs.ReplaceToken(ctx, c.ID, c.Version, "synthetic-new-token", nil); result <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("check did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, connection.ErrCredentialCheck) {
			t.Fatal("cancelled check accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled check continued")
	}
	saved, _ := cs.Store.Get(context.Background(), c.ID)
	if saved.Version != c.Version {
		t.Fatal("cancelled check persisted")
	}
	// A subsequent successful replacement advances authority. Accepted source
	// with the prior version must fail before metadata, checkout or log writes.
	cs.TokenProbe = func(context.Context, *connection.Connection) error { return nil }
	saved.Enabled = true
	if err := cs.Store.Update(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	oldVersion := saved.Version
	if _, err := cs.ReplaceToken(context.Background(), saved.ID, oldVersion, "synthetic-new-token", nil); err != nil {
		t.Fatal(err)
	}
	beforeReads := reads.Load()
	var log strings.Builder
	plan := &model.PipelineSourcePlan{Source: model.GitSourceIdentity{ConnectionID: c.ID, Provider: "gitea", Repository: "team/one", RepositoryID: "31"}, ConnectionVersion: oldVersion, CommitSHA: strings.Repeat("a", 40)}
	if _, err := access.CheckoutSource(context.Background(), plan, &log); err == nil || reads.Load() != beforeReads || log.Len() != 0 {
		t.Fatal("old admitted connection acquired replacement authority")
	}
}
