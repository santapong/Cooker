package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// freshHostKey returns a brand-new SSH public key for use in tests.
func freshHostKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	return sshPub
}

func TestHostKeyCallback_TOFUPinsOnFirstConnect(t *testing.T) {
	key := freshHostKey(t)
	var pinned string
	cb := hostKeyCallback("", false /* not strict, TOFU on */, func(s string) { pinned = s })
	if err := cb("hostname", &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 22}, key); err != nil {
		t.Fatalf("first connect should accept; got %v", err)
	}
	if pinned == "" {
		t.Fatalf("onPin was not invoked")
	}
	if pinned != serialisePublicKey(key) {
		t.Fatalf("pinned %q != serialised %q", pinned, serialisePublicKey(key))
	}
}

func TestHostKeyCallback_StrictWithoutPinRefuses(t *testing.T) {
	key := freshHostKey(t)
	cb := hostKeyCallback("", true /* strict */, nil)
	err := cb("hostname", &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 22}, key)
	if err == nil || !errors.Is(err, ErrUnknownHostKey) {
		t.Fatalf("want ErrUnknownHostKey, got %v", err)
	}
}

func TestHostKeyCallback_PinMatchAccepts(t *testing.T) {
	key := freshHostKey(t)
	pin := serialisePublicKey(key)
	cb := hostKeyCallback(pin, true, nil)
	if err := cb("h", &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 22}, key); err != nil {
		t.Fatalf("matching pin should accept; got %v", err)
	}
}

func TestHostKeyCallback_PinMismatchRefuses(t *testing.T) {
	good := freshHostKey(t)
	evil := freshHostKey(t)
	cb := hostKeyCallback(serialisePublicKey(good), true, nil)
	err := cb("h", &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 22}, evil)
	if err == nil || !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("want ErrHostKeyMismatch, got %v", err)
	}
}

func TestSerialisePublicKey_TrimsTrailingNewline(t *testing.T) {
	key := freshHostKey(t)
	got := serialisePublicKey(key)
	if got == "" {
		t.Fatalf("empty")
	}
	if got[len(got)-1] == '\n' {
		t.Fatalf("trailing newline not stripped: %q", got)
	}
}
