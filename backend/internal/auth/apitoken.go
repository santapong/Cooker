package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// TokenSubjectPrefix is prepended to an API token's id to form the
// Subject of a token-authenticated request (e.g. "token:<uuid>"). It
// keeps token identities namespaced apart from OIDC subjects and lets the
// token service refuse self-replication by inspecting the Subject prefix.
const TokenSubjectPrefix = "token:"

// lastUsedThrottle is the minimum interval between last_used_at writes for
// a single token. The first authenticated use after a process start (or
// after this window) writes; uses inside the window are skipped. This caps
// hot-path write amplification: a token hammering the API drives at most
// one UPDATE per minute instead of one per request.
const lastUsedThrottle = time.Minute

// APITokenAuthenticator verifies `ck_`-prefixed bearer credentials against
// the api_tokens store. It is consulted by Middleware only when a
// credential carries the API-token prefix, ahead of the local-JWT and OIDC
// paths, and never alters how those paths behave.
type APITokenAuthenticator struct {
	store store.APITokenStore

	// lastTouched throttles last_used_at writes: tokenID -> UnixNano of
	// the last write. Best-effort; a lost entry only costs one extra
	// UPDATE. Read/written lock-free via sync.Map on the hot path.
	lastTouched sync.Map

	// now is overridable in tests; defaults to time.Now.
	now func() time.Time
	// touch performs the throttled store write. Split out so it can be
	// invoked synchronously in tests (production fires it in a goroutine).
	async bool
}

// NewAPITokenAuthenticator builds an authenticator over the token store.
func NewAPITokenAuthenticator(s store.APITokenStore) *APITokenAuthenticator {
	return &APITokenAuthenticator{store: s, now: time.Now, async: true}
}

// Authenticate verifies a plaintext token and returns the Claims a
// successful token bearer should carry. The returned Claims mirror what
// downstream handlers consume from OIDC/local auth: Subject is
// "token:<id>", Name is the token's label, Roles is the token's single
// stored role. ACR/AMR are intentionally empty so a token can never
// satisfy the step-up MFA gate (RequireMFA) — a leaked token must not be
// able to drive an MFA-gated destructive route. ok is false (with a nil
// Claims) for any unknown, expired, or otherwise invalid token; the
// caller maps that to a generic 401, identical to a bad JWT.
func (a *APITokenAuthenticator) Authenticate(ctx context.Context, plaintext string) (*Claims, bool) {
	if a == nil || a.store == nil {
		return nil, false
	}
	hash := model.HashAPIToken(plaintext)
	tok, err := a.store.GetByHash(ctx, hash)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			// A store/DB error is not an auth decision; log it server-side
			// and still fail closed so a flaky DB can't admit a request.
			slog.Warn("auth: api token lookup failed", "err", err)
		}
		return nil, false
	}
	// Defence in depth: the lookup is already an exact-hash match, but
	// compare in constant time so the decision never depends on byte-wise
	// short-circuiting (and stays correct if the lookup ever loosens).
	if subtle.ConstantTimeCompare([]byte(tok.Hash), []byte(hash)) != 1 {
		return nil, false
	}
	now := a.now()
	if tok.Expired(now) {
		// Expired tokens are rejected exactly like a revoked/absent one;
		// the caller returns a generic 401.
		return nil, false
	}

	a.maybeTouch(ctx, tok.ID, now)

	return &Claims{
		Subject: TokenSubjectPrefix + tok.ID,
		Email:   tok.CreatedByEmail,
		Name:    tok.Name,
		Groups:  []string{},
		Roles:   []string{tok.Role},
		// ACR/AMR deliberately left empty: token auth never satisfies MFA.
	}, true
}

// maybeTouch records last_used_at at most once per lastUsedThrottle per
// token. It updates the in-memory stamp first (so concurrent requests in
// the same window all skip the write) and only then issues the store
// write. The write is fire-and-forget in production so it never adds
// latency to the authenticated request.
func (a *APITokenAuthenticator) maybeTouch(ctx context.Context, id string, now time.Time) {
	prev, ok := a.lastTouched.Load(id)
	if ok {
		if now.Sub(time.Unix(0, prev.(int64))) < lastUsedThrottle {
			return
		}
	}
	a.lastTouched.Store(id, now.UnixNano())

	write := func() {
		// A short bound so a slow DB can't pin a goroutine forever; this
		// is a non-critical stamp.
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := a.store.TouchLastUsed(wctx, id, now); err != nil {
			slog.Warn("auth: touch api token last_used_at failed", "err", err)
		}
	}
	if a.async {
		go write()
		return
	}
	write()
}
