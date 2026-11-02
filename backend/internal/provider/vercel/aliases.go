package vercel

import (
	"context"
	"net/url"
	"sort"
	"time"
)

// DeploymentAliasDetails is a separate optional observation. A newer deployment
// metadata read must not make retained aliases look freshly verified.
type DeploymentAliasDetails struct {
	DeploymentID string    `json:"deploymentId"`
	Aliases      []string  `json:"aliases"`
	ObservedAt   time.Time `json:"observedAt"`
	Error        string    `json:"error,omitempty"`
}

// AliasesAPI is optional independently of DetailsAPI to preserve older adapters.
type AliasesAPI interface {
	DeploymentAliases(context.Context, string, string, string) (*DeploymentAliasDetails, error)
}

// DeploymentAliases reads the authoritative aliases bound to the exact ID.
// Only alias names are decoded: protectionBypass and raw provider values never
// enter the safe projection. Schema checked against Vercel's v2 API on2026-10-02.
func (c *Client) DeploymentAliases(ctx context.Context, token, team, id string) (*DeploymentAliasDetails, error) {
	if err := validateScope(token, team); err != nil {
		return nil, err
	}
	if !validProviderID(id) {
		return nil, invalidResponse()
	}
	var raw struct {
		Aliases *[]struct {
			Alias string `json:"alias"`
		} `json:"aliases"`
	}
	if err := c.get(ctx, token, "/v2/deployments/"+id+"/aliases", scopeQuery(team), &raw); err != nil {
		return nil, err
	}
	if raw.Aliases == nil || len(*raw.Aliases) > 200 {
		return nil, invalidResponse()
	}
	out := &DeploymentAliasDetails{DeploymentID: id, Aliases: []string{}, ObservedAt: time.Now().UTC()}
	seen := map[string]bool{}
	for _, item := range *raw.Aliases {
		link := deploymentURL(item.Alias)
		u, err := url.Parse(link)
		if link == "" || err != nil || (u.Path != "" && u.Path != "/") {
			return nil, invalidResponse()
		}
		u.Path = ""
		link = u.String()
		if !seen[link] {
			out.Aliases = append(out.Aliases, link)
			seen[link] = true
		}
	}
	sort.Strings(out.Aliases)
	return out, nil
}

func cloneAliasDetails(in *DeploymentAliasDetails) *DeploymentAliasDetails {
	if in == nil {
		return nil
	}
	out := *in
	out.Aliases = append([]string{}, in.Aliases...)
	return &out
}
