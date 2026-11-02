package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/provider/vercel"
)

func adoptionBoundaryRouter(claims *auth.Claims) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &Handler{VercelAdoptions: &vercel.LandingAdoptionService{Store: vercel.NewMemoryLandingAdoptions(vercel.NewMemory())}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if claims != nil {
			c.Set("user", claims)
		}
		c.Next()
	})
	// Deliberately omit router RBAC here: the handlers must retain their own
	// authorization boundary if a future route is registered incorrectly.
	r.GET("/adoptions", h.ListVercelLandingAdoptions)
	r.POST("/adoptions", h.CreateVercelLandingAdoption)
	r.PUT("/adoptions/:id", h.UpdateVercelLandingAdoption)
	return r
}

func TestLandingAdoptionHTTPAuthenticationAndWriteRoles(t *testing.T) {
	for _, test := range []struct {
		name   string
		claims *auth.Claims
		status int
	}{
		{name: "anonymous", status: http.StatusUnauthorized},
		{name: "viewer", claims: &auth.Claims{Subject: "viewer", Roles: []string{"viewer"}}, status: http.StatusForbidden},
		{name: "operator", claims: &auth.Claims{Subject: "operator", Roles: []string{"operator"}}, status: http.StatusForbidden},
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			t.Run(test.name+method, func(t *testing.T) {
				r := adoptionBoundaryRouter(test.claims)
				path := "/adoptions"
				if method == http.MethodPut {
					path += "/binding"
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
				if w.Code != test.status {
					t.Fatalf("write boundary: got %d, want %d", w.Code, test.status)
				}
			})
		}
	}
}

func TestLandingAdoptionHTTPReadRequiresSessionAndAllowsViewer(t *testing.T) {
	for _, test := range []struct {
		name   string
		claims *auth.Claims
		status int
	}{
		{name: "anonymous", status: http.StatusUnauthorized},
		{name: "viewer", claims: &auth.Claims{Subject: "viewer", Roles: []string{"viewer"}}, status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			adoptionBoundaryRouter(test.claims).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/adoptions", nil))
			if w.Code != test.status {
				t.Fatalf("read boundary: got %d, want %d", w.Code, test.status)
			}
			if test.status == http.StatusOK && !strings.Contains(w.Body.String(), `"adoptions":[]`) {
				t.Fatal("empty inventory shape changed")
			}
		})
	}
}

func TestLandingAdoptionHTTPRejectsMalformedOrUnexpectedAuthority(t *testing.T) {
	r := adoptionBoundaryRouter(&auth.Claims{Subject: "admin", Roles: []string{"admin"}})
	for _, body := range []string{"", `null`, `[]`, `{`, `{} {}`, `{"actor":"forged"}`, `{"permittedActions":["deploy"]}`, `{"connectionId":"` + strings.Repeat("x", 5000) + `"}`} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/adoptions", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid DTO accepted: status %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "forged") {
			t.Fatal("request data leaked into parser error")
		}
	}
}
