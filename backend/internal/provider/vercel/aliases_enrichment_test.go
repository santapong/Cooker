package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// This adapter independently opts in to AliasesAPI. The existing detailFixture
// intentionally remains an older adapter without authoritative alias reads.
type aliasFixture struct {
	*detailFixture
	muAliases                         sync.Mutex
	activeAll, maximumAll, totalCalls int
	aliasIDs                          []string
	aliasError                        error
	aliasNames                        []string
	aliasObservedAt                   time.Time
	aliasMismatch, aliasZeroTime      bool
	requestWait                       time.Duration
}

func (f *aliasFixture) beginRequest(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.muAliases.Lock()
	f.activeAll++
	f.totalCalls++
	if f.activeAll > f.maximumAll {
		f.maximumAll = f.activeAll
	}
	delay := f.requestWait
	f.muAliases.Unlock()
	finish := func() { f.muAliases.Lock(); f.activeAll--; f.muAliases.Unlock() }
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			finish()
			return nil, ctx.Err()
		}
	}
	return finish, nil
}

func (f *aliasFixture) ProjectDetails(ctx context.Context, token, team, id string) (*ProjectDetails, error) {
	finish, err := f.beginRequest(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	return f.detailFixture.ProjectDetails(ctx, token, team, id)
}

func (f *aliasFixture) DeploymentDetails(ctx context.Context, token, team, id string) (*Deployment, error) {
	finish, err := f.beginRequest(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	return f.detailFixture.DeploymentDetails(ctx, token, team, id)
}

func (f *aliasFixture) DeploymentAliases(ctx context.Context, _, _, id string) (*DeploymentAliasDetails, error) {
	finish, err := f.beginRequest(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	f.muAliases.Lock()
	f.aliasIDs = append(f.aliasIDs, id)
	names := append([]string{}, f.aliasNames...)
	at, mismatch, zero, failure := f.aliasObservedAt, f.aliasMismatch, f.aliasZeroTime, f.aliasError
	f.muAliases.Unlock()
	if failure != nil {
		return nil, failure
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if zero {
		at = time.Time{}
	}
	if mismatch {
		id = "dpl_foreign"
	}
	return &DeploymentAliasDetails{DeploymentID: id, Aliases: names, ObservedAt: at}, nil
}

func aliasService(t *testing.T, projects int) (*Service, *aliasFixture, *Connection) {
	t.Helper()
	svc, detail, c := detailService(t, NewMemory(), projects)
	api := &aliasFixture{detailFixture: detail, aliasNames: []string{"https://actual.vercel.app"}}
	svc.API = api
	return svc, api, c
}

func aliasProduction(t *testing.T, svc *Service, c *Connection) (*Record, *Deployment) {
	t.Helper()
	r, err := svc.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Snapshot.Projects) != 1 || r.Snapshot.Projects[0].Details == nil || r.Snapshot.Projects[0].Details.ProductionDeployment == nil {
		t.Fatal("fixture has no exact production observation")
	}
	return r, r.Snapshot.Projects[0].Details.ProductionDeployment
}

func TestAliasEnrichmentSharesFourRequestBoundAndReadsOnlyProduction(t *testing.T) {
	svc, api, c := aliasService(t, 20)
	api.requestWait = 3 * time.Millisecond
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	api.muAliases.Lock()
	maximum, total, ids := api.maximumAll, api.totalCalls, append([]string{}, api.aliasIDs...)
	api.muAliases.Unlock()
	if maximum > 4 || maximum < 2 || total != 80 || len(ids) != 20 {
		t.Fatalf("alias/detail request budget max=%d total=%d aliases=%d", maximum, total, len(ids))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !strings.HasPrefix(id, "dpl_current_prj_") || seen[id] {
			t.Fatal("alias endpoint queried preview/former production or duplicate identity")
		}
		seen[id] = true
	}
	r, err := svc.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.Snapshot.Projects {
		d := p.Details.ProductionDeployment
		if d.Details.AliasDetails == nil || d.Details.AliasDetails.DeploymentID != d.ID || len(d.Details.AliasDetails.Aliases) != 1 {
			t.Fatal("production aliases lost exact deployment binding")
		}
	}
	for _, d := range r.Snapshot.Deployments {
		if d.Details != nil && d.Details.AliasDetails != nil {
			t.Fatal("alias enrichment attached to unrelated recent history")
		}
	}
	r.Snapshot.Projects[0].Details.ProductionDeployment.Details.AliasDetails.Aliases[0] = "https://modified.vercel.app"
	again, _ := svc.Store.Get(context.Background(), c.ID)
	if again.Snapshot.Projects[0].Details.ProductionDeployment.Details.AliasDetails.Aliases[0] == "https://modified.vercel.app" {
		t.Fatal("memory persistence shares nested authoritative aliases")
	}
}

func TestAliasFailureRetainsExactIdentityAndIndependentObservationTime(t *testing.T) {
	svc, api, c := aliasService(t, 1)
	api.aliasObservedAt = time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	before, old := aliasProduction(t, svc, c)
	api.aliasError = errors.New("private-canary provider response")
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	after, current := aliasProduction(t, svc, c)
	aliases := current.Details.AliasDetails
	if aliases == nil || aliases.DeploymentID != current.ID || len(aliases.Aliases) != 1 || aliases.Error != aliasesFailure || !aliases.ObservedAt.Equal(old.Details.AliasDetails.ObservedAt) {
		t.Fatal("alias failure lost prior exact-identity observation")
	}
	if !current.Details.ObservedAt.After(old.Details.ObservedAt) || !current.Details.ObservedAt.After(aliases.ObservedAt) || current.Details.Error != "" || !after.LastSuccessAt.After(*before.LastSuccessAt) {
		t.Fatal("alias failure contaminated current metadata/core freshness")
	}
	encoded, _ := json.Marshal(after.Snapshot)
	if strings.Contains(string(encoded), "private-canary") {
		t.Fatal("raw alias failure entered persisted snapshot")
	}
}

func TestChangedProductionDoesNotReusePreviousAliases(t *testing.T) {
	svc, api, c := aliasService(t, 1)
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	api.muDetails.Lock()
	api.current = "changed"
	api.muDetails.Unlock()
	api.aliasError = errors.New("unavailable")
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	_, current := aliasProduction(t, svc, c)
	aliases := current.Details.AliasDetails
	if current.ID != "dpl_changed_prj_00" || aliases == nil || aliases.DeploymentID != current.ID || len(aliases.Aliases) != 0 || !aliases.ObservedAt.IsZero() || aliases.Error != aliasesFailure {
		t.Fatal("changed production borrowed prior alias values/freshness")
	}
}

func TestAuthoritativeEmptyAliasRefreshClearsPreviousObservation(t *testing.T) {
	svc, api, c := aliasService(t, 1)
	api.aliasObservedAt = time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	api.aliasError = errors.New("temporarily unavailable")
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	api.aliasError = nil
	api.aliasNames = nil
	api.aliasObservedAt = api.aliasObservedAt.Add(time.Hour)
	if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
		t.Fatal(err)
	}
	_, current := aliasProduction(t, svc, c)
	aliases := current.Details.AliasDetails
	if aliases == nil || aliases.Aliases == nil || len(aliases.Aliases) != 0 || aliases.Error != "" || !aliases.ObservedAt.Equal(api.aliasObservedAt) {
		t.Fatal("authoritative empty response retained stale aliases/error")
	}
}

func TestAliasEnrichmentRejectsWrongIdentityAndMissingObservation(t *testing.T) {
	for _, zero := range []bool{false, true} {
		svc, api, c := aliasService(t, 1)
		api.aliasMismatch = !zero
		api.aliasZeroTime = zero
		if _, err := svc.Sync(context.Background(), c.ID, true); err != nil {
			t.Fatal(err)
		}
		_, current := aliasProduction(t, svc, c)
		aliases := current.Details.AliasDetails
		if aliases == nil || aliases.DeploymentID != current.ID || len(aliases.Aliases) != 0 || !aliases.ObservedAt.IsZero() || aliases.Error != aliasesFailure {
			t.Fatal("invalid alias observation accepted")
		}
	}
}

func TestAliasRateLimitCancelsFanoutAndPersistsCooldown(t *testing.T) {
	svc, api, c := aliasService(t, 20)
	api.requestWait = time.Millisecond
	api.aliasError = &APIError{StatusCode: 429, RetryAfter: 5 * time.Minute, Message: "private-canary"}
	receipt, err := svc.Sync(context.Background(), c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	api.muAliases.Lock()
	aliasCalls, total, maximum := len(api.aliasIDs), api.totalCalls, api.maximumAll
	api.muAliases.Unlock()
	if aliasCalls < 1 || aliasCalls > 4 || total > 3*enrichmentConcurrency || maximum > 4 || receipt.LastSuccessAt == nil || receipt.RetryAt == nil || receipt.RetryAt.Before(time.Now().Add(4*time.Minute)) {
		t.Fatalf("unbounded alias backoff: aliases=%d total=%d max=%d receipt=%+v", aliasCalls, total, maximum, receipt)
	}
	r, err := svc.Store.Get(context.Background(), c.ID)
	if err != nil || len(r.Snapshot.Deployments) != 40 {
		t.Fatal("optional alias rate limit discarded successful core history")
	}
	rateLimitVisible := false
	for _, p := range r.Snapshot.Projects {
		if p.Details == nil || p.Details.ProductionDeployment == nil || p.Details.ProductionDeployment.Details.AliasDetails == nil {
			continue
		}
		if strings.Contains(p.Details.ProductionDeployment.Details.AliasDetails.Error, "rate limited") {
			rateLimitVisible = true
		}
	}
	if !rateLimitVisible {
		t.Fatal("optional alias rate limit was not explicitly labelled")
	}
	encoded, _ := json.Marshal(r.Snapshot)
	if strings.Contains(string(encoded), "private-canary") {
		t.Fatal("alias rate-limit response entered snapshot")
	}
	if _, err = svc.Sync(context.Background(), c.ID, true); !errors.Is(err, ErrBusy) {
		t.Fatal("manual sync bypassed alias cooldown")
	}
	api.muAliases.Lock()
	defer api.muAliases.Unlock()
	if api.totalCalls != total || len(api.aliasIDs) != aliasCalls {
		t.Fatal("backoff retry performed extra detail/alias calls")
	}
}
