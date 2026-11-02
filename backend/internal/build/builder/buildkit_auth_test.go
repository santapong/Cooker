package builder

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session/auth"
)

func TestBuildKitExportsOCIArtifactNotPrivateDaemonImage(t *testing.T) {
	opt, err := solveOptions(Request{ContextDir: t.TempDir(), Tags: []string{"registry.example/app:v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(opt.Exports) != 1 || opt.Exports[0].Type != client.ExporterOCI || opt.Exports[0].Attrs["tar"] != "false" {
		t.Fatalf("unexpected handoff: %+v", opt.Exports)
	}
	if _, ok := opt.Exports[0].Attrs["push"]; ok {
		t.Fatal("builder must not publish app image before the Push stage")
	}
}
func TestBuildKitRegistrySessionResolvesExactHostAndFailsClosed(t *testing.T) {
	var host string
	s := &registrySession{resolve: func(_ context.Context, h string) (authn.Authenticator, error) {
		host = h
		return authn.FromConfig(authn.AuthConfig{Username: "fixture", Password: "fixture-value"}), nil
	}}
	res, err := s.Credentials(context.Background(), &auth.CredentialsRequest{Host: "registry.example"})
	if err != nil || host != "registry.example" || res.Secret != "fixture-value" || res.Username != "fixture" {
		t.Fatalf("unexpected credentials: host=%s error=%v", host, err)
	}
	s.resolve = func(context.Context, string) (authn.Authenticator, error) {
		return nil, fmt.Errorf("sensitive backend detail")
	}
	_, err = s.Credentials(context.Background(), &auth.CredentialsRequest{Host: "registry.example"})
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("credential failure was swallowed or exposed: %v", err)
	}
}
