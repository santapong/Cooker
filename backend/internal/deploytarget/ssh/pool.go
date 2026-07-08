package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/santapong/cooker/internal/deploytarget"
	"github.com/santapong/cooker/internal/model"
)

// requireHost validates the Host record carries every SSH field this
// adapter needs. Surfaces early; never tries to dial with a half-
// populated record.
func requireHost(h *model.Host) error {
	if h == nil {
		return fmt.Errorf("%w: ssh: host nil", deploytarget.ErrUnavailable)
	}
	if h.Kind != model.HostKindSSHDocker {
		return fmt.Errorf("%w: ssh: host kind %q not %q",
			deploytarget.ErrUnavailable, h.Kind, model.HostKindSSHDocker)
	}
	if h.SSHEndpoint == "" {
		return fmt.Errorf("%w: ssh: host %s missing SSHEndpoint", deploytarget.ErrUnavailable, h.ID)
	}
	if h.SSHUser == "" {
		return fmt.Errorf("%w: ssh: host %s missing SSHUser", deploytarget.ErrUnavailable, h.ID)
	}
	if h.SSHPrivateKeyRef == "" {
		return fmt.Errorf("%w: ssh: host %s missing SSHPrivateKeyRef", deploytarget.ErrUnavailable, h.ID)
	}
	return nil
}

// dialHost opens (or returns a cached) ssh client for h. The
// returned client must not be Close'd by the caller — Target owns
// the lifecycle and closes via CloseAll. lw receives any TOFU-pin
// log lines; nil silently discards them.
//
// Concurrent first-dials for the same host are serialised through a
// per-host Mutex so only one connection is established and cached;
// subsequent callers reuse it.
func (t *Target) dialHost(ctx context.Context, h *model.Host, lw io.Writer) (sshClient, error) {
	if err := requireHost(h); err != nil {
		return nil, err
	}

	t.mu.Lock()
	if c, ok := t.clientCache[h.ID]; ok {
		t.mu.Unlock()
		return c, nil
	}
	cmu, ok := t.connectMu[h.ID]
	if !ok {
		cmu = &sync.Mutex{}
		t.connectMu[h.ID] = cmu
	}
	t.mu.Unlock()

	cmu.Lock()
	defer cmu.Unlock()

	// Re-check after acquiring the per-host lock: the previous
	// holder may have populated the cache.
	t.mu.Lock()
	if c, ok := t.clientCache[h.ID]; ok {
		t.mu.Unlock()
		return c, nil
	}
	t.mu.Unlock()

	pemKey, err := t.PrivateKeyResolver(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("ssh: load private key for host %s: %w", h.ID, err)
	}
	signer, err := gossh.ParsePrivateKey(pemKey)
	if err != nil {
		return nil, fmt.Errorf("ssh: parse private key for host %s: %w", h.ID, err)
	}

	// Capture the pinned serialised key from the callback so we can
	// persist it after Dial succeeds. We do not call PinHostKey from
	// inside the callback because the callback may run on a goroutine
	// owned by gossh; persisting through the store there could race
	// with concurrent Update.
	var pinned string
	cb := hostKeyCallback(h.SSHKnownHostKey, h.SSHStrictHostKey, func(s string) {
		pinned = s
	})

	cfg := &gossh.ClientConfig{
		User:            h.SSHUser,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: cb,
		Timeout:         t.connectTimeout(),
	}

	addr := h.SSHEndpoint
	// SSHEndpoint may be "host" alone — fall back to SSHPort or 22.
	if _, _, err := net.SplitHostPort(addr); err != nil {
		port := h.SSHPort
		if port == 0 {
			port = 22
		}
		addr = net.JoinHostPort(addr, strconv.Itoa(port))
	}

	c, err := t.dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh: dial %s@%s: %w", h.SSHUser, addr, err)
	}

	// Persist the TOFU-captured key (if any) so subsequent connects
	// enforce strict equality. Best-effort: a persist failure does
	// NOT fail the deploy because the connection itself was already
	// verified by the callback. The next Deploy's HostResolver will
	// Get a fresh row with the pin set; we deliberately do NOT mutate
	// the caller's *model.Host pointer here, since shared pointers
	// would race.
	if pinned != "" && t.PinHostKey != nil {
		if err := t.PinHostKey(ctx, h, pinned); err != nil {
			logf(lw, "[ssh] warn: failed to persist pinned host key: %v\n", err)
		} else {
			logf(lw, "[ssh] pinned host key for %s: %s\n", h.ID, pinned)
		}
	}

	t.mu.Lock()
	t.clientCache[h.ID] = c
	t.mu.Unlock()
	return c, nil
}

func (t *Target) connectTimeout() time.Duration {
	if t.ConnectTimeout > 0 {
		return t.ConnectTimeout
	}
	return 15 * time.Second
}

// dialHostFresh evicts the cached client for h and dials a fresh
// connection. Used by the evict-and-retry path when a cached client
// turns out to be dead (AD-H5).
func (t *Target) dialHostFresh(ctx context.Context, h *model.Host, lw io.Writer) (sshClient, error) {
	t.Evict(h.ID) // drop the dead entry
	return t.dialHost(ctx, h, lw)
}

// CloseAll closes any cached SSH connections. Called by the server
// on shutdown.
func (t *Target) CloseAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, c := range t.clientCache {
		_ = c.Close()
		delete(t.clientCache, id)
	}
}

// Evict closes and removes the cached SSH client for hostID. Called
// by HostService.DeleteHost so a deleted host doesn't leave a stale
// open TCP socket pinned in the cache. Safe to call for unknown
// hostIDs (no-op); concurrent with other Deploy / Status / Logs
// calls (they'll re-dial on next use if needed).
func (t *Target) Evict(hostID string) {
	t.mu.Lock()
	c, ok := t.clientCache[hostID]
	if ok {
		delete(t.clientCache, hostID)
	}
	delete(t.connectMu, hostID)
	t.mu.Unlock()
	if ok {
		_ = c.Close()
	}
}
