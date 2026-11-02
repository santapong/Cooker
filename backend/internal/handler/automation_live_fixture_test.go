//go:build integration

package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type automationLiveFixture struct {
	t       *testing.T
	ctx     context.Context
	project string
	ids     []string
	volumes []string
	report  map[string]any
}

func newAutomationLiveFixture(t *testing.T, ctx context.Context) *automationLiveFixture {
	f := &automationLiveFixture{t: t, ctx: ctx, project: "cooker-git-flow-" + uuid.NewString()[:8], report: map[string]any{"startedAt": time.Now().UTC().Format(time.RFC3339)}}
	f.report["project"] = f.project
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		clean := true
		run := func(args ...string) string {
			out, err := exec.CommandContext(cleanup, "docker", args...).Output()
			if err != nil {
				t.Errorf("owned fixture cleanup %s failed: %v", args[0], err)
				clean = false
			}
			return strings.TrimSpace(string(out))
		}
		label := "label=com.docker.compose.project=" + f.project
		for _, container := range strings.Fields(run("ps", "-aq", "--filter", label)) {
			run("rm", "-f", "-v", container)
		}
		for _, kind := range []string{"network", "volume"} {
			for _, id := range strings.Fields(run(kind, "ls", "-q", "--filter", label)) {
				run(kind, "rm", id)
			}
		}
		for _, id := range f.ids {
			run("rm", "-f", "-v", id)
		}
		for _, id := range f.ids {
			if run("ps", "-aq", "--filter", "id="+id) != "" {
				clean = false
			}
		}
		for _, volume := range f.volumes {
			if run("volume", "ls", "-q", "--filter", "name=^"+volume+"$") != "" {
				clean = false
			}
		}
		for _, args := range [][]string{{"ps", "-aq", "--filter", label}, {"network", "ls", "-q", "--filter", label}, {"volume", "ls", "-q", "--filter", label}} {
			if run(args...) != "" {
				clean = false
			}
		}
		if !clean {
			t.Error("owned full-flow resources remain")
		}
		f.report["cleanupVerified"], f.report["passed"] = clean, !t.Failed()
		if path := os.Getenv("COOKER_GITEA_FLOW_EVIDENCE_FILE"); path != "" {
			data, err := json.MarshalIndent(f.report, "", "  ")
			if err != nil {
				t.Error(err)
			} else if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
				t.Error(err)
			}
		}
		t.Logf("full-flow cleanup verified=%t project=%s", clean, f.project)
	})
	return f
}

func (f *automationLiveFixture) docker(args ...string) string {
	f.t.Helper()
	out, err := exec.CommandContext(f.ctx, "docker", args...).Output()
	if err != nil {
		f.t.Fatalf("fixture Docker %s: %v", args[0], err)
	}
	return strings.TrimSpace(string(out))
}

func (f *automationLiveFixture) container(args ...string) string {
	f.t.Helper()
	id := f.docker(append([]string{"run", "-d"}, args...)...)
	f.ids = append(f.ids, id)
	mounts := f.docker("inspect", "--format", "{{range .Mounts}}{{if eq .Type \"volume\"}}{{.Name}}{{println}}{{end}}{{end}}", id)
	f.volumes = append(f.volumes, strings.Fields(mounts)...)
	return id
}

func (f *automationLiveFixture) postgres() string {
	f.t.Helper()
	id := f.container("--name", f.project+"-pg", "-e", "POSTGRES_DB=cooker_gitea_flow", "-e", "POSTGRES_PASSWORD=disposable-fixture-only", "-p", "127.0.0.1::5432", "postgres:16-alpine")
	port := f.docker("port", id, "5432/tcp")
	if !strings.HasPrefix(port, "127.0.0.1:") || strings.Contains(port, "\n") {
		f.t.Fatal("PostgreSQL not bound to loopback")
	}
	dsn := "postgres://postgres:disposable-fixture-only@" + port + "/cooker_gitea_flow?sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		f.t.Fatal(err)
	}
	defer db.Close()
	f.wait("PostgreSQL ready", func() bool { return db.PingContext(f.ctx) == nil })
	return dsn
}

func (f *automationLiveFixture) wait(label string, fn func() bool) {
	f.t.Helper()
	for end := time.Now().Add(75 * time.Second); time.Now().Before(end); {
		if fn() {
			return
		}
		select {
		case <-f.ctx.Done():
			f.t.Fatal(f.ctx.Err())
		case <-time.After(150 * time.Millisecond):
		}
	}
	f.t.Fatalf("timed out: %s", label)
}

func (f *automationLiveFixture) request(client *http.Client, method, url string, payload any, headers map[string]string, want int, result any) []byte {
	f.t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(f.ctx, method, url, bytes.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		f.t.Fatalf("fixture HTTP %s transport failed: %v", method, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		f.t.Fatal(err)
	}
	if resp.StatusCode != want {
		// Return only the product's generic error string; response bodies can
		// otherwise contain newly generated fixture tokens.
		var failure struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &failure)
		f.t.Fatalf("fixture HTTP %s expected %d got %d (%s %s)", method, want, resp.StatusCode, failure.Error, failure.Message)
	}
	if result != nil {
		if err := json.Unmarshal(data, result); err != nil {
			f.t.Fatal("decode fixture response", err)
		}
	}
	return data
}

func fixtureBasic(user, password string) string {
	r, _ := http.NewRequest("GET", "http://fixture.invalid", nil)
	r.SetBasicAuth(user, password)
	return r.Header.Get("Authorization")
}

func flowError(format string, args ...any) error { return fmt.Errorf(format, args...) }
