package server

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
)

// ticketGrant is what a consumed ticket authorises: the OIDC subject it
// was minted for, the roles that subject held at mint time, and the
// exact WS path the ticket is scoped to. wsTicketGate enforces all three
// — the subject for audit/identity, the roles for the per-route role
// guard, and the path so a ticket minted for one resource can never be
// replayed against another (closes CR-6 / SEC-H1+H2).
type ticketGrant struct {
	Subject string   `json:"sub"`
	Roles   []string `json:"roles"`
	// Path is the request path the ticket authorises (no query string),
	// e.g. "/ws/pipeline-run/abc123". The connecting URL's path must
	// match exactly. Because the resource id (runId/appId/buildId/...)
	// is part of the path, binding the path binds the resource scope.
	Path string `json:"path"`
}

// ticketStore abstracts ticket issuance + consumption so the server
// can swap between the in-memory store and the Redis-backed store
// based on COOKER_WS_TICKET_BACKEND. A ticket binds {subject, roles,
// path} at mint time; Consume returns that grant so the gate can
// enforce role + resource scope at connect time.
type ticketStore interface {
	Issue(grant ticketGrant) (string, time.Time, error)
	Consume(tok string) (ticketGrant, bool)
}

// wsTicketGate returns a Gin middleware that requires a valid
// single-use ticket on the WebSocket upgrade request. The ticket is
// read from the ?ticket= query string. On success it enforces BOTH:
//
//  1. the role required for the route group (the gate is configured
//     with the minimum roles, mirroring the HTTP route guards), and
//  2. the resource scope — the ticket's bound path must equal the
//     request path, so a ticket minted for /ws/pipeline-run/A cannot
//     be replayed against /ws/pipeline-run/B or /ws/kubernetes/watch.
//
// The grant's subject + roles are stored in the Gin context (under
// "ws-subject" and "ws-roles") so handlers and the audit log can read
// the authenticated identity, finally consuming what mint bound.
func (s *Server) wsTicketGate() gin.HandlerFunc {
	return func(c *gin.Context) {
		tok := c.Query("ticket")
		if tok == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing ticket; obtain one via POST /api/v1/ws-tickets",
			})
			return
		}
		grant, ok := s.wsTickets.Consume(tok)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired ticket",
			})
			return
		}
		// Resource scope: the ticket is single-purpose. It only works
		// for the exact path it was minted for. The resource id lives in
		// the path, so this is the WS-layer equivalent of the HTTP
		// loadRunForPipeline/path-param ownership check.
		if grant.Path != c.Request.URL.Path {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "ticket not valid for this resource",
			})
			return
		}
		// Role scope: enforce the minimum role the route requires. The
		// roles were captured from the caller's claims at mint time, so
		// a viewer who somehow obtained an operator-scoped ticket (they
		// cannot — mint applies the same check) is still rejected here.
		if !routeRoleAllowed(c, grant.Roles) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "insufficient permissions",
			})
			return
		}
		c.Set("ws-subject", grant.Subject)
		c.Set("ws-roles", grant.Roles)
		c.Next()
	}
}

// wsRouteRoles maps a WS path to the minimum roles required to connect,
// mirroring the HTTP route guards in router.go. A path absent from the
// map requires only authentication (any role), matching the read-level
// HTTP routes (e.g. GET /pipelines/:id/runs/:runId/logs/:stageId).
//
// Routes that over HTTP require writeRole (operator+) are listed here so
// a viewer cannot reach them over WS:
//   - /ws/kubernetes/watch       ↔ GET /kubernetes/* (writeRole)
//   - /ws/runtime/:appId/.../logs ↔ runtime/cluster state (writeRole)
var wsWriteRoles = []auth.Role{auth.RoleOperator, auth.RoleAdmin}

// routeRoleRequirement returns the minimum roles for a WS request path,
// or nil when the route is read-level (any authenticated user). The
// match is by path prefix/segment because the runtime route is
// parameterised (/ws/runtime/:appId/:serviceId/logs).
func routeRoleRequirement(path string) []auth.Role {
	switch {
	case path == "/ws/kubernetes/watch":
		return wsWriteRoles
	case strings.HasPrefix(path, "/ws/runtime/"):
		return wsWriteRoles
	default:
		return nil
	}
}

// routeRoleAllowed reports whether the supplied roles satisfy the role
// requirement of the request's path. Read-level routes (nil requirement)
// always pass once authenticated.
func routeRoleAllowed(c *gin.Context, roles []string) bool {
	required := routeRoleRequirement(c.Request.URL.Path)
	if len(required) == 0 {
		return true
	}
	for _, req := range required {
		for _, have := range roles {
			if have == string(req) {
				return true
			}
		}
	}
	return false
}

// hasAnyRole reports whether claims hold at least one of the roles.
func hasAnyRole(claims *auth.Claims, roles []auth.Role) bool {
	if claims == nil {
		return false
	}
	for _, req := range roles {
		for _, have := range claims.Roles {
			if have == string(req) {
				return true
			}
		}
	}
	return false
}

// wsTicketRequest is the POST /api/v1/ws-tickets body. The client must
// declare the exact WS path it will connect to so the ticket can be
// scoped to that resource and the route's role requirement enforced at
// mint time. The frontend derives this from the WS URL it is about to
// open (the path portion, without query string).
type wsTicketRequest struct {
	Path string `json:"path"`
}

// validWSPaths is the set of literal/parameterised WS routes a ticket
// may be minted for. The mint endpoint validates the requested path
// against this set so a ticket can only ever be scoped to a real WS
// route (not, say, an arbitrary attacker-chosen string that the gate
// would later compare equal to a crafted request path).
//
// Each entry is matched with matchWSPath, which understands the `:param`
// segments. Keep this in lockstep with the ws group in registerRoutes.
var validWSPaths = []string{
	"/ws/pipeline-run/:runId",
	"/ws/app-run/:runId",
	"/ws/docker/build/:buildId",
	"/ws/runs/:runId/stages/:stageId/logs",
	"/ws/runtime/:appId/:serviceId/logs",
	"/ws/kubernetes/watch",
}

// matchWSPath reports whether a concrete request path matches one of the
// registered WS route patterns. A pattern segment beginning with ":" is
// a wildcard that matches any single non-empty segment.
func matchWSPath(path string) bool {
	reqSegs := strings.Split(strings.Trim(path, "/"), "/")
	for _, pattern := range validWSPaths {
		patSegs := strings.Split(strings.Trim(pattern, "/"), "/")
		if len(patSegs) != len(reqSegs) {
			continue
		}
		ok := true
		for i, ps := range patSegs {
			if strings.HasPrefix(ps, ":") {
				if reqSegs[i] == "" {
					ok = false
					break
				}
				continue
			}
			if ps != reqSegs[i] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// issueWSTicket mints a single-use, 60s, resource-scoped WS ticket. It
// binds the caller's identity (subject) and roles into the ticket and
// enforces the route's role requirement up-front, so a viewer cannot
// even obtain a ticket for an operator-gated WS route. The ticket is
// scoped to the exact path supplied in the request body; the gate later
// rejects any connection whose path differs (closing CR-6).
func (s *Server) issueWSTicket(c *gin.Context) {
	claims := auth.GetUser(c)
	if claims == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	var req wsTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error": "ws-tickets requires a {\"path\":\"/ws/...\"} body naming the target resource",
		})
		return
	}
	// Normalise: strip any query string the client may have included.
	path := req.Path
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if !matchWSPath(path) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error": "unknown ws path",
		})
		return
	}
	// Enforce the route's role requirement at mint time. This mirrors
	// the HTTP route guards (operator+ for kubernetes/runtime) so the
	// 403 happens before a ticket ever exists for a privileged route.
	if required := routeRoleRequirement(path); len(required) > 0 && !hasAnyRole(claims, required) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "insufficient permissions for this ws resource",
		})
		return
	}
	grant := ticketGrant{
		Subject: claims.Subject,
		Roles:   claims.Roles,
		Path:    path,
	}
	tok, exp, err := s.wsTickets.Issue(grant)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "ticket issuance failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ticket":     tok,
		"expires_at": exp.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// wsTicket is a short-lived, single-use token authorising one
// WebSocket connection. Browsers cannot attach an Authorization
// header to a WS upgrade, so the client first POSTs to
// /api/v1/ws-tickets with its bearer token to obtain a ticket and
// then opens the WS with ?ticket=<value>. The ticket carries the
// minted grant (subject, roles, path) so the gate can enforce role +
// resource scope without a second round-trip.
type wsTicket struct {
	grant   ticketGrant
	expires time.Time
}

// wsTicketStore is an in-memory, TTL-bound, single-use ticket store.
// Multi-replica deployments need shared state (Redis) — this store
// is intentionally simple and matches the per-process rate limiter's
// scope. The threat model: an attacker who already has read access
// to the bearer token (XSS, intercepted Authorization header) does
// not benefit from this layer, so single-use + 60s TTL is sufficient
// against the realistic risk (a leaked WS URL in browser history /
// referer headers being replayed). The grant binds the ticket to one
// resource + role so a leaked or mis-scoped ticket cannot be widened.
type wsTicketStore struct {
	ttl     time.Duration
	mu      sync.Mutex
	tickets map[string]wsTicket
}

func newWSTicketStore(ttl time.Duration) *wsTicketStore {
	s := &wsTicketStore{
		ttl:     ttl,
		tickets: make(map[string]wsTicket),
	}
	// Don't start a GC ticker for non-positive TTLs (test fixtures
	// pass TTL=0 to validate the expiry path; real callers always
	// pass a positive duration).
	if ttl > 0 {
		go s.gc(ttl)
	}
	return s
}

// Issue returns a fresh ticket bound to the given grant. The ticket
// is 32 bytes of crypto/rand, base64url-encoded.
func (s *wsTicketStore) Issue(grant ticketGrant) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	tok := base64.RawURLEncoding.EncodeToString(buf)
	expires := time.Now().Add(s.ttl)
	s.mu.Lock()
	s.tickets[tok] = wsTicket{grant: grant, expires: expires}
	s.mu.Unlock()
	return tok, expires, nil
}

// Consume looks up a ticket, validates expiry, and deletes it. The
// returned grant carries the subject/roles/path the ticket was issued
// with. ok=false means: not found, expired, or already consumed.
func (s *wsTicketStore) Consume(tok string) (ticketGrant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, found := s.tickets[tok]
	if !found {
		return ticketGrant{}, false
	}
	delete(s.tickets, tok)
	if time.Now().After(t.expires) {
		return ticketGrant{}, false
	}
	return t.grant, true
}

func (s *wsTicketStore) gc(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for now := range ticker.C {
		s.mu.Lock()
		for k, t := range s.tickets {
			if now.After(t.expires) {
				delete(s.tickets, k)
			}
		}
		s.mu.Unlock()
	}
}
