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
	"strings"
	"sync"
	"time"

	"github.com/santapong/cooker/internal/deploytarget"
	"github.com/santapong/cooker/internal/model"
)

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

	// PullTimeout caps `docker pull` per deploy. Zero defaults to 10 min.
	// A slow image pull must not block the executor for the entire run
	// deadline (M ssh/ssh.go:373).
	PullTimeout time.Duration

	// CmdTimeout caps short commands (docker stop/rm/run/inspect).
	// Zero defaults to 30s.
	CmdTimeout time.Duration

	// dial is the SSH dialer; production = realDial. Tests override.
	dial dialFunc

	// clientCache caches one *sshClient per host id. Protected by mu.
	// SSH connection setup is expensive; reusing the client across
	// Deploy / Status / Logs avoids re-handshaking 4x in a single
	// run. The mutex is per-Target, not per-package.
	mu          sync.Mutex
	clientCache map[string]sshClient
	// connectMu serialises concurrent first-dials per host. Without
	// it, two simultaneous Deploys to a never-before-seen host both
	// miss the cache, both dial, and one of the resulting clients
	// leaks. Keyed by host ID; entries are created on first connect
	// and never removed (the long-tail count is bounded by host
	// inventory). Protected by mu for map writes; the per-host Mutex
	// pointer itself is what holds the singleflight.
	connectMu map[string]*sync.Mutex
}

// New constructs an SSH Target with the production dialer. Callers
// must set HostResolver and PrivateKeyResolver before first use.
func New() *Target {
	return &Target{
		dial:        realDial,
		clientCache: map[string]sshClient{},
		connectMu:   map[string]*sync.Mutex{},
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

func (t *Target) pullTimeout() time.Duration {
	if t.PullTimeout > 0 {
		return t.PullTimeout
	}
	return 10 * time.Minute
}

func (t *Target) cmdTimeout() time.Duration {
	if t.CmdTimeout > 0 {
		return t.CmdTimeout
	}
	return 30 * time.Second
}

// runCmdCtx is runCmd with a per-command deadline. The session is
// closed when the deadline fires, which causes Run() to return an
// error; the context error is returned in that case (M ssh.go:373).
func runCmdCtx(ctx context.Context, timeout time.Duration, client sshClient, cmd string, lw io.Writer) error {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		err error
	}
	done := make(chan result, 1)
	s, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	stdout := writerFanout(lw, nil)
	s.SetStdout(stdout)
	s.SetStderr(stdout)
	go func() {
		done <- result{s.Run(cmd)}
	}()
	select {
	case <-cmdCtx.Done():
		s.Close() // unblock Run
		return fmt.Errorf("ssh: command timed out after %s: %w", timeout, cmdCtx.Err())
	case r := <-done:
		s.Close()
		return r.err
	}
}

// runCmdQuietCtx is runCmdQuiet with a per-command deadline.
func runCmdQuietCtx(ctx context.Context, timeout time.Duration, client sshClient, cmd string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	s, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	go func() {
		out, err := s.CombinedOutput(cmd)
		done <- result{out, err}
	}()
	select {
	case <-cmdCtx.Done():
		s.Close()
		return "", fmt.Errorf("ssh: command timed out after %s: %w", timeout, cmdCtx.Err())
	case r := <-done:
		s.Close()
		return string(r.out), r.err
	}
}

// logf writes a formatted line to w; nil w silently discards. Used
// for in-band TOFU / step-progress log lines.
func logf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format, args...)
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

// resolveLogWriter returns the per-call writer for this Deploy.
// Order: spec.LogWriter (per-call override) → t.LogWriter (Target
// default) → nil (no logging).
func (t *Target) resolveLogWriter(spec deploytarget.Spec) io.Writer {
	if spec.LogWriter != nil {
		return spec.LogWriter
	}
	return t.LogWriter
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
	lw := t.resolveLogWriter(spec)
	client, err := t.dialHost(ctx, host, lw)
	if err != nil {
		return err
	}

	containerName := containerNameFor(spec.AppID)
	image := spec.Image
	if !validImageRef(image) {
		return fmt.Errorf("ssh: rejected image ref %q", image)
	}

	// 1. pull — the first real command on the cached client. If the
	// session open fails (dead cached connection), evict the client and
	// re-dial once before surfacing the error (AD-H5). TCP keepalive in
	// realDial surfaces dead connections faster, but the evict-and-retry
	// is the fallback for connections that went idle between keepalives.
	// Per-command timeout (M ssh.go:373): docker pull can block forever
	// on a slow registry; cap it at PullTimeout (default 10 min).
	logf(lw, "[ssh] docker pull %s\n", image)
	pullCmd := fmt.Sprintf("docker pull %s", shQuote(image))
	if err := runCmdCtx(ctx, t.pullTimeout(), client, pullCmd, lw); err != nil {
		// If the error is a session-open failure (wrapped "session: ...")
		// try once more with a fresh connection.
		if strings.Contains(err.Error(), "session:") {
			logf(lw, "[ssh] session error (%v); evicting cached client and re-dialling\n", err)
			var dialErr error
			client, dialErr = t.dialHostFresh(ctx, host, lw)
			if dialErr != nil {
				return dialErr
			}
			if err2 := runCmdCtx(ctx, t.pullTimeout(), client, pullCmd, lw); err2 != nil {
				return fmt.Errorf("ssh: docker pull: %w", err2)
			}
		} else {
			return fmt.Errorf("ssh: docker pull: %w", err)
		}
	}

	// 2. stop (best-effort) — short timeout: stop should be near-instant
	logf(lw, "[ssh] docker stop %s (best-effort)\n", containerName)
	if out, err := runCmdQuietCtx(ctx, t.cmdTimeout(), client, fmt.Sprintf("docker stop %s", shQuote(containerName))); err != nil {
		// Tolerate "no such container" but log everything else.
		if !strings.Contains(out, "No such container") {
			logf(lw, "[ssh] stop: %v (output: %s)\n", err, strings.TrimSpace(out))
		}
	}

	// 3. rm (best-effort)
	logf(lw, "[ssh] docker rm %s (best-effort)\n", containerName)
	if out, err := runCmdQuietCtx(ctx, t.cmdTimeout(), client, fmt.Sprintf("docker rm %s", shQuote(containerName))); err != nil {
		if !strings.Contains(out, "No such container") {
			logf(lw, "[ssh] rm: %v (output: %s)\n", err, strings.TrimSpace(out))
		}
	}

	// 4. run
	dockerRun := composeRunCommand(containerName, image, spec.Env, spec.Ports)
	logf(lw, "[ssh] %s\n", dockerRun)
	if err := runCmdCtx(ctx, t.cmdTimeout(), client, dockerRun, lw); err != nil {
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
	client, err := t.dialHost(ctx, host, t.LogWriter)
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
	client, err := t.dialHost(ctx, host, out)
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
		// Defer at the top of this function will Close the session
		// when we return; don't double-close here.
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
//
// A leading "-" or "." is rejected to prevent the shell-stripped
// quoted form from being interpreted as a flag by `docker pull` /
// `docker run` (e.g. an image ref like "-it" would otherwise reach
// docker as a positional that the flag parser claims). OCI image
// refs never start with a dash in legitimate usage.
func validImageRef(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' || s[0] == '.' {
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

var _ deploytarget.Target = (*Target)(nil)
