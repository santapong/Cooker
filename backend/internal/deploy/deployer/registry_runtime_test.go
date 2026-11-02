package deployer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/santapong/cooker/internal/build/registryauth"
)

func ownedRegistryDockerCLI(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "docker-owned")
	calls := filepath.Join(root, "calls")
	config := filepath.Join(root, "captured-config.json")
	t.Setenv("COOKER_TEST_CALLS", calls)
	t.Setenv("COOKER_TEST_CONFIG", config)
	t.Setenv("DOCKER_CONFIG", filepath.Join(root, "host-config"))
	t.Setenv("DOCKER_AUTH_CONFIG", "host-canary")
	t.Setenv("REGISTRY_AUTH_FILE", "host-canary")
	script := `#!/bin/sh
printf '%s\n' "$DOCKER_CONFIG" >> "$COOKER_TEST_CALLS"
printf '%s\n' "$@" >> "$COOKER_TEST_CALLS"
[ -z "$DOCKER_AUTH_CONFIG" ] && [ -z "$REGISTRY_AUTH_FILE" ] || exit 91
[ -f "$DOCKER_CONFIG/config.json" ] || exit 92
cp "$DOCKER_CONFIG/config.json" "$COOKER_TEST_CONFIG"
if [ "$1" = image ] && [ "$COOKER_TEST_COLD" = yes ]; then exit 1; fi
if [ "$1" = pull ] && [ "$COOKER_TEST_FAIL_PULL" = yes ]; then exit 42; fi
for arg in "$@"; do
 if [ "$arg" = config ]; then printf '%s\n' registry.invalid/owned:v1; fi
done
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return bin, calls, config
}

func TestRuntimeRegistryConfigIsScopedAndRemoved(t *testing.T) {
	for _, kind := range []Kind{KindDockerRun, KindCompose} {
		t.Run(string(kind), func(t *testing.T) {
			bin, calls, config := ownedRegistryDockerCLI(t)
			var output bytes.Buffer
			reads := 0
			resolver := func(_ context.Context, host string) (authn.Authenticator, error) {
				reads++
				if host != "registry.invalid" {
					t.Fatal("unexpected registry host")
				}
				return authn.FromConfig(authn.AuthConfig{Username: "owned", Password: "synthetic-registry-canary"}), nil
			}
			req := Request{Kind: kind, Name: "owned-runtime", Image: "registry.invalid/owned:v1", ComposeFile: "/owned/reviewed.yaml", ComposeService: "web", RegistryAuth: resolver, RegistryHosts: []string{"registry.invalid"}, LogWriter: &output}
			var err error
			if kind == KindDockerRun {
				_, err = (&DockerRun{Bin: bin}).Deploy(context.Background(), req)
			} else {
				_, err = (&Compose{Bin: bin}).Deploy(context.Background(), req)
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			var c struct {
				Auths map[string]struct {
					Auth string `json:"auth"`
				} `json:"auths"`
			}
			if json.Unmarshal(raw, &c) != nil {
				t.Fatal("invalid private config")
			}
			if len(c.Auths) != 1 || c.Auths["registry.invalid"].Auth != base64.StdEncoding.EncodeToString([]byte("owned:synthetic-registry-canary")) || reads != 1 {
				t.Fatal("unscoped registry config")
			}
			recorded, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(recorded)), "\n")
			if len(lines) == 0 || strings.Contains(string(recorded), "synthetic-registry-canary") || strings.Contains(output.String(), "synthetic-registry-canary") {
				t.Fatal("credential entered arguments/output")
			}
			if _, err := os.Stat(lines[0]); !os.IsNotExist(err) {
				t.Fatal("private config retained after deployment")
			}
			for _, line := range lines {
				if strings.Contains(line, "cooker-registry-") && line != lines[0] {
					t.Fatal("commands used different registry authority")
				}
			}
		})
	}
}

func TestRuntimeRegistryFailurePrecedesWorkloadMutation(t *testing.T) {
	for _, kind := range []Kind{KindDockerRun, KindCompose} {
		for _, failure := range []string{"authority", "pull"} {
			t.Run(string(kind)+"/"+failure, func(t *testing.T) {
				bin, calls, _ := ownedRegistryDockerCLI(t)
				t.Setenv("COOKER_TEST_COLD", "yes")
				t.Setenv("COOKER_TEST_FAIL_PULL", "yes")
				resolver := func(context.Context, string) (authn.Authenticator, error) {
					if failure == "authority" {
						return nil, errors.New("owned denial")
					}
					return authn.Anonymous, nil
				}
				req := Request{Kind: kind, Name: "owned-runtime", Image: "registry.invalid/owned:v1", ComposeFile: "/owned/reviewed.yaml", ComposeService: "web", RegistryAuth: resolver, RegistryHosts: []string{"registry.invalid"}}
				var err error
				if kind == KindDockerRun {
					_, err = (&DockerRun{Bin: bin}).Deploy(context.Background(), req)
				} else {
					_, err = (&Compose{Bin: bin}).Deploy(context.Background(), req)
				}
				if err == nil {
					t.Fatal("denied runtime proceeded")
				}
				recorded, _ := os.ReadFile(calls)
				if strings.Contains(string(recorded), "\nrm\n") || strings.Contains(string(recorded), "\nrun\n") || strings.Contains(string(recorded), "\nup\n") {
					t.Fatal("workload mutation preceded image/authority acceptance")
				}
			})
		}
	}
}

func TestRuntimeAnonymousConfigNeverUsesHostCredentialStore(t *testing.T) {
	bin, _, config := ownedRegistryDockerCLI(t)
	resolver := func(context.Context, string) (authn.Authenticator, error) {
		t.Fatal("anonymous plan queried credential")
		return nil, registryauth.ErrUnavailable
	}
	if _, err := (&DockerRun{Bin: bin}).Deploy(context.Background(), Request{Kind: KindDockerRun, Name: "owned", Image: "public:v1", RegistryAuth: resolver}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(config)
	if err != nil || !bytes.Contains(data, []byte("cooker-anonymous.invalid")) || bytes.Contains(data, []byte("credsStore")) {
		t.Fatal("anonymous isolation missing")
	}
}
