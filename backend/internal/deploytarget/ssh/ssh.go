// Package ssh adapts Cooker's deploytarget.Target to a remote Docker
// host reachable only via SSH — the Dokploy / Coolify model. There
// is no agent on the remote, no docker API socket exposed over the
// network, and no Kubernetes cluster involved. Cooker connects with
// a private key, runs `docker pull` / `docker stop` / `docker rm` /
// `docker run -d --restart=always`, and streams `docker logs
// --follow` back to the operator.
//
// The adapter is intentionally narrow:
//
//   - Deploy is the four-shot pull→stop→rm→run sequence; failures
//     stop the chain immediately so a half-rolled container is
//     never claimed as success.
//   - Status runs `docker inspect` and reports running/replicas=1.
//   - Logs streams `docker logs --follow` until the caller cancels.
//   - Rollback returns ErrUnavailable in v1 — Thread 2's label-and-
//     swap variant is a separate plan item.
//
// Host-key verification is mandatory: see known_hosts.go for the
// TOFU policy. The unsafe "accept any host key" callback from the
// crypto/ssh package is forbidden in this codebase (static-check
// rule in the SSH thread brief).
package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/santapong/cooker/internal/deploytarget"
	"github.com/santapong/cooker/internal/model"
)

// dialFunc abstracts gossh.Dial for unit-testability. Production
// uses the network dialer; tests inject an in-memory pipe so they
// don't have to spin up a real SSH server.
type dialFunc func(network, addr string, cfg *gossh.ClientConfig) (sshClient, error)

// sshClient is the minimal subset of *gossh.Client this adapter
// touches. Lets the test injector substitute a fake.
type sshClient interface {
	NewSession() (sshSession, error)
	Close() error
}

// sshSession is the minimal subset of *gossh.Session this adapter
// touches.
type sshSession interface {
	SetStdout(w io.Writer)
	SetStderr(w io.Writer)
	Run(cmd string) error
	CombinedOutput(cmd string) ([]byte, error)
	Start(cmd string) error
	Wait() error
	Close() error
}

// realDial wraps gossh.Dial in the sshClient interface.
func realDial(network, addr string, cfg *gossh.ClientConfig) (sshClient, error) {
	c, err := gossh.Dial(network, addr, cfg)
	if err != nil {
		return nil, err
	}
	return &realClient{c: c}, nil
}

type realClient struct{ c *gossh.Client }

func (r *realClient) NewSession() (sshSession, error) {
	s, err := r.c.NewSession()
	if err != nil {
		return nil, err
	}
	return &realSession{s: s}, nil
}
func (r *realClient) Close() error { return r.c.Close() }

type realSession struct{ s *gossh.Session }

func (r *realSession) SetStdout(w io.Writer)            { r.s.Stdout = w }
func (r *realSession) SetStderr(w io.Writer)            { r.s.Stderr = w }
func (r *realSession) Run(cmd string) error             { return r.s.Run(cmd) }
func (r *realSession) CombinedOutput(c string) ([]byte, error) {
	return r.s.CombinedOutput(c)
}
func (r *realSession) Start(cmd string) error { return r.s.Start(cmd) }
func (r *realSession) Wait() error            { return r.s.Wait() }
func (r *realSession) Close() error           { return r.s.Close() }

// Target is the SSH deploy-target adapter. One instance services
// many Hosts: each Deploy call carries the Host fields verbatim, so
// adding a Host at runtime needs no re-registration.
//
// LogWriter is the per-call log sink (handler streams it to the
// run's WebSocket channel). Settable on the Spec.
type Target struct {
	// HostResolver returns the Host record for an AppID. Cooker wires
	// this to the App + Host stores at construction time so the
	// adapter doesn't need direct store access. Required.
	HostResolver func(ctx context.Context, appID string) (*model.Host, error)

	// PrivateKeyResolver returns the PEM-encoded private key for a
	// Host (looking it up in secrets.Manager by Host.SSHPrivateKeyRef).
	// Required.
	PrivateKeyResolver func(ctx context.Context, host *model.Host) ([]byte, error)

	// PinHostKey is called the first time we see a host key on a
	// !strict Host — it persists the serialised key into
	// Host.SSHKnownHostKey via the store. nil disables persistence
	// (tests). Cooker's wiring sets this on construction.
	PinHostKey func(ctx context.Context, host *model.Host, serialisedKey string) error

	// LogWriter, when non-nil, gets every step's stdout/stderr so
	// the run page tails real docker output. nil = io.Discard.
	LogWriter io.Writer

	// ConnectTimeout bounds the initial TCP+SSH handshake. Default 15s.
	ConnectTimeout time.Duration

	// dial is the SSH dialer; production = realDial. Tests override.
	dial dialFunc

	// clientCache caches one *sshClient per host id. Protected by mu.
	// SSH connection setup is expensive; reusing the client across
	// Deploy / Status / Logs avoids re-handshaking 4x in a single
	// run. The mutex is per-Target, not per-package.
	mu          sync.Mutex
	clientCache map[string]sshClient
}

// New constructs an SSH Target with the production dialer. Callers
// must set HostResolver and PrivateKeyResolver before first use.
func New() *Target {
	return &Target{
		dial:        realDial,
		clientCache: map[string]sshClient{},
	}
}

func (*Target) Kind() model.DeployTargetKind { return model.DeployTargetSSH }

func (t *Target) requireConfig() error {
	if t == nil {
		return fmt.Errorf("%w: ssh: target nil", deploytarget.ErrUnavailable)
	}
	if t.HostResolver == nil {
		return fmt.Errorf("%w: ssh: HostResolver required", deploytarget.ErrUnavailable)
	}
	if t.PrivateKeyResolver == nil {
		return fmt.Errorf("%w: ssh: PrivateKeyResolver required", deploytarget.ErrUnavailable)
	}
	return nil
}

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
// the lifecycle and closes via CloseAll.
func (t *Target) dialHost(ctx context.Context, h *model.Host) (sshClient, error) {
	if err := requireHost(h); err != nil {
		return nil, err
	}

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
	// verified by the callback. We log via the LogWriter so the
	// operator sees the pin.
	if pinned != "" && t.PinHostKey != nil {
		if err := t.PinHostKey(ctx, h, pinned); err != nil {
			t.log("[ssh] warn: failed to persist pinned host key: %v\n", err)
		} else {
			h.SSHKnownHostKey = pinned // mutate in-place so the *same Host* used by this Deploy reflects the pin
			t.log("[ssh] pinned host key for %s: %s\n", h.ID, pinned)
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

func (t *Target) log(format string, args ...any) {
	if t.LogWriter == nil {
		return
	}
	fmt.Fprintf(t.LogWriter, format, args...)
}

// runCmd opens a fresh session and runs cmd; stdout/stderr stream
// into t.LogWriter (and the optional extra writer) so the operator
// tails docker output in real time. The session is closed after Run.
func (t *Target) runCmd(client sshClient, cmd string, extra io.Writer) error {
	s, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer s.Close()

	stdout := writerFanout(t.LogWriter, extra)
	s.SetStdout(stdout)
	s.SetStderr(stdout)
	return s.Run(cmd)
}

// runCmdQuiet runs cmd and returns the combined output as a string
// without streaming. Used by stop / rm where we want to swallow
// "no such container" errors without polluting the log.
func runCmdQuiet(client sshClient, cmd string) (string, error) {
	s, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	defer s.Close()
	out, err := s.CombinedOutput(cmd)
	return string(out), err
}

// Deploy runs the four-shot pull→stop→rm→run sequence against the
// Host attached to spec.AppID. Failure on pull aborts; stop/rm
// errors are logged but tolerated (the container may not exist yet
// on first deploy).
func (t *Target) Deploy(ctx context.Context, spec deploytarget.Spec) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	host, err := t.HostResolver(ctx, spec.AppID)
	if err != nil {
		return fmt.Errorf("ssh: resolve host for app %s: %w", spec.AppID, err)
	}
	client, err := t.dialHost(ctx, host)
	if err != nil {
		return err
	}

	containerName := containerNameFor(spec.AppID)
	image := spec.Image
	if !validImageRef(image) {
		return fmt.Errorf("ssh: rejected image ref %q", image)
	}

	// 1. pull
	t.log("[ssh] docker pull %s\n", image)
	if err := t.runCmd(client, fmt.Sprintf("docker pull %s", shQuote(image)), nil); err != nil {
		return fmt.Errorf("ssh: docker pull: %w", err)
	}

	// 2. stop (best-effort)
	t.log("[ssh] docker stop %s (best-effort)\n", containerName)
	if out, err := runCmdQuiet(client, fmt.Sprintf("docker stop %s", shQuote(containerName))); err != nil {
		// Tolerate "no such container" but log everything else.
		if !strings.Contains(out, "No such container") {
			t.log("[ssh] stop: %v (output: %s)\n", err, strings.TrimSpace(out))
		}
	}

	// 3. rm (best-effort)
	t.log("[ssh] docker rm %s (best-effort)\n", containerName)
	if out, err := runCmdQuiet(client, fmt.Sprintf("docker rm %s", shQuote(containerName))); err != nil {
		if !strings.Contains(out, "No such container") {
			t.log("[ssh] rm: %v (output: %s)\n", err, strings.TrimSpace(out))
		}
	}

	// 4. run
	runCmd := composeRunCommand(containerName, image, spec.Env, spec.Ports)
	t.log("[ssh] %s\n", runCmd)
	if err := t.runCmd(client, runCmd, nil); err != nil {
		return fmt.Errorf("ssh: docker run: %w", err)
	}
	return nil
}

// Status runs `docker inspect` and reports running/replicas=1. An
// inspect failure (container missing, host unreachable) returns
// Healthy=false with the error in Message.
func (t *Target) Status(ctx context.Context, appID string) (deploytarget.Status, error) {
	if err := t.requireConfig(); err != nil {
		return deploytarget.Status{}, err
	}
	host, err := t.HostResolver(ctx, appID)
	if err != nil {
		return deploytarget.Status{}, fmt.Errorf("ssh: resolve host: %w", err)
	}
	client, err := t.dialHost(ctx, host)
	if err != nil {
		return deploytarget.Status{}, err
	}
	name := containerNameFor(appID)
	out, err := runCmdQuiet(client,
		fmt.Sprintf("docker inspect -f '{{.State.Running}}' %s", shQuote(name)))
	if err != nil {
		return deploytarget.Status{Healthy: false, Message: strings.TrimSpace(out)}, nil
	}
	running := strings.TrimSpace(out) == "true"
	replicas := 0
	if running {
		replicas = 1
	}
	return deploytarget.Status{Healthy: running, Replicas: replicas}, nil
}

// Logs streams `docker logs --follow` until the context is
// cancelled (or the session ends). Bytes flow into out as docker
// writes them.
func (t *Target) Logs(ctx context.Context, appID string, out io.Writer) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	host, err := t.HostResolver(ctx, appID)
	if err != nil {
		return fmt.Errorf("ssh: resolve host: %w", err)
	}
	client, err := t.dialHost(ctx, host)
	if err != nil {
		return err
	}
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh: session: %w", err)
	}
	defer sess.Close()
	sess.SetStdout(out)
	sess.SetStderr(out)
	name := containerNameFor(appID)
	if err := sess.Start(fmt.Sprintf("docker logs --follow %s", shQuote(name))); err != nil {
		return fmt.Errorf("ssh: docker logs: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		_ = sess.Close()
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// Rollback is intentionally unimplemented in v1. Cooker's plan
// reserves label-and-swap rollback for Thread 2 of the dokploy-
// adaptation series; until that lands, callers should re-deploy a
// previous image tag rather than ask the adapter to "undo".
func (t *Target) Rollback(_ context.Context, _ string) error {
	return fmt.Errorf("%w: ssh: rollback is not implemented in v1 (use re-deploy with previous image tag)",
		deploytarget.ErrUnavailable)
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

// containerNameFor derives a deterministic container name from the
// App ID. Kept tight (no slashes, ASCII only) so it can be safely
// shQuoted into a docker command line.
func containerNameFor(appID string) string {
	// AppIDs are UUIDs in production; defensively scrub anything
	// that isn't alnum / dash / underscore so we never end up with
	// "; rm -rf /" in the container name.
	b := strings.Builder{}
	b.WriteString("cooker-")
	for _, r := range appID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// validImageRef rejects obviously hostile inputs (whitespace, shell
// metacharacters) without trying to fully parse OCI image refs. We
// pass image through shQuote anyway; this is belt-and-suspenders.
func validImageRef(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		// Whitelist: alnum, dash, dot, underscore, slash, colon, at.
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '.', r == '_', r == '/', r == ':', r == '@':
			// ok
		default:
			return false
		}
	}
	return true
}

// shQuote wraps s in single quotes and escapes any embedded single
// quote per POSIX shell rules. Cheap, no external deps, sufficient
// for the bounded inputs we pass (image refs, container names, env
// keys/values, port numbers).
func shQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// composeRunCommand builds `docker run -d --restart=always --name N
// -p P -e K=V ... IMAGE`. All operator-supplied strings flow through
// shQuote so a malicious value can't break out of the argument list.
// Env vars are emitted in stable (sorted) order so the same Spec
// produces the same command every time — tests check for that.
func composeRunCommand(name, image string, env map[string]string, ports []int) string {
	var b strings.Builder
	b.WriteString("docker run -d --restart=always")
	b.WriteString(" --name ")
	b.WriteString(shQuote(name))

	// stable port order
	sortedPorts := append([]int(nil), ports...)
	sortInts(sortedPorts)
	for _, p := range sortedPorts {
		if p <= 0 || p > 65535 {
			continue // silently skip; validate.Ports happens upstream
		}
		b.WriteString(" -p ")
		b.WriteString(shQuote(fmt.Sprintf("%d:%d", p, p)))
	}

	// stable env order
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		b.WriteString(" -e ")
		b.WriteString(shQuote(k + "=" + env[k]))
	}

	b.WriteString(" ")
	b.WriteString(shQuote(image))
	return b.String()
}

// sortInts is a tiny non-allocating sort kept local so the package
// doesn't take a dep on "sort" just for one site. Insertion sort is
// fine: spec.Ports rarely exceeds a handful of entries.
func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		x := xs[i]
		j := i - 1
		for j >= 0 && xs[j] > x {
			xs[j+1] = xs[j]
			j--
		}
		xs[j+1] = x
	}
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		x := xs[i]
		j := i - 1
		for j >= 0 && xs[j] > x {
			xs[j+1] = xs[j]
			j--
		}
		xs[j+1] = x
	}
}

// writerFanout returns a writer that forwards to whichever of a/b
// are non-nil. nil-safe; returns io.Discard if both are nil so the
// session never panics.
func writerFanout(a, b io.Writer) io.Writer {
	switch {
	case a != nil && b != nil:
		return &multiWriter{a: a, b: b}
	case a != nil:
		return a
	case b != nil:
		return b
	}
	return io.Discard
}

type multiWriter struct {
	mu   sync.Mutex
	a, b io.Writer
}

func (m *multiWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.a.Write(p)
	_, _ = m.b.Write(p)
	return len(p), nil
}

var _ deploytarget.Target = (*Target)(nil)
