package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/store/memory"
)

func setupLocalAuth(t *testing.T, allowSignup bool) (*gin.Engine, *LocalAuthHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st := memory.New()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	issuer, err := local.NewIssuer(key, time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	h := NewLocalAuthHandler(st.Users, issuer, allowSignup)
	r := gin.New()
	r.POST("/signup", h.Signup)
	r.POST("/signin", h.Signin)
	r.GET("/me", func(c *gin.Context) {
		// Stub the auth middleware: parse bearer, set user.
		tok := c.GetHeader("Authorization")
		if len(tok) > 7 && tok[:7] == "Bearer " {
			claims, err := issuer.Verify(tok[7:])
			if err == nil {
				c.Set("user", &auth.Claims{
					Subject: claims.Subject,
					Email:   claims.Email,
					Name:    claims.Name,
					Roles:   []string{claims.Role},
				})
			}
		}
		h.Me(c)
	})
	return r, h
}

func postJSON(t *testing.T, r http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestSignupFirstUserBecomesAdmin(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	rec := postJSON(t, r, "/signup", map[string]string{
		"email":    "first@example.com",
		"password": "passphrase-12",
		"name":     "First User",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("expected non-empty token")
	}
	if resp.User.Role != "admin" {
		t.Fatalf("first user should be admin; got %q", resp.User.Role)
	}
}

func TestSignupSecondUserBecomesViewer(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	postJSON(t, r, "/signup", map[string]string{"email": "first@x.com", "password": "passphrase-12"})
	rec := postJSON(t, r, "/signup", map[string]string{"email": "second@x.com", "password": "passphrase-12"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp tokenResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.User.Role != "viewer" {
		t.Fatalf("second user should default to viewer; got %q", resp.User.Role)
	}
}

func TestSignupRejectsDuplicateEmail(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	postJSON(t, r, "/signup", map[string]string{"email": "dup@x.com", "password": "passphrase-12"})
	rec := postJSON(t, r, "/signup", map[string]string{"email": "DUP@x.com", "password": "passphrase-12"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate email; got %d", rec.Code)
	}
}

func TestSignupRejectsShortPassword(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	rec := postJSON(t, r, "/signup", map[string]string{"email": "x@x.com", "password": "short"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on short password; got %d", rec.Code)
	}
}

func TestSignupRejectsBadEmail(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	rec := postJSON(t, r, "/signup", map[string]string{"email": "not-an-email", "password": "passphrase-12"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad email; got %d", rec.Code)
	}
}

func TestSignupBlockedWhenDisabled(t *testing.T) {
	r, _ := setupLocalAuth(t, false)
	rec := postJSON(t, r, "/signup", map[string]string{"email": "x@x.com", "password": "passphrase-12"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when signup disabled; got %d", rec.Code)
	}
}

func TestSigninSucceeds(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	postJSON(t, r, "/signup", map[string]string{"email": "u@x.com", "password": "passphrase-12"})
	rec := postJSON(t, r, "/signin", map[string]string{"email": "U@x.com", "password": "passphrase-12"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSigninRejectsWrongPassword(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	postJSON(t, r, "/signup", map[string]string{"email": "u@x.com", "password": "passphrase-12"})
	rec := postJSON(t, r, "/signin", map[string]string{"email": "u@x.com", "password": "wrong-password"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong password; got %d", rec.Code)
	}
}

func TestSigninRejectsUnknownUser(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	rec := postJSON(t, r, "/signin", map[string]string{"email": "nobody@x.com", "password": "passphrase-12"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unknown user; got %d", rec.Code)
	}
}

func TestMeReturnsAuthenticatedProfile(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	rec := postJSON(t, r, "/signup", map[string]string{"email": "u@x.com", "password": "passphrase-12", "name": "User"})
	var resp tokenResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("/me status = %d, body = %s", rec2.Code, rec2.Body.String())
	}
	var me meUser
	_ = json.Unmarshal(rec2.Body.Bytes(), &me)
	if me.Email != "u@x.com" {
		t.Fatalf("expected email u@x.com; got %q", me.Email)
	}
	if me.Role != "admin" {
		t.Fatalf("expected role admin (first user); got %q", me.Role)
	}
}

func TestMeRequiresAuth(t *testing.T) {
	r, _ := setupLocalAuth(t, true)
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without bearer; got %d", rec.Code)
	}
}
