package registryauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/google/go-containerregistry/pkg/authn"
)

func TestRegistryEndpointBoundsAndAliases(t *testing.T) {
	for _, host := range []string{"docker.io", "registry-1.docker.io", "index.docker.io"} {
		_, h, err := Endpoint(host)
		if err != nil || h != "index.docker.io" {
			t.Fatal("Docker Hub alias changed")
		}
	}
	for _, raw := range []string{"https://user:SECRET@registry.example", "https://registry.example?token=SECRET", "https://registry.example#SECRET", "file:///etc/credential", "bad\nvalue", "", strings.Repeat("a", 513)} {
		if _, _, err := Endpoint(raw); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("unsafe endpoint accepted or echoed")
		}
	}
}

func TestDockerConfigIsPrivateScopedAndRemoved(t *testing.T) {
	const canary = "owned-credential-canary"
	ownedHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(ownedHome, "config.json"), []byte(`{"auths":{"other.example":{"auth":"host-secret"}},"credsStore":"unsafe-helper"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", ownedHome)
	t.Setenv("DOCKER_AUTH_CONFIG", "host-config-canary")
	var path string
	calls := 0
	failure := errors.New("fixture callback failure")
	resolver := func(_ context.Context, host string) (authn.Authenticator, error) {
		calls++
		if host != "ghcr.io" {
			t.Fatal("unselected auth host requested")
		}
		return authn.FromConfig(authn.AuthConfig{Username: "ci", Password: canary}), nil
	}
	err := WithDockerConfig(context.Background(), resolver, []string{"ghcr.io"}, func(dir string) error {
		path = dir
		d, err := os.Stat(dir)
		if err != nil || d.Mode().Perm() != 0700 {
			t.Fatal("auth dir permissions")
		}
		p := filepath.Join(dir, "config.json")
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("auth file permissions")
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Auths map[string]authn.AuthConfig `json:"auths"`
		}
		if json.Unmarshal(data, &config) != nil || len(config.Auths) != 1 || config.Auths["ghcr.io"].Auth != base64.StdEncoding.EncodeToString([]byte("ci:"+canary)) || strings.Contains(string(data), "unsafe-helper") {
			t.Fatal("auth scope leaked host config")
		}
		for _, v := range DockerEnvironment(dir) {
			if strings.Contains(v, canary) || strings.HasPrefix(v, "DOCKER_AUTH_CONFIG=") || v == "DOCKER_CONFIG="+ownedHome {
				t.Fatal("credential or host config entered process environment")
			}
		}
		return failure
	})
	if !errors.Is(err, failure) || calls != 1 {
		t.Fatal("callback or resolver contract changed")
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("auth file survived callback failure")
	}
	if err = WithDockerConfig(context.Background(), nil, []string{"ghcr.io"}, func(dir string) error {
		data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
		if string(data) != `{"auths":{"cooker-anonymous.invalid":{}}}` {
			t.Fatal("nil auth borrowed host credentials")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAnonymousDockerConfigDoesNotDiscoverOSHelper(t *testing.T) {
	owned := t.TempDir()
	marker := filepath.Join(owned, "helper-called")
	t.Setenv("COOKER_OWNED_HELPER_MARKER", marker)
	for _, name := range []string{"docker-credential-pass", "docker-credential-secretservice"} {
		script := "#!/bin/sh\nprintf called > \"$COOKER_OWNED_HELPER_MARKER\"\nprintf '{\"Username\":\"unrelated\",\"Secret\":\"owned-helper-canary\"}'\n"
		if err := os.WriteFile(filepath.Join(owned, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", owned+string(os.PathListSeparator)+os.Getenv("PATH"))
	previous := dockerconfig.Dir()
	defer dockerconfig.SetDir(previous)
	if err := WithDockerConfig(context.Background(), nil, nil, func(dir string) error {
		dockerconfig.SetDir(dir)
		cfg := dockerconfig.LoadDefaultConfigFile(io.Discard)
		if !cfg.ContainsAuth() || cfg.CredentialsStore != "" {
			t.Fatal("anonymous config discovered a global helper")
		}
		auth, err := cfg.GetAuthConfig("ghcr.io")
		if err != nil || auth.Username != "" || auth.Password != "" {
			t.Fatal("anonymous config borrowed helper credentials")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned helper was invoked")
	}
}

func TestDockerConfigFailureDoesNotStartCommand(t *testing.T) {
	called := false
	err := WithDockerConfig(context.Background(), func(context.Context, string) (authn.Authenticator, error) {
		return nil, errors.New("provider-secret-canary")
	}, []string{"ghcr.io"}, func(string) error { called = true; return nil })
	if !errors.Is(err, ErrUnavailable) || called || strings.Contains(err.Error(), "canary") {
		t.Fatal("credential failure started command or leaked error")
	}
}
