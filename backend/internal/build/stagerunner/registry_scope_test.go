package stagerunner

import (
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
	"k8s.io/client-go/kubernetes/fake"
)

func TestDockerStageAcceptedConfigIsPrivateScopedAndRemoved(t *testing.T) {
	for _, selected := range []bool{true, false} {
		for _, exit := range []string{"0", "7"} {
			t.Run(strings.Join([]string{map[bool]string{true: "selected", false: "anonymous"}[selected], exit}, "-"), func(t *testing.T) {
				root := t.TempDir()
				capture := filepath.Join(root, "capture")
				// This owned fake only inspects the ephemeral config and never invokes Docker.
				script := "#!/bin/sh\nset -eu\n[ -z \"${DOCKER_AUTH_CONFIG+x}\" ]\n[ -z \"${REGISTRY_AUTH_FILE+x}\" ]\n[ \"$(stat -c %a \"$DOCKER_CONFIG\")\" = 700 ]\n[ \"$(stat -c %a \"$DOCKER_CONFIG/config.json\")\" = 600 ]\nprintf '%s' \"$DOCKER_CONFIG\" > '" + capture + ".path'\ncat \"$DOCKER_CONFIG/config.json\" > '" + capture + ".json'\nprintf '%s\\n' \"$@\" > '" + capture + ".argv'\nexit " + exit + "\n"
				bin := filepath.Join(root, "owned-docker")
				if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("DOCKER_CONFIG", filepath.Join(root, "must-not-read-owner-config"))
				t.Setenv("DOCKER_AUTH_CONFIG", "owned-ambient-canary")
				t.Setenv("REGISTRY_AUTH_FILE", "must-not-read-owner-auth")
				calls := 0
				resolve := registryauth.Resolver(func(_ context.Context, host string) (authn.Authenticator, error) {
					calls++
					if host != "ghcr.io" {
						t.Fatal("resolver queried unaccepted host")
					}
					return authn.FromConfig(authn.AuthConfig{Username: "owned-ci", Password: "owned-stage-registry-canary"}), nil
				})
				var hosts []string
				if selected {
					hosts = []string{"ghcr.io"}
				}
				res, err := (&DockerRun{Bin: bin}).Run(context.Background(), Request{Image: "ghcr.io/owned/image:exact", RegistryAuth: resolve, RegistryHosts: hosts})
				if err != nil || res.ExitCode != map[string]int{"0": 0, "7": 7}[exit] {
					t.Fatalf("runner result: %v", err)
				}
				path, err := os.ReadFile(capture + ".path")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = os.Stat(string(path)); !os.IsNotExist(err) {
					t.Fatal("private config retained after stage")
				}
				data, err := os.ReadFile(capture + ".json")
				if err != nil {
					t.Fatal(err)
				}
				var config struct {
					Auths map[string]map[string]string `json:"auths"`
				}
				if json.Unmarshal(data, &config) != nil {
					t.Fatal("invalid private config")
				}
				if len(config.Auths) != 1 || strings.Contains(string(data), "credsStore") || strings.Contains(string(data), "currentContext") || strings.Contains(string(data), "owned-ambient-canary") {
					t.Fatal("host configuration copied")
				}
				if selected {
					expected := base64.StdEncoding.EncodeToString([]byte("owned-ci:owned-stage-registry-canary"))
					if config.Auths["ghcr.io"]["auth"] != expected || calls != 1 {
						t.Fatal("accepted credentials absent")
					}
				} else if _, ok := config.Auths["cooker-anonymous.invalid"]; !ok || calls != 0 {
					t.Fatal("empty plan invoked resolver/helper discovery")
				}
				argv, _ := os.ReadFile(capture + ".argv")
				if strings.Contains(string(argv), "owned-stage-registry-canary") || strings.Contains(string(argv), "owned-ci") {
					t.Fatal("credentials entered argv")
				}
			})
		}
	}
}

func TestDockerStageRegistryFailureAndCancellationNeverStartsCLI(t *testing.T) {
	for _, name := range []string{"resolver-error", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "started")
			bin := filepath.Join(root, "owned-docker")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "cancelled" {
				cancel()
			}
			resolve := registryauth.Resolver(func(context.Context, string) (authn.Authenticator, error) {
				return nil, errors.New("owned-provider-secret-canary")
			})
			_, err := (&DockerRun{Bin: bin}).Run(ctx, Request{Image: "ghcr.io/owned/image:exact", RegistryAuth: resolve, RegistryHosts: []string{"ghcr.io"}})
			if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "owned-provider-secret-canary") {
				t.Fatal("unsafe resolver failure")
			}
			if _, err = os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("failed credential transport started CLI")
			}
		})
	}
}

func TestKubeStageAcceptedAuthorityFailsBeforeAnyClusterAction(t *testing.T) {
	for _, selected := range []bool{true, false} {
		t.Run(map[bool]string{true: "selected", false: "anonymous"}[selected], func(t *testing.T) {
			client := fake.NewSimpleClientset()
			runner, err := NewKube(KubeConfig{Client: client})
			if err != nil {
				t.Fatal(err)
			}
			reads := 0
			resolve := registryauth.Resolver(func(context.Context, string) (authn.Authenticator, error) { reads++; return authn.Anonymous, nil })
			var hosts []string
			if selected {
				hosts = []string{"ghcr.io"}
			}
			if _, err = runner.Run(context.Background(), Request{Image: "ghcr.io/owned/image", RegistryAuth: resolve, RegistryHosts: hosts}); !errors.Is(err, ErrUnavailable) || reads != 0 || len(client.Actions()) != 0 {
				t.Fatal("unsupported accepted transport read credentials or created Job")
			}
		})
	}
}
