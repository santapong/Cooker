package service

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/secrets/keepsave"
)

// fakeSecretsManager implements secrets.Manager with a scripted List.
type fakeSecretsManager struct {
	listErr error
	delay   time.Duration
}

func (f *fakeSecretsManager) Get(context.Context, string, string) ([]byte, error) { return nil, nil }
func (f *fakeSecretsManager) Put(context.Context, string, string, []byte) error   { return nil }
func (f *fakeSecretsManager) Delete(context.Context, string, string) error        { return nil }
func (f *fakeSecretsManager) List(ctx context.Context, _ string) ([]string, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	return []string{"DB_PASSWORD"}, nil
}

func TestCheckSecrets_OK(t *testing.T) {
	res := CheckSecrets(context.Background(), &fakeSecretsManager{}, "database", "env-1")
	if !res.OK || res.Error != "" || res.Backend != "database" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.LatencyMS < 0 {
		t.Fatalf("negative latency: %d", res.LatencyMS)
	}
}

func TestCheckSecrets_Classification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string // substring of the classified error
	}{
		{"keepsave 401", &keepsave.HTTPError{Status: 401, Message: "bad key"}, "authentication failed"},
		{"keepsave 403", &keepsave.HTTPError{Status: 403, Message: "no access"}, "authentication failed"},
		{"keepsave 500 is not auth", &keepsave.HTTPError{Status: 500, Message: "boom"}, "keepsave: 500"},
		{"wrapped auth error", errorWrap(&keepsave.HTTPError{Status: 401, Message: "x"}), "authentication failed"},
		{"deadline", context.DeadlineExceeded, "unreachable"},
		{"net op error", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, "unreachable"},
		{"other", errors.New("unexpected payload"), "unexpected payload"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := CheckSecrets(context.Background(), &fakeSecretsManager{listErr: tc.err}, "keepsave", "env-1")
			if res.OK {
				t.Fatal("probe should fail")
			}
			if !strings.Contains(res.Error, tc.want) {
				t.Fatalf("error %q does not contain %q", res.Error, tc.want)
			}
		})
	}
}

func TestCheckSecrets_HonorsCallerContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := CheckSecrets(ctx, &fakeSecretsManager{delay: 5 * time.Second}, "vault", "env-1")
	if res.OK {
		t.Fatal("probe should time out")
	}
	if !strings.Contains(res.Error, "unreachable") {
		t.Fatalf("timeout should classify as unreachable, got %q", res.Error)
	}
}

func errorWrap(err error) error { return &wrappedErr{err} }

type wrappedErr struct{ inner error }

func (w *wrappedErr) Error() string { return "list secrets: " + w.inner.Error() }
func (w *wrappedErr) Unwrap() error { return w.inner }
