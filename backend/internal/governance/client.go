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
//
// Enforced reports whether the gate is currently configured to *act* on a
// deny for this (service, env). Grovernance v1.1+ returns enforced=false
// when its catalog row for the service has enforcement_mode=advisory for
// the env in question — Cooker logs the deny and lets the deploy proceed.
// EnforcementMode carries the same signal as a human-readable label.
type Decision struct {
	Decision        string `json:"decision"` // "allow" | "deny"
	Reason          string `json:"reason"`
	PolicyID        string `json:"policy_id"`
	AuditID         string `json:"audit_id"`
	Enforced        bool   `json:"enforced"`
	EnforcementMode string `json:"enforcement_mode"`
}

// Allowed reports whether the decision permits the action.
func (d Decision) Allowed() bool { return d.Decision == "allow" }

// Client is the HTTP client for Grovernance's authorize endpoint.
type Client struct {
	BaseURL           string
	BootstrapServices map[string]struct{}
	FailOpenEnvs      map[string]struct{}
	HTTPClient        *http.Client
	// CallerToken authenticates Cooker to Grovernance on every outbound
	// /authorize call. Grovernance v1.1+ requires a KeepSave service-account
	// token with the governance.authorize scope when its
	// GOVERNANCE_REQUIRE_CALLER_AUTH is true (the production default).
	// Empty token == no Authorization header — accepted by Grovernance only
	// when its caller-auth is disabled.
	CallerToken string
	// DelegateToken authenticates Cooker for the delegated-actor path used
	// by the pipeline-deploy executor (AuthorizeOnBehalf). It must hold the
	// governance.authorize_on_behalf scope on the gate side. Distinct from
	// CallerToken so the two scopes can be split across two KeepSave
	// service accounts if the operator wants the tighter blast radius.
	// Empty token disables the executor hook (no-op AuthorizeOnBehalf).
	DelegateToken string
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

// WithCallerToken returns c with the caller token set. Returns c unchanged on
// nil receiver so the no-op disabled-client path keeps working.
func (c *Client) WithCallerToken(tok string) *Client {
	if c == nil {
		return c
	}
	c.CallerToken = strings.TrimSpace(tok)
	return c
}

// WithDelegateToken sets the token used on AuthorizeOnBehalf calls.
func (c *Client) WithDelegateToken(tok string) *Client {
	if c == nil {
		return c
	}
	c.DelegateToken = strings.TrimSpace(tok)
	return c
}

// DelegationEnabled reports whether the executor hook can call out — the
// integration is enabled, the bootstrap rule doesn't apply, AND we have a
// delegate token in hand. The hook treats an unset DelegateToken as "skip
// governance for this stage" so partial rollouts (Milestone A landed but
// delegate scope not yet provisioned) keep working.
func (c *Client) DelegationEnabled() bool {
	return c != nil && c.BaseURL != "" && c.DelegateToken != ""
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
	if c.CallerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.CallerToken)
	}

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
		Token       string             `json:"token,omitempty"`
		Preresolved *preresolvedActor  `json:"preresolved,omitempty"`
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

// PreresolvedActor is the shape Cooker submits on AuthorizeOnBehalf. Matches
// the Grovernance request DTO; exported so the executor hook can construct
// one from a PipelineRun without needing a parallel type.
type PreresolvedActor struct {
	Kind   string   `json:"kind"`   // "human" | "service"
	ID     string   `json:"id"`
	Groups []string `json:"groups,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

type preresolvedActor = PreresolvedActor

// AuthorizeOnBehalf is the delegated path. The caller supplies an
// already-resolved actor (typically captured at run-start and persisted on
// the PipelineRun) instead of a token. The Authorization header carries the
// delegate token, which must hold governance.authorize_on_behalf on the gate.
//
// Treats an unset BaseURL or unset DelegateToken as "skip the call" and
// returns Allow + a synthetic PolicyID. The executor's pre-stage hook
// short-circuits in that case so partial rollouts keep working.
func (c *Client) AuthorizeOnBehalf(ctx context.Context, actor PreresolvedActor, service, env, requestID string) (Decision, error) {
	if !c.DelegationEnabled() {
		return Decision{Decision: "allow", Reason: "governance delegation disabled", PolicyID: "cooker.governance.delegation_disabled"}, nil
	}
	if _, ok := c.BootstrapServices[strings.ToLower(service)]; ok {
		return Decision{Decision: "allow", Reason: "bootstrap-exempt service", PolicyID: "cooker.governance.bootstrap"}, nil
	}

	payload := authorizeRequest{Action: "deploy"}
	payload.Actor.Preresolved = &actor
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
	req.Header.Set("Authorization", "Bearer "+c.DelegateToken)

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
