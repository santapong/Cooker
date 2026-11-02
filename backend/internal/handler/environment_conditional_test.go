package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type metadataOnlySecretFixture struct {
	keys                                       []string
	listErr                                    error
	callsGet, callsPut, callsDelete, callsList int
	onList                                     func(context.Context)
}

func (f *metadataOnlySecretFixture) Get(context.Context, string, string) ([]byte, error) {
	f.callsGet++
	return []byte("owned-reveal-secret-canary"), nil
}
func (f *metadataOnlySecretFixture) Put(context.Context, string, string, []byte) error {
	f.callsPut++
	return f.listErr
}
func (f *metadataOnlySecretFixture) Delete(context.Context, string, string) error {
	f.callsDelete++
	return f.listErr
}
func (f *metadataOnlySecretFixture) List(ctx context.Context, _ string) ([]string, error) {
	f.callsList++
	if f.onList != nil {
		f.onList(ctx)
	}
	return f.keys, f.listErr
}

func conditionalHandlerFixture(t *testing.T) (*Handler, *gin.Engine) {
	t.Helper()
	h := newTestHandler(t)
	if err := h.Store.Environments.Create(context.Background(), &model.Environment{ID: "conditional-env", Name: "Production", Version: 7, PlainVars: map[string]string{"DATABASE_NAME": "app"}}); err != nil {
		t.Fatal(err)
	}
	admin := &auth.Claims{Subject: "owned-admin", Roles: []string{string(auth.RoleAdmin)}}
	viewer := &auth.Claims{Subject: "owned-viewer", Roles: []string{string(auth.RoleViewer)}}
	r := gin.New()
	r.GET("/environments/:id/secret-keys", withUser(h.ListEnvironmentSecretKeys, viewer))
	r.PUT("/environments/:id/secrets/:key", withUser(h.PutSecret, admin))
	r.DELETE("/environments/:id/secrets/:key", withUser(h.DeleteSecret, admin))
	r.PUT("/environments/:id", withUser(h.UpdateEnvironment, admin))
	return h, r
}
func conditionalRequest(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(method, path, body))
	return w
}

func TestConditionalSecretHTTPMetadataAndStaleEdits(t *testing.T) {
	h, r := conditionalHandlerFixture(t)
	w := conditionalRequest(r, http.MethodPut, "/environments/conditional-env/secrets/PASSWORD", map[string]any{"value": "owned-http-secret-canary", "version": 7})
	if w.Code != 200 || strings.Contains(w.Body.String(), "canary") {
		t.Fatal("conditional creation failed or leaked")
	}
	var result struct{ Generation int }
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Generation != 8 {
		t.Fatal("wrong committed generation")
	}
	w = conditionalRequest(r, http.MethodGet, "/environments/conditional-env/secret-keys", nil)
	var metadata struct {
		Keys               []string
		Generation         int
		ConditionalEditing bool
		SourceAvailability string
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &metadata) != nil || metadata.Generation != 8 || !metadata.ConditionalEditing || metadata.SourceAvailability != "available" || !reflect.DeepEqual(metadata.Keys, []string{"PASSWORD"}) || w.Header().Get("Cache-Control") != "no-store, private" || strings.Contains(w.Body.String(), "canary") {
		t.Fatal("metadata contract failed")
	}
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPut, "/environments/conditional-env/secrets/PASSWORD", map[string]any{"value": "stale-private-value", "version": 7}},
		{http.MethodDelete, "/environments/conditional-env/secrets/PASSWORD?version=7", nil},
	} {
		w = conditionalRequest(r, tc.method, tc.path, tc.body)
		if w.Code != 409 || strings.Contains(w.Body.String(), "private-value") {
			t.Fatal("stale edit not rejected safely")
		}
	}
	got, err := h.Secrets.Get(context.Background(), "conditional-env", "PASSWORD")
	if err != nil || string(got) != "owned-http-secret-canary" {
		t.Fatal("stale request changed stored value")
	}
	w = conditionalRequest(r, http.MethodDelete, "/environments/conditional-env/secrets/PASSWORD?version=8", nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Generation != 9 {
		t.Fatal("fresh delete generation failed")
	}
}

func TestConditionalSecretHTTPExternalNeverDowngrades(t *testing.T) {
	h, r := conditionalHandlerFixture(t)
	f := &metadataOnlySecretFixture{keys: []string{"Z", "A", "A"}}
	h.Secrets = f
	w := conditionalRequest(r, http.MethodGet, "/environments/conditional-env/secret-keys", nil)
	var m struct {
		Keys               []string
		Generation         int
		ConditionalEditing bool
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &m) != nil || m.ConditionalEditing || m.Generation != 7 || !reflect.DeepEqual(m.Keys, []string{"A", "Z"}) || !reflect.DeepEqual(f.keys, []string{"Z", "A", "A"}) || f.callsGet != 0 {
		t.Fatal("external metadata invented CAS or retrieved values")
	}
	w = conditionalRequest(r, http.MethodPut, "/environments/conditional-env/secrets/K", map[string]any{"value": "must-not-mutate", "version": 7})
	if w.Code != 501 {
		t.Fatal("unsupported conditional Put accepted")
	}
	w = conditionalRequest(r, http.MethodDelete, "/environments/conditional-env/secrets/K?version=7", nil)
	if w.Code != 501 || f.callsPut != 0 || f.callsDelete != 0 {
		t.Fatal("unsupported conditional delete downgraded")
	}
	// Existing unspecified routes remain available, without a generation claim.
	w = conditionalRequest(r, http.MethodPut, "/environments/conditional-env/secrets/K", map[string]string{"value": "legacy"})
	if w.Code != 200 || strings.Contains(w.Body.String(), "generation") || f.callsPut != 1 {
		t.Fatal("legacy external compatibility changed")
	}
}

func TestConditionalSecretHTTPValidationAndRolesBeforeMutation(t *testing.T) {
	h, r := conditionalHandlerFixture(t)
	f := &metadataOnlySecretFixture{}
	h.Secrets = f
	for _, value := range []any{nil, -1, 1.5, "7", true} {
		w := conditionalRequest(r, http.MethodPut, "/environments/conditional-env/secrets/K", map[string]any{"value": "private", "version": value})
		if w.Code != 400 {
			t.Fatalf("invalid body generation status=%d", w.Code)
		}
	}
	for _, query := range []string{"version=", "version=-1", "version=1.5", "version=7&version=7", "version=92233720368547758080"} {
		w := conditionalRequest(r, http.MethodDelete, "/environments/conditional-env/secrets/K?"+query, nil)
		if w.Code != 400 {
			t.Fatalf("invalid query generation status=%d", w.Code)
		}
	}
	if f.callsPut != 0 || f.callsDelete != 0 {
		t.Fatal("invalid requests mutated external backend")
	}
	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleOperator, auth.RoleApprover} {
		rr := gin.New()
		claims := &auth.Claims{Roles: []string{string(role)}}
		rr.PUT("/environments/:id/secrets/:key", withUser(h.PutSecret, claims))
		rr.DELETE("/environments/:id/secrets/:key", withUser(h.DeleteSecret, claims))
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			w := conditionalRequest(rr, method, "/environments/conditional-env/secrets/K?version=7", map[string]any{"value": "private", "version": 7})
			if w.Code != 403 {
				t.Fatal("nonadmin mutation permitted")
			}
		}
	}
	if f.callsPut != 0 || f.callsDelete != 0 {
		t.Fatal("role gate called provider")
	}
}

func TestConditionalSecretHTTPKeyCollisionAndPlainAlias(t *testing.T) {
	h, r := conditionalHandlerFixture(t)
	w := conditionalRequest(r, http.MethodPut, "/environments/conditional-env/secrets/DATABASE_NAME", map[string]any{"value": "protected", "version": 7})
	if w.Code != 400 {
		t.Fatal("readable key accepted as secret")
	}
	w = conditionalRequest(r, http.MethodPut, "/environments/conditional-env/secrets/PASSWORD", map[string]any{"value": "owned-collision-canary", "version": 7})
	if w.Code != 200 {
		t.Fatal("setup")
	}
	for _, field := range []string{"plainVars", "variables"} {
		w = conditionalRequest(r, http.MethodPut, "/environments/conditional-env", map[string]any{"name": "Production", "version": 8, field: map[string]string{"PASSWORD": "exposed-value"}})
		if w.Code != 400 || strings.Contains(w.Body.String(), "canary") || strings.Contains(w.Body.String(), "exposed-value") {
			t.Fatal("secret became readable configuration")
		}
	}
	e, err := h.Store.Environments.Get(context.Background(), "conditional-env")
	if err != nil || e.Version != 8 || e.PlainVars["PASSWORD"] != "" {
		t.Fatal("collision changed environment")
	}
}

func TestSecretKeysMetadataPrivateUnavailableMalformedAndGenerationChange(t *testing.T) {
	h, r := conditionalHandlerFixture(t)
	f := &metadataOnlySecretFixture{keys: []string{"K"}}
	h.Secrets = f
	for _, env := range []*model.Environment{{ID: "registry_owned", Name: "private"}, {ID: "friendly", Name: " _REGISTRIES (internal) "}} {
		if err := h.Store.Environments.Create(context.Background(), env); err != nil {
			t.Fatal(err)
		}
		w := conditionalRequest(r, http.MethodGet, "/environments/"+env.ID+"/secret-keys", nil)
		if w.Code != 404 || f.callsList != 0 || f.callsGet != 0 {
			t.Fatal("private environment listed credential keys")
		}
	}
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	defer slog.SetDefault(old)
	for _, keys := range [][]string{{"bad\nkey"}, {strings.Repeat("x", 257)}, make([]string, 10001)} {
		f.keys = keys
		w := conditionalRequest(r, http.MethodGet, "/environments/conditional-env/secret-keys", nil)
		if w.Code != 503 || strings.Contains(w.Body.String(), `"keys"`) {
			t.Fatal("malformed metadata accepted as empty inventory")
		}
	}
	f.keys = nil
	f.listErr = errors.New("owned-provider-secret-canary")
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		path := "/environments/conditional-env/secret-keys"
		body := any(nil)
		if method != http.MethodGet {
			path = "/environments/conditional-env/secrets/K"
			body = map[string]string{"value": "owned-input-secret-canary"}
		}
		w := conditionalRequest(r, method, path, body)
		if w.Code != 503 || strings.Contains(w.Body.String(), "canary") {
			t.Fatal("backend error leaked or treated as success")
		}
	}
	if strings.Contains(log.String(), "canary") {
		t.Fatal("raw secret/provider error logged")
	}
	f.listErr = nil
	f.keys = []string{"K"}
	f.onList = func(ctx context.Context) {
		e, err := h.Store.Environments.Get(ctx, "conditional-env")
		if err != nil {
			t.Fatal(err)
		}
		cp := *e
		cp.Name = "Changed"
		if err := h.Store.Environments.Update(ctx, &cp); err != nil {
			t.Fatal(err)
		}
	}
	w := conditionalRequest(r, http.MethodGet, "/environments/conditional-env/secret-keys", nil)
	if w.Code != 409 || strings.Contains(w.Body.String(), `"keys"`) {
		t.Fatal("metadata generation drift accepted")
	}
	if f.callsGet != 0 {
		t.Fatal("metadata revealed values")
	}
	h.Secrets = nil
	w = conditionalRequest(r, http.MethodGet, "/environments/conditional-env/secret-keys", nil)
	if w.Code != 503 {
		t.Fatal("missing manager represented available")
	}
}

func TestSecretKeysMetadataCanceledRequestIsUnavailable(t *testing.T) {
	h, r := conditionalHandlerFixture(t)
	f := &metadataOnlySecretFixture{keys: []string{"K"}, onList: func(ctx context.Context) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("metadata provider read lacks deadline")
		}
	}}
	h.Secrets = f
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := newRequest(http.MethodGet, "/environments/conditional-env/secret-keys", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 503 || strings.Contains(w.Body.String(), `"keys"`) {
		t.Fatal("cancelled metadata treated as available")
	}
}

type environmentUnavailableFixture struct{ store.EnvironmentStore }

func (f environmentUnavailableFixture) Get(context.Context, string) (*model.Environment, error) {
	return nil, errors.New("owned-store-private-canary")
}
func TestConditionalSecretHTTPEnvironmentLookupSafeFailure(t *testing.T) {
	h, r := conditionalHandlerFixture(t)
	h.Store.Environments = environmentUnavailableFixture{h.Store.Environments}
	f := &metadataOnlySecretFixture{}
	h.Secrets = f
	for _, tc := range []struct {
		method, path string
		body         any
	}{{http.MethodGet, "/environments/conditional-env/secret-keys", nil}, {http.MethodPut, "/environments/conditional-env/secrets/K", map[string]any{"value": "private", "version": 7}}, {http.MethodDelete, "/environments/conditional-env/secrets/K?version=" + strconv.Itoa(7), nil}} {
		w := conditionalRequest(r, tc.method, tc.path, tc.body)
		if w.Code != 503 || strings.Contains(w.Body.String(), "canary") {
			t.Fatal("lookup error was leaked or misclassified")
		}
	}
	if f.callsPut+f.callsDelete+f.callsList != 0 {
		t.Fatal("unknown environment accessed provider")
	}
}

func TestConditionalEnvironmentCreateIgnoresClientGenerationAndSecretNames(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.POST("/environments", h.CreateEnvironment)
	w := conditionalRequest(r, http.MethodPost, "/environments", map[string]any{
		"name": "New environment", "version": 99, "secretKeys": []string{"invented"},
		"plainVars": map[string]string{"DATABASE_NAME": "app"},
	})
	var env model.Environment
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &env) != nil || env.Version != 1 || len(env.SecretKeys) != 0 {
		t.Fatal("creation returned untrusted generation or secret metadata")
	}
	stored, err := h.Store.Environments.Get(context.Background(), env.ID)
	if err != nil || stored.Version != 1 || len(stored.Secrets) != 0 || len(stored.SecretKeys) != 0 {
		t.Fatal("client generation or secret metadata entered storage")
	}
}
