package server

import (
	"testing"
	"time"
)

func TestWSTicketStore_IssueAndConsume(t *testing.T) {
	s := newWSTicketStore(60 * time.Second)
	tok, exp, err := s.Issue("alice")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok == "" {
		t.Fatal("empty ticket")
	}
	if !exp.After(time.Now()) {
		t.Errorf("expiry %v not in future", exp)
	}

	sub, ok := s.Consume(tok)
	if !ok {
		t.Fatal("Consume rejected fresh ticket")
	}
	if sub != "alice" {
		t.Errorf("subject = %q, want alice", sub)
	}
}

func TestWSTicketStore_SingleUse(t *testing.T) {
	s := newWSTicketStore(60 * time.Second)
	tok, _, _ := s.Issue("alice")

	if _, ok := s.Consume(tok); !ok {
		t.Fatal("first consume should succeed")
	}
	if _, ok := s.Consume(tok); ok {
		t.Error("second consume should fail (single-use)")
	}
}

func TestWSTicketStore_Expires(t *testing.T) {
	s := newWSTicketStore(0) // zero TTL — every ticket is already expired
	tok, _, _ := s.Issue("alice")

	// Sleep 1ms to be safe past the expiry boundary.
	time.Sleep(time.Millisecond)
	if _, ok := s.Consume(tok); ok {
		t.Error("expired ticket should be rejected")
	}
}

func TestWSTicketStore_UnknownToken(t *testing.T) {
	s := newWSTicketStore(60 * time.Second)
	if _, ok := s.Consume("not-a-real-ticket"); ok {
		t.Error("unknown token should be rejected")
	}
}

func TestWSTicketStore_TokensAreUnique(t *testing.T) {
	s := newWSTicketStore(60 * time.Second)
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, _, _ := s.Issue("alice")
		if seen[tok] {
			t.Fatalf("duplicate ticket on iteration %d", i)
		}
		seen[tok] = true
	}
}
