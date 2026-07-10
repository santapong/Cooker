package ssh

import (
	"errors"
	"fmt"
	"net"

	gossh "golang.org/x/crypto/ssh"
)

// ErrHostKeyMismatch is returned when the server's offered host key
// does not match the one previously pinned for the Host. Cooker
// refuses to connect rather than silently auto-replace.
var ErrHostKeyMismatch = errors.New("ssh: host key mismatch")

// ErrUnknownHostKey is returned when no host key is pinned and TOFU
// is disabled (Host.SSHStrictHostKey=true with empty
// SSHKnownHostKey). The operator must populate the known key out of
// band before the first connect.
var ErrUnknownHostKey = errors.New("ssh: no pinned host key and TOFU disabled")

// hostKeyCallback returns an ssh.HostKeyCallback that implements
// trust-on-first-use (TOFU) host-key verification. The callback
// honours the per-Host policy:
//
//   - if pinned != "", the server's offered key must serialise to the
//     same string; mismatch → ErrHostKeyMismatch.
//   - if pinned == "" and strict, refuse → ErrUnknownHostKey.
//   - if pinned == "" and !strict, accept and invoke onPin(key) so
//     the caller can persist the key for future connects.
//
// strict and pinned come from the Host; onPin is the persistence
// hook (Target.persistKnownHostKey). The "accept any key" callback
// in golang.org/x/crypto/ssh is forbidden in this package — this
// callback is the only acceptable implementation of HostKeyCallback
// for Cooker's SSH deploy target.
//
// onPin may be nil (e.g. in unit tests where pinning is observed via
// the captured value); a nil onPin still accepts the first-connect
// key, the caller just doesn't get a callback.
func hostKeyCallback(pinned string, strict bool, onPin func(serialised string)) gossh.HostKeyCallback {
	return func(_ string, _ net.Addr, key gossh.PublicKey) error {
		serialised := serialisePublicKey(key)
		if pinned != "" {
			if serialised != pinned {
				return fmt.Errorf("%w: server offered %s; pinned %s",
					ErrHostKeyMismatch, fingerprint(key), pinned)
			}
			return nil
		}
		// pinned == ""
		if strict {
			return fmt.Errorf("%w: server offered %s",
				ErrUnknownHostKey, fingerprint(key))
		}
		// TOFU: accept and record.
		if onPin != nil {
			onPin(serialised)
		}
		return nil
	}
}

// serialisePublicKey returns the "ssh-<algo> <base64>" form Cooker
// pins in Host.SSHKnownHostKey. Stable across reboots; cheap to
// compare. Trailing newline trimmed.
func serialisePublicKey(key gossh.PublicKey) string {
	b := gossh.MarshalAuthorizedKey(key)
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}
	return string(b)
}

// fingerprint returns the SHA256 fingerprint of key in the form
// OpenSSH prints, useful for error messages.
func fingerprint(key gossh.PublicKey) string {
	return gossh.FingerprintSHA256(key)
}
