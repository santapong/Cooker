package pusher

import (
	"context"
	"fmt"
	"io"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/santapong/cooker/internal/build/registryauth"
)

// Crane publishes a private OCI artifact or copies an existing remote image/index.
// It does not assume access to Docker or BuildKit daemon image stores.
type Crane struct {
	UserAgent string
}

func NewCrane() *Crane { return &Crane{UserAgent: "cooker/0.1"} }

// authForRequest builds an authn.Keychain from the request's Auth
// callback. Nil is anonymous; host Docker config/helpers are never consulted.
func authForRequest(req Request) authn.Keychain {
	if req.Auth == nil {
		return registryauth.Keychain(context.Background(), nil)
	}
	return authn.NewKeychainFromHelper(authHelper(req.Auth))
}

type authHelper func(host string) (user, pass string, ok bool)

func (a authHelper) Get(host string) (string, string, error) {
	user, pass, ok := a(host)
	if !ok {
		// L crane.go:38-43: return empty credentials (not an error) for
		// the no-credentials case so the go-containerregistry wrapper
		// produces an anonymous authenticator rather than a hard failure.
		// The wrapper already converts any error to Anonymous, but
		// returning ("", "", nil) makes the intent explicit: this host is
		// intentionally unauthenticated.
		return "", "", nil
	}
	return user, pass, nil
}

func (c *Crane) Push(ctx context.Context, req Request) (Result, error) {
	dstRef, err := name.ParseReference(req.Target)
	if err != nil {
		return Result{}, fmt.Errorf("%w: parse target: %v", ErrUnavailable, err)
	}
	keychain := authForRequest(req)
	if req.RegistryAuth != nil {
		keychain = registryauth.Keychain(ctx, req.RegistryAuth)
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "cooker"
	}
	opts := []remote.Option{remote.WithContext(ctx), remote.WithAuthFromKeychain(keychain), remote.WithUserAgent(ua)}
	logf(req.LogWriter, "Publishing image to %s\n", req.Target)
	expected := ""
	if req.Artifact != nil {
		path, desc, loadErr := req.Artifact.Contents()
		if loadErr != nil {
			return Result{}, fmt.Errorf("load build artifact: %w", loadErr)
		}
		expected = desc.Digest.String()
		switch {
		case desc.MediaType.IsIndex():
			root, loadErr := path.ImageIndex()
			if loadErr != nil {
				return Result{}, fmt.Errorf("load artifact index: %w", loadErr)
			}
			index, loadErr := root.ImageIndex(desc.Digest)
			if loadErr != nil {
				return Result{}, fmt.Errorf("load artifact index: %w", loadErr)
			}
			err = remote.WriteIndex(dstRef, index, opts...)
		case desc.MediaType.IsImage():
			img, loadErr := path.Image(desc.Digest)
			if loadErr != nil {
				return Result{}, fmt.Errorf("load artifact image: %w", loadErr)
			}
			err = remote.Write(dstRef, img, opts...)
		default:
			return Result{}, fmt.Errorf("unsupported build artifact media type %q", desc.MediaType)
		}
	} else {
		srcRef, parseErr := name.ParseReference(req.Source)
		if parseErr != nil {
			return Result{}, fmt.Errorf("%w: parse source: %v", ErrUnavailable, parseErr)
		}
		desc, loadErr := remote.Get(srcRef, opts...)
		if loadErr != nil {
			return Result{}, fmt.Errorf("%w: load remote source: %v", ErrUnavailable, loadErr)
		}
		expected = desc.Digest.String()
		if desc.MediaType.IsIndex() {
			idx, loadErr := desc.ImageIndex()
			if loadErr != nil {
				return Result{}, loadErr
			}
			err = remote.WriteIndex(dstRef, idx, opts...)
		} else {
			img, loadErr := desc.Image()
			if loadErr != nil {
				return Result{}, loadErr
			}
			err = remote.Write(dstRef, img, opts...)
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("crane publish: %w", err)
	}
	// A successful upload is not enough evidence to deploy: bind the target to
	// the exact uploaded digest. A concurrent tag writer must not change it silently.
	published, err := remote.Head(dstRef, opts...)
	if err != nil {
		return Result{}, fmt.Errorf("image uploaded but digest verification failed: %w", err)
	}
	if published.Digest.String() != expected {
		return Result{}, fmt.Errorf("image uploaded but target digest changed before verification")
	}
	logf(req.LogWriter, "Pushed image to %s@%s\n", req.Target, expected)
	return Result{Digest: expected}, nil
}

// logf writes a formatted line to w when w is non-nil. Callers use this
// to surface adapter progress to the executor's stage-log capture +
// WebSocket broadcaster without sprinkling nil-checks at every call
// site. Errors are swallowed: log streams are best-effort, never a
// reason to fail a push.
func logf(w io.Writer, format string, args ...interface{}) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, format, args...)
}

var _ Pusher = (*Crane)(nil)
