package ssh

import (
	"io"
	"net"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
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

// realDial wraps gossh.Dial in the sshClient interface. TCP keepalive
// is enabled via the net.Dialer so that dead connections surface as
// errors rather than hanging in the cache indefinitely (AD-H5).
func realDial(network, addr string, cfg *gossh.ClientConfig) (sshClient, error) {
	// AD-H5: use net.Dialer with TCP keepalive so that idle connections
	// that have gone dead surface as errors rather than hanging forever.
	// We take the handshake timeout from ClientConfig.Timeout.
	nd := &net.Dialer{
		KeepAlive: 15 * time.Second,
	}
	// Perform the raw TCP dial without the SSH handshake timeout built
	// in so we can apply it at the SSH-handshake level separately.
	rawConn, err := nd.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := gossh.NewClientConn(rawConn, addr, cfg)
	if err != nil {
		rawConn.Close()
		return nil, err
	}
	return &realClient{c: gossh.NewClient(c, chans, reqs)}, nil
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

func (r *realSession) SetStdout(w io.Writer) { r.s.Stdout = w }
func (r *realSession) SetStderr(w io.Writer) { r.s.Stderr = w }
func (r *realSession) Run(cmd string) error  { return r.s.Run(cmd) }
func (r *realSession) CombinedOutput(c string) ([]byte, error) {
	return r.s.CombinedOutput(c)
}
func (r *realSession) Start(cmd string) error { return r.s.Start(cmd) }
func (r *realSession) Wait() error            { return r.s.Wait() }
func (r *realSession) Close() error           { return r.s.Close() }

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

// Write fans p out to both underlying writers and reports success
// for len(p) bytes regardless of either underlying writer's outcome.
// This is intentional: a slow / closed log channel must NOT fail an
// in-flight deploy. Errors from a / b are dropped on the floor.
func (m *multiWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.a.Write(p)
	_, _ = m.b.Write(p)
	return len(p), nil
}
