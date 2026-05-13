// Package flyio adapts Cooker's deploytarget.Target to fly.io's
// Machines REST API (https://api.machines.dev). The adapter assumes
// one fly App per Cooker App; spec.AppID is used as the fly app slug.
//
// Auth: FLY_API_TOKEN is supplied via Target.Token (read from env in
// the constructor below). The user-agent identifies Cooker so fly's
// support can correlate requests.
package flyio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/deploytarget"
	"github.com/santapong/cooker/internal/model"
)

// Target is the fly.io Machines API adapter.
type Target struct {
	Token  string
	Region string
	HTTP   *http.Client
}

// New constructs a fly.io target.
func New(token, region string) *Target {
	return &Target{
		Token:  token,
		Region: region,
		HTTP:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (*Target) Kind() model.DeployTargetKind { return model.DeployTargetFly }

const baseURL = "https://api.machines.dev/v1"

func (t *Target) requireConfig() error {
	if t == nil || t.Token == "" {
		return fmt.Errorf("%w: fly: Token required (set via Helm secretKeyRef)", deploytarget.ErrUnavailable)
	}
	return nil
}

func (t *Target) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var br io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		br = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, br)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "cooker-deploytarget-fly/0.1")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// Deploy creates / updates a Machine for spec.AppID. fly.io's
// machines API treats a Machine as the deployable unit; here we
// keep one Machine per app, named after the AppID.
func (t *Target) Deploy(ctx context.Context, spec deploytarget.Spec) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	region := t.Region
	if region == "" {
		region = "iad"
	}
	envMap := map[string]string{}
	for k, v := range spec.Env {
		envMap[k] = v
	}
	services := []map[string]any{}
	for _, p := range spec.Ports {
		services = append(services, map[string]any{
			"protocol":      "tcp",
			"internal_port": p,
			"ports": []map[string]any{
				{"port": 80, "handlers": []string{"http"}},
				{"port": 443, "handlers": []string{"tls", "http"}},
			},
		})
	}
	machineCfg := map[string]any{
		"image":    spec.Image,
		"env":      envMap,
		"services": services,
	}
	body := map[string]any{
		"name":   spec.AppID,
		"region": region,
		"config": machineCfg,
	}
	_, code, err := t.do(ctx, http.MethodPost, "/apps/"+spec.AppID+"/machines", body)
	if err != nil {
		return fmt.Errorf("fly: create machine: %w", err)
	}
	if code >= 200 && code < 300 {
		return nil
	}
	if code == http.StatusNotFound {
		// App doesn't exist; create it then retry.
		if _, _, err := t.do(ctx, http.MethodPost, "/apps", map[string]any{
			"app_name": spec.AppID,
		}); err != nil {
			return fmt.Errorf("fly: create app: %w", err)
		}
		_, code, err = t.do(ctx, http.MethodPost, "/apps/"+spec.AppID+"/machines", body)
		if err != nil {
			return fmt.Errorf("fly: create machine after app create: %w", err)
		}
	}
	if code >= 400 {
		return fmt.Errorf("fly: deploy failed with status %d", code)
	}
	return nil
}

func (t *Target) Status(ctx context.Context, appID string) (deploytarget.Status, error) {
	if err := t.requireConfig(); err != nil {
		return deploytarget.Status{}, err
	}
	data, code, err := t.do(ctx, http.MethodGet, "/apps/"+appID+"/machines", nil)
	if err != nil {
		return deploytarget.Status{}, err
	}
	if code != 200 {
		return deploytarget.Status{}, fmt.Errorf("fly: status code %d", code)
	}
	var machines []struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &machines); err != nil {
		return deploytarget.Status{}, err
	}
	healthy := 0
	for _, m := range machines {
		if strings.EqualFold(m.State, "started") {
			healthy++
		}
	}
	return deploytarget.Status{
		Healthy:  healthy > 0,
		Replicas: healthy,
		URL:      fmt.Sprintf("https://%s.fly.dev", appID),
	}, nil
}

func (t *Target) Logs(_ context.Context, _ string, _ io.Writer) error {
	// fly logs come from the GraphQL/agent path; defer until needed.
	return fmt.Errorf("%w: fly logs: streaming not yet wired", deploytarget.ErrUnavailable)
}

func (t *Target) Rollback(ctx context.Context, appID string) error {
	// fly's "rollback" depends on app release history (GraphQL).
	// Without that wiring we can at least restart machines so a
	// crash-looping deploy gets healed.
	if err := t.requireConfig(); err != nil {
		return err
	}
	_, code, err := t.do(ctx, http.MethodPost, "/apps/"+appID+"/machines/restart", nil)
	if err != nil {
		return fmt.Errorf("fly: restart: %w", err)
	}
	if code >= 400 {
		return fmt.Errorf("fly: restart status %d", code)
	}
	return nil
}

var _ deploytarget.Target = (*Target)(nil)
