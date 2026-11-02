package keepsave

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store/memory"
)

const projectUUID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
const secretUUID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
const envUUID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
const ksCanary = "SECRET_CANARY_KEEPSAVE_VALUE"

type revisionFixture struct {
	current revisionMetadata
	history revisionValue
	fail    bool
	mode    string
	calls   int
	path    string
}

func versionFixture(t *testing.T) (*Manager, *revisionFixture) {
	t.Helper()
	created, _ := time.Parse(time.RFC3339Nano, "2026-10-02T00:00:00.123Z")
	record := revisionMetadata{ID: secretUUID, ProjectID: projectUUID, EnvironmentID: envUUID, Environment: "production", Key: "PASSWORD", Revision: 1, CreatedAt: created}
	value := ksCanary
	f := &revisionFixture{current: record, history: revisionValue{revisionMetadata: record, Value: &value}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		f.path = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		if f.fail {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(ksCanary))
			return
		}
		if f.mode == "oversized" {
			_, _ = w.Write([]byte(strings.Repeat("a", (1<<20)+2)))
			return
		}
		if f.mode == "malformed" {
			_, _ = w.Write([]byte(ksCanary))
			return
		}
		if r.URL.Path == "/api/v1/projects/"+projectUUID+"/secrets" {
			if r.URL.Query().Get("environment") != "production" {
				http.NotFound(w, r)
				return
			}
			if f.mode == "legacy" {
				_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{{"id": secretUUID, "key": "PASSWORD", "version": 1, "value": ksCanary}}})
				return
			}
			if f.mode == "missing" {
				_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []any{}})
				return
			}
			records := []revisionMetadata{f.current}
			if f.mode == "duplicate" {
				records = append(records, f.current)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": records})
			return
		}
		if r.URL.Path == "/api/v1/projects/"+projectUUID+"/secrets/"+secretUUID+"/versions/1" {
			if f.mode == "deleted" {
				http.NotFound(w, r)
				return
			}
			if f.mode == "legacy-history" {
				_ = json.NewEncoder(w).Encode(map[string]any{"secret_id": secretUUID, "version": 1, "value": ksCanary})
				return
			}
			_ = json.NewEncoder(w).Encode(f.history)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	st := memory.New()
	if err := st.Environments.Create(context.Background(), &model.Environment{ID: "env", Name: "production"}); err != nil {
		t.Fatal(err)
	}
	return New(NewClient(srv.URL, "fake-local-test"), st.Environments, projectUUID), f
}
func TestKeepSaveRevisionHistoryAcrossRotation(t *testing.T) {
	m, f := versionFixture(t)
	r, err := m.Pin(context.Background(), "env", "PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	f.current.Revision = 2
	b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
	if err != nil || string(b) != ksCanary {
		t.Fatal("old revision failed", err)
	}
	if !strings.HasSuffix(f.path, "/versions/1") {
		t.Fatal("latest read")
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), ksCanary) || strings.Contains(string(raw), m.client.APIKey) {
		t.Fatal("secret in reference")
	}
}
func TestKeepSaveScopeRejectedBeforeRequest(t *testing.T) {
	for _, which := range []string{"authority", "project", "env", "key", "environment-rename", "provider"} {
		t.Run(which, func(t *testing.T) {
			m, f := versionFixture(t)
			r, _ := m.Pin(context.Background(), "env", "PASSWORD")
			f.calls = 0
			switch which {
			case "authority":
				m.client.BaseURL = "https://other.test"
			case "project":
				m.projectID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
			case "env":
				r.Scope.EnvironmentID = "other"
			case "key":
				r.Scope.Key = "other"
			case "environment-rename":
				env, _ := m.envs.Get(context.Background(), "env")
				env.Name = "renamed"
				_ = m.envs.Update(context.Background(), env)
			case "provider":
				r.Provider = "vault"
			}
			if _, err := m.GetPinned(context.Background(), "env", "PASSWORD", r); !errors.Is(err, secrets.ErrVersionMismatch) {
				t.Fatal(err)
			}
			if f.calls != 0 {
				t.Fatal("read before scope fence")
			}
		})
	}
}
func TestKeepSaveUnavailableRecreatedMalformedAndSafeErrors(t *testing.T) {
	for _, which := range []string{"permission", "deleted", "recreated-id", "recreated-time", "wrong-revision", "wrong-environment", "wrong-project", "wrong-key", "deleted-record", "legacy-history", "malformed", "oversized"} {
		t.Run(which, func(t *testing.T) {
			m, f := versionFixture(t)
			r, _ := m.Pin(context.Background(), "env", "PASSWORD")
			switch which {
			case "permission":
				f.fail = true
			case "recreated-id":
				f.history.ID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
			case "recreated-time":
				f.history.CreatedAt = f.history.CreatedAt.Add(time.Hour)
			case "wrong-revision":
				f.history.Revision = 2
			case "wrong-environment":
				f.history.Environment = "preview"
			case "wrong-project":
				f.history.ProjectID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
			case "wrong-key":
				f.history.Key = ksCanary
			case "deleted-record":
				f.history.Deleted = true
			default:
				f.mode = which
			}
			b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
			if err == nil || b != nil || strings.Contains(err.Error(), ksCanary) {
				t.Fatal("unsafe failure")
			}
		})
	}
}
func TestKeepSavePinRequiresRevisionProtocol(t *testing.T) {
	for _, which := range []string{"legacy", "duplicate", "missing", "bad-id", "bad-created", "malformed", "permission"} {
		t.Run(which, func(t *testing.T) {
			m, f := versionFixture(t)
			switch which {
			case "bad-id":
				f.current.ID = ksCanary
			case "bad-created":
				f.current.CreatedAt = time.Time{}
			case "permission":
				f.fail = true
			default:
				f.mode = which
			}
			r, err := m.Pin(context.Background(), "env", "PASSWORD")
			if err == nil || r.Resource != "" || strings.Contains(err.Error(), ksCanary) {
				t.Fatal("unsafe pin")
			}
			if which == "legacy" && !errors.Is(err, secrets.ErrVersionUnsupported) {
				t.Fatal(err)
			}
		})
	}
}
func TestKeepSaveEmptyRevisionAndRedirectRefusal(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		m, f := versionFixture(t)
		r, _ := m.Pin(context.Background(), "env", "PASSWORD")
		f.history.Value = nil
		b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
		if err != nil || b == nil || len(b) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("redirect", func(t *testing.T) {
		called := 0
		destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }))
		defer destination.Close()
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
		defer origin.Close()
		client := NewClient(origin.URL, "fake-local-test")
		var out any
		if err := client.versionRead(context.Background(), "/read", &out); !errors.Is(err, secrets.ErrVersionUnavailable) || called != 0 {
			t.Fatal("followed redirect")
		}
	})
}
