package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/provider/neon"
)

func neonBoundaryRouter(claims *auth.Claims) *gin.Engine {
	return neonConfiguredRouter(&Handler{Neon: &neon.ConnectionService{Store: neon.NewMemoryConnections()}}, claims)
}
func neonConfiguredRouter(h *Handler, claims *auth.Claims) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if claims != nil {
			c.Set("user", claims)
		}
		c.Next()
	})
	r.GET("/neon", h.ListNeonConnections)
	r.GET("/neon/:id", h.GetNeonConnection)
	r.POST("/neon", h.CreateNeonConnection)
	r.PUT("/neon/:id", h.UpdateNeonConnection)
	r.DELETE("/neon/:id", h.DeleteNeonConnection)
	r.POST("/neon/:id/sync", h.SyncNeonConnection)
	r.GET("/neon/:id/endpoints", h.GetNeonEndpoints)
	return r
}

type neonHTTPMetadataFixture struct{}

func (neonHTTPMetadataFixture) Projects(_ context.Context, _ string, org string) (*neon.ProjectInventory, error) {
	now := time.Now().UTC()
	return &neon.ProjectInventory{OrganizationID: org, Complete: true, ObservedAt: now, Projects: []neon.Project{{ID: "project-one", Name: "Fixture", OrganizationID: org, RegionID: "aws-us-east-2", CreatedAt: now, UpdatedAt: now}}}, nil
}
func (neonHTTPMetadataFixture) Branches(_ context.Context, _ string, project string) (*neon.BranchInventory, error) {
	now := time.Now().UTC()
	return &neon.BranchInventory{ProjectID: project, Complete: true, ObservedAt: now, Branches: []neon.Branch{{ID: "br-one", ProjectID: project, Name: "main", CreatedAt: now, UpdatedAt: now}}}, nil
}
func TestNeonHTTPAdminEncryptedConnectionJourney(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	s := &neon.ConnectionService{Store: neon.NewMemoryConnections(), Codec: codec, API: neonHTTPMetadataFixture{}}
	r := neonConfiguredRouter(&Handler{Neon: s}, &auth.Claims{Subject: "admin", Roles: []string{"admin"}})
	call := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("HTTP journey status %d,want %d", w.Code, want)
		}
		if strings.Contains(w.Body.String(), "synthetic-neon-http-credential") {
			t.Fatal("credential echoed")
		}
		return w
	}
	w := call(http.MethodPost, "/neon", `{"name":"HTTP fixture","organizationId":"org-fixture","accountMode":"unknown","apiKey":"synthetic-neon-http-credential","projectIds":["project-one"],"acknowledgeKeyPermissions":true}`, 201)
	var created struct{ Connection neon.Connection }
	if json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Connection.ID == "" || created.Connection.Version != 1 {
		t.Fatal("creation receipt missing")
	}
	base := "/neon/" + created.Connection.ID
	stored, err := s.Store.Get(context.Background(), created.Connection.ID)
	if err != nil || strings.Contains(string(stored.Credential), "synthetic-neon-http-credential") {
		t.Fatal("plaintext storage or missing record")
	}
	w = call(http.MethodPost, base+"/sync", ``, 200)
	if !strings.Contains(w.Body.String(), `"state":"observed"`) || !strings.Contains(w.Body.String(), `"br-one"`) {
		t.Fatal("successful metadata read missing")
	}
	w = call(http.MethodPut, base, `{"version":1,"enabled":false}`, 200)
	if !strings.Contains(w.Body.String(), `"state":"paused"`) {
		t.Fatal("pause state missing")
	}
	call(http.MethodPost, base+"/sync", ``, 422)
	call(http.MethodDelete, base+"?version=1", ``, 409)
	w = call(http.MethodDelete, base+"?version=2", ``, 200)
	if !strings.Contains(w.Body.String(), `"providerResourcesChanged":false`) {
		t.Fatal("disconnect semantics missing")
	}
	call(http.MethodGet, base, ``, 404)
}
func TestNeonHTTPIndependentRolesAndSafeRead(t *testing.T) {
	for _, tc := range []struct {
		role        string
		read, write int
	}{{"", 401, 401}, {"viewer", 200, 403}, {"operator", 200, 403}} {
		var claims *auth.Claims
		if tc.role != "" {
			claims = &auth.Claims{Subject: tc.role, Roles: []string{tc.role}}
		}
		r := neonBoundaryRouter(claims)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/neon", nil))
		if w.Code != tc.read {
			t.Fatal("wrong read boundary", tc.role, w.Code)
		}
		if tc.read == 200 && (!strings.Contains(w.Body.String(), `"connections":[]`) || !strings.Contains(w.Body.String(), `"databaseHealth":"not_checked"`)) {
			t.Fatal("read result fabricated health or omitted inventory")
		}
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			path := "/neon"
			if method != http.MethodPost {
				path += "/fixture?version=1"
			}
			w = httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
			if w.Code != tc.write {
				t.Fatal("wrong configuration boundary", tc.role, method, w.Code)
			}
		}
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/neon/fixture/endpoints?version=1", nil))
		if w.Code != tc.write {
			t.Fatal("provider read bypassed admin boundary", tc.role, w.Code)
		}
	}
}
func TestNeonHTTPRejectsAmbiguousCredentialBodies(t *testing.T) {
	r := neonBoundaryRouter(&auth.Claims{Subject: "admin", Roles: []string{"admin"}})
	for _, body := range []string{`null`, `[]`, `{} {}`, `{"apiKey":null}`, `{"APIKey":"one","apiKey":"two"}`, `{"version":1,"version":2}`, `{"version":1,"verſion":2}`, `{"projectIdſ":[]}`, `{"credential":"forged"}`, `{"snapshot":{}}`, strings.Repeat("x", 17<<10)} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			path := "/neon"
			if method == http.MethodPut {
				path += "/fixture"
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
			if w.Code != http.StatusBadRequest {
				t.Fatal("ambiguous secret body accepted", method, w.Code)
			}
			if strings.Contains(w.Body.String(), "forged") || strings.Contains(w.Body.String(), "two") {
				t.Fatal("request material echoed")
			}
		}
	}
}
