package pusher

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// Crane pushes images using go-containerregistry's crane API. The
// source ref must already be available either in the local daemon
// (via crane.Pull from "daemon:") or as a remote ref. Target is a
// remote reference (registry/repo:tag).
type Crane struct {
	UserAgent string
}

func NewCrane() *Crane { return &Crane{UserAgent: "cooker/0.1"} }

// authForRequest builds an authn.Keychain from the request's Auth
// callback. Falls back to authn.DefaultKeychain (~/.docker/config.json
// + cred helpers) when Auth is nil.
func authForRequest(req Request) authn.Keychain {
	if req.Auth == nil {
		return authn.DefaultKeychain
	}
	return authn.NewKeychainFromHelper(authHelper(req.Auth))
}

type authHelper func(host string) (user, pass string, ok bool)

func (a authHelper) Get(host string) (string, string, error) {
	user, pass, ok := a(host)
	if !ok {
		return "", "", fmt.Errorf("crane: no credentials for %q", host)
	}
	return user, pass, nil
}

func (c *Crane) Push(ctx context.Context, req Request) (Result, error) {
	srcRef, err := name.ParseReference(req.Source)
	if err != nil {
		return Result{}, fmt.Errorf("%w: parse source: %v", ErrUnavailable, err)
	}
	dstRef, err := name.ParseReference(req.Target)
	if err != nil {
		return Result{}, fmt.Errorf("%w: parse target: %v", ErrUnavailable, err)
	}
	keychain := authForRequest(req)
	ua := c.UserAgent
	if ua == "" {
		ua = "cooker"
	}
	img, err := remote.Image(srcRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(keychain),
		remote.WithUserAgent(ua),
	)
	if err != nil {
		return Result{}, fmt.Errorf("%w: load source: %v", ErrUnavailable, err)
	}
	if err := remote.Write(dstRef, img,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(keychain),
		remote.WithUserAgent(ua),
	); err != nil {
		// Surface registry transport errors verbatim — they carry the
		// status code and registry-side message that operators need.
		var terr *transport.Error
		if errors.As(err, &terr) {
			return Result{}, fmt.Errorf("crane push: %w", err)
		}
		return Result{}, fmt.Errorf("%w: push: %v", ErrUnavailable, err)
	}
	digest, err := crane.Digest(req.Target,
		crane.WithContext(ctx),
		crane.WithAuthFromKeychain(keychain),
		crane.WithUserAgent(ua),
	)
	if err != nil {
		return Result{}, nil // digest fetch is best-effort
	}
	return Result{Digest: digest}, nil
}

var _ Pusher = (*Crane)(nil)
