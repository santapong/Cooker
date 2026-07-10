package ssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/santapong/cooker/internal/deploy/deploytarget"
	"github.com/santapong/cooker/internal/model"
)

// generateEd25519PEM returns a freshly minted ed25519 private key
// PEM-encoded as OpenSSH expects, plus the matching public key
// suitable for pinning. Used by the dial-injection tests so a real
// key parses cleanly through ParsePrivateKey.
func generateEd25519PEM(t *testing.T) ([]byte, gossh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 keygen: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("pkcs8 marshal: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh public key: %v", err)
	}
	return pemBytes, sshPub
}

// ---- composeRunCommand -----------------------------------------------------

func TestComposeRunCommand_BasicShape(t *testing.T) {
	got := composeRunCommand("cooker-app-1", "ghcr.io/example/web:v1", nil, nil)
	want := `docker run -d --restart=always --name 'cooker-app-1' 'ghcr.io/example/web:v1'`
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestComposeRunCommand_PortsAndEnvAreStable(t *testing.T) {
	// Same Spec twice must produce the same string — tests rely on
	// stable ordering across map iteration order.
	env := map[string]string{
		"DATABASE_URL": "postgres://x",
		"APP_ENV":      "prod",
		"FEATURE_X":    "1",
	}
	ports := []int{8080, 22, 80}
	first := composeRunCommand("c", "img:tag", env, ports)
	second := composeRunCommand("c", "img:tag", env, ports)
	if first != second {
		t.Fatalf("composeRunCommand is non-deterministic:\n %q\n %q", first, second)
	}
	// Env keys appear in sorted order.
	if !strings.Contains(first, "'APP_ENV=prod'") ||
		!strings.Contains(first, "'DATABASE_URL=postgres://x'") ||
		!strings.Contains(first, "'FEATURE_X=1'") {
		t.Fatalf("missing env: %s", first)
	}
	if strings.Index(first, "APP_ENV") > strings.Index(first, "DATABASE_URL") {
		t.Fatalf("env not sorted: %s", first)
	}
	// Ports appear in sorted order.
	if strings.Index(first, "'22:22'") > strings.Index(first, "'80:80'") {
		t.Fatalf("ports not sorted: %s", first)
	}
}

func TestComposeRunCommand_EscapesSingleQuoteInEnv(t *testing.T) {
	got := composeRunCommand("c", "img:tag", map[string]string{
		"NOTE": "it's tricky",
	}, nil)
	if !strings.Contains(got, `'NOTE=it'\''s tricky'`) {
		t.Fatalf("single-quote escape missing: %s", got)
	}
}

func TestComposeRunCommand_SkipsOutOfRangePorts(t *testing.T) {
	got := composeRunCommand("c", "img:tag", nil, []int{0, -5, 65536, 80})
	if !strings.Contains(got, "'80:80'") {
		t.Fatalf("expected port 80: %s", got)
	}
	if strings.Contains(got, "65536") || strings.Contains(got, ":0") || strings.Contains(got, "-5") {
		t.Fatalf("invalid ports leaked: %s", got)
	}
}

// ---- containerNameFor ------------------------------------------------------

func TestContainerNameFor_StripsMetacharacters(t *testing.T) {
	cases := map[string]string{
		"abc-123":          "cooker-abc-123",
		"with space":       "cooker-with-space",
		"semicolon;rm -rf": "cooker-semicolon-rm--rf",
		"":                 "cooker-",
	}
	for in, want := range cases {
		got := containerNameFor(in)
		if got != want {
			t.Errorf("containerNameFor(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- validImageRef ---------------------------------------------------------

func TestValidImageRef(t *testing.T) {
	good := []string{"nginx", "nginx:1.25", "ghcr.io/org/app:v1", "registry.example.com:5000/app@sha256:abc"}
	bad := []string{
		"",
		"img with space",
		"img;rm -rf /",
		"img$(date)",
		"img\nimg",
		// Leading-dash refs would be parsed by docker run/pull as
		// flags after the shell strips the quoting; reject up front.
		"-it",
		"--privileged",
		// Leading dot is rejected too (no real image ref starts with one).
		".hidden",
	}
	for _, s := range good {
		if !validImageRef(s) {
			t.Errorf("validImageRef(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if validImageRef(s) {
			t.Errorf("validImageRef(%q) = true, want false", s)
		}
	}
}

// ---- requireConfig / requireHost ------------------------------------------

func TestRequireConfig_Errors(t *testing.T) {
	tt := []struct {
		name string
		t    *Target
		want string
	}{
		{"nil target", nil, "ssh: target nil"},
		{"missing host resolver", &Target{}, "HostResolver required"},
		{"missing key resolver", &Target{HostResolver: func(context.Context, string) (*model.Host, error) { return nil, nil }}, "PrivateKeyResolver required"},
	}
	for _, c := range tt {
		t.Run(c.name, func(t *testing.T) {
			err := c.t.requireConfig()
			if err == nil || !errors.Is(err, deploytarget.ErrUnavailable) {
				t.Fatalf("want ErrUnavailable, got %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want substring %q, got %v", c.want, err)
			}
		})
	}
}

func TestRequireHost_Errors(t *testing.T) {
	for _, c := range []struct {
		name string
		h    *model.Host
		want string
	}{
		{"nil host", nil, "host nil"},
		{"wrong kind", &model.Host{Kind: model.HostKindDocker}, "host kind"},
		{"no endpoint", &model.Host{Kind: model.HostKindSSHDocker}, "missing SSHEndpoint"},
		{"no user", &model.Host{Kind: model.HostKindSSHDocker, SSHEndpoint: "x:22"}, "missing SSHUser"},
		{"no key ref", &model.Host{Kind: model.HostKindSSHDocker, SSHEndpoint: "x:22", SSHUser: "u"}, "missing SSHPrivateKeyRef"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := requireHost(c.h)
			if err == nil || !errors.Is(err, deploytarget.ErrUnavailable) {
				t.Fatalf("want ErrUnavailable, got %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want substring %q, got %v", c.want, err)
			}
		})
	}
}

// ---- fake SSH client (for Deploy / Status tests) --------------------------

// fakeClient captures the commands issued by the adapter. Tests
// inspect .commands after the call to assert pull/stop/rm/run order.
type fakeClient struct {
	mu       sync.Mutex
	commands []string
	// runErr, if set for a particular command prefix, makes that
	// session.Run return the error. Match by HasPrefix.
	runErr map[string]error
	// runOut, if set for a prefix, is written to stdout before Run
	// returns (used to simulate "No such container").
	runOut map[string]string
	closed bool
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		runErr: map[string]error{},
		runOut: map[string]string{},
	}
}

func (f *fakeClient) NewSession() (sshSession, error) {
	return &fakeSession{c: f}, nil
}

func (f *fakeClient) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

type fakeSession struct {
	c      *fakeClient
	stdout io.Writer
	stderr io.Writer
}

func (s *fakeSession) SetStdout(w io.Writer) { s.stdout = w }
func (s *fakeSession) SetStderr(w io.Writer) { s.stderr = w }
func (s *fakeSession) Run(cmd string) error {
	s.c.mu.Lock()
	s.c.commands = append(s.c.commands, cmd)
	out := ""
	for prefix, o := range s.c.runOut {
		if strings.HasPrefix(cmd, prefix) {
			out = o
			break
		}
	}
	var err error
	for prefix, e := range s.c.runErr {
		if strings.HasPrefix(cmd, prefix) {
			err = e
			break
		}
	}
	s.c.mu.Unlock()
	if out != "" && s.stdout != nil {
		_, _ = s.stdout.Write([]byte(out))
	}
	return err
}
func (s *fakeSession) CombinedOutput(cmd string) ([]byte, error) {
	s.c.mu.Lock()
	s.c.commands = append(s.c.commands, cmd)
	out := ""
	for prefix, o := range s.c.runOut {
		if strings.HasPrefix(cmd, prefix) {
			out = o
			break
		}
	}
	var err error
	for prefix, e := range s.c.runErr {
		if strings.HasPrefix(cmd, prefix) {
			err = e
			break
		}
	}
	s.c.mu.Unlock()
	return []byte(out), err
}
func (s *fakeSession) Start(cmd string) error {
	s.c.mu.Lock()
	s.c.commands = append(s.c.commands, cmd)
	s.c.mu.Unlock()
	return nil
}
func (s *fakeSession) Wait() error  { return nil }
func (s *fakeSession) Close() error { return nil }

// newTargetWithFakeDial returns a Target whose dial returns the
// supplied fake. The Host is wrapped in a HostResolver / key
// resolver that satisfies the adapter's preconditions.
func newTargetWithFakeDial(t *testing.T, fake *fakeClient, host *model.Host) (*Target, []byte) {
	t.Helper()
	pem, _ := generateEd25519PEM(t)
	tgt := &Target{
		dial: func(string, string, *gossh.ClientConfig) (sshClient, error) {
			return fake, nil
		},
		clientCache: map[string]sshClient{},
		connectMu:   map[string]*sync.Mutex{},
		HostResolver: func(_ context.Context, appID string) (*model.Host, error) {
			h := *host
			h.ID = appID
			return &h, nil
		},
		PrivateKeyResolver: func(context.Context, *model.Host) ([]byte, error) {
			return pem, nil
		},
	}
	return tgt, pem
}

func baseSSHHost() *model.Host {
	return &model.Host{
		ID:               "app-1",
		Kind:             model.HostKindSSHDocker,
		SSHEndpoint:      "1.2.3.4:22",
		SSHUser:          "deploy",
		SSHPrivateKeyRef: "secrets://hosts/app-1/key",
		// pinned to avoid TOFU branch in deploy-flow test
		SSHKnownHostKey:  "ssh-ed25519 AAAA",
		SSHStrictHostKey: true,
	}
}

// ---- Deploy happy path -----------------------------------------------------

func TestDeploy_RunsPullStopRmRunInOrder(t *testing.T) {
	fake := newFakeClient()
	// Stop / rm have nothing to stop on first deploy — simulate.
	fake.runOut["docker stop"] = "Error response from daemon: No such container: cooker-app-1"
	fake.runErr["docker stop"] = errors.New("exit status 1")
	fake.runOut["docker rm"] = "Error response from daemon: No such container: cooker-app-1"
	fake.runErr["docker rm"] = errors.New("exit status 1")

	tgt, _ := newTargetWithFakeDial(t, fake, baseSSHHost())
	// pin the host key so dialHost takes the strict path with no callback work
	tgt.HostResolver = func(_ context.Context, appID string) (*model.Host, error) {
		h := baseSSHHost()
		h.ID = appID
		return h, nil
	}
	// Bypass TOFU entirely by replacing the dial to skip key check
	tgt.dial = func(string, string, *gossh.ClientConfig) (sshClient, error) {
		return fake, nil
	}

	err := tgt.Deploy(context.Background(), deploytarget.Spec{
		AppID: "app-1",
		Image: "nginx:1.25",
		Env:   map[string]string{"X": "1"},
		Ports: []int{80},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	fake.mu.Lock()
	got := append([]string(nil), fake.commands...)
	fake.mu.Unlock()

	if len(got) != 4 {
		t.Fatalf("want 4 commands (pull/stop/rm/run), got %d: %#v", len(got), got)
	}
	checks := []struct {
		idx  int
		want string
	}{
		{0, "docker pull 'nginx:1.25'"},
		{1, "docker stop 'cooker-app-1'"},
		{2, "docker rm 'cooker-app-1'"},
	}
	for _, c := range checks {
		if got[c.idx] != c.want {
			t.Errorf("cmd[%d] = %q, want %q", c.idx, got[c.idx], c.want)
		}
	}
	if !strings.Contains(got[3], "docker run -d --restart=always") ||
		!strings.Contains(got[3], "--name 'cooker-app-1'") ||
		!strings.Contains(got[3], "-p '80:80'") ||
		!strings.Contains(got[3], "-e 'X=1'") ||
		!strings.HasSuffix(got[3], "'nginx:1.25'") {
		t.Errorf("run cmd malformed: %q", got[3])
	}
}

func TestDeploy_PullFailureAborts(t *testing.T) {
	fake := newFakeClient()
	fake.runErr["docker pull"] = errors.New("manifest unknown")

	tgt, _ := newTargetWithFakeDial(t, fake, baseSSHHost())
	err := tgt.Deploy(context.Background(), deploytarget.Spec{
		AppID: "app-1",
		Image: "nginx:does-not-exist",
	})
	if err == nil || !strings.Contains(err.Error(), "docker pull") {
		t.Fatalf("want pull error, got %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.commands) != 1 {
		t.Fatalf("only pull should have run, got %d cmds: %#v", len(fake.commands), fake.commands)
	}
}

func TestDeploy_RejectsHostileImage(t *testing.T) {
	fake := newFakeClient()
	tgt, _ := newTargetWithFakeDial(t, fake, baseSSHHost())
	err := tgt.Deploy(context.Background(), deploytarget.Spec{
		AppID: "app-1",
		Image: "nginx; rm -rf /",
	})
	if err == nil || !strings.Contains(err.Error(), "rejected image ref") {
		t.Fatalf("want rejection, got %v", err)
	}
}

func TestDeploy_RejectsHostWithWrongKind(t *testing.T) {
	fake := newFakeClient()
	host := baseSSHHost()
	host.Kind = model.HostKindDocker // wrong
	tgt, _ := newTargetWithFakeDial(t, fake, host)
	err := tgt.Deploy(context.Background(), deploytarget.Spec{AppID: "app-1", Image: "nginx"})
	if err == nil || !errors.Is(err, deploytarget.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

// ---- Status ----------------------------------------------------------------

func TestStatus_RunningTrue(t *testing.T) {
	fake := newFakeClient()
	fake.runOut["docker inspect"] = "true\n"
	tgt, _ := newTargetWithFakeDial(t, fake, baseSSHHost())
	st, err := tgt.Status(context.Background(), "app-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Healthy || st.Replicas != 1 {
		t.Fatalf("want healthy/replicas=1, got %+v", st)
	}
}

func TestStatus_RunningFalse(t *testing.T) {
	fake := newFakeClient()
	fake.runOut["docker inspect"] = "false\n"
	tgt, _ := newTargetWithFakeDial(t, fake, baseSSHHost())
	st, err := tgt.Status(context.Background(), "app-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Healthy || st.Replicas != 0 {
		t.Fatalf("want unhealthy/replicas=0, got %+v", st)
	}
}

// ---- Rollback (v1 unimplemented) ------------------------------------------

func TestRollback_ReturnsUnavailable(t *testing.T) {
	tgt := &Target{
		HostResolver: func(context.Context, string) (*model.Host, error) {
			return baseSSHHost(), nil
		},
		PrivateKeyResolver: func(context.Context, *model.Host) ([]byte, error) {
			return nil, nil
		},
	}
	err := tgt.Rollback(context.Background(), "app-1")
	if err == nil || !errors.Is(err, deploytarget.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

// ---- LogWriter capture -----------------------------------------------------

func TestDeploy_LogWriterReceivesStepLines(t *testing.T) {
	fake := newFakeClient()
	fake.runOut["docker stop"] = ""
	fake.runOut["docker rm"] = ""
	var buf bytes.Buffer
	tgt, _ := newTargetWithFakeDial(t, fake, baseSSHHost())
	tgt.LogWriter = &buf
	if err := tgt.Deploy(context.Background(), deploytarget.Spec{
		AppID: "app-1",
		Image: "nginx",
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	logs := buf.String()
	for _, want := range []string{"docker pull nginx", "docker stop cooker-app-1", "docker run -d --restart=always"} {
		if !strings.Contains(logs, want) {
			t.Errorf("LogWriter missing %q\n--- got ---\n%s", want, logs)
		}
	}
	// And the redaction-style invariant: no private key fingerprint
	// or PEM block leaks via the log writer.
	if strings.Contains(logs, "PRIVATE KEY") {
		t.Fatalf("LogWriter leaked PRIVATE KEY block: %s", logs)
	}
}

// ---- Kind ------------------------------------------------------------------

func TestKind(t *testing.T) {
	if (&Target{}).Kind() != model.DeployTargetSSH {
		t.Fatalf("Kind wrong")
	}
}

// ---- Concurrent dialHost: client cache is single-shot per host ------------

func TestDialHost_CachesClientPerHost(t *testing.T) {
	fake := newFakeClient()
	dialCount := 0
	var mu sync.Mutex
	tgt := &Target{
		clientCache: map[string]sshClient{},
		connectMu:   map[string]*sync.Mutex{},
		dial: func(string, string, *gossh.ClientConfig) (sshClient, error) {
			mu.Lock()
			dialCount++
			mu.Unlock()
			return fake, nil
		},
	}
	pemBytes, _ := generateEd25519PEM(t)
	tgt.PrivateKeyResolver = func(context.Context, *model.Host) ([]byte, error) { return pemBytes, nil }
	h := baseSSHHost()
	for i := 0; i < 3; i++ {
		if _, err := tgt.dialHost(context.Background(), h, nil); err != nil {
			t.Fatalf("dialHost: %v", err)
		}
	}
	// First dial primes the cache; remaining 2 hit it.
	if dialCount != 1 {
		t.Fatalf("expected 1 dial across 3 calls, got %d", dialCount)
	}
	tgt.CloseAll()
}

// TestDialHost_ConcurrentFirstDial verifies that two simultaneous
// first-connect Deploys for the same host result in exactly one
// dial — i.e. the per-host singleflight that protects against the
// "two clients dialed, one leaked" race documented in the audit.
func TestDialHost_ConcurrentFirstDial(t *testing.T) {
	fake := newFakeClient()
	dialCount := 0
	var mu sync.Mutex
	tgt := &Target{
		clientCache: map[string]sshClient{},
		connectMu:   map[string]*sync.Mutex{},
		dial: func(string, string, *gossh.ClientConfig) (sshClient, error) {
			mu.Lock()
			dialCount++
			mu.Unlock()
			return fake, nil
		},
	}
	pemBytes, _ := generateEd25519PEM(t)
	tgt.PrivateKeyResolver = func(context.Context, *model.Host) ([]byte, error) { return pemBytes, nil }
	h := baseSSHHost()

	const goroutines = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := tgt.dialHost(context.Background(), h, nil); err != nil {
				errCh <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("dialHost: %v", err)
	}
	mu.Lock()
	got := dialCount
	mu.Unlock()
	if got != 1 {
		t.Fatalf("expected exactly 1 dial across %d concurrent calls, got %d", goroutines, got)
	}
	tgt.CloseAll()
}

// TestEvict_DropsCachedClient verifies that Evict closes the cached
// SSH client and removes the cache entry so a subsequent dialHost
// re-dials. Wires the audit finding "SSH client cache outlives Host
// deletion" closed.
func TestEvict_DropsCachedClient(t *testing.T) {
	fake := newFakeClient()
	dialCount := 0
	var mu sync.Mutex
	tgt := &Target{
		clientCache: map[string]sshClient{},
		connectMu:   map[string]*sync.Mutex{},
		dial: func(string, string, *gossh.ClientConfig) (sshClient, error) {
			mu.Lock()
			dialCount++
			mu.Unlock()
			return fake, nil
		},
	}
	pemBytes, _ := generateEd25519PEM(t)
	tgt.PrivateKeyResolver = func(context.Context, *model.Host) ([]byte, error) { return pemBytes, nil }
	h := baseSSHHost()
	if _, err := tgt.dialHost(context.Background(), h, nil); err != nil {
		t.Fatalf("dialHost: %v", err)
	}
	tgt.Evict(h.ID)
	if !fake.closed {
		t.Fatalf("Evict should have closed the cached client")
	}
	// Next dialHost re-dials.
	if _, err := tgt.dialHost(context.Background(), h, nil); err != nil {
		t.Fatalf("dialHost after Evict: %v", err)
	}
	mu.Lock()
	got := dialCount
	mu.Unlock()
	if got != 2 {
		t.Fatalf("expected 2 dials (one before Evict, one after), got %d", got)
	}
}

// TestDeploy_SpecLogWriterOverridesTarget verifies that a per-call
// Spec.LogWriter wins over the Target-level LogWriter — preventing
// the cross-deploy log interleaving documented in the audit (two
// concurrent Deploys must not tee into the same Target.LogWriter).
func TestDeploy_SpecLogWriterOverridesTarget(t *testing.T) {
	fake := newFakeClient()
	tgt, _ := newTargetWithFakeDial(t, fake, baseSSHHost())
	var targetBuf, specBuf bytes.Buffer
	tgt.LogWriter = &targetBuf
	if err := tgt.Deploy(context.Background(), deploytarget.Spec{
		AppID:     "app-1",
		Image:     "nginx",
		LogWriter: &specBuf,
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !strings.Contains(specBuf.String(), "docker pull nginx") {
		t.Fatalf("spec writer should have captured pull line; got %q", specBuf.String())
	}
	if strings.Contains(targetBuf.String(), "docker pull nginx") {
		t.Fatalf("target writer should NOT have received logs when spec writer was set; got %q", targetBuf.String())
	}
}

// Diagnostic: simply build a fake ssh.Signer to ensure
// generateEd25519PEM produces a key gossh.ParsePrivateKey accepts.
// This was the silent failure mode of the first cut.
func TestGenerateEd25519PEMRoundTrips(t *testing.T) {
	pemBytes, _ := generateEd25519PEM(t)
	if _, err := gossh.ParsePrivateKey(pemBytes); err != nil {
		t.Fatalf("ParsePrivateKey: %v\nPEM:\n%s", err, pemBytes)
	}
}

// Sanity: the "no unsafe-host-key-callback" rule isn't bypassed by
// callers passing in their own dialer. The configured callback is
// always hostKeyCallback (see ssh.go line "HostKeyCallback: cb,").
// The package-level grep in the CI test plan is the source of truth;
// this test exists as a no-op marker so refactor reviewers see the
// invariant called out.
func TestNoUnsafeHostKeyCallbackReference(t *testing.T) {
	_ = fmt.Sprintf("%T", gossh.PublicKeys)
}
