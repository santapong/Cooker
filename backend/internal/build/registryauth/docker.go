package registryauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Use a wire DTO, not authn.AuthConfig: its MarshalJSON recomputes auth from
// Username/Password and would replace an explicitly encoded token with ":".
type dockerAuth struct {
	Auth          string `json:"auth,omitempty"`
	IdentityToken string `json:"identitytoken,omitempty"`
	RegistryToken string `json:"registrytoken,omitempty"`
}

// WithDockerConfig writes a private, short-lived auth file for only the supplied
// accepted hosts. No helpers/currentContext/host auth file is copied. Callers
// must pass DockerEnvironment(path) to every related CLI command, including
// inspect/tag commands. Credentials never become arguments or process env.
func WithDockerConfig(ctx context.Context, resolver Resolver, hosts []string, use func(string) error) error {
	if len(hosts) > 32 || use == nil {
		return ErrUnavailable
	}
	auths := map[string]dockerAuth{}
	for _, raw := range hosts {
		_, host, err := Endpoint(raw)
		if err != nil {
			return ErrUnavailable
		}
		if _, exists := auths[host]; exists {
			continue
		}
		a, err := Resolve(ctx, resolver, host)
		if err != nil {
			return ErrUnavailable
		}
		cfg, err := a.Authorization()
		if err != nil || cfg == nil {
			return ErrUnavailable
		}
		// Keep only explicit auth fields. Never propagate a credentials helper.
		clean := dockerAuth{IdentityToken: cfg.IdentityToken, RegistryToken: cfg.RegistryToken}
		if cfg.Username != "" || cfg.Password != "" {
			clean.Auth = base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))
		} else {
			clean.Auth = cfg.Auth
		}
		if clean.Auth != "" || clean.IdentityToken != "" || clean.RegistryToken != "" {
			auths[host] = clean
		}
	}
	// Docker's LoadDefaultConfigFile auto-detects OS helpers when ContainsAuth
	// is false. An explicit empty marker prevents that discovery, even for an
	// entirely anonymous plan. It grants no credentials to any host.
	if len(auths) == 0 {
		auths["cooker-anonymous.invalid"] = dockerAuth{}
	}
	data, err := json.Marshal(struct {
		Auths map[string]dockerAuth `json:"auths"`
	}{auths})
	if err != nil || ctx.Err() != nil {
		return ErrUnavailable
	}
	dir, err := os.MkdirTemp("", "cooker-registry-")
	if err != nil {
		return ErrUnavailable
	}
	defer os.RemoveAll(dir)
	if os.Chmod(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "config.json"), data, 0600) != nil {
		return ErrUnavailable
	}
	return use(dir)
}

// DockerEnvironment overrides inherited configuration authority without reading
// it. This hook also supports the separately wired runtime pull paths.
func DockerEnvironment(dir string) []string {
	result := make([]string, 0, len(os.Environ())+1)
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if key == "DOCKER_CONFIG" || key == "DOCKER_AUTH_CONFIG" || key == "REGISTRY_AUTH_FILE" {
			continue
		}
		result = append(result, v)
	}
	return append(result, "DOCKER_CONFIG="+dir)
}
