package server

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestRuntimeLogSnapshotRoutesRequireRuntimeLogRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	issuer, err := local.NewIssuer(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mw, err := auth.NewMiddleware(context.Background(), config.OIDCConfig{})
	if err != nil {
		t.Fatal(err)
	}
	mw.EnableLocalAuth(issuer)
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	h := handler.New(memory.New(), codec, nil)
	s := &Server{router: r, handler: h, config: &config.Config{}, oidcMW: mw}
	s.registerRoutes()
	for _, role := range []string{"", "viewer", "operator", "admin"} {
		t.Run(role, func(t *testing.T) {
			token := ""
			if role != "" {
				token, _, err = issuer.Issue(role, role+"@fixture.invalid", role, role)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, request := range []struct{ method, path string }{{"GET", "log-replicas"}, {"POST", "log-sessions"}, {"GET", "log-sessions/missing"}, {"GET", "log-sessions/missing/download"}, {"DELETE", "log-sessions/missing"}} {
				req := httptest.NewRequest(request.method, "/api/v1/apps/owned/services/web/"+request.path, strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				want := 503
				if role == "" {
					want = 401
				} else if role == "viewer" {
					want = 403
				}
				if w.Code != want {
					t.Fatalf("%s %s role=%s status=%d want=%d", request.method, request.path, role, w.Code, want)
				}
			}
		})
	}
}
