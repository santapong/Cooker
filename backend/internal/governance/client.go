// Package governance plugs Cooker into the Grovernance Platform's deploy gate.
// Cooker calls Grovernance's POST /authorize before each deploy; a DENY aborts
// the deploy with the reason returned by Grovernance.
//
// The client encodes two operational policies:
//
//   - Bootstrap exemption — services in BootstrapServices bypass the gate.
//     Required so Grovernance itself can be deployed through Cooker without a
//     circular dependency.
//   - Fail-open / fail-closed — when Grovernance is unreachable, deploys to
//     envs in FailOpenEnvs are permitted (logged); deploys to other envs are
//     denied. Production is fail-closed by default.
//
// When BaseURL is empty, the client returns Allow without an HTTP call. This
// makes the integration opt-in: an operator that has not yet deployed
// Grovernance keeps Cooker's existing behaviour.
package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Decision mirrors the response shape of Grovernance's POST /authorize.
type Decision struct {
	Decision string `json:"decision"` // "allow" | "deny"
	Reason   string `json:"reason"`
	PolicyID string `json:"policy_id"`
	AuditID  string `json:"audit_id"`
}

// Allowed reports whether the decision permits the action.
func (d Decision) Allowed() bool { return d.Decision == "allow" }

// Client is the HTTP client for Grovernance's authorize endpoint.
type Client struct {
	BaseURL           string
	BootstrapServices map[string]struct{}
	FailOpenEnvs      map[string]struct{}
	HTTPClient        *http.Client
}

// New builds a Client from the parsed config values. BaseURL == "" disables
// the integration (Authorize returns Allow). FailOpenEnvs / BootstrapServices
// are normalised to lower-case sets for case-insensitive matching.
func New(baseURL string, bootstrap, failOpen []string) *Client {
	return &Client{
		BaseURL:           strings.TrimRight(baseURL, "/"),
		BootstrapServices: toSet(bootstrap),
		FailOpenEnvs:      toSet(failOpen),
		HTTPClient:        &http.Client{Timeout: 2 * time.Second},
	}
}

// Enabled reports whether the client will make HTTP calls. A disabled client
// short-circuits every Authorize call with Decision{Allow}.
func (c *Client) Enabled() bool { return c != nil && c.BaseURL != "" }

// Authorize calls POST /authorize with the supplied actor token, service, env,
// and request ID. The returned Decision indicates the verdict; the returned
// error is non-nil only when the call cannot be completed AND the env is not
// fail-open. Callers should treat a non-nil error as fail-closed.
func (c *Client) Authorize(ctx context.Context, token, service, env, requestID string) (Decision, error) {
	if !c.Enabled() {
		return Decision{Decision: "allow", Reason: "governance disabled", PolicyID: "cooker.governance.disabled"}, nil
	}
	if _, ok := c.BootstrapServices[strings.ToLower(service)]; ok {
		return Decision{Decision: "allow", Reason: "bootstrap-exempt service", PolicyID: "cooker.governance.bootstrap"}, nil
	}

	payload := authorizeRequest{
		Action: "deploy",
	}
	payload.Actor.Token = token
	payload.Resource.Service = service
	payload.Resource.Env = env
	payload.Context.RequestID = requestID

	body, err := json.Marshal(payload)
	if err != nil {
		return Decision{}, fmt.Errorf("governance: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/authorize", bytes.NewReader(body))
	if err != nil {
		return Decision{}, fmt.Errorf("governance: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return c.transportFallback(env, fmt.Errorf("governance: post: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.transportFallback(env, fmt.Errorf("governance: unexpected status %d", resp.StatusCode))
	}

	var decision Decision
	if err := json.NewDecoder(resp.Body).Decode(&decision); err != nil {
		return Decision{}, fmt.Errorf("governance: decode response: %w", err)
	}
	if decision.Decision != "allow" && decision.Decision != "deny" {
		return Decision{}, fmt.Errorf("governance: invalid decision %q", decision.Decision)
	}
	return decision, nil
}

// transportFallback returns Allow when the env is configured as fail-open,
// otherwise returns the transport error so the caller can fail closed.
func (c *Client) transportFallback(env string, cause error) (Decision, error) {
	if _, ok := c.FailOpenEnvs[strings.ToLower(env)]; ok {
		return Decision{
			Decision: "allow",
			Reason:   "governance unreachable; env is fail-open: " + cause.Error(),
			PolicyID: "cooker.governance.fail_open",
		}, nil
	}
	return Decision{}, errors.Join(ErrGovernanceUnreachable, cause)
}

// ErrGovernanceUnreachable is returned (joined with the transport error) when
// the governance endpoint cannot be reached AND the env is fail-closed. The
// caller should respond 503 to its own client.
var ErrGovernanceUnreachable = errors.New("governance unreachable (fail-closed)")

type authorizeRequest struct {
	Actor struct {
		Token string `json:"token"`
	} `json:"actor"`
	Action   string `json:"action"`
	Resource struct {
		Service string `json:"service"`
		Env     string `json:"env"`
	} `json:"resource"`
	Context struct {
		RequestID string `json:"request_id"`
	} `json:"context"`
}

func toSet(s []string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	return out
}
