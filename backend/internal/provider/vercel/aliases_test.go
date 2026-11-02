package vercel

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDeploymentAliasesExactScopeAndSafeProjection(t *testing.T) {
	for _, team := range []string{"", "team_fixture"} {
		t.Run("scope_"+team, func(t *testing.T) {
			calls := 0
			c := clientFixture(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Path != "/v2/deployments/dpl_actual/aliases" || req.URL.Query().Get("teamId") != team || len(req.URL.Query()) > 1 {
					t.Fatal("alias request was not bound to exact deployment/account")
				}
				return clientResponse(`{"aliases":[{"alias":"Zeta.vercel.app/","uid":"private-canary","protectionBypass":{"private-canary":{"scope":"alias"}},"deployment":{"id":"private-canary"}},{"alias":"alpha.vercel.app","env":{"TOKEN":"private-canary"}},{"alias":"https://alpha.vercel.app/","createdAt":1700000000000}],"protectionBypass":"private-canary","token":"private-canary"}`), nil
			})
			before := time.Now().UTC()
			got, err := c.DeploymentAliases(context.Background(), clientTestToken, team, "dpl_actual")
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || got.DeploymentID != "dpl_actual" || got.ObservedAt.Before(before) || got.ObservedAt.After(time.Now().UTC()) || got.Error != "" || !reflect.DeepEqual(got.Aliases, []string{"https://alpha.vercel.app", "https://zeta.vercel.app"}) {
				t.Fatalf("unexpected alias projection: %+v", got)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			for _, excluded := range []string{"private-canary", "protectionBypass", "env", "token", "uid", clientTestToken} {
				if strings.Contains(string(encoded), excluded) {
					t.Fatalf("unapproved alias field escaped: %s", excluded)
				}
			}
		})
	}
}

func TestDeploymentAliasesExplicitEmptyIsAnObservation(t *testing.T) {
	c := clientFixture(t, func(*http.Request) (*http.Response, error) {
		return clientResponse(`{"aliases":[]}`), nil
	})
	got, err := c.DeploymentAliases(context.Background(), clientTestToken, "", "dpl_empty")
	if err != nil || got == nil || got.Aliases == nil || len(got.Aliases) != 0 || got.DeploymentID != "dpl_empty" || got.ObservedAt.IsZero() {
		t.Fatalf("empty authoritative alias list was not observed: %+v, %v", got, err)
	}
}

func TestDeploymentAliasesRejectsIncompleteUnsafeAndOversizedResponses(t *testing.T) {
	cases := []struct{ name, body string }{
		{"missing list", `{}`},
		{"null response", `null`},
		{"null list", `{"aliases":null}`},
		{"wrong list shape", `{"aliases":{}}`},
		{"missing name", `{"aliases":[{}]}`},
		{"null item", `{"aliases":[null]}`},
		{"numeric name", `{"aliases":[{"alias":123}]}`},
		{"malformed json", `{"aliases":[`},
		{"partial valid then unsafe", `{"aliases":[{"alias":"good.vercel.app"},{"alias":"https://user:private-canary@bad.vercel.app"}]}`},
	}
	for _, value := range []string{"", "http://app.vercel.app", "https://app.vercel.app:443", "https://app.vercel.app/path", "https://app.vercel.app/?token=private-canary", "https://app.vercel.app/#private-canary", "https://127.0.0.1", "https://host.local", "https://host.internal", "https://localhost", "https://app.vercel.app%2f.evil.com", "https://app.vercel.app\\evil", "app.vercel.app\nprivate-canary", strings.Repeat("a", 2050)} {
		body, _ := json.Marshal(map[string]any{"aliases": []map[string]string{{"alias": value}}})
		cases = append(cases, struct{ name, body string }{"unsafe_" + value, string(body)})
	}
	oversized := make([]map[string]string, 201)
	for i := range oversized {
		oversized[i] = map[string]string{"alias": "duplicate.vercel.app"}
	}
	body, _ := json.Marshal(map[string]any{"aliases": oversized})
	cases = append(cases, struct{ name, body string }{"oversized even with duplicates", string(body)})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := clientFixture(t, func(*http.Request) (*http.Response, error) { return clientResponse(tc.body), nil })
			got, err := c.DeploymentAliases(context.Background(), clientTestToken, "", "dpl_one")
			clientRequireCode(t, err, "invalid_response")
			if got != nil || strings.Contains(err.Error(), "private-canary") {
				t.Fatal("invalid alias response returned partial or unsafe observations")
			}
		})
	}
}

func TestDeploymentAliasesRejectsInvalidInputsBeforeNetwork(t *testing.T) {
	c := clientFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid alias input reached network")
		return nil, nil
	})
	for _, id := range []string{"", "../foreign", "dpl_one?project=other", "dpl_one/other", "dpl_one\n"} {
		_, err := c.DeploymentAliases(context.Background(), clientTestToken, "", id)
		clientRequireCode(t, err, "invalid_response")
	}
	_, err := c.DeploymentAliases(context.Background(), "unsafe token", "", "dpl_one")
	clientRequireCode(t, err, "invalid_input")
	_, err = c.DeploymentAliases(context.Background(), clientTestToken, "a-team-slug", "dpl_one")
	clientRequireCode(t, err, "invalid_input")
}

func TestDeploymentAliasesSafeFailureAndRateLimit(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 302, 500} {
		c := clientFixture(t, func(*http.Request) (*http.Response, error) {
			r := clientResponse(`{"error":{"message":"private-response private-canary"}}`)
			r.StatusCode = status
			r.Header.Set("Retry-After", "600")
			return r, nil
		})
		got, err := c.DeploymentAliases(context.Background(), clientTestToken, "", "dpl_one")
		if got != nil || err == nil || strings.Contains(err.Error(), "private-canary") || strings.Contains(err.Error(), "private-response") {
			t.Fatal("alias failure leaked response or returned success")
		}
		if status == 429 {
			e := clientRequireCode(t, err, "rate_limited")
			if e.RetryAfter != 10*time.Minute {
				t.Fatal("alias rate limit lost provider cooldown")
			}
		}
	}
}

func TestAliasDetailsClonesDoNotShareSlices(t *testing.T) {
	at := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	original := &DeploymentAliasDetails{DeploymentID: "dpl_one", Aliases: []string{"https://one.vercel.app"}, ObservedAt: at, Error: "retained"}
	copy := cloneAliasDetails(original)
	copy.Aliases[0] = "https://changed.vercel.app"
	copy.Error = "changed"
	if original.Aliases[0] != "https://one.vercel.app" || original.Error != "retained" || !copy.ObservedAt.Equal(at) || cloneAliasDetails(nil) != nil {
		t.Fatal("alias clone shares observation state")
	}
	deployment := Deployment{ID: "dpl_one", Details: &DeploymentDetails{AliasDetails: original}}
	cloned := cloneDeployment(deployment)
	cloned.Details.AliasDetails.Aliases[0] = "https://nested.vercel.app"
	if deployment.Details.AliasDetails.Aliases[0] != "https://one.vercel.app" {
		t.Fatal("deployment clone shares nested alias slice")
	}
}
