package server

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// wsTicketGate returns a Gin middleware that requires a valid
// single-use ticket on the WebSocket upgrade request. The ticket
// is read from the ?ticket= query string. On success the ticket's
// subject is stored in the Gin context under "ws-subject".
func (s *Server) wsTicketGate() gin.HandlerFunc {
	return func(c *gin.Context) {
		tok := c.Query("ticket")
		if tok == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing ticket; obtain one via POST /api/v1/ws-tickets",
			})
			return
		}
		subject, ok := s.wsTickets.Consume(tok)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired ticket",
			})
			return
		}
		c.Set("ws-subject", subject)
		c.Next()
	}
}

// wsTicket is a short-lived, single-use token authorising one
// WebSocket connection. Browsers cannot attach an Authorization
// header to a WS upgrade, so the client first POSTs to
// /api/v1/ws-tickets with its bearer token to obtain a ticket and
// then opens the WS with ?ticket=<value>.
type wsTicket struct {
	subject  string
	expires  time.Time
}

// wsTicketStore is an in-memory, TTL-bound, single-use ticket store.
// Multi-replica deployments need shared state (Redis) — this store
// is intentionally simple and matches the per-process rate limiter's
// scope. The threat model: an attacker who already has read access
// to the bearer token (XSS, intercepted Authorization header) does
// not benefit from this layer, so single-use + 60s TTL is sufficient
// against the realistic risk (a leaked WS URL in browser history /
// referer headers being replayed).
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

// Issue returns a fresh ticket for the given subject. The ticket
// is 32 bytes of crypto/rand, base64url-encoded.
func (s *wsTicketStore) Issue(subject string) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	tok := base64.RawURLEncoding.EncodeToString(buf)
	expires := time.Now().Add(s.ttl)
	s.mu.Lock()
	s.tickets[tok] = wsTicket{subject: subject, expires: expires}
	s.mu.Unlock()
	return tok, expires, nil
}

// Consume looks up a ticket, validates expiry, and deletes it. The
// returned subject is the OIDC subject the ticket was issued to.
// ok=false means: not found, expired, or already consumed.
func (s *wsTicketStore) Consume(tok string) (subject string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, found := s.tickets[tok]
	if !found {
		return "", false
	}
	delete(s.tickets, tok)
	if time.Now().After(t.expires) {
		return "", false
	}
	return t.subject, true
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
