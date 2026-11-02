package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/moby/buildkit/session/auth"
	"github.com/santapong/cooker/internal/build/registryauth"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDockerBuildHasNoHostConfigFallback(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	bin := filepath.Join(dir, "docker-fixture")
	t.Setenv("COOKER_OWNED_CLI_RECORD", record)
	t.Setenv("DOCKER_CONFIG", filepath.Join(dir, "unrelated-host-config"))
	script := `#!/bin/sh
set -eu
test -f "$DOCKER_CONFIG/config.json"
test "$(cat "$DOCKER_CONFIG/config.json")" = '{"auths":{"cooker-anonymous.invalid":{}}}'
printf '%s\n' "$DOCKER_CONFIG" >> "$COOKER_OWNED_CLI_RECORD"
if [ "$1" = image ]; then printf 'sha256:fixture\n'; fi
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := (&DockerSock{Bin: bin}).Build(context.Background(), Request{ContextDir: dir, Tags: []string{"example/app:v1"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(record)
	paths := strings.Fields(string(data))
	if len(paths) != 2 || paths[0] != paths[1] {
		t.Fatal("build and inspect did not share isolated config")
	}
	if _, err = os.Stat(paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("build auth config survived")
	}
}

func TestDockerAuthFileCannotEnterBuildContext(t *testing.T) {
	if _, err := (&DockerSock{Bin: "sh"}).Build(context.Background(), Request{ContextDir: "/", Tags: []string{"example/image:v1"}}); !errors.Is(err, registryauth.ErrUnavailable) {
		t.Fatal("auth file was not excluded from broad build context")
	}
}

func TestKubeBuildersRejectAcceptedRegistryTransportBeforeMutation(t *testing.T) {
	for _, kind := range []string{"kaniko", "buildah"} {
		t.Run(kind, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			var b Builder
			var err error
			if kind == "kaniko" {
				b, err = NewKaniko(KanikoConfig{Client: client})
			} else {
				b, err = NewBuildah(BuildahConfig{Client: client})
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, resolver := range []registryauth.Resolver{func(context.Context, string) (authn.Authenticator, error) { return authn.Anonymous, nil }, func(context.Context, string) (authn.Authenticator, error) {
				return nil, errors.New("owned-secret-canary")
			}} {
				_, err = b.Build(context.Background(), Request{ContextDir: "/context", Tags: []string{"registry/image:v1"}, RegistryAuth: resolver})
				if !errors.Is(err, ErrUnavailable) || len(client.Actions()) != 0 || strings.Contains(err.Error(), "canary") {
					t.Fatal("unsupported transport mutated Kubernetes or lost unavailable status")
				}
			}
		})
	}
}

func TestBuildKitNilResolverIsAnonymous(t *testing.T) {
	res, err := (&registrySession{}).Credentials(context.Background(), &auth.CredentialsRequest{Host: "ghcr.io"})
	if err != nil || res.Username != "" || res.Secret != "" {
		t.Fatal("nil session resolver acquired authority")
	}
}
