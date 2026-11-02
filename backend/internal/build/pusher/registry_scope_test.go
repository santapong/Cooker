package pusher

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

func TestCraneNilAuthNeverUsesOwnedHostConfig(t *testing.T) {
	config := t.TempDir()
	t.Setenv("DOCKER_CONFIG", config)
	data := `{"auths":{"ghcr.io":{"auth":"` + base64.StdEncoding.EncodeToString([]byte("host-user:owned-host-secret-canary")) + `"}}}`
	if err := os.WriteFile(filepath.Join(config, "config.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	ref, _ := name.ParseReference("ghcr.io/owned/fixture:v1")
	a, err := authForRequest(Request{}).Resolve(ref.Context().Registry)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Authorization()
	if cfg.Username != "" || cfg.Password != "" {
		t.Fatal("nil auth consulted host Docker config")
	}
}

func TestDockerPushUsesScopedConfigAndCleansIt(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	bin := filepath.Join(dir, "docker-fixture")
	t.Setenv("COOKER_OWNED_CLI_RECORD", record)
	t.Setenv("DOCKER_CONFIG", filepath.Join(dir, "unrelated-host-config"))
	encoded := base64.StdEncoding.EncodeToString([]byte("ci:owned-push-canary"))
	script := `#!/bin/sh
set -eu
test -f "$DOCKER_CONFIG/config.json"
grep -q '` + encoded + `' "$DOCKER_CONFIG/config.json"
printf '%s\n' "$DOCKER_CONFIG" >> "$COOKER_OWNED_CLI_RECORD"
if [ "$1" = push ]; then printf 'v1: digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa size: 1\n'; fi
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	calls := 0
	resolver := func(_ context.Context, h string) (authn.Authenticator, error) {
		calls++
		if h != "ghcr.io" {
			t.Fatal("wrong host requested")
		}
		return authn.FromConfig(authn.AuthConfig{Username: "ci", Password: "owned-push-canary"}), nil
	}
	res, err := (&DockerSock{Bin: bin}).Push(context.Background(), Request{Source: "local:image", Target: "ghcr.io/owned/image:v1", RegistryAuth: resolver})
	if err != nil || res.Digest == "" || calls != 1 {
		t.Fatal("scoped push failed", err)
	}
	data, _ := os.ReadFile(record)
	paths := strings.Fields(string(data))
	if len(paths) != 2 || paths[0] != paths[1] {
		t.Fatal("tag/push config scope differs")
	}
	if _, err = os.Stat(paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("push auth file survived")
	}
}
