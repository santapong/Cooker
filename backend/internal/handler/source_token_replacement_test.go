package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/source/connection"
)

type replacementFailureStore struct {
	connection.Store
	err error
}

func (s replacementFailureStore) Update(context.Context, *connection.Connection) error { return s.err }

func TestSourceTokenReplacementAPIIsVersionedWriteOnlyAndPreservesMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	cs := &connection.Service{Store: connection.NewMemory(), Codec: codec}
	c := &connection.Connection{Name: "Owned Git", Provider: "github", AuthMode: "token", Repositories: []string{"team/one"}, Enabled: true}
	if err := cs.CreateWithCredentials(context.Background(), c, connection.Credentials{Token: "synthetic-old-token", WebhookSecret: strings.Repeat("h", 32)}); err != nil {
		t.Fatal(err)
	}
	probes := 0
	cs.TokenProbe = func(context.Context, *connection.Connection) error { probes++; return nil }
	h := &Handler{SourceConnections: cs}
	r := gin.New()
	r.POST("/connections/:id/token-replacement", h.ReplaceSourceToken)
	r.GET("/connections", h.ListSourceConnections)
	body := `{"version":1,"token":"synthetic-new-token","provider":"gitea","origin":"https://evil.invalid","repositories":["other/repo"],"webhookSecret":"attacker-hook","enabled":false}`
	req := httptest.NewRequest("POST", "/connections/"+c.ID+"/token-replacement", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || probes != 1 {
		t.Fatal("replacement API failed")
	}
	var result struct {
		Connection          connection.Connection `json:"connection"`
		CheckedRepositories int                   `json:"checkedRepositories"`
		Check               string                `json:"check"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Connection.Version != 2 || result.Connection.Provider != "github" || !result.Connection.Enabled || len(result.Connection.Repositories) != 1 || result.Connection.Repositories[0] != "team/one" || result.Check != "repository_metadata_access" || result.CheckedRepositories != 1 {
		t.Fatal("replacement API edited unrelated fields or omitted check scope")
	}
	stored, _ := cs.Store.Get(context.Background(), c.ID)
	creds, _ := cs.OpenCredentials(stored)
	if creds.Token != "synthetic-new-token" || creds.WebhookSecret != strings.Repeat("h", 32) {
		t.Fatal("wrong persisted credentials")
	}
	for _, private := range []string{"synthetic-new-token", "synthetic-old-token", strings.Repeat("h", 32), "attacker-hook"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("replacement API leaked credentials")
		}
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/connections", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "synthetic-new-token") || strings.Contains(w.Body.String(), "credential") {
		t.Fatal("connection listing reveals token")
	}
}

func TestSourceTokenReplacementAPIFailuresAreFixedAndDoNotPersist(t *testing.T) {
	for _, mode := range []string{"malformed", "oversized", "missing-version", "stale", "missing", "unavailable", "provider-error", "store-error"} {
		t.Run(mode, func(t *testing.T) {
			codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
			store := connection.NewMemory()
			cs := &connection.Service{Store: store, Codec: codec}
			c := &connection.Connection{Name: "Owned Git", Provider: "github", AuthMode: "token", Repositories: []string{"team/one"}, Enabled: true}
			if err := cs.CreateWithCredentials(context.Background(), c, connection.Credentials{Token: "synthetic-old-token", WebhookSecret: strings.Repeat("h", 32)}); err != nil {
				t.Fatal(err)
			}
			probes := 0
			cs.TokenProbe = func(context.Context, *connection.Connection) error {
				probes++
				if mode == "provider-error" {
					return errors.New("synthetic-private-canary")
				}
				return nil
			}
			body, id, want, wantProbes := `{"version":1,"token":"synthetic-new-token"}`, c.ID, 400, 0
			switch mode {
			case "malformed":
				body = `{"version":1,"token":123}`
			case "oversized":
				body = `{"version":1,"token":"` + strings.Repeat("x", 33<<10) + `"}`
			case "missing-version":
				body = `{"token":"synthetic-new-token"}`
			case "stale":
				body = `{"version":99,"token":"synthetic-new-token"}`
				want = 409
			case "missing":
				id = "missing"
				want = 404
			case "unavailable":
				cs.TokenProbe = nil
				want = 503
			case "provider-error":
				want = 422
				wantProbes = 1
			case "store-error":
				cs.Store = replacementFailureStore{Store: store, err: errors.New("synthetic-private-canary")}
				want = 503
				wantProbes = 1
			}
			r := gin.New()
			r.POST("/connections/:id/token-replacement", (&Handler{SourceConnections: cs}).ReplaceSourceToken)
			req := httptest.NewRequest("POST", "/connections/"+id+"/token-replacement", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != want || probes != wantProbes || strings.Contains(w.Body.String(), "synthetic-private-canary") || strings.Contains(w.Body.String(), "synthetic-new-token") {
				t.Fatal("unsafe replacement failure response")
			}
			after, _ := store.Get(context.Background(), c.ID)
			if after.Version != c.Version || string(after.Credential) != string(c.Credential) {
				t.Fatal("failed replacement changed credentials")
			}
		})
	}
}
