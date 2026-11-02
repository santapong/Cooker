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

func updateReviewBoundaryRouter(claims *auth.Claims) *gin.Engine {
	gin.SetMode(gin.TestMode)
	connections := vercel.NewMemory()
	adoptions := vercel.NewMemoryLandingAdoptions(connections)
	h := &Handler{
		VercelAdoptions:     &vercel.LandingAdoptionService{Store: adoptions},
		VercelUpdateReviews: &vercel.LandingUpdateReviewService{Store: vercel.NewMemoryLandingUpdateReviews(connections, adoptions)},
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if claims != nil {
			c.Set("user", claims)
		}
		c.Next()
	})
	// No surrounding RBAC: handlers must preserve their own denial boundary.
	r.GET("/reviews", h.ListVercelLandingUpdateReviews)
	r.POST("/reviews", h.CreateVercelLandingUpdateReview)
	return r
}

func TestLandingUpdateReviewHTTPRolesAndReadOnlyHistory(t *testing.T) {
	for _, tc := range []struct {
		name        string
		claims      *auth.Claims
		read, write int
	}{
		{name: "anonymous", read: http.StatusUnauthorized, write: http.StatusUnauthorized},
		{name: "viewer", claims: &auth.Claims{Subject: "viewer", Roles: []string{"viewer"}}, read: http.StatusOK, write: http.StatusForbidden},
		{name: "operator", claims: &auth.Claims{Subject: "operator", Roles: []string{"operator"}}, read: http.StatusOK, write: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := updateReviewBoundaryRouter(tc.claims)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/reviews", nil))
			if w.Code != tc.read {
				t.Fatalf("read status %d, want %d", w.Code, tc.read)
			}
			if tc.read == http.StatusOK && (!strings.Contains(w.Body.String(), `"reviews":[]`) || !strings.Contains(w.Body.String(), `"historyLimit":100`)) {
				t.Fatal("missing recent history contract")
			}
			w = httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/reviews", strings.NewReader(`{}`)))
			if w.Code != tc.write {
				t.Fatalf("write status %d, want %d", w.Code, tc.write)
			}
		})
	}
}

func TestLandingUpdateReviewHTTPRejectsClientIssuedBaseline(t *testing.T) {
	r := updateReviewBoundaryRouter(&auth.Claims{Subject: "admin", Roles: []string{"admin"}})
	for _, body := range []string{`null`, `[]`, `{} {}`, `{"actor":"forged"}`, `{"artifactSha256":"client"}`, `{"baselineAliases":["https://attacker.example"]}`, `{"submissionAvailable":true}`, `{"adoptionId":"` + strings.Repeat("x", 5000) + `"}`} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/reviews", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("client baseline accepted: status %d", w.Code)
		}
	}
}
