package vault

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/secrets"
)

const vaultTime = "2026-10-02T00:00:00.123Z"
const vaultCanary = "SECRET_CANARY_VAULT_VALUE"

type vaultFixture struct {
	current   int
	created   string
	values    map[string]string
	fail      bool
	mode      string
	calls     int
	requested string
}

func fixture(t *testing.T) (*Manager, *vaultFixture) {
	t.Helper()
	f := &vaultFixture{current: 1, created: vaultTime, values: map[string]string{"1": vaultCanary}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		w.Header().Set("Content-Type", "application/json")
		if f.fail {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(vaultCanary))
			return
		}
		switch r.URL.Path {
		case "/v1/secret/metadata/cooker/env/PASSWORD":
			metadata := map[string]any{"created_time": f.created, "deletion_time": "", "destroyed": false}
			if f.mode == "bad-created" {
				metadata["created_time"] = vaultCanary
			}
			if f.mode == "deleted" {
				metadata["deletion_time"] = vaultTime
			}
			if f.mode == "destroyed" {
				metadata["destroyed"] = true
			}
			if f.mode == "missing-destroyed" {
				delete(metadata, "destroyed")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"current_version": f.current, "versions": map[string]any{"1": metadata, "2": metadata}, "custom_metadata": map[string]any{"private": vaultCanary}}})
		case "/v1/secret/data/cooker/env/PASSWORD":
			f.requested = r.URL.Query().Get("version")
			value, ok := f.values[f.requested]
			if !ok {
				http.NotFound(w, r)
				return
			}
			metadata := map[string]any{"version": 1, "created_time": f.created, "deletion_time": "", "destroyed": false}
			if f.mode == "wrong-version" {
				metadata["version"] = 2
			}
			if f.mode == "bad-number" {
				metadata["version"] = 1.5
			}
			if f.mode == "read-deleted" {
				metadata["deletion_time"] = vaultTime
			}
			if f.mode == "read-destroyed" {
				metadata["destroyed"] = true
			}
			data := map[string]any{"value": value}
			if f.mode == "non-string" {
				data["value"] = 123
			}
			if f.mode == "missing-value" {
				delete(data, "value")
			}
			if f.mode == "missing-metadata" {
				metadata = nil
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data, "metadata": metadata}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	m, err := New(Config{Address: srv.URL, Mount: "secret", Prefix: "cooker", Token: "fake-local-test"})
	if err != nil {
		t.Fatal(err)
	}
	return m, f
}
func TestVaultExactVersionAcrossRotation(t *testing.T) {
	m, f := fixture(t)
	r, err := m.Pin(context.Background(), "env", "PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	f.current = 2
	f.values["2"] = "rotated-value"
	b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
	if err != nil || string(b) != vaultCanary || f.requested != "1" {
		t.Fatal("unpinned read", err)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), vaultCanary) {
		t.Fatal("secret in metadata")
	}
}
func TestVaultScopeRejectedBeforeRequest(t *testing.T) {
	for _, which := range []string{"authority", "mount", "prefix", "env", "key"} {
		t.Run(which, func(t *testing.T) {
			m, f := fixture(t)
			r, _ := m.Pin(context.Background(), "env", "PASSWORD")
			f.calls = 0
			switch which {
			case "authority":
				m.authority = "https://other.test"
			case "mount":
				m.mount = "other"
			case "prefix":
				m.prefix = "other"
			case "env":
				r.Scope.EnvironmentID = "other"
			case "key":
				r.Scope.Key = "other"
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
func TestVaultRecreateDeletionMalformedAndSafeErrors(t *testing.T) {
	for _, which := range []string{"recreate", "deleted-history", "permission", "wrong-version", "bad-number", "read-deleted", "read-destroyed", "non-string", "missing-value", "missing-metadata"} {
		t.Run(which, func(t *testing.T) {
			m, f := fixture(t)
			r, err := m.Pin(context.Background(), "env", "PASSWORD")
			if err != nil {
				t.Fatal(err)
			}
			switch which {
			case "recreate":
				f.created = "2026-10-03T00:00:00Z"
			case "deleted-history":
				delete(f.values, "1")
			case "permission":
				f.fail = true
			default:
				f.mode = which
			}
			b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
			if err == nil || b != nil || strings.Contains(err.Error(), vaultCanary) {
				t.Fatal("unsafe failure")
			}
		})
	}
}
func TestVaultPinUnavailableMetadata(t *testing.T) {
	for _, which := range []string{"bad-created", "deleted", "destroyed", "missing-destroyed", "permission", "zero"} {
		t.Run(which, func(t *testing.T) {
			m, f := fixture(t)
			if which == "permission" {
				f.fail = true
			} else if which == "zero" {
				f.current = 0
			} else {
				f.mode = which
			}
			r, err := m.Pin(context.Background(), "env", "PASSWORD")
			if err == nil || r.Resource != "" || strings.Contains(err.Error(), vaultCanary) {
				t.Fatal("unsafe pin")
			}
		})
	}
}
func TestVaultEmptyValue(t *testing.T) {
	m, f := fixture(t)
	r, _ := m.Pin(context.Background(), "env", "PASSWORD")
	f.values["1"] = ""
	b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
	if err != nil || b == nil || len(b) != 0 {
		t.Fatal(err)
	}
}

func TestVaultVersionReadsRejectRedirects(t *testing.T) {
	called := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer origin.Close()
	m, err := New(Config{Address: origin.URL, Prefix: "cooker", Token: "fake-local-test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Pin(context.Background(), "env", "PASSWORD"); !errors.Is(err, secrets.ErrVersionUnavailable) || called != 0 {
		t.Fatal("redirected immutable read")
	}
}
