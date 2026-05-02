package keepsave

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/secrets"
	"github.com/cooker-ci/cooker/internal/store/memory"
)

// fakeKeepSave mimics the subset of the KeepSave HTTP API the adapter
// exercises. Storage is in-memory; secrets are keyed by environment.
type fakeKeepSave struct {
	mu   sync.Mutex
	data map[string]map[string]secretDTO
	next int
}

func newFake() *fakeKeepSave {
	return &fakeKeepSave{data: map[string]map[string]secretDTO{}}
}

func (f *fakeKeepSave) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if !strings.HasPrefix(r.URL.Path, "/api/v1/projects/") {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/projects/"), "/")
	switch {
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "secrets":
		env := r.URL.Query().Get("environment")
		out := []secretDTO{}
		for _, dto := range f.data[env] {
			out = append(out, dto)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"secrets": out})
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "secrets":
		var req struct{ Key, Value, Environment string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.next++
		dto := secretDTO{ID: "sec_" + strconv.Itoa(f.next), Key: req.Key, Value: req.Value, EnvironmentID: req.Environment}
		if f.data[req.Environment] == nil {
			f.data[req.Environment] = map[string]secretDTO{}
		}
		f.data[req.Environment][req.Key] = dto
		_ = json.NewEncoder(w).Encode(map[string]any{"secret": dto})
	case r.Method == http.MethodPut && len(parts) == 3 && parts[1] == "secrets":
		secretID := parts[2]
		var req struct{ Value string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		for env, m := range f.data {
			for k, dto := range m {
				if dto.ID == secretID {
					dto.Value = req.Value
					f.data[env][k] = dto
					_ = json.NewEncoder(w).Encode(map[string]any{"secret": dto})
					return
				}
			}
		}
		http.Error(w, `{"error":{"code":404,"message":"not found"}}`, http.StatusNotFound)
	case r.Method == http.MethodDelete && len(parts) == 3 && parts[1] == "secrets":
		secretID := parts[2]
		for env, m := range f.data {
			for k, dto := range m {
				if dto.ID == secretID {
					delete(f.data[env], k)
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		w.WriteHeader(http.StatusNotFound)
	default:
		http.NotFound(w, r)
	}
}

func TestKeepSaveRoundTrip(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	st := memory.New()
	if err := st.Environments.Create(context.Background(), &model.Environment{ID: "e1", Name: "prod"}); err != nil {
		t.Fatal(err)
	}

	m := New(NewClient(srv.URL, "ks_test"), st.Environments, "proj-1")
	ctx := context.Background()

	if err := m.Put(ctx, "e1", "DB", []byte("v1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := m.Get(ctx, "e1", "DB")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "v1" {
		t.Errorf("got %q want v1", got)
	}

	if err := m.Put(ctx, "e1", "DB", []byte("v2")); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = m.Get(ctx, "e1", "DB")
	if string(got) != "v2" {
		t.Errorf("after update: got %q want v2", got)
	}

	keys, err := m.List(ctx, "e1")
	if err != nil || len(keys) != 1 || keys[0] != "DB" {
		t.Errorf("list: keys=%v err=%v", keys, err)
	}

	if err := m.Delete(ctx, "e1", "DB"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := m.Get(ctx, "e1", "DB"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("after delete: expected ErrNotFound, got %v", err)
	}
}

func TestKeepSaveUnknownEnv(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	st := memory.New()
	m := New(NewClient(srv.URL, "k"), st.Environments, "p")
	if _, err := m.Get(context.Background(), "missing", "K"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestKeepSaveDeleteIdempotent(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	st := memory.New()
	st.Environments.Create(context.Background(), &model.Environment{ID: "e1", Name: "prod"})
	m := New(NewClient(srv.URL, "k"), st.Environments, "p")
	if err := m.Delete(context.Background(), "e1", "absent"); err != nil {
		t.Errorf("delete absent: %v", err)
	}
}
