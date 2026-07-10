// Package render adapts Cooker's deploytarget.Target to Render
// (https://render.com) via their REST API at api.render.com/v1.
//
// Render uses its own concept of "Service"; Cooker's AppID maps to
// the Render service name. Each Deploy triggers a new build at the
// configured image.
package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/santapong/cooker/internal/deploy/deploytarget"
	"github.com/santapong/cooker/internal/model"
)

const baseURL = "https://api.render.com/v1"

// Target is the Render adapter.
type Target struct {
	Token   string // RENDER_API_KEY
	OwnerID string // optional: scope queries to one workspace
	HTTP    *http.Client
}

// New constructs a Render target.
func New(token, ownerID string) *Target {
	return &Target{
		Token:   token,
		OwnerID: ownerID,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (*Target) Kind() model.DeployTargetKind { return model.DeployTargetRender }

func (t *Target) requireConfig() error {
	if t == nil || t.Token == "" {
		return fmt.Errorf("%w: render: Token required (set via Helm secretKeyRef)", deploytarget.ErrUnavailable)
	}
	return nil
}

func (t *Target) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var br io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		br = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, br)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "cooker-deploytarget-render/0.1")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// AD-H3: cap response reads at 4 MiB so a pathological API
	// response can't exhaust the process heap (mirrors clientgo.go:160).
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("render: %s %s: %d %s", method, path, resp.StatusCode, string(data))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

type renderService struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// renderServiceDetail is the full Render service object returned by
// GET /services/<id>. ServiceDetails is a nested object; using a
// dot in a Go JSON tag (e.g. `json:"serviceDetails.url"`) is treated
// as a literal key, not a dot-path — hence the nested struct (R-2 fix).
type renderServiceDetail struct {
	Suspended      string `json:"suspended"`
	ServiceDetails struct {
		URL string `json:"url"`
	} `json:"serviceDetails"`
}

func (t *Target) findServiceID(ctx context.Context, name string) (string, error) {
	var resp []struct {
		Service renderService `json:"service"`
	}
	path := "/services?name=" + name
	if t.OwnerID != "" {
		path += "&ownerId=" + t.OwnerID
	}
	if _, err := t.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", err
	}
	for _, r := range resp {
		if r.Service.Name == name {
			return r.Service.ID, nil
		}
	}
	return "", nil
}

// Deploy triggers a new deploy on an existing Render service named
// spec.AppID. Render service creation is intentionally out-of-scope
// — operators wire the service once via the Render UI / Blueprint.
func (t *Target) Deploy(ctx context.Context, spec deploytarget.Spec) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	id, err := t.findServiceID(ctx, spec.AppID)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("render: service %q not found; create it via the Render dashboard or Blueprint first", spec.AppID)
	}
	body := map[string]any{
		"clearCache": "do_not_clear",
	}
	if spec.Image != "" {
		body["imageUrl"] = spec.Image
	}
	if _, err := t.do(ctx, http.MethodPost, "/services/"+id+"/deploys", body, nil); err != nil {
		return err
	}
	return nil
}

func (t *Target) Status(ctx context.Context, appID string) (deploytarget.Status, error) {
	if err := t.requireConfig(); err != nil {
		return deploytarget.Status{}, err
	}
	id, err := t.findServiceID(ctx, appID)
	if err != nil {
		return deploytarget.Status{}, err
	}
	if id == "" {
		return deploytarget.Status{}, fmt.Errorf("render: service %q not found", appID)
	}
	var resp struct {
		Service renderServiceDetail `json:"service"`
	}
	if _, err := t.do(ctx, http.MethodGet, "/services/"+id, nil, &resp); err != nil {
		return deploytarget.Status{}, err
	}
	return deploytarget.Status{
		Healthy: resp.Service.Suspended != "suspended",
		URL:     resp.Service.ServiceDetails.URL,
	}, nil
}

func (t *Target) Logs(_ context.Context, _ string, _ io.Writer) error {
	return fmt.Errorf("%w: render logs: streaming not yet wired", deploytarget.ErrUnavailable)
}

func (t *Target) Rollback(ctx context.Context, appID string) error {
	if err := t.requireConfig(); err != nil {
		return err
	}
	id, err := t.findServiceID(ctx, appID)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("render: service %q not found", appID)
	}
	// Render's "rollback" = redeploy a previous deploy ID. List
	// deploys, pick the second-most-recent succeeded one, redeploy.
	var deploys []struct {
		Deploy struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"deploy"`
	}
	if _, err := t.do(ctx, http.MethodGet, "/services/"+id+"/deploys?limit=20", nil, &deploys); err != nil {
		return err
	}
	var prev string
	found := 0
	for _, d := range deploys {
		if d.Deploy.Status == "live" {
			found++
			if found == 2 {
				prev = d.Deploy.ID
				break
			}
		}
	}
	if prev == "" {
		return fmt.Errorf("render: no previous live deploy to roll back to")
	}
	_, err = t.do(ctx, http.MethodPost, "/services/"+id+"/rollback", map[string]string{
		"deployId": prev,
	}, nil)
	return err
}

var _ deploytarget.Target = (*Target)(nil)
