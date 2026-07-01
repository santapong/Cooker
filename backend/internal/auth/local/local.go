// Package local implements email + password authentication that runs
// alongside the OIDC path. When COOKER_LOCAL_AUTH_ENABLED=true, signup
// and signin endpoints register users in the UserStore, hash passwords
// with bcrypt, and issue HS256 JWTs that the auth middleware accepts
// in place of an OIDC bearer token.
//
// Local accounts are not a replacement for the OIDC IdP — they are a
// small-team escape hatch and the path Cooker recommends only when
// integrating with Keycloak / Google / Okta is overkill (homelab,
// single-user demo, etc.). SECURITY.md documents the trade-offs.
package local

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/santapong/cooker/internal/store"
)

// IssuerName is encoded as the JWT `iss` claim. Used by the
// middleware to disambiguate Cooker-issued tokens from OIDC
// IdP-issued ones (those carry the IdP's issuer instead).
const IssuerName = "cooker-local"

// MinPasswordLength is the bare-minimum we accept on signup. Kept
// modest so the UI doesn't have to lecture users about character
// classes — anyone serious will use OIDC.
const MinPasswordLength = 8

// Default token lifetime when COOKER_LOCAL_AUTH_TOKEN_TTL isn't set.
// Long enough to avoid frequent re-auth; short enough that a stolen
// token expires inside a working day.
const DefaultTokenTTL = 12 * time.Hour

// MinSigningKeyLength is the floor on COOKER_LOCAL_AUTH_JWT_SIGNING_KEY.
// HS256 over a short secret is brute-forceable; 32 raw bytes (or 43+
// base64 chars) is the lower bound we'll boot with.
const MinSigningKeyLength = 32

// Errors returned by this package. Callers map these to HTTP status
// codes in the handler layer.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrPasswordTooShort   = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	ErrEmailRequired      = errors.New("email is required")
	ErrEmailInvalid       = errors.New("email looks malformed")
	ErrTokenInvalid       = errors.New("token invalid or expired")
	ErrSigningKeyTooShort = fmt.Errorf("local-auth JWT signing key must be at least %d bytes", MinSigningKeyLength)
)

// Role names used by the bootstrap policy. These mirror
// auth.RoleAdmin / auth.RoleViewer by value; they are duplicated as
// literals here because internal/auth imports this package (oidc.go),
// so importing auth back would create a cycle.
const (
	roleAdmin  = "admin"
	roleViewer = "viewer"
)

// BootstrapRole implements the "first user becomes admin" policy: it
// grants admin to the very first account (users.Count == 0) and viewer
// to everyone after. A failed Count call is not fatal — it logs and
// falls back to viewer (the safe default), returning a nil error so the
// caller can still create the account. This is the domain rule the
// Signup handler consults; keeping it here (rather than in the handler)
// makes the policy testable at the service tier.
func BootstrapRole(ctx context.Context, users store.UserStore) (string, error) {
	n, err := users.Count(ctx)
	if err != nil {
		slog.Warn("auth/local: count users failed; defaulting new user to viewer", "error", err)
		return roleViewer, nil
	}
	if n == 0 {
		return roleAdmin, nil
	}
	return roleViewer, nil
}

// HashPassword bcrypt-hashes a plaintext password. Cost is bcrypt's
// DefaultCost (10) — matches what every other Go project ships and
// keeps signup latency under ~80ms on a modern CPU.
func HashPassword(plaintext string) (string, error) {
	if len(plaintext) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt: %w", err)
	}
	return string(h), nil
}

// VerifyPassword returns nil when plaintext matches hash. Wrong
// passwords return ErrInvalidCredentials so the handler can respond
// with a single 401 message regardless of whether the email or
// password was wrong (avoids account-existence enumeration).
func VerifyPassword(hash, plaintext string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
	if err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// NormaliseEmail trims and lowercases an email. Returns
// ErrEmailRequired or ErrEmailInvalid for obviously bad input. We
// don't do RFC-5322 parsing — the IdP path doesn't either, and we'd
// rather accept odd-but-valid addresses than reject them with a regex.
func NormaliseEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" {
		return "", ErrEmailRequired
	}
	if !strings.Contains(e, "@") || strings.HasPrefix(e, "@") || strings.HasSuffix(e, "@") {
		return "", ErrEmailInvalid
	}
	return e, nil
}

// Claims is the JWT payload Cooker issues. Subject is the user's UUID
// from the UserStore. Email and Name are duplicated so the middleware
// can populate auth.Claims without an extra DB round-trip.
type Claims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// Issuer wraps an HS256 signing key + token TTL.
type Issuer struct {
	signingKey []byte
	ttl        time.Duration
}

// NewIssuer constructs an Issuer. Returns ErrSigningKeyTooShort if
// the key is shorter than MinSigningKeyLength bytes — short HS256
// keys are trivially brute-forceable and we refuse to boot with one.
func NewIssuer(signingKey []byte, ttl time.Duration) (*Issuer, error) {
	if len(signingKey) < MinSigningKeyLength {
		return nil, ErrSigningKeyTooShort
	}
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}
	return &Issuer{signingKey: signingKey, ttl: ttl}, nil
}

// Issue produces a signed JWT for the user. exp is set to ttl from
// now. The returned tuple includes the expiry so handlers can pass it
// back to the client for proactive re-auth.
func (i *Issuer) Issue(userID, email, name, role string) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(i.ttl)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    IssuerName,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		Email: email,
		Name:  name,
		Role:  role,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.signingKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign: %w", err)
	}
	return signed, exp, nil
}

// Verify parses and validates a token. Returns ErrTokenInvalid on
// any parse / signature / expiry error; the underlying jwt-go error
// is intentionally swallowed so callers don't leak detail to clients.
func (i *Issuer) Verify(raw string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(raw, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, ErrTokenInvalid
		}
		return i.signingKey, nil
	}, jwt.WithIssuer(IssuerName))
	if err != nil || tok == nil || !tok.Valid {
		return nil, ErrTokenInvalid
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// LooksLikeLocalToken cheap-checks whether a bearer token is one
// Cooker issued. Callers use this to route tokens to the right
// verifier (local issuer vs OIDC IDTokenVerifier) without paying for
// a full parse. False negatives just fall through to the OIDC path.
func LooksLikeLocalToken(raw string) bool {
	// HS256-signed JWT with "iss":"cooker-local" decodes to a header
	// that starts with eyJ... . We do an unverified parse of just the
	// claims to look at iss; this is cheap (single base64 decode +
	// JSON unmarshal of the middle segment).
	parts := strings.SplitN(raw, ".", 3)
	if len(parts) != 3 {
		return false
	}
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	tok, _, err := parser.ParseUnverified(raw, jwt.MapClaims{})
	if err != nil {
		return false
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return false
	}
	iss, _ := claims["iss"].(string)
	return iss == IssuerName
}
